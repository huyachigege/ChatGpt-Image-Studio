package api

import (
	"context"
	"testing"

	"chatgpt2api/internal/config"
	"chatgpt2api/internal/imagehistory"
)

func TestImageConversationPromptMetadataCacheAddsIncrementalConversation(t *testing.T) {
	cfg := config.New(t.TempDir())
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	cfg.Storage.ImageConversationStorage = "server"
	server := NewServer(cfg, nil, nil)
	t.Cleanup(func() { server.Close() })

	if metadata := server.imageConversationPromptMetadata(context.Background(), "user-1"); len(metadata) != 0 {
		t.Fatalf("initial metadata len = %d, want 0", len(metadata))
	}

	server.addImageConversationPromptMetadataCache("user-1", imagehistory.Conversation{
		ID:     "conversation-1",
		UserID: "user-1",
		Prompt: "cached prompt",
		Images: []imagehistory.Image{{URL: "/v1/files/image/user-1/result-1.png"}},
	})

	metadata := server.imageConversationPromptMetadata(context.Background(), "user-1")
	match, ok := lookupImageGalleryPromptMetadata(metadata, imageGalleryItem{
		Name: "user-1/result-1.png",
		URL:  "/v1/files/image/user-1/result-1.png",
	})
	if !ok {
		t.Fatal("cached metadata did not include incrementally added conversation")
	}
	if match.Prompt != "cached prompt" || match.ConversationID != "conversation-1" {
		t.Fatalf("cached metadata = %+v, want prompt and conversation id from incremental conversation", match)
	}
}

func TestLookupImageGalleryPromptMetadataMatchesLegacyURLForms(t *testing.T) {
	metadata := imageGalleryPromptMetadata{Prompt: "a prompt", ConversationID: "conversation-1", TurnID: "turn-1"}
	metadataByName := map[string]imageGalleryPromptMetadata{}
	addImageGalleryPromptMetadata(metadataByName, "http://127.0.0.1:8080/v1/files/image/user-1/result-abc.png?download=1", metadata)

	match, ok := lookupImageGalleryPromptMetadata(metadataByName, imageGalleryItem{
		Name: "user-1/result-abc.png",
		URL:  "/v1/files/image/user-1/result-abc.png",
	})
	if !ok {
		t.Fatal("lookupImageGalleryPromptMetadata() did not match scoped gallery item")
	}
	if match != metadata {
		t.Fatalf("lookupImageGalleryPromptMetadata() = %+v, want %+v", match, metadata)
	}

	match, ok = lookupImageGalleryPromptMetadata(metadataByName, imageGalleryItem{
		Name: "result-abc.png",
		URL:  "/v1/files/image/result-abc.png",
	})
	if !ok {
		t.Fatal("lookupImageGalleryPromptMetadata() did not match basename-only gallery item")
	}
	if match != metadata {
		t.Fatalf("lookupImageGalleryPromptMetadata() = %+v, want %+v", match, metadata)
	}
}
