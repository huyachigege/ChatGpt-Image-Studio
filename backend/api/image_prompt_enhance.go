package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"chatgpt2api/internal/config"
)

const maxPromptEnhanceReferenceImages = 4

type imagePromptEnhanceRequest struct {
	Prompt              string                        `json:"prompt"`
	Mode                string                        `json:"mode,omitempty"`
	SourceImages        []imageTaskSourceImagePayload `json:"sourceImages,omitempty"`
	Size                string                        `json:"size,omitempty"`
	Quality             string                        `json:"quality,omitempty"`
	ConversationContext string                        `json:"conversationContext,omitempty"`
	ConversationInput   []imageConversationInputItem  `json:"conversationInput,omitempty"`
	Auto                bool                          `json:"auto,omitempty"`
}

type imagePromptEnhanceResponse struct {
	Prompt  string   `json:"prompt"`
	Prompts []string `json:"prompts,omitempty"`
}

func (s *Server) handleEnhanceImagePrompt(w http.ResponseWriter, r *http.Request) {
	var req imagePromptEnhanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" && !hasPromptEnhanceSourceImages(req.SourceImages) {
		writeAPIError(w, http.StatusBadRequest, "prompt_or_reference_required", "prompt or reference image is required")
		return
	}
	provider, ok := s.firstExternalResponsesProvider()
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "external_responses_not_configured", "external_responses 还未配置，请先设置至少一个可用 provider")
		return
	}
	imageURLs, err := s.buildPromptEnhanceImageURLs(req.SourceImages, identityFromContext(r.Context()).UserID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_reference_image", err.Error())
		return
	}
	prompts, err := runImagePromptEnhanceModel(r, provider, req, imageURLs)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "prompt_enhance_failed", err.Error())
		return
	}
	prompts = normalizeEnhancedPrompts(prompts)
	if len(prompts) == 0 {
		writeAPIError(w, http.StatusBadGateway, "prompt_enhance_empty", "prompt enhance model returned empty text")
		return
	}
	writeJSON(w, http.StatusOK, imagePromptEnhanceResponse{Prompt: prompts[0], Prompts: prompts})
}

func hasPromptEnhanceSourceImages(sources []imageTaskSourceImagePayload) bool {
	for _, source := range sources {
		if !strings.EqualFold(strings.TrimSpace(source.Role), "mask") && (strings.TrimSpace(source.DataURL) != "" || strings.TrimSpace(source.URL) != "") {
			return true
		}
	}
	return false
}

func (s *Server) firstExternalResponsesProvider() (config.ExternalResponsesProviderConfig, bool) {
	providers := s.externalResponsesProviders()
	if len(providers) == 0 {
		return config.ExternalResponsesProviderConfig{}, false
	}
	return providers[0], true
}

func (s *Server) buildPromptEnhanceImageURLs(sources []imageTaskSourceImagePayload, userID string) ([]string, error) {
	urls := make([]string, 0, min(len(sources), maxPromptEnhanceReferenceImages))
	for _, source := range sources {
		if strings.EqualFold(strings.TrimSpace(source.Role), "mask") {
			continue
		}
		data, err := s.resolveTaskSourceImageBytes(imageTaskSourceImage{ID: source.ID, Role: source.Role, Name: source.Name, DataURL: source.DataURL, URL: source.URL})
		if err != nil {
			return nil, err
		}
		url, err := s.buildExternalResponsesImageReference(data, userID, "prompt-reference")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(url) != "" {
			urls = append(urls, url)
		}
		if len(urls) >= maxPromptEnhanceReferenceImages {
			break
		}
	}
	return urls, nil
}

func runImagePromptEnhanceModel(r *http.Request, provider config.ExternalResponsesProviderConfig, req imagePromptEnhanceRequest, imageURLs []string) ([]string, error) {
	imageParts := make([]map[string]any, 0, len(imageURLs))
	for _, imageURL := range imageURLs {
		if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
			imageParts = append(imageParts, map[string]any{"type": "input_image", "image_url": imageURL, "detail": "high"})
		}
	}
	input := buildPromptEnhanceInputItems(req, imageParts)
	payload := map[string]any{
		"model":        provider.Model,
		"input":        input,
		"instructions": buildImagePromptEnhanceInstructions(req.Auto),
		"reasoning": map[string]any{
			"effort": "xhigh",
		},
		"stream": false,
		"store":  false,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Timeout: newExternalResponsesClientForProvider(provider).httpClient.Timeout}
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")+"/v1/responses", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(provider.APIKey))
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("prompt enhance model returned %d: %s", resp.StatusCode, summarizeCPAError(body))
	}
	text := extractResponsesText(body)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("prompt enhance model returned empty text")
	}
	return parseEnhancedPrompts(text), nil
}

