package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type RemoteAccountInfo struct {
	Email                    string           `json:"email"`
	UserID                   string           `json:"user_id"`
	AccountType              string           `json:"type"`
	Quota                    int              `json:"quota"`
	LimitsProgress           []map[string]any `json:"limits_progress"`
	DefaultModelSlug         string           `json:"default_model_slug"`
	RestoreAt                string           `json:"restore_at"`
	Status                   string           `json:"status"`
	CodexQuotaKnown          bool             `json:"codex_quota_known,omitempty"`
	Codex7dUsedPercent       *float64         `json:"codex_7d_used_percent,omitempty"`
	Codex7dResetAfterSeconds *int             `json:"codex_7d_reset_after_seconds,omitempty"`
	Codex7dWindowMinutes     *int             `json:"codex_7d_window_minutes,omitempty"`
	Codex5hUsedPercent       *float64         `json:"codex_5h_used_percent,omitempty"`
	Codex5hResetAfterSeconds *int             `json:"codex_5h_reset_after_seconds,omitempty"`
	Codex5hWindowMinutes     *int             `json:"codex_5h_window_minutes,omitempty"`
}

type CodexRateLimitInfo struct {
	CodexQuotaKnown          bool
	Codex7dUsedPercent       *float64
	Codex7dResetAfterSeconds *int
	Codex7dWindowMinutes     *int
	Codex5hUsedPercent       *float64
	Codex5hResetAfterSeconds *int
	Codex5hWindowMinutes     *int
}

var accountTypeMap = map[string]string{
	"free":       "Free",
	"plus":       "Plus",
	"personal":   "Plus",
	"pro":        "Pro",
	"team":       "Team",
	"business":   "Team",
	"enterprise": "Team",
}

const proFallbackImageGenQuota = 999

var defaultCodexDesktopUserAgent = "Codex Desktop/0.131.0-alpha.9 (Windows 10.0.26200; x86_64) unknown (Codex Desktop; 26.513.40821)"

func FetchAccountInfo(ctx context.Context, accessToken string, authData map[string]any, timeout time.Duration) (*RemoteAccountInfo, error) {
	return FetchAccountInfoWithProxy(ctx, accessToken, authData, timeout, "")
}

func FetchAccountInfoWithProxy(ctx context.Context, accessToken string, authData map[string]any, timeout time.Duration, proxyURL string) (*RemoteAccountInfo, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, fmt.Errorf("access token is required")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: newChromeTransport(proxyURL),
	}

	codexLimits, err := fetchCodexRateLimits(ctx, client, accessToken, authData)
	if err != nil {
		return nil, err
	}

	accountType := detectAccountTypeFromAuthData(authData)
	if accountType == "" {
		accountType = "Free"
	}
	if codexLimits.Codex5hWindowMinutes != nil && accountType == "Free" {
		accountType = "Plus"
	}
	status := "正常"
	if codexLimits.Codex7dUsedPercent != nil && *codexLimits.Codex7dUsedPercent >= 100 {
		status = "限流"
	}

	return &RemoteAccountInfo{
		Email:                    firstString(authData, "email"),
		UserID:                   firstString(authData, "user_id", "account_id"),
		AccountType:              accountType,
		Status:                   status,
		CodexQuotaKnown:          codexLimits.CodexQuotaKnown,
		Codex7dUsedPercent:       codexLimits.Codex7dUsedPercent,
		Codex7dResetAfterSeconds: codexLimits.Codex7dResetAfterSeconds,
		Codex7dWindowMinutes:     codexLimits.Codex7dWindowMinutes,
		Codex5hUsedPercent:       codexLimits.Codex5hUsedPercent,
		Codex5hResetAfterSeconds: codexLimits.Codex5hResetAfterSeconds,
		Codex5hWindowMinutes:     codexLimits.Codex5hWindowMinutes,
	}, nil
}

