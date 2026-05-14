package handler

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestResolveChatGPTAccountIDFromAccessToken(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct-123",
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	accessToken := "header." + strings.TrimRight(base64.URLEncoding.EncodeToString(payload), "=") + ".sig"
	if got := resolveChatGPTAccountID(accessToken, map[string]any{}); got != "acct-123" {
		t.Fatalf("resolveChatGPTAccountID() = %q, want %q", got, "acct-123")
	}
}

func TestDecodeImageDataURL(t *testing.T) {
	dataURL := encodeImageDataURL([]byte("hello"), "image/png")
	got, err := decodeImageDataURL(dataURL)
	if err != nil {
		t.Fatalf("decodeImageDataURL() returned error: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("decodeImageDataURL() = %q, want %q", string(got), "hello")
	}
}

func TestParseResponsesSSEDeduplicatesFinalImages(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","result":"aGVsbG8=","output_format":"png"}}`,
		"",
		`data: {"type":"response.completed","response":{"output":[{"type":"image_generation_call","result":"aGVsbG8=","output_format":"png"}]}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")

	client := &ResponsesClient{}
	images, err := client.parseResponsesSSE(strings.NewReader(stream), "prompt")
	if err != nil {
		t.Fatalf("parseResponsesSSE() returned error: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("parseResponsesSSE() len = %d, want %d", len(images), 1)
	}
	if got, want := images[0].URL, "data:image/png;base64,aGVsbG8="; got != want {
		t.Fatalf("parseResponsesSSE() url = %q, want %q", got, want)
	}
}

func TestParseResponsesSSECapturesResponseID(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_123","output":[{"type":"image_generation_call","result":"aGVsbG8=","output_format":"png"}]}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")

	client := &ResponsesClient{}
	images, err := client.parseResponsesSSE(strings.NewReader(stream), "prompt")
	if err != nil {
		t.Fatalf("parseResponsesSSE() returned error: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("parseResponsesSSE() len = %d, want %d", len(images), 1)
	}
	if got := images[0].ResponseID; got != "resp_123" {
		t.Fatalf("parseResponsesSSE() response id = %q, want %q", got, "resp_123")
	}
}

func TestParseResponsesSSEAcceptsLargeImageEvents(t *testing.T) {
	largeB64 := strings.Repeat("A", 10*1024*1024+4096)
	stream := `data: {"type":"response.completed","response":{"output":[{"type":"image_generation_call","result":"` + largeB64 + `","output_format":"png"}]}}` + "\n\n"

	client := &ResponsesClient{}
	images, err := client.parseResponsesSSE(strings.NewReader(stream), "prompt")
	if err != nil {
		t.Fatalf("parseResponsesSSE() returned error: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("parseResponsesSSE() len = %d, want %d", len(images), 1)
	}
	if !strings.HasPrefix(images[0].URL, "data:image/png;base64,") {
		prefix := images[0].URL
		if len(prefix) > 32 {
			prefix = prefix[:32]
		}
		t.Fatalf("parseResponsesSSE() url prefix = %q", prefix)
	}
}

func TestSupportsResponsesInlineEdit(t *testing.T) {
	tests := []struct {
		name   string
		images [][]byte
		mask   []byte
		want   bool
	}{
		{
			name:   "single image and mask are allowed within threshold",
			images: [][]byte{make([]byte, 8)},
			mask:   make([]byte, 8),
			want:   true,
		},
		{
			name:   "single small image is allowed",
			images: [][]byte{make([]byte, 32*1024)},
			want:   true,
		},
		{
			name:   "multiple images are rejected",
			images: [][]byte{make([]byte, 8), make([]byte, 8)},
			want:   false,
		},
		{
			name:   "image at inline byte limit is allowed",
			images: [][]byte{make([]byte, maxResponsesInlineEditBytes)},
			want:   true,
		},
		{
			name:   "image above inline byte limit is rejected",
			images: [][]byte{make([]byte, maxResponsesInlineEditBytes+1)},
			want:   false,
		},
		{
			name:   "mask above inline byte limit is rejected",
			images: [][]byte{make([]byte, 8)},
			mask:   make([]byte, maxResponsesInlineEditBytes+1),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SupportsResponsesInlineEdit(tt.images, tt.mask); got != tt.want {
				t.Fatalf("SupportsResponsesInlineEdit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateImageWithPreviousResponseUsesNativePreviousResponse(t *testing.T) {
	var requestBody map[string]any
	client := NewResponsesClientWithProxyAndConfig("token", "", map[string]any{}, ImageRequestConfig{})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, &requestBody); err != nil {
			return nil, err
		}
		stream := `data: {"type":"response.completed","response":{"output":[{"type":"image_generation_call","result":"aGVsbG8=","output_format":"png"}]}}` + "\n\n"
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream)), Header: http.Header{}}, nil
	})}

	_, err := client.GenerateImageWithPreviousResponse(t.Context(), "make it realistic", "gpt-5.5", 1, "1536x1024", "high", "", "resp_123")
	if err != nil {
		t.Fatalf("GenerateImageWithPreviousResponse() returned error: %v", err)
	}
	if got := requestBody["previous_response_id"]; got != "resp_123" {
		t.Fatalf("payload previous_response_id = %v, want resp_123", got)
	}
	if got := requestBody["store"]; got != true {
		t.Fatalf("payload store = %v, want true", got)
	}
	input := requestBody["input"].([]any)[0].(map[string]any)
	content := input["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if strings.Contains(text, "resp_123") {
		t.Fatalf("prompt text contains response id: %q", text)
	}
	tool := requestBody["tools"].([]any)[0].(map[string]any)
	if got := tool["action"]; got != "generate" {
		t.Fatalf("tool action = %v, want generate", got)
	}
}

func TestResponsesEditIncludesMultipleThumbnailInputImages(t *testing.T) {
	var requestBody map[string]any
	client := NewResponsesClientWithProxyAndConfig("token", "", map[string]any{}, ImageRequestConfig{})
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, &requestBody); err != nil {
			return nil, err
		}
		stream := `data: {"type":"response.completed","response":{"output":[{"type":"image_generation_call","result":"aGVsbG8=","output_format":"png"}]}}` + "\n\n"
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream)), Header: http.Header{}}, nil
	})}

	_, err := client.EditImageByUpload(t.Context(), "edit these", "gpt-5.4-mini", [][]byte{testPNGBytes(), testPNGBytes()}, nil, "1536x1024", "high")
	if err != nil {
		t.Fatalf("EditImageByUpload() returned error: %v", err)
	}
	input := requestBody["input"].([]any)[0].(map[string]any)
	content := input["content"].([]any)
	imageCount := 0
	for _, item := range content {
		part := item.(map[string]any)
		if part["type"] == "input_image" {
			imageCount++
			if !strings.HasPrefix(part["image_url"].(string), "data:image/jpeg;base64,") {
				t.Fatalf("image_url = %q, want jpeg data url", part["image_url"])
			}
		}
	}
	if imageCount != 2 {
		t.Fatalf("input_image count = %d, want 2", imageCount)
	}
}

func testPNGBytes() []byte {
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

func TestBuildResponsesImageGenerationToolIncludesSupportedSize(t *testing.T) {
	tool, err := buildResponsesImageGenerationToolWithOptions(responsesImageToolOptions{
		RequestedModel: "gpt-image-2",
		Action:         "edit",
		Size:           "1536x1024",
		Quality:        "hd",
		Background:     "opaque",
		Mask:           []byte("mask"),
	})
	if err != nil {
		t.Fatalf("buildResponsesImageGenerationTool() returned error: %v", err)
	}

	if _, ok := tool["model"]; ok {
		t.Fatalf("tool model = %v, want omitted for gpt-image-2", tool["model"])
	}
	if got := tool["action"]; got != "edit" {
		t.Fatalf("tool action = %v, want %q", got, "edit")
	}
	if got := tool["size"]; got != "1536x1024" {
		t.Fatalf("tool size = %v, want %q", got, "1536x1024")
	}
	if got := tool["quality"]; got != "high" {
		t.Fatalf("tool quality = %v, want %q", got, "high")
	}
	if got := tool["background"]; got != "opaque" {
		t.Fatalf("tool background = %v, want %q", got, "opaque")
	}
	maskField, ok := tool["input_image_mask"].(map[string]any)
	if !ok {
		t.Fatalf("tool input_image_mask missing: %#v", tool)
	}
	if _, ok := maskField["image_url"]; !ok {
		t.Fatalf("tool input_image_mask.image_url missing: %#v", tool)
	}
}

func TestBuildResponsesImageGenerationToolRejectsUnsupportedSize(t *testing.T) {
	tool, err := buildResponsesImageGenerationToolWithOptions(responsesImageToolOptions{
		RequestedModel: "gpt-image-2",
		Action:         "generate",
		Size:           "8192x8192",
	})
	if err == nil {
		t.Fatalf("buildResponsesImageGenerationTool() returned nil error, tool=%#v", tool)
	}
}

func TestBuildResponsesImageGenerationToolPreservesExplicitCodexModel(t *testing.T) {
	tool, err := buildResponsesImageGenerationToolWithOptions(responsesImageToolOptions{
		RequestedModel: "gpt-5.4-mini",
		Action:         "generate",
		Size:           "1536x1024",
	})
	if err != nil {
		t.Fatalf("buildResponsesImageGenerationTool() returned error: %v", err)
	}
	if got := tool["model"]; got != "gpt-5.4-mini" {
		t.Fatalf("tool model = %v, want %q", got, "gpt-5.4-mini")
	}
}

func TestNormalizeResponsesImageToolModel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "gpt image 1 is omitted", input: "gpt-image-1", want: ""},
		{name: "gpt image 2 is omitted", input: "gpt-image-2", want: ""},
		{name: "auto is omitted", input: "auto", want: ""},
		{name: "gpt 5 4 mini is preserved", input: "gpt-5.4-mini", want: "gpt-5.4-mini"},
		{name: "unknown values are omitted", input: "unknown-model", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeResponsesImageToolModel(tt.input); got != tt.want {
				t.Fatalf("normalizeResponsesImageToolModel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResponsesClientUsesStableSessionID(t *testing.T) {
	client := NewResponsesClientWithProxyAndConfig("token", "", map[string]any{"account_id": "acct-1"}, ImageRequestConfig{})
	if strings.TrimSpace(client.sessionID) == "" {
		t.Fatal("sessionID = empty, want stable responses session id")
	}
}

func TestResponsesClientAllowsMissingAccountID(t *testing.T) {
	client := NewResponsesClientWithProxyAndConfig("token", "", map[string]any{}, ImageRequestConfig{})
	client.SetSessionID("session-1")
	if client.accountID != "" {
		t.Fatalf("accountID = %q, want empty", client.accountID)
	}
	if client.sessionID != "session-1" {
		t.Fatalf("sessionID = %q, want injected session", client.sessionID)
	}
}

func TestNewResponsesClientWithProxyAndConfigUsesProvidedSSETimeout(t *testing.T) {
	requestConfig := ImageRequestConfig{
		RequestTimeout: 15 * time.Second,
		SSETimeout:     75 * time.Second,
		PollInterval:   4 * time.Second,
		PollMaxWait:    33 * time.Second,
	}

	client := NewResponsesClientWithProxyAndConfig("token", "http://proxy.local", map[string]any{
		"account_id": "acct-1",
	}, requestConfig)

	if client.httpClient.Timeout != requestConfig.SSETimeout+30*time.Second {
		t.Fatalf("responses stream timeout = %v, want %v", client.httpClient.Timeout, requestConfig.SSETimeout+30*time.Second)
	}
	if client.backend.httpClient.Timeout != requestConfig.RequestTimeout {
		t.Fatalf("backend request timeout = %v, want %v", client.backend.httpClient.Timeout, requestConfig.RequestTimeout)
	}
	if client.backend.pollInterval != requestConfig.PollInterval {
		t.Fatalf("backend poll interval = %v, want %v", client.backend.pollInterval, requestConfig.PollInterval)
	}
	if client.backend.pollMaxWait != requestConfig.SSETimeout {
		t.Fatalf("backend poll max wait = %v, want %v", client.backend.pollMaxWait, requestConfig.SSETimeout)
	}
}
