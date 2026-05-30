package api

import (
	"strings"
	"testing"
)

func TestBuildResponsesInputItemsFromConversationUsesCurrentPromptAsAuthority(t *testing.T) {
	encoded := encodeImageConversationInput([]imageConversationInputItem{
		{
			Role: "user",
			Content: []imageConversationInputContent{
				{Type: "input_text", Text: "1. 生成男性魔尊，玄黑血红服装"},
				{Type: "input_image", ImageURL: "https://example.com/old.png", Detail: "high"},
			},
		},
		{
			Role: "assistant",
			Content: []imageConversationInputContent{
				{Type: "output_text", Text: "上游修订提示: 男性魔尊"},
			},
		},
		{
			Role: "user",
			Content: []imageConversationInputContent{
				{Type: "input_text", Text: "3. 生成女性虫族女王，银蓝绿色生物甲壳"},
			},
		},
	})

	payload := buildResponsesInputItemsFromPrompt(encoded, nil)
	if len(payload) != 1 {
		t.Fatalf("payload len = %d, want 1", len(payload))
	}
	message, ok := payload[0].(map[string]any)
	if !ok {
		t.Fatalf("payload[0] = %#v, want object", payload[0])
	}
	content, ok := message["content"].([]map[string]any)
	if !ok || len(content) != 2 {
		t.Fatalf("content = %#v, want text plus one image", message["content"])
	}
	text, _ := content[0]["text"].(string)
	if !strings.Contains(text, "当前最终生图任务") || !strings.Contains(text, "女性虫族女王") {
		t.Fatalf("text = %q, want current task marker and current prompt", text)
	}
	for _, forbidden := range []string{"男性魔尊", "玄黑血红", "上游修订提示"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("text = %q, should not include raw historical detail %q", text, forbidden)
		}
	}
	if got := content[1]["image_url"]; got != "https://example.com/old.png" {
		t.Fatalf("image_url = %v, want latest historical image url", got)
	}
}

func TestBuildResponsesInputItemsFromConversationManualImageSkipsHistoricalImage(t *testing.T) {
	encoded := encodeImageConversationInput([]imageConversationInputItem{
		{
			Role: "user",
			Content: []imageConversationInputContent{
				{Type: "input_text", Text: "1. 生成角色 A"},
				{Type: "input_image", ImageURL: "https://example.com/old.png", Detail: "high"},
			},
		},
		{
			Role: "user",
			Content: []imageConversationInputContent{
				{Type: "input_text", Text: "2. 生成角色 B"},
			},
		},
	})

	payload := buildResponsesInputItemsFromPrompt(encoded, []map[string]any{
		{"type": "input_image", "image_url": "https://example.com/manual.png", "detail": "high"},
	})
	message := payload[0].(map[string]any)
	content := message["content"].([]map[string]any)
	if len(content) != 2 {
		t.Fatalf("content len = %d, want text plus manual image only", len(content))
	}
	if got := content[1]["image_url"]; got != "https://example.com/manual.png" {
		t.Fatalf("image_url = %v, want manual image", got)
	}
}
