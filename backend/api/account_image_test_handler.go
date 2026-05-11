package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"chatgpt2api/handler"
	"chatgpt2api/internal/accounts"
	"chatgpt2api/internal/outboundproxy"
)

type accountImageTestResult struct {
	OK             bool   `json:"ok"`
	AccountID      string `json:"accountId"`
	AccountEmail   string `json:"accountEmail"`
	Route          string `json:"route"`
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	RequestBody    string `json:"requestBody,omitempty"`
	ResponseStatus int    `json:"responseStatus,omitempty"`
	ResponseBody   string `json:"responseBody,omitempty"`
	ImageURL       string `json:"imageUrl,omitempty"`
	Error          string `json:"error,omitempty"`
	LatencyMS      int64  `json:"latencyMs"`
}

func (s *Server) handleAccountImageTest(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.PathValue("id"))
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "account id is required"})
		return
	}

	var body struct {
		Route string `json:"route"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	route := strings.ToLower(strings.TrimSpace(body.Route))

	store := s.getStore()
	if store == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "account store not available"})
		return
	}

	auth, account, err := store.FindImageAuthByID(accountID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": fmt.Sprintf("account not found: %v", err)})
		return
	}
	if route == "" {
		if accounts.AccountSupportsImageRoute(account, "responses") {
			route = "responses"
		} else {
			route = "legacy"
		}
	}
	if route == "responses" && !accounts.AccountSupportsImageRoute(account, "responses") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "current image account does not support responses test route"})
		return
	}

	testPrompt := "a simple red circle on white background"
	proxyURL := s.cfg.ChatGPTProxyURL()
	model := normalizeRequestedImageModel("", s.cfg.ChatGPT.Model)

	result := accountImageTestResult{
		AccountID:    account.ID,
		AccountEmail: account.Email,
		Prompt:       testPrompt,
		Route:        route,
		Model:        model,
	}

	ct := newCapturingTransport(proxyURL)
	requestConfig := s.imageRequestConfig()
	ctx, cancel := context.WithTimeout(r.Context(), requestConfig.SSETimeout+30*time.Second)
	defer cancel()

	startedAt := time.Now()
	var images []handler.ImageResult
	var genErr error

	switch route {
	case "responses":
		responsesModel := s.resolveImageUpstreamModel("gpt-image-2", account.Type)
		result.Model = responsesModel
		rc := handler.NewResponsesClientWithProxyAndConfig(auth.AccessToken, proxyURL, auth.Data, requestConfig)
		images, genErr = rc.GenerateImage(ctx, testPrompt, responsesModel, 1, "", "", "")
	case "legacy", "conversation":
		result.Route = "legacy"
		client := handler.NewChatGPTClientWithTransport(auth.AccessToken, proxyURL, auth.Data, requestConfig, ct)
		images, genErr = client.GenerateImage(ctx, testPrompt, model, 1, "", "", "")
		result.RequestBody = truncateForDisplay(ct.lastReqBody, 4000)
		result.ResponseBody = truncateForDisplay(ct.lastRespBody, 8000)
		result.ResponseStatus = ct.lastStatusCode
	default:
		result.Route = "legacy"
		client := handler.NewChatGPTClientWithTransport(auth.AccessToken, proxyURL, auth.Data, requestConfig, ct)
		images, genErr = client.GenerateImage(ctx, testPrompt, model, 1, "", "", "")
		result.RequestBody = truncateForDisplay(ct.lastReqBody, 4000)
		result.ResponseBody = truncateForDisplay(ct.lastRespBody, 8000)
		result.ResponseStatus = ct.lastStatusCode
	}

	result.LatencyMS = time.Since(startedAt).Milliseconds()

	if result.RequestBody == "" {
		if ct.lastReqBody != "" {
			result.RequestBody = truncateForDisplay(ct.lastReqBody, 4000)
		}
	}
	if result.ResponseBody == "" {
		if ct.lastRespBody != "" {
			result.ResponseBody = truncateForDisplay(ct.lastRespBody, 8000)
		}
	}
	if result.ResponseStatus == 0 && ct.lastStatusCode > 0 {
		result.ResponseStatus = ct.lastStatusCode
	}

	if genErr != nil {
		result.OK = false
		result.Error = genErr.Error()
		if isImageRateLimitError(genErr) {
			store.RemoveImageRouteCapability(auth.AccessToken, result.Route)
		}
	} else if len(images) > 0 {
		result.OK = true
		result.ImageURL = images[0].URL
		if result.ResponseBody == "" {
			result.ResponseBody = fmt.Sprintf("success: %d image(s)", len(images))
		}
	} else {
		result.OK = false
		result.Error = "no images returned"
	}

	writeJSON(w, http.StatusOK, result)
}

func truncateForDisplay(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "..."
}

type capturingTransport struct {
	inner          http.RoundTripper
	lastReqBody    string
	lastRespBody   string
	lastStatusCode int
}

func (t *capturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		bodyBytes, _ := io.ReadAll(req.Body)
		t.lastReqBody = string(bodyBytes)
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}
	resp, err := t.inner.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	t.lastStatusCode = resp.StatusCode
	respBytes, _ := io.ReadAll(resp.Body)
	t.lastRespBody = string(respBytes)
	resp.Body = io.NopCloser(bytes.NewReader(respBytes))
	return resp, nil
}

func newCapturingTransport(proxyURL string) *capturingTransport {
	inner, err := outboundproxy.NewHTTPTransport(proxyURL)
	if err != nil || inner == nil {
		return &capturingTransport{inner: http.DefaultTransport.(*http.Transport)}
	}
	return &capturingTransport{inner: inner}
}