func buildImagePromptEnhanceInstructions(auto bool) string {
	if auto {
		return "你是提示词工程师。请理解用户画图/编辑的真实意图，结合上下文和参考图生成可直接用于后续图像生成/编辑的增强提示词。即使存在歧义，也必须自动选择最符合上下文和本轮意图的一条最佳提示词，只返回 1 个提示词。必须保留用户核心意图，不编造品牌、身份、敏感文字。只输出 JSON：{\"prompts\":[\"...\"]}。"
	}
	return "你是提示词工程师。请理解用户画图/编辑的真实意图，结合上下文和参考图生成可直接用于后续图像生成/编辑的增强提示词。无论意图是否明确，都至少返回 1 个候选；若存在关键歧义，返回 2 到 4 个互斥方向的候选，每个候选都应可直接使用。必须保留用户核心意图，不编造品牌、身份、敏感文字。只输出 JSON：{\"prompts\":[\"...\"]}。"
}

func buildPromptEnhanceInputItems(req imagePromptEnhanceRequest, imageParts []map[string]any) []any {
	currentText := buildImagePromptEnhancePrompt(req, len(imageParts))
	items := normalizeImageConversationInput(req.ConversationInput)
	if len(items) == 0 {
		items = []imageConversationInputItem{{
			Role: "user",
			Content: []imageConversationInputContent{{
				Type: "input_text",
				Text: currentText,
			}},
		}}
	} else {
		items = append(items, imageConversationInputItem{
			Role: "user",
			Content: []imageConversationInputContent{{
				Type: "input_text",
				Text: currentText,
			}},
		})
	}
	return imageConversationInputItemsToPayload(appendImagesToLastUserInput(items, imageParts))
}

func buildImagePromptEnhancePrompt(req imagePromptEnhanceRequest, imageCount int) string {
	return fmt.Sprintf(`这是同一个图片创作会话的历史上下文。请延续用户之前确认的主体、风格、构图、角色/物体关系与已完成修改；如果本轮提示与历史冲突，以本轮提示为准。
%s

本轮用户请求：
%s

模式：%s
尺寸：%s
质量：%s
参考图数量：%d

任务：
- 理解用户真实图像生成/编辑意图，完善为后续可直接提交给图像模型的增强提示词。
- 全自动模式：%t。
- 如果全自动模式为 true，即使存在歧义，也自动选择最符合上下文和本轮意图的一条最佳提示词，只返回 1 个增强提示词。
- 如果全自动模式为 false，至少返回 1 个候选；如果存在关键歧义，返回 2 到 4 个互斥候选，每个候选都应完整可用，并明确体现不同方向。
- 有参考图时，提炼安全的主体特征、风格、构图、材质、光线、色彩、背景与镜头语言。
- 补充具体可视约束：主体、场景、构图、视角、光线、色彩、细节、风格、画质、尺寸/比例建议。
- 每个候选提示词都尽量明确画幅比例/构图比例建议，例如“建议画幅比例 2:3 竖版”“16:9 横版宽屏”“1:1 方图”；如用户或设置已有明确尺寸/比例，以现有设置为准，不要冲突。
- 不编造品牌、人物身份、商标、敏感文字，不改变用户核心设定。
- 只输出 JSON：{"prompts":["提示词1","提示词2"]}。`, strings.TrimSpace(req.ConversationContext), strings.TrimSpace(req.Prompt), strings.TrimSpace(req.Mode), strings.TrimSpace(req.Size), strings.TrimSpace(req.Quality), imageCount, req.Auto)
}

func parseEnhancedPrompts(text string) []string {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	var payload struct {
		Prompts []string `json:"prompts"`
		Prompt  string   `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		if len(payload.Prompts) > 0 {
			return payload.Prompts
		}
		if strings.TrimSpace(payload.Prompt) != "" {
			return []string{payload.Prompt}
		}
	}
	return []string{trimmed}
}

func normalizeEnhancedPrompts(prompts []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(prompts))
	for _, prompt := range prompts {
		prompt = strings.TrimSpace(prompt)
		if prompt == "" || seen[prompt] {
			continue
		}
		seen[prompt] = true
		result = append(result, prompt)
	}
	return result
}
