package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
)

func TestDetectAccountTypeFromAuthDataDecodesIDToken(t *testing.T) {
	payload := map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type": "plus",
		},
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	idToken := "e30." + base64.RawURLEncoding.EncodeToString(rawPayload) + ".sig"

	if got := detectAccountTypeFromAuthData(map[string]any{"id_token": idToken}); got != "Plus" {
		t.Fatalf("expected Plus, got %q", got)
	}
	if got := detectAccountTypeFromAuthData(map[string]any{"id_token": payload}); got != "Plus" {
		t.Fatalf("expected Plus from decoded id_token object, got %q", got)
	}
}

func TestDetectCodexAccountTypeFromRateLimitHeadersDetectsPlusWindow(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-secondary-window-minutes", "300")

	if got := detectCodexAccountTypeFromRateLimitHeaders(headers); got != "Plus" {
		t.Fatalf("expected Plus, got %q", got)
	}
}

func TestDetectCodexAccountTypeFromRateLimitHeadersIgnoresFreeWindow(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-secondary-window-minutes", "0")

	if got := detectCodexAccountTypeFromRateLimitHeaders(headers); got != "" {
		t.Fatalf("expected no override, got %q", got)
	}
}
