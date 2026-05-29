package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chatgpt2api/internal/accounts"
	"chatgpt2api/internal/config"
)

func TestHandleEnhanceImagePromptRequiresPromptOrReference(t *testing.T) {
	server := newPromptEnhanceTestServer(t, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/image/prompt-enhance", strings.NewReader(`{"prompt":"","sourceImages":[]}`))
	req.Header.Set("Authorization", "Bearer "+server.cfg.App.AuthKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandleEnhanceImagePromptUsesFixedImageURL(t *testing.T) {
	var requestBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer external-key" {
			t.Fatalf("Authorization = %q, want bearer key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"output_text":"{\"prompts\":[\"电影感写实提示词，主体明确，构图稳定，光线柔和，细节丰富\",\"赛博雨夜提示词\"]}"}`)
	}))
	defer upstream.Close()

	server := newPromptEnhanceTestServer(t, upstream, nil)
	body := `{
		"prompt":"换成雨夜",
		"mode":"generate",
		"sourceImages":[
			{"id":"source-1","role":"image","name":"source.png","dataUrl":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC"},
			{"id":"mask-1","role":"mask","name":"mask.png","dataUrl":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC"}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/image/prompt-enhance", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+server.cfg.App.AuthKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload imagePromptEnhanceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(payload.Prompt, "电影感写实提示词") {
		t.Fatalf("prompt = %q, want upstream text", payload.Prompt)
	}
	if len(payload.Prompts) != 2 {
		t.Fatalf("prompts = %#v, want two options", payload.Prompts)
	}
	if reasoning := requestBody["reasoning"].(map[string]any); reasoning["effort"] != "xhigh" {
		t.Fatalf("reasoning = %#v, want xhigh effort", reasoning)
	}

	input := requestBody["input"].([]any)[0].(map[string]any)
	content := input["content"].([]any)
	imageURLs := make([]string, 0)
	for _, item := range content {
		part := item.(map[string]any)
		if part["type"] == "input_image" {
			imageURLs = append(imageURLs, part["image_url"].(string))
		}
	}
	if len(imageURLs) != 1 {
		t.Fatalf("image urls = %#v, want one non-mask reference", imageURLs)
	}
	if !strings.HasPrefix(imageURLs[0], externalResponsesPublicImageBaseURL+"/v1/files/image/.thumbs/prompt-reference-") {
		t.Fatalf("image url = %q, want fixed public image host", imageURLs[0])
	}
}

func TestHandleEnhanceImagePromptReturnsUpstreamEmptyError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"output_text":""}`)
	}))
	defer upstream.Close()

	server := newPromptEnhanceTestServer(t, upstream, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/image/prompt-enhance", strings.NewReader(`{"prompt":"画一只猫"}`))
	req.Header.Set("Authorization", "Bearer "+server.cfg.App.AuthKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body = %s", rec.Code, rec.Body.String())
	}
}

func newPromptEnhanceTestServer(t *testing.T, upstream *httptest.Server, configure func(*config.Config)) *Server {
	t.Helper()
	cfg := config.New(t.TempDir())
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.App.AuthKey = "test-auth"
	if upstream != nil {
		cfg.ExternalResponses.Enabled = true
		cfg.ExternalResponses.BaseURL = upstream.URL
		cfg.ExternalResponses.APIKey = "external-key"
		cfg.ExternalResponses.Model = "external-model"
		cfg.ExternalResponses.RequestTimeout = 5
	}
	if configure != nil {
		configure(cfg)
	}
	store, err := accounts.NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore() returned error: %v", err)
	}
	server := NewServer(cfg, store, nil)
	t.Cleanup(func() { server.Close() })
	return server
}
