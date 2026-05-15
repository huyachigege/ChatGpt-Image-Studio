package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"chatgpt2api/handler"
	"chatgpt2api/internal/config"
)

type fallbackStubClient struct {
	generateCalls int
	generateErr   error
	resultURL     string
	route         string
	name          string
	callOrder     *[]string
}

func (c *fallbackStubClient) DownloadBytes(url string) ([]byte, error) { return []byte(url), nil }
func (c *fallbackStubClient) DownloadAsBase64(ctx context.Context, url string) (string, error) {
	_ = ctx
	return url, nil
}
func (c *fallbackStubClient) GenerateImage(ctx context.Context, prompt, model string, n int, size, quality, background string) ([]handler.ImageResult, error) {
	_ = ctx
	_ = prompt
	_ = model
	_ = n
	_ = size
	_ = quality
	_ = background
	c.generateCalls++
	if c.callOrder != nil && c.name != "" {
		*c.callOrder = append(*c.callOrder, c.name)
	}
	if c.generateErr != nil {
		return nil, c.generateErr
	}
	return []handler.ImageResult{{URL: firstNonEmpty(c.resultURL, "stub://image")}}, nil
}
func (c *fallbackStubClient) EditImageByUpload(ctx context.Context, prompt, model string, images [][]byte, mask []byte, size, quality string) ([]handler.ImageResult, error) {
	return c.GenerateImage(ctx, prompt, model, 1, size, quality, "")
}
func (c *fallbackStubClient) InpaintImageByMask(ctx context.Context, prompt string, model string, originalFileID string, originalGenID string, conversationID string, parentMessageID string, mask []byte, size string, quality string) ([]handler.ImageResult, error) {
	return c.GenerateImage(ctx, prompt, model, 1, size, quality, "")
}
func (c *fallbackStubClient) LastRoute() string      { return firstNonEmpty(c.route, "responses") }
func (c *fallbackStubClient) ImageToolModel() string { return "official-model" }

func TestExternalResponsesClientUsesConfiguredEndpointKeyAndModel(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer external-key" {
			t.Fatalf("Authorization = %q, want bearer key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ext\",\"output\":[{\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\",\"output_format\":\"png\",\"revised_prompt\":\"revised\"}]}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := newExternalResponsesClient(config.ExternalResponsesConfig{
		Enabled:        true,
		BaseURL:        server.URL,
		APIKey:         "external-key",
		Model:          "external-model",
		RequestTimeout: 5,
	})
	client.SetRequestedImageModel("gpt-5.5")
	client.SetInstructions("follow system hint")
	images, err := client.GenerateImage(context.Background(), "draw", "ignored", 1, "1024x1024", "high", "transparent")
	if err != nil {
		t.Fatalf("GenerateImage() returned error: %v", err)
	}
	if got := requestBody["model"]; got != "external-model" {
		t.Fatalf("request model = %v, want external-model", got)
	}
	if got := requestBody["instructions"]; got != "follow system hint" {
		t.Fatalf("request instructions = %v, want system hint", got)
	}
	if got := requestBody["store"]; got != false {
		t.Fatalf("request store = %v, want false", got)
	}
	if include, ok := requestBody["include"].([]any); !ok || len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("request include = %#v, want encrypted reasoning include", requestBody["include"])
	}
	tool := requestBody["tools"].([]any)[0].(map[string]any)
	if got := tool["model"]; got != "gpt-5.5" {
		t.Fatalf("tool model = %v, want gpt-5.5", got)
	}
	if got := tool["action"]; got != "generate" {
		t.Fatalf("tool action = %v, want generate", got)
	}
	if len(images) != 1 || !strings.HasPrefix(images[0].URL, "data:image/png;base64,") {
		t.Fatalf("images = %#v, want data URL image", images)
	}
	if images[0].ResponseID != "resp_ext" {
		t.Fatalf("ResponseID = %q, want resp_ext", images[0].ResponseID)
	}
}

func TestExternalResponsesClientMatchesOfficialPreviousResponseAndReferencePayload(t *testing.T) {
	requests := make([]map[string]any, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requests = append(requests, body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ext\",\"output\":[{\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\",\"output_format\":\"png\"}]}}\n\n")
	}))
	defer server.Close()

	client := newExternalResponsesClient(config.ExternalResponsesConfig{BaseURL: server.URL, APIKey: "key", Model: "external-model"})
	if _, err := client.GenerateImageWithPreviousResponse(context.Background(), " make it realistic ", "ignored", 1, "", "", "", "resp_123"); err != nil {
		t.Fatalf("GenerateImageWithPreviousResponse() returned error: %v", err)
	}
	if got := requests[0]["previous_response_id"]; got != "resp_123" {
		t.Fatalf("previous_response_id = %v, want resp_123", got)
	}
	input := requests[0]["input"].([]any)[0].(map[string]any)
	content := input["content"].([]any)
	if got := content[0].(map[string]any)["text"]; got != "make it realistic" {
		t.Fatalf("prompt text = %q, want trimmed official prompt", got)
	}

	if _, err := client.GenerateImageWithReferenceImages(context.Background(), "draw", "ignored", 1, "", "", "", [][]byte{apiTestPNGBytes(), apiTestPNGBytes()}); err != nil {
		t.Fatalf("GenerateImageWithReferenceImages() returned error: %v", err)
	}
	input = requests[1]["input"].([]any)[0].(map[string]any)
	content = input["content"].([]any)
	if text := content[0].(map[string]any)["text"].(string); !strings.Contains(text, "visual references for continuity") {
		t.Fatalf("reference prompt = %q, want official reference hint", text)
	}
	imageCount := 0
	for _, item := range content {
		part := item.(map[string]any)
		if part["type"] == "input_image" {
			imageCount++
			if !strings.HasPrefix(part["image_url"].(string), "data:image/jpeg;base64,") {
				t.Fatalf("reference image_url = %q, want jpeg thumbnail", part["image_url"])
			}
		}
	}
	if imageCount != 2 {
		t.Fatalf("reference image count = %d, want 2", imageCount)
	}
	tool := requests[1]["tools"].([]any)[0].(map[string]any)
	if got := tool["action"]; got != "generate" {
		t.Fatalf("reference tool action = %v, want generate", got)
	}
}

