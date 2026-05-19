package api

import "testing"

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