func buildAccountHeaders(accessToken string, authData map[string]any) map[string]string {
	headers := map[string]string{
		"accept":             "*/*",
		"accept-language":    "zh-CN,zh;q=0.9,en;q=0.8",
		"authorization":      "Bearer " + strings.TrimSpace(accessToken),
		"content-type":       "application/json",
		"oai-language":       "zh-CN",
		"origin":             "https://chatgpt.com",
		"referer":            "https://chatgpt.com/",
		"sec-fetch-dest":     "empty",
		"sec-fetch-mode":     "cors",
		"sec-fetch-site":     "same-origin",
		"user-agent":         stringOrDefault(authData, "user-agent", defaultCodexDesktopUserAgent),
		"sec-ch-ua":          stringOrDefault(authData, "sec-ch-ua", `"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`),
		"sec-ch-ua-mobile":   stringOrDefault(authData, "sec-ch-ua-mobile", "?0"),
		"sec-ch-ua-platform": stringOrDefault(authData, "sec-ch-ua-platform", `"Windows"`),
	}
	if deviceID := firstString(authData, "oai-device-id", "oai_device_id", "device_id"); deviceID != "" {
		headers["oai-device-id"] = deviceID
	}
	if sessionID := firstString(authData, "oai-session-id", "oai_session_id", "session_id"); sessionID != "" {
		headers["oai-session-id"] = sessionID
	}
	if cookies := firstString(authData, "cookies", "cookie"); cookies != "" {
		headers["cookie"] = cookies
	}
	return headers
}

func doJSONRequest(ctx context.Context, client *http.Client, method, url string, headers map[string]string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	payload := map[string]any{}
	if len(data) == 0 {
		return payload, nil
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func detectAccountType(accessToken string, mePayload, initPayload map[string]any) string {
	tokenPayload := decodeAccessTokenPayload(accessToken)

	if authPayload, ok := tokenPayload["https://api.openai.com/auth"].(map[string]any); ok {
		if matched := normalizeAccountType(authPayload["chatgpt_plan_type"]); matched != "" {
			return matched
		}
	}

	for _, payload := range []any{mePayload, initPayload, tokenPayload} {
		if matched := searchAccountType(payload); matched != "" {
			return matched
		}
	}

	return "Free"
}

func detectAccountTypeFromAuthData(authData map[string]any) string {
	if len(authData) == 0 {
		return ""
	}
	if authPayload, ok := authData["https://api.openai.com/auth"].(map[string]any); ok {
		if matched := normalizeAccountType(authPayload["chatgpt_plan_type"]); matched != "" {
			return matched
		}
	}
	for _, key := range []string{"chatgpt_plan_type", "plan_type", "account_type"} {
		if matched := normalizeAccountType(authData[key]); matched != "" {
			return matched
		}
	}
	if idToken, ok := authData["id_token"].(map[string]any); ok {
		if matched := searchAccountType(idToken); matched != "" {
			return matched
		}
	}
	if idToken := strings.TrimSpace(stringValue(authData["id_token"])); idToken != "" {
		if matched := searchAccountType(decodeAccessTokenPayload(idToken)); matched != "" {
			return matched
		}
	}
	return ""
}

func detectCodexAccountTypeByRateLimits(ctx context.Context, client *http.Client, accessToken string, authData map[string]any) (string, error) {
	info, err := fetchCodexRateLimits(ctx, client, accessToken, authData)
	if err != nil {
		return "", err
	}
	if info.Codex5hWindowMinutes != nil && *info.Codex5hWindowMinutes > 0 && *info.Codex5hWindowMinutes <= 360 {
		return "Plus", nil
	}
	return "", nil
}

func fetchCodexRateLimits(ctx context.Context, client *http.Client, accessToken string, authData map[string]any) (*CodexRateLimitInfo, error) {
	if client == nil {
		return nil, fmt.Errorf("http client is required")
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("access token is required")
	}
	accountID := resolveChatGPTAccountID(accessToken, authData)

	payload := map[string]any{
		"model":        "gpt-5.4-mini",
		"input":        []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "hi"}}}},
		"stream":       true,
		"store":        false,
		"instructions": "You are a helpful assistant.",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexResponsesBaseURL+"/responses", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if accountID != "" {
		req.Header.Set("chatgpt-account-id", accountID)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Version", "0.125.0")
	req.Header.Set("User-Agent", "Codex Desktop/0.131.0-alpha.9 (Windows 10.0.26200; x86_64) unknown (Codex Desktop; 26.513.40821)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("codex rate limit probe returned HTTP %d", resp.StatusCode)
	}
	return parseCodexRateLimitHeaders(resp.Header), nil
}

func detectCodexAccountTypeFromRateLimitHeaders(headers http.Header) string {
	info := parseCodexRateLimitHeaders(headers)
	if info.Codex5hWindowMinutes != nil && *info.Codex5hWindowMinutes > 0 && *info.Codex5hWindowMinutes <= 360 {
		return "Plus"
	}
	return ""
}