func TestFallbackResponsesClientFallsBackToSecondClient(t *testing.T) {
	primary := &fallbackStubClient{generateErr: fmt.Errorf("official down"), route: "responses"}
	external := &fallbackStubClient{resultURL: "stub://external", route: "external_responses"}
	client := newNamedFallbackResponsesClient(primary, external, "official responses", "external responses")

	images, err := client.GenerateImage(context.Background(), "draw", "model", 1, "", "", "")
	if err != nil {
		t.Fatalf("GenerateImage() returned error: %v", err)
	}
	if primary.generateCalls != 1 || external.generateCalls != 1 {
		t.Fatalf("calls primary=%d external=%d, want 1/1", primary.generateCalls, external.generateCalls)
	}
	if len(images) != 1 || images[0].URL != "stub://external" {
		t.Fatalf("images = %#v, want external result", images)
	}
	if got := client.LastRoute(); got != "external_responses" {
		t.Fatalf("LastRoute = %q, want external_responses", got)
	}
	if got := client.LastFallbackReason(); !strings.Contains(got, "official down") {
		t.Fatalf("LastFallbackReason = %q, want official error", got)
	}
}

func TestFallbackResponsesClientDoesNotFallbackOnCanceledContext(t *testing.T) {
	primary := &fallbackStubClient{generateErr: context.Canceled, route: "external_responses"}
	official := &fallbackStubClient{route: "responses"}
	client := newFallbackResponsesClient(primary, official)

	_, err := client.GenerateImage(context.Background(), "draw", "model", 1, "", "", "")
	if err == nil {
		t.Fatal("GenerateImage() returned nil error, want context.Canceled")
	}
	if official.generateCalls != 0 {
		t.Fatalf("official calls = %d, want 0", official.generateCalls)
	}
}

func TestNewResponsesWorkflowClientFallsBackOfficialExternalLegacy(t *testing.T) {
	cfg := config.New(t.TempDir())
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if err := cfg.ApplyOverrides(map[string]map[string]any{
		"external_responses": {
			"enabled":         true,
			"base_url":        "https://external.example",
			"api_key":         "key",
			"model":           "external-model",
			"request_timeout": 5,
		},
	}); err != nil {
		t.Fatalf("ApplyOverrides() returned error: %v", err)
	}
	server := NewServer(cfg, nil, nil)
	defer server.Close()
	callOrder := []string{}
	server.responsesClientFactory = func(accessToken, proxyURL string, authData map[string]any, requestConfig handler.ImageRequestConfig) imageWorkflowClient {
		return &fallbackStubClient{name: "official", route: "responses", generateErr: fmt.Errorf("official down"), callOrder: &callOrder}
	}
	server.externalResponsesClientFactory = func(cfg config.ExternalResponsesConfig) imageWorkflowClient {
		return &fallbackStubClient{name: "external", route: "external_responses", generateErr: fmt.Errorf("external down"), callOrder: &callOrder}
	}
	server.officialClientFactory = func(accessToken, proxyURL string, authData map[string]any, requestConfig handler.ImageRequestConfig) imageWorkflowClient {
		return &fallbackStubClient{name: "legacy", route: "legacy", resultURL: "stub://legacy", callOrder: &callOrder}
	}

	client := server.newResponsesWorkflowClient("token", nil)
	images, err := client.GenerateImage(context.Background(), "draw", "model", 1, "", "", "")
	if err != nil {
		t.Fatalf("GenerateImage() returned error: %v", err)
	}
	if strings.Join(callOrder, ",") != "official,external,legacy" {
		t.Fatalf("call order = %v, want official/external/legacy", callOrder)
	}
	if len(images) != 1 || images[0].URL != "stub://legacy" {
		t.Fatalf("images = %#v, want legacy result", images)
	}
	if got := client.(*fallbackResponsesClient).LastRoute(); got != "legacy" {
		t.Fatalf("LastRoute = %q, want legacy", got)
	}
}

func TestExternalResponsesClientTimeoutDefault(t *testing.T) {
	client := newExternalResponsesClient(config.ExternalResponsesConfig{BaseURL: "https://example.com", APIKey: "key", Model: "model"})
	if client.httpClient.Timeout != 300*time.Second {
		t.Fatalf("timeout = %v, want 300s", client.httpClient.Timeout)
	}
}

func apiTestPNGBytes() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xff, 0xff, 0x3f,
		0x00, 0x05, 0xfe, 0x02, 0xfe, 0xdc, 0xcc, 0x59,
		0xe7, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
}
