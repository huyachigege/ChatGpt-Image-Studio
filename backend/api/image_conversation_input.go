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
	if items := selectRecentImageConversationInputItems(decodeImageConversationInput(prompt), len(imageParts) > 0); len(items) > 0 {
		return imageConversationInputItemsToPayload(appendImagesToLastUserInput(items, imageParts))
	}
	content := []map[string]any{{"type": "input_text", "text": strings.TrimSpace(prompt)}}
	content = append(content, imageParts...)
	return []any{map[string]any{"role": "user", "content": content}}
}

func selectRecentImageConversationInputItems(items []imageConversationInputItem, hasManualImages bool) []imageConversationInputItem {
	items = normalizeImageConversationInput(items)
	if len(items) == 0 {
		return nil
	}

	lastUserIndex := -1
	previousUserIndex := -1
	for index := len(items) - 1; index >= 0; index-- {
		if !strings.EqualFold(strings.TrimSpace(items[index].Role), "user") {
			continue
		}
		if lastUserIndex < 0 {
			lastUserIndex = index
			continue
		}
		previousUserIndex = index
		break
	}
	if lastUserIndex < 0 {
		return nil
	}

	result := make([]imageConversationInputItem, 0, 2)
	if previousUserIndex >= 0 {
		previous := filterImageConversationInputItem(items[previousUserIndex], true)
		if len(previous.Content) > 0 {
			result = append(result, previous)
		}
	}
	current := filterImageConversationInputItem(items[lastUserIndex], hasManualImages)
	result = append(result, current)
	if !hasManualImages && !imageConversationInputItemsHaveImage(result) {
		for index := lastUserIndex; index >= 0; index-- {
			if imageURL := firstImageConversationInputURL(items[index]); imageURL != "" {
				result[len(result)-1].Content = append(result[len(result)-1].Content, imageConversationInputContent{Type: "input_image", ImageURL: imageURL, Detail: "high"})
				break
			}
		}
	}
	return normalizeImageConversationInput(result)
}

func filterImageConversationInputItem(item imageConversationInputItem, dropImages bool) imageConversationInputItem {
	filtered := imageConversationInputItem{Role: item.Role, Content: make([]imageConversationInputContent, 0, len(item.Content))}
	for _, part := range item.Content {
		if dropImages && part.Type == "input_image" {
			continue
		}
		filtered.Content = append(filtered.Content, part)
	}
	return filtered
}

func imageConversationInputItemsHaveImage(items []imageConversationInputItem) bool {
	for _, item := range items {
		if firstImageConversationInputURL(item) != "" {
			return true
		}
	}
	return false
}

func firstImageConversationInputURL(item imageConversationInputItem) string {
	for _, part := range item.Content {
		if part.Type == "input_image" && strings.TrimSpace(part.ImageURL) != "" {
			return strings.TrimSpace(part.ImageURL)
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
