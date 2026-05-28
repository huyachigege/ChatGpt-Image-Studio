package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestDeleteImageGalleryBeforeDeletesOnlyOldVisibleItems(t *testing.T) {
	cfg := config.New(t.TempDir())
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	server := NewServer(cfg, nil, nil)
	t.Cleanup(func() { server.Close() })

	root := cfg.ResolvePath(cfg.Storage.ImageDir)
	oldPath := writeGalleryTestPNG(t, filepath.Join(root, "user-1", "old.png"))
	newPath := writeGalleryTestPNG(t, filepath.Join(root, "user-1", "new.png"))
	otherPath := writeGalleryTestPNG(t, filepath.Join(root, "user-2", "old-other.png"))
	oldTime := time.Now().AddDate(0, -2, 0)
	newTime := time.Now().AddDate(0, 0, -7)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatalf("chtimes new: %v", err)
	}
	if err := os.Chtimes(otherPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes other: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/image/gallery/delete-before", bytes.NewReader([]byte(`{"months":1}`)))
	req = req.WithContext(context.WithValue(req.Context(), authIdentityContextKey{}, authIdentity{UserID: "user-1", Username: "user-1"}))
	rec := httptest.NewRecorder()
	server.handleDeleteImageGalleryBefore(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		DeletedCount int      `json:"deletedCount"`
		Deleted      []string `json:"deleted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.DeletedCount != 1 || len(payload.Deleted) != 1 || payload.Deleted[0] != "user-1/old.png" {
		t.Fatalf("payload = %+v, want only user-1/old.png deleted", payload)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old image still exists or stat error = %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new image stat error = %v", err)
	}
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatalf("other user image stat error = %v", err)
	}
}

func TestDeleteImageGalleryBeforeRejectsInvalidMonths(t *testing.T) {
	cfg := config.New(t.TempDir())
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	server := NewServer(cfg, nil, nil)
	t.Cleanup(func() { server.Close() })

	req := httptest.NewRequest(http.MethodPost, "/api/image/gallery/delete-before", bytes.NewReader([]byte(`{"months":2}`)))
	req = req.WithContext(context.WithValue(req.Context(), authIdentityContextKey{}, authIdentity{UserID: "user-1", Username: "user-1"}))
	rec := httptest.NewRecorder()
	server.handleDeleteImageGalleryBefore(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
}

func writeGalleryTestPNG(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir image dir: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer file.Close()
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return path
}