func parseCodexRateLimitHeaders(headers http.Header) *CodexRateLimitInfo {
	info := &CodexRateLimitInfo{}
	applyCodexRateLimitWindow(info,
		parseHeaderPercent(headers.Get("x-codex-primary-used-percent")),
		parseHeaderIntPtr(headers.Get("x-codex-primary-reset-after-seconds")),
		parseHeaderIntPtr(headers.Get("x-codex-primary-window-minutes")),
	)
	applyCodexRateLimitWindow(info,
		parseHeaderPercent(headers.Get("x-codex-secondary-used-percent")),
		parseHeaderIntPtr(headers.Get("x-codex-secondary-reset-after-seconds")),
		parseHeaderIntPtr(headers.Get("x-codex-secondary-window-minutes")),
	)
	info.CodexQuotaKnown = info.Codex7dUsedPercent != nil || info.Codex7dResetAfterSeconds != nil || info.Codex7dWindowMinutes != nil || info.Codex5hUsedPercent != nil || info.Codex5hResetAfterSeconds != nil || info.Codex5hWindowMinutes != nil
	return info
}

func applyCodexRateLimitWindow(info *CodexRateLimitInfo, usedPercent *float64, resetAfterSeconds *int, windowMinutes *int) {
	if info == nil || windowMinutes == nil {
		return
	}
	if *windowMinutes > 0 && *windowMinutes <= 360 {
		info.Codex5hUsedPercent = usedPercent
		info.Codex5hResetAfterSeconds = resetAfterSeconds
		info.Codex5hWindowMinutes = windowMinutes
		return
	}
	info.Codex7dUsedPercent = usedPercent
	info.Codex7dResetAfterSeconds = resetAfterSeconds
	info.Codex7dWindowMinutes = windowMinutes
}

func parseHeaderInt(value string) int {
	if parsed := parseHeaderIntPtr(value); parsed != nil {
		return *parsed
	}
	return 0
}

func parseHeaderIntPtr(value string) *int {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func parseHeaderPercent(value string) *float64 {
	value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil
	}
	return &parsed
}

func decodeAccessTokenPayload(accessToken string) map[string]any {
	parts := strings.Split(strings.TrimSpace(accessToken), ".")
	if len(parts) < 2 {
		return map[string]any{}
	}
	payload := parts[1]
	payload += strings.Repeat("=", (4-len(payload)%4)%4)
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return map[string]any{}
	}
	result := map[string]any{}
	if err := json.Unmarshal(decoded, &result); err != nil {
		return map[string]any{}
	}
	return result
}

func normalizeAccountType(value any) string {
	return accountTypeMap[strings.ToLower(strings.TrimSpace(stringValue(value)))]
}

func searchAccountType(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			lowerKey := strings.ToLower(strings.TrimSpace(key))
			if matched := normalizeAccountType(item); matched != "" {
				if strings.Contains(lowerKey, "plan") || strings.Contains(lowerKey, "type") || strings.Contains(lowerKey, "subscription") || strings.Contains(lowerKey, "workspace") || strings.Contains(lowerKey, "tier") {
					return matched
				}
			}
		}
		for _, item := range typed {
			if matched := searchAccountType(item); matched != "" {
				return matched
			}
		}
	case []any:
		for _, item := range typed {
			if matched := searchAccountType(item); matched != "" {
				return matched
			}
		}
	default:
		return normalizeAccountType(typed)
	}
	return ""
}

func normalizeLimitsProgress(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]map[string]any); ok {
			return typed
		}
		return []map[string]any{}
	}

	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if typed, ok := item.(map[string]any); ok {
			result = append(result, typed)
		}
	}
	return result
}

func extractQuotaAndRestoreAt(items []map[string]any) (int, string) {
	for _, item := range items {
		if strings.TrimSpace(stringValue(item["feature_name"])) != "image_gen" {
			continue
		}
		return intValue(item["remaining"]), stringValue(item["reset_after"])
	}
	return 0, ""
}

func hasLimitFeature(items []map[string]any, featureName string) bool {
	target := strings.TrimSpace(strings.ToLower(featureName))
	for _, item := range items {
		if strings.TrimSpace(strings.ToLower(stringValue(item["feature_name"]))) != target {
			continue
		}
		return true
	}
	return false
}

func stringOrDefault(data map[string]any, key, fallback string) string {
	if value := firstString(data, key); value != "" {
		return value
	}
	return fallback
}

func firstString(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringValue(data[key])); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	case string:
		var n int
		fmt.Sscanf(strings.TrimSpace(typed), "%d", &n)
		return n
	default:
		return 0
	}
}
