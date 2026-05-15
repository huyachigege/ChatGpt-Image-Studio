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
	resultURL      string
	route          string
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
func (c *fallbackStubClient) LastRoute() string { return firstNonEmpty(c.route, "responses") }
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
	images, err := client.GenerateImage(context.Background(), "draw", "ignored", 1, "1024x1024", "high", "transparent")
	if err != nil {
		t.Fatalf("GenerateImage() returned error: %v", err)
	}
	if got := requestBody["model"]; got != "external-model" {
		t.Fatalf("request model = %v, want external-model", got)
	}
	if len(images) != 1 || !strings.HasPrefix(images[0].URL, "data:image/png;base64,") {
		t.Fatalf("images = %#v, want data URL image", images)
	}
	if images[0].ResponseID != "resp_ext" {
		t.Fatalf("ResponseID = %q, want resp_ext", images[0].ResponseID)
	}
}

func TestFallbackResponsesClientFallsBackToOfficial(t *testing.T) {
	primary := &fallbackStubClient{generateErr: fmt.Errorf("external down"), route: "external_responses"}
	official := &fallbackStubClient{resultURL: "stub://official", route: "responses"}
	client := newFallbackResponsesClient(primary, official)

	images, err := client.GenerateImage(context.Background(), "draw", "model", 1, "", "", "")
	if err != nil {
		t.Fatalf("GenerateImage() returned error: %v", err)
	}
	if primary.generateCalls != 1 || official.generateCalls != 1 {
		t.Fatalf("calls primary=%d official=%d, want 1/1", primary.generateCalls, official.generateCalls)
	}
	if len(images) != 1 || images[0].URL != "stub://official" {
		t.Fatalf("images = %#v, want official result", images)
	}
	if got := client.LastRoute(); got != "responses" {
		t.Fatalf("LastRoute = %q, want responses", got)
	}
	if got := client.LastFallbackReason(); !strings.Contains(got, "external down") {
		t.Fatalf("LastFallbackReason = %q, want external error", got)
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

func TestNewResponsesWorkflowClientWrapsExternalWhenEnabled(t *testing.T) {
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
	server.responsesClientFactory = func(accessToken, proxyURL string, authData map[string]any, requestConfig handler.ImageRequestConfig) imageWorkflowClient {
		return &fallbackStubClient{route: "responses"}
	}
	server.externalResponsesClientFactory = func(cfg config.ExternalResponsesConfig) imageWorkflowClient {
		return &fallbackStubClient{route: "external_responses"}
	}

	client := server.newResponsesWorkflowClient("token", nil)
	if _, ok := client.(*fallbackResponsesClient); !ok {
		t.Fatalf("newResponsesWorkflowClient() = %T, want *fallbackResponsesClient", client)
	}
}

func TestExternalResponsesClientTimeoutDefault(t *testing.T) {
	client := newExternalResponsesClient(config.ExternalResponsesConfig{BaseURL: "https://example.com", APIKey: "key", Model: "model"})
	if client.httpClient.Timeout != 300*time.Second {
		t.Fatalf("timeout = %v, want 300s", client.httpClient.Timeout)
	}
}
