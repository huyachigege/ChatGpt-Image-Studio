package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type RemoteAccountInfo struct {
	Email            string           `json:"email"`
	UserID           string           `json:"user_id"`
	AccountType      string           `json:"type"`
	Quota            int              `json:"quota"`
	LimitsProgress   []map[string]any `json:"limits_progress"`
	DefaultModelSlug string           `json:"default_model_slug"`
	RestoreAt        string           `json:"restore_at"`
	Status           string           `json:"status"`
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

	meHeaders := buildAccountHeaders(accessToken, authData)
	meHeaders["x-openai-target-path"] = "/backend-api/me"
	meHeaders["x-openai-target-route"] = "/backend-api/me"

	initHeaders := buildAccountHeaders(accessToken, authData)
	initBody := map[string]any{
		"gizmo_id":                nil,
		"requested_default_model": nil,
		"conversation_id":         nil,
		"timezone_offset_min":     -480,
	}

	type responseResult struct {
		payload map[string]any
		err     error
	}

	meCh := make(chan responseResult, 1)
	initCh := make(chan responseResult, 1)

	go func() {
		payload, err := doJSONRequest(ctx, client, http.MethodGet, baseURL+"/me", meHeaders, nil)
		if err != nil {
			meCh <- responseResult{err: fmt.Errorf("/backend-api/me failed: %w", err)}
			return
		}
		meCh <- responseResult{payload: payload}
	}()

	go func() {
		payload, err := doJSONRequest(ctx, client, http.MethodPost, baseURL+"/conversation/init", initHeaders, initBody)
		if err != nil {
			initCh <- responseResult{err: fmt.Errorf("/backend-api/conversation/init failed: %w", err)}
			return
		}
		initCh <- responseResult{payload: payload}
	}()

	meResp := <-meCh
	initResp := <-initCh

	if meResp.err != nil {
		return nil, meResp.err
	}
	if initResp.err != nil {
		return nil, initResp.err
	}

	limitsProgress := normalizeLimitsProgress(initResp.payload["limits_progress"])
	quota, restoreAt := extractQuotaAndRestoreAt(limitsProgress)
	accountType := detectAccountType(accessToken, meResp.payload, initResp.payload)
	if authDataType := detectAccountTypeFromAuthData(authData); accountType == "Free" && authDataType != "" && authDataType != "Free" {
		accountType = authDataType
	}
	if accountType == "Free" {
		if detected, err := detectCodexAccountTypeByRateLimits(ctx, client, accessToken, authData); err == nil && detected != "" {
			accountType = detected
		}
	}
	if accountType == "Pro" && !hasLimitFeature(limitsProgress, "image_gen") {
		quota = proFallbackImageGenQuota
		limitsProgress = append(limitsProgress, map[string]any{
			"feature_name": "image_gen",
			"remaining":    quota,
		})
	}
	status := "正常"
	if quota == 0 {
		status = "限流"
	}

	return &RemoteAccountInfo{
		Email:            stringValue(meResp.payload["email"]),
		UserID:           stringValue(meResp.payload["id"]),
		AccountType:      accountType,
		Quota:            quota,
		LimitsProgress:   limitsProgress,
		DefaultModelSlug: stringValue(initResp.payload["default_model_slug"]),
		RestoreAt:        restoreAt,
		Status:           status,
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
		"user-agent":         stringOrDefault(authData, "user-agent", defaultUserAgent),
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
	if client == nil {
		return "", fmt.Errorf("http client is required")
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return "", fmt.Errorf("access token is required")
	}
	accountID := resolveChatGPTAccountID(accessToken, authData)
	if strings.TrimSpace(accountID) == "" {
		return "", nil
	}

	payload := map[string]any{
		"model": defaultUpstreamModel,
		"input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "hi"}}}},
		"instructions": `你是一个专业的 AI 画图助手。

你的职责是根据用户需求，直接生成最终图片，而不是解释过程、分析需求，或输出给别的模型使用的提示词。

工作原则：
1. 准确理解用户想要的画面内容，保留主题、主体、场景、风格、构图、色调、光线、材质和情绪。
2. 当用户描述不完整时，自动补足合理且有助于成图的视觉细节，但不要擅自改变核心设定。
3. 默认直接产出图片，不额外输出提示词、解释、分析过程或改写说明。
4. 普通画图需求正常处理，包括人物、场景、海报、产品图、封面图、插画、概念图、影视感画面等。
5. 如果需求包含追逐、打斗、对峙、爆炸、武器等冲突元素，应保留戏剧张力和视觉表现力，但采用非血腥、非伤害细节、电影化表达方式。
6. 不生成血腥、残肢、内脏、伤口特写、虐待、处决、未成年人受伤、明显死亡细节等高风险内容。
7. 对冲突场面，优先呈现为紧张对峙、动作瞬间、动态追逐、爆炸余波、被击退、失去平衡、烟尘与压迫感，而不直接展示具体伤害过程。
8. 如果用户未指定风格，可根据主题自动选择最合适的视觉风格，使画面自然、完整、可用。
9. 整体目标是提高成图质量、稳定性和通过率，同时尽量保留用户原始创作意图。
10. 除非用户明确要求文字说明，否则不要输出说明文字；默认只生成图片结果。`,
		"stream": true,
		"store":  false,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexResponsesBaseURL+"/responses", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Chatgpt-Account-Id", accountID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", codexResponsesUserAgent)
	req.Header.Set("Originator", codexResponsesOriginator)
	req.Header.Set("Session_id", uuid.NewString())
	req.Header.Set("Connection", "Keep-Alive")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("codex rate limit probe returned HTTP %d", resp.StatusCode)
	}
	return detectCodexAccountTypeFromRateLimitHeaders(resp.Header), nil
}

func detectCodexAccountTypeFromRateLimitHeaders(headers http.Header) string {
	windows := []int{
		parseHeaderInt(headers.Get("x-codex-primary-window-minutes")),
		parseHeaderInt(headers.Get("x-codex-secondary-window-minutes")),
	}
	for _, window := range windows {
		if window > 0 && window <= 360 {
			return "Plus"
		}
	}
	return ""
}

func parseHeaderInt(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	var result int
	if _, err := fmt.Sscanf(value, "%d", &result); err != nil {
		return 0
	}
	return result
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
