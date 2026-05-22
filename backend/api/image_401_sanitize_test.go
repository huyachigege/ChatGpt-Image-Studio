package api

import (
	"errors"
	"strings"
	"testing"
)

func TestSanitizeImageUserFacingMessageHides401Details(t *testing.T) {
	err := newRequestError("source_account_unavailable", "当前账号 401 不可用，已从账号池删除并切换下一个账号")
	message := sanitizeImageUserFacingMessage(err)
	for _, token := range []string{"401", "删除", "账号池", "Unauthorized", "invalid_token"} {
		if strings.Contains(message, token) {
			t.Fatalf("sanitized message %q contains %q", message, token)
		}
	}
	if !strings.Contains(message, "图片服务暂时不可用") {
		t.Fatalf("sanitized message = %q, want neutral service unavailable message", message)
	}
}

func TestSanitizeImageUserFacingMessageKeepsBusinessErrors(t *testing.T) {
	err := newRequestError("user_free_quota_exhausted", "今日 free 图片额度不足")
	if got := sanitizeImageUserFacingMessage(err); got != err.Error() {
		t.Fatalf("sanitize business error = %q, want %q", got, err.Error())
	}
}

func TestExternalResponsesUnavailableIsRetryableFallback(t *testing.T) {
	err := newRequestError("external_responses_unavailable", "图片服务外部通道不可用，正在切换其他可用通道")
	if got := classifyImageAttemptError(err); got != imageAttemptRetryableDisableResponses {
		t.Fatalf("classifyImageAttemptError = %v, want %v", got, imageAttemptRetryableDisableResponses)
	}
	if !shouldRetryImageRequestWithNextAccount(err) {
		t.Fatalf("shouldRetryImageRequestWithNextAccount returned false for external unavailable")
	}
}

func TestRaw403IsRetryableFallback(t *testing.T) {
	err := errors.New("upload image 0: pre-upload returned 403: forbidden")
	if got := classifyImageAttemptError(err); got != imageAttemptRetryableDisableResponses {
		t.Fatalf("classifyImageAttemptError = %v, want %v", got, imageAttemptRetryableDisableResponses)
	}
	if !shouldRetryImageRequestWithNextAccount(err) {
		t.Fatalf("shouldRetryImageRequestWithNextAccount returned false for 403")
	}
}

func TestRaw401IsSanitized(t *testing.T) {
	message := sanitizeImageUserFacingMessage(errors.New("responses returned 401: unauthorized invalid_token"))
	for _, token := range []string{"401", "unauthorized", "invalid_token"} {
		if strings.Contains(strings.ToLower(message), token) {
			t.Fatalf("sanitized message %q contains %q", message, token)
		}
	}
}
