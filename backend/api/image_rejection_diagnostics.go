package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"chatgpt2api/handler"
	"chatgpt2api/internal/config"
)

type imageRejectionDiagnosticRequest struct {
	Prompt       string                        `json:"prompt"`
	Error        string                        `json:"error,omitempty"`
	Mode         string                        `json:"mode,omitempty"`
	SourceImages []imageTaskSourceImagePayload `json:"sourceImages,omitempty"`
	Size         string                        `json:"size,omitempty"`
	Quality      string                        `json:"quality,omitempty"`
}

type imageRejectionDiagnosticReference struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	DataURL string `json:"dataUrl,omitempty"`
}

type imageRejectionDiagnosticResponse struct {
	Reason                 string                               `json:"reason"`
	RevisedPrompt          string                               `json:"revisedPrompt"`
	ReferencePrompt        string                               `json:"referencePrompt,omitempty"`
	OmitOriginalReferences bool                                 `json:"omitOriginalReferences"`
	ReferenceImages        []imageRejectionDiagnosticReference   `json:"referenceImages,omitempty"`
	Notes                  []string                             `json:"notes,omitempty"`
}

func (s *Server) handleDiagnoseImageRejection(w http.ResponseWriter, r *http.Request) {
	var req imageRejectionDiagnosticRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeAPIError(w, http.StatusBadRequest, "prompt_required", "prompt is required")
		return
	}

	images, _ := s.resolveDiagnosticSourceImages(req.SourceImages)
	result := fallbackImageRejectionDiagnostic(req, len(images) > 0)

	cfg := s.cfg.ExternalResponsesConfig()
	if externalResponsesConfigReady(cfg) {
		if analyzed, err := runImageRejectionDiagnosticModel(r.Context(), cfg, req, images); err == nil {
			result = mergeImageRejectionDiagnostic(result, analyzed)
		}
	}

	if strings.TrimSpace(result.ReferencePrompt) != "" && externalResponsesConfigReady(cfg) {
		if refs := s.generateDiagnosticReferenceImage(r.Context(), r, cfg, result.ReferencePrompt, req.Size, req.Quality); len(refs) > 0 {
			result.ReferenceImages = refs
		}
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) resolveDiagnosticSourceImages(sources []imageTaskSourceImagePayload) ([][]byte, error) {
	images := make([][]byte, 0, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source.Role) == "mask" {
			continue
		}
		data, err := s.resolveTaskSourceImageBytes(imageTaskSourceImage{
			ID:      source.ID,
			Role:    source.Role,
			Name:    source.Name,
			DataURL: source.DataURL,
			URL:     source.URL,
		})
		if err != nil {
			return images, err
		}
		images = append(images, data)
		if len(images) >= 3 {
			break
		}
	}
	return images, nil
}

func externalResponsesConfigReady(cfg config.ExternalResponsesConfig) bool {
	return cfg.Enabled && strings.TrimSpace(cfg.BaseURL) != "" && strings.TrimSpace(cfg.APIKey) != "" && strings.TrimSpace(cfg.Model) != ""
}

func fallbackImageRejectionDiagnostic(req imageRejectionDiagnosticRequest, hasReference bool) imageRejectionDiagnosticResponse {
	prompt := strings.TrimSpace(req.Prompt)
	errorText := strings.ToLower(req.Error)
	omitReferences := hasReference && (strings.Contains(errorText, "sexual") || strings.Contains(errorText, "safety"))
	revised := prompt
	replacements := []struct{ old, new string }{
		{"身材高挑模特型", "气质成熟的成年女性"},
		{"修身红色紧身裙", "得体红色晚礼服"},
		{"红色紧身裙", "红色晚礼服"},
		{"暗中打量她", "观察局势"},
		{"打量她", "观察局势"},
		{"暗藏危机", "带有悬疑张力"},
	}
	for _, item := range replacements {
		revised = strings.ReplaceAll(revised, item.old, item.new)
	}
	if !strings.Contains(revised, "成年人") && !strings.Contains(revised, "成年") {
		revised += "，所有人物均为成年人"
	}
	if !strings.Contains(revised, "不强调身体特征") {
		revised += "，服装得体克制，姿态自然，镜头客观，不强调身体特征，整体呈现悬疑电影氛围"
	}

	referencePrompt := ""
	if omitReferences {
		referencePrompt = "写实电影级动漫画风，昏暗夜店酒吧环境，环形大型卡座，霓虹灯光，冷暖对比光影，成年人群像，悬疑紧张氛围，服装得体，姿态自然，客观广角镜头，重点表现空间布局和电影光影"
	}
	return imageRejectionDiagnosticResponse{
		Reason:                 "安全系统可能把原提示词或参考图中的服装、姿态、夜店场景和被观察关系组合判为成人化风险。",
		RevisedPrompt:          revised,
		ReferencePrompt:        referencePrompt,
		OmitOriginalReferences: omitReferences,
		Notes:                  []string{"已把高风险表达改成悬疑氛围表达", "重试时会避免继续携带可能导致拒绝的原参考图"},
	}
}

