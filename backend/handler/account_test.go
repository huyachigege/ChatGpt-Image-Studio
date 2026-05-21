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

func TestParseCodexRateLimitHeadersKeepsWeeklyWhenSecondaryWindowIsZero(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "15")
	headers.Set("x-codex-primary-reset-after-seconds", "604800")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-secondary-used-percent", "99")
	headers.Set("x-codex-secondary-reset-after-seconds", "300")
	headers.Set("x-codex-secondary-window-minutes", "0")

	info := parseCodexRateLimitHeaders(headers)
	if info.Codex7dWindowMinutes == nil || *info.Codex7dWindowMinutes != 10080 {
		t.Fatalf("Codex7dWindowMinutes = %v, want 10080", info.Codex7dWindowMinutes)
	}
	if info.Codex7dUsedPercent == nil || *info.Codex7dUsedPercent != 15 {
		t.Fatalf("Codex7dUsedPercent = %v, want 15", info.Codex7dUsedPercent)
	}
	if info.Codex5hWindowMinutes != nil || info.Codex5hUsedPercent != nil || info.Codex5hResetAfterSeconds != nil {
		t.Fatalf("5h window = (%v,%v,%v), want nil", info.Codex5hWindowMinutes, info.Codex5hUsedPercent, info.Codex5hResetAfterSeconds)
	}
}

func TestParseCodexRateLimitHeadersClassifiesWindowsByLength(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "25")
	headers.Set("x-codex-primary-reset-after-seconds", "120")
	headers.Set("x-codex-primary-window-minutes", "300")
	headers.Set("x-codex-secondary-used-percent", "40")
	headers.Set("x-codex-secondary-reset-after-seconds", "604800")
	headers.Set("x-codex-secondary-window-minutes", "10080")

	info := parseCodexRateLimitHeaders(headers)
	if info.Codex7dWindowMinutes == nil || *info.Codex7dWindowMinutes != 10080 {
		t.Fatalf("Codex7dWindowMinutes = %v, want 10080", info.Codex7dWindowMinutes)
	}
	if info.Codex5hWindowMinutes == nil || *info.Codex5hWindowMinutes != 300 {
		t.Fatalf("Codex5hWindowMinutes = %v, want 300", info.Codex5hWindowMinutes)
	}
	if info.Codex5hResetAfterSeconds == nil || *info.Codex5hResetAfterSeconds != 120 {
		t.Fatalf("Codex5hResetAfterSeconds = %v, want 120", info.Codex5hResetAfterSeconds)
	}
}
