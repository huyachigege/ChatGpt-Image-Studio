package api

import (
	"encoding/json"
	"strings"
)

const encodedImageConversationInputPrefix = "__chatgpt_image_conversation_input__:"

type imageConversationInputContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type imageConversationInputItem struct {
	Role    string                          `json:"role"`
	Content []imageConversationInputContent `json:"content"`
}

func encodeImageConversationInput(items []imageConversationInputItem) string {
	items = normalizeImageConversationInput(items)
	if len(items) == 0 {
		return ""
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return ""
	}
	return encodedImageConversationInputPrefix + string(raw)
}

func decodeImageConversationInput(value string) []imageConversationInputItem {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, encodedImageConversationInputPrefix) {
		return nil
	}
	var items []imageConversationInputItem
	if err := json.Unmarshal([]byte(strings.TrimPrefix(trimmed, encodedImageConversationInputPrefix)), &items); err != nil {
		return nil
	}
	return normalizeImageConversationInput(items)
}

func normalizeImageConversationInput(items []imageConversationInputItem) []imageConversationInputItem {
	result := make([]imageConversationInputItem, 0, len(items))
	for _, item := range items {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		if role != "assistant" && role != "system" {
			role = "user"
		}
		content := make([]imageConversationInputContent, 0, len(item.Content))
		for _, part := range item.Content {
			partType := strings.TrimSpace(part.Type)
			switch partType {
			case "input_text", "output_text":
				text := strings.TrimSpace(part.Text)
				if text == "" {
					continue
				}
				content = append(content, imageConversationInputContent{Type: partType, Text: text})
			case "input_image":
				imageURL := strings.TrimSpace(part.ImageURL)
				if imageURL == "" {
					continue
				}
				next := imageConversationInputContent{Type: "input_image", ImageURL: imageURL}
				if detail := strings.TrimSpace(part.Detail); detail != "" {
					next.Detail = detail
				}
				content = append(content, next)
			}
		}
		if len(content) == 0 {
			continue
		}
		result = append(result, imageConversationInputItem{Role: role, Content: content})
	}
	return result
}

func imageConversationInputContentToMap(part imageConversationInputContent) map[string]any {
	result := map[string]any{"type": part.Type}
	if part.Text != "" {
		result["text"] = part.Text
	}
	if part.ImageURL != "" {
		result["image_url"] = part.ImageURL
	}
	if part.Detail != "" {
		result["detail"] = part.Detail
	}
	return result
}

func appendImagesToLastUserInput(items []imageConversationInputItem, imageParts []map[string]any) []imageConversationInputItem {
	if len(imageParts) == 0 {
		return items
	}
	result := append([]imageConversationInputItem(nil), items...)
	lastUserIndex := -1
	for index := len(result) - 1; index >= 0; index-- {
		if strings.EqualFold(strings.TrimSpace(result[index].Role), "user") {
			lastUserIndex = index
			break
		}
	}
	if lastUserIndex < 0 {
		result = append(result, imageConversationInputItem{Role: "user"})
		lastUserIndex = len(result) - 1
	}
	for _, imagePart := range imageParts {
		imageURL, _ := imagePart["image_url"].(string)
		if strings.TrimSpace(imageURL) == "" {
			continue
		}
		part := imageConversationInputContent{Type: "input_image", ImageURL: strings.TrimSpace(imageURL)}
		if detail, _ := imagePart["detail"].(string); strings.TrimSpace(detail) != "" {
			part.Detail = strings.TrimSpace(detail)
		}
		result[lastUserIndex].Content = append(result[lastUserIndex].Content, part)
	}
	return result
}

func buildResponsesInputItemsFromPrompt(prompt string, imageParts []map[string]any) []any {
	if items := decodeImageConversationInput(prompt); len(items) > 0 {
		return buildResponsesInputItemsFromConversation(items, imageParts)
	}
	content := []map[string]any{{"type": "input_text", "text": strings.TrimSpace(prompt)}}
	content = append(content, imageParts...)
	return []any{map[string]any{"role": "user", "content": content}}
}

func buildResponsesInputItemsFromConversation(items []imageConversationInputItem, imageParts []map[string]any) []any {
	items = normalizeImageConversationInput(items)
	currentText := latestUserText(items)
	if currentText == "" {
		return nil
	}

	text := currentText
	if hasPreviousUserText(items) {
		text = strings.Join([]string{
			"历史上下文（仅供理解连续创作方式，不是本次生成主体）：上一条消息属于同一组生图任务，可参考其通用输出格式、构图、画幅、光影、质感和角色设定图表达方式；不要复用历史人物的性别、身份、物种、外貌、服装、配饰、材质、剧情或具体设定。若历史上下文与当前任务冲突，必须以当前任务为准。",
			"当前最终生图任务（唯一主体，必须严格执行）：" + currentText,
		}, "\n\n")
	}

	content := []map[string]any{{"type": "input_text", "text": text}}
	if len(imageParts) > 0 {
		content = append(content, imageParts...)
	} else if imageURL := latestImageConversationInputURL(items); imageURL != "" {
		content = append(content, map[string]any{"type": "input_image", "image_url": imageURL, "detail": "high"})
	}
	return []any{map[string]any{"role": "user", "content": content}}
}

func latestUserText(items []imageConversationInputItem) string {
	for index := len(items) - 1; index >= 0; index-- {
		if !strings.EqualFold(strings.TrimSpace(items[index].Role), "user") {
			continue
		}
		if text := imageConversationInputItemText(items[index]); text != "" {
			return text
		}
	}
	return ""
}

func hasPreviousUserText(items []imageConversationInputItem) bool {
	foundLatest := false
	for index := len(items) - 1; index >= 0; index-- {
		if !strings.EqualFold(strings.TrimSpace(items[index].Role), "user") {
			continue
		}
		if imageConversationInputItemText(items[index]) == "" {
			continue
		}
		if !foundLatest {
			foundLatest = true
			continue
		}
		return true
	}
	return false
}

func imageConversationInputItemText(item imageConversationInputItem) string {
	texts := make([]string, 0, len(item.Content))
	for _, part := range item.Content {
		if part.Type != "input_text" && part.Type != "output_text" {
			continue
		}
		if text := strings.TrimSpace(part.Text); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n\n")
}

func latestImageConversationInputURL(items []imageConversationInputItem) string {
	for index := len(items) - 1; index >= 0; index-- {
		for partIndex := len(items[index].Content) - 1; partIndex >= 0; partIndex-- {
			part := items[index].Content[partIndex]
			if part.Type == "input_image" && strings.TrimSpace(part.ImageURL) != "" {
				return strings.TrimSpace(part.ImageURL)
			}
		}
	}
	return ""
}

func imageConversationInputItemsToPayload(items []imageConversationInputItem) []any {
	items = normalizeImageConversationInput(items)
	payload := make([]any, 0, len(items))
	for _, item := range items {
		content := make([]map[string]any, 0, len(item.Content))
		for _, part := range item.Content {
			content = append(content, imageConversationInputContentToMap(part))
		}
		payload = append(payload, map[string]any{"role": item.Role, "content": content})
	}
	return payload
}