func mergeImageRejectionDiagnostic(base, next imageRejectionDiagnosticResponse) imageRejectionDiagnosticResponse {
	if strings.TrimSpace(next.Reason) != "" {
		base.Reason = strings.TrimSpace(next.Reason)
	}
	if strings.TrimSpace(next.RevisedPrompt) != "" {
		base.RevisedPrompt = strings.TrimSpace(next.RevisedPrompt)
	}
	if strings.TrimSpace(next.ReferencePrompt) != "" {
		base.ReferencePrompt = strings.TrimSpace(next.ReferencePrompt)
	}
	base.OmitOriginalReferences = base.OmitOriginalReferences || next.OmitOriginalReferences
	if len(next.Notes) > 0 {
		base.Notes = next.Notes
	}
	return base
}

func runImageRejectionDiagnosticModel(ctx context.Context, cfg config.ExternalResponsesConfig, req imageRejectionDiagnosticRequest, images [][]byte) (imageRejectionDiagnosticResponse, error) {
	content := []map[string]any{{
		"type": "input_text",
		"text": buildImageRejectionDiagnosticPrompt(req, len(images)),
	}}
	for _, image := range images {
		if len(image) == 0 {
			continue
		}
		content = append(content, map[string]any{
			"type":      "input_image",
			"image_url": handler.EncodeResponsesImageDataURL(image),
		})
	}
	payload := map[string]any{
		"model": cfg.Model,
		"input": []any{map[string]any{
			"role":    "user",
			"content": content,
		}},
		"instructions": "You are an image safety and prompt-rewrite assistant. Analyze only. Return strict JSON. Do not generate explicit content.",
		"stream":       false,
		"store":        false,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return imageRejectionDiagnosticResponse{}, err
	}
	httpClient := &http.Client{Timeout: newExternalResponsesClient(cfg).httpClient.Timeout}
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")+"/v1/responses", bytes.NewReader(raw))
	if err != nil {
		return imageRejectionDiagnosticResponse{}, err
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	reqHTTP.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(reqHTTP)
	if err != nil {
		return imageRejectionDiagnosticResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return imageRejectionDiagnosticResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return imageRejectionDiagnosticResponse{}, fmt.Errorf("diagnostic model returned %d: %s", resp.StatusCode, summarizeCPAError(body))
	}
	text := extractResponsesText(body)
	if text == "" {
		return imageRejectionDiagnosticResponse{}, fmt.Errorf("diagnostic model returned empty text")
	}
	return parseImageRejectionDiagnosticJSON(text)
}

func buildImageRejectionDiagnosticPrompt(req imageRejectionDiagnosticRequest, imageCount int) string {
	return fmt.Sprintf(`Analyze why this image-generation request was rejected and rewrite it for a safer retry.

Original prompt:
%s

Generation mode: %s
Error: %s
Reference image count: %d

Return strict JSON with this shape:
{
  "reason": "short Chinese diagnosis",
  "revisedPrompt": "safe rewritten Chinese prompt preserving the core intent",
  "referencePrompt": "prompt for generating a new safe reference image, or empty string if no new reference is needed",
  "omitOriginalReferences": true,
  "notes": ["short Chinese note"]
}

Rules:
- If reference images likely caused the rejection, set omitOriginalReferences=true and do not rely on them for retry.
- Preserve scene, composition, lighting, mood, and story intent.
- Convert risky clothing, gaze, pose, violence, or identity details into safe positive cinematic wording.
- In revisedPrompt, avoid sensitive negative terms; describe only the safe replacement visuals.`, strings.TrimSpace(req.Prompt), strings.TrimSpace(req.Mode), strings.TrimSpace(req.Error), imageCount)
}

func extractResponsesText(body []byte) string {
	var payload struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if strings.TrimSpace(payload.OutputText) != "" {
		return strings.TrimSpace(payload.OutputText)
	}
	var parts []string
	for _, output := range payload.Output {
		for _, content := range output.Content {
			if strings.TrimSpace(content.Text) != "" {
				parts = append(parts, strings.TrimSpace(content.Text))
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func parseImageRejectionDiagnosticJSON(text string) (imageRejectionDiagnosticResponse, error) {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		trimmed = trimmed[start : end+1]
	}
	var result imageRejectionDiagnosticResponse
	if err := json.Unmarshal([]byte(trimmed), &result); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Server) generateDiagnosticReferenceImage(ctx context.Context, r *http.Request, cfg config.ExternalResponsesConfig, prompt, size, quality string) []imageRejectionDiagnosticReference {
	client := newExternalResponsesClient(cfg)
	client.SetInstructions("Generate a safe non-explicit reference image for a retry. Avoid risky clothing, poses, and suggestive framing.")
	results, err := client.GenerateImage(ctx, prompt, "", 1, firstNonEmpty(size, "1024x1024"), firstNonEmpty(quality, "medium"), "")
	if err != nil || len(results) == 0 {
		return nil
	}
	items := buildImageResponse(r, client, results[:1], "url", "", s.cfg.ResolvePath(s.cfg.Storage.ImageDir))
	refs := make([]imageRejectionDiagnosticReference, 0, len(items))
	for index, item := range items {
		url := stringValue(item["url"])
		b64 := stringValue(item["b64_json"])
		if url == "" && b64 == "" {
			continue
		}
		ref := imageRejectionDiagnosticReference{
			ID:   firstNonEmpty(stringValue(item["id"]), fmt.Sprintf("diagnostic-reference-%d", index)),
			Name: "diagnostic-reference.png",
			URL:  url,
		}
		if b64 != "" {
			ref.DataURL = "data:image/png;base64," + b64
		}
		refs = append(refs, ref)
	}
	return refs
}
