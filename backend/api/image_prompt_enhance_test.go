package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	dataURL := validPromptEnhancePNGDataURL(t)
	body := fmt.Sprintf(`{
		"prompt":"换成雨夜",
		"mode":"generate",
		"sourceImages":[
			{"id":"source-1","role":"image","name":"source.png","dataUrl":%q},
			{"id":"mask-1","role":"mask","name":"mask.png","dataUrl":%q}
		]
	}`, dataURL, dataURL)
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

func TestHandleEnhanceImagePromptUsesConfiguredReferenceImageBaseURL(t *testing.T) {
	var imageURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		input := requestBody["input"].([]any)[0].(map[string]any)
		content := input["content"].([]any)
		for _, item := range content {
			part := item.(map[string]any)
			if part["type"] == "input_image" {
				imageURL = part["image_url"].(string)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"output_text":"{\"prompts\":[\"ok\"]}"}`)
	}))
	defer upstream.Close()

	server := newPromptEnhanceTestServer(t, upstream, func(cfg *config.Config) {
		cfg.ExternalResponses.ReferenceImageBaseURL = "https://proxy.example/http://origin.example"
	})
	req := httptest.NewRequest(http.MethodPost, "/api/image/prompt-enhance", strings.NewReader(fmt.Sprintf(`{"prompt":"测试","sourceImages":[{"role":"image","dataUrl":%q}]}`, validPromptEnhancePNGDataURL(t))))
	req.Header.Set("Authorization", "Bearer "+server.cfg.App.AuthKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(imageURL, "https://proxy.example/http://origin.example/v1/files/image/.thumbs/prompt-reference-") {
		t.Fatalf("image url = %q, want configured reference image base url", imageURL)
	}
}

func TestHandleEnhanceImagePromptUsesBase64ReferenceImage(t *testing.T) {
	var imageURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requestBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		input := requestBody["input"].([]any)[0].(map[string]any)
		content := input["content"].([]any)
		for _, item := range content {
			part := item.(map[string]any)
			if part["type"] == "input_image" {
				imageURL = part["image_url"].(string)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"output_text":"{\"prompts\":[\"ok\"]}"}`)
	}))
	defer upstream.Close()

	server := newPromptEnhanceTestServer(t, upstream, func(cfg *config.Config) {
		cfg.ExternalResponses.ReferenceImageMode = "base64"
	})
	req := httptest.NewRequest(http.MethodPost, "/api/image/prompt-enhance", strings.NewReader(fmt.Sprintf(`{"prompt":"测试","sourceImages":[{"role":"image","dataUrl":%q}]}`, validPromptEnhancePNGDataURL(t))))
	req.Header.Set("Authorization", "Bearer "+server.cfg.App.AuthKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(imageURL, "data:image/jpeg;base64,") {
		t.Fatalf("image url = %q, want jpeg data url", imageURL)
	}
}

func TestWaitPromptEnhanceImageURLAvailableWaitsForFile(t *testing.T) {
	cfg := config.New(t.TempDir())
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	server := NewServer(cfg, nil, nil)
	t.Cleanup(func() { server.Close() })

	imageDir := filepath.Join(cfg.ResolvePath(cfg.Storage.ImageDir), ".thumbs")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() returned error: %v", err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(imageDir, "delayed.png"), []byte("ready"), 0o644)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.waitPromptEnhanceImageURLAvailable(ctx, externalResponsesPublicImageBaseURL+"/v1/files/image/.thumbs/delayed.png"); err != nil {
		t.Fatalf("waitPromptEnhanceImageURLAvailable() returned error: %v", err)
	}
}

func validPromptEnhancePNGDataURL(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
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
