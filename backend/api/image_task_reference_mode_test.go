package api

import (
	"context"
	"testing"

	"chatgpt2api/internal/config"
)

func TestNormalizeCreateImageTaskRequestMovesGenerateSourceImagesToReferences(t *testing.T) {
	server := &Server{}
	req := server.normalizeCreateImageTaskRequest(context.Background(), createImageTaskRequest{
		Mode: "generate",
		SourceImages: []imageTaskSourceImagePayload{
			{ID: "source-1", Role: "image", Name: "a.png", URL: "https://example.com/a.png"},
		},
	})
	if req.Mode != "generate" {
		t.Fatalf("Mode = %q, want generate", req.Mode)
	}
	if len(req.SourceImages) != 0 {
		t.Fatalf("SourceImages len = %d, want 0", len(req.SourceImages))
	}
	if len(req.ReferenceImages) != 1 || req.ReferenceImages[0].URL != "https://example.com/a.png" {
		t.Fatalf("ReferenceImages = %#v, want moved source image", req.ReferenceImages)
	}
}

func TestNormalizeCreateImageTaskRequestSwitchesMultiImageEditToGenerate(t *testing.T) {
	server := &Server{}
	req := server.normalizeCreateImageTaskRequest(context.Background(), createImageTaskRequest{
		Mode: "edit",
		SourceImages: []imageTaskSourceImagePayload{
			{ID: "source-1", Role: "image", Name: "a.png", URL: "https://example.com/a.png"},
			{ID: "source-2", Role: "image", Name: "b.png", URL: "https://example.com/b.png"},
			{ID: "mask-1", Role: "mask", Name: "mask.png", URL: "https://example.com/mask.png"},
		},
	})
	if req.Mode != "generate" {
		t.Fatalf("Mode = %q, want generate", req.Mode)
	}
	if len(req.SourceImages) != 0 {
		t.Fatalf("SourceImages len = %d, want 0", len(req.SourceImages))
	}
	if len(req.ReferenceImages) != 2 {
		t.Fatalf("ReferenceImages len = %d, want 2", len(req.ReferenceImages))
	}
}

func TestNormalizeCreateImageTaskRequestKeepsSingleImageEdit(t *testing.T) {
	server := &Server{}
	req := server.normalizeCreateImageTaskRequest(context.Background(), createImageTaskRequest{
		Mode: "edit",
		SourceImages: []imageTaskSourceImagePayload{
			{ID: "source-1", Role: "image", Name: "a.png", URL: "https://example.com/a.png"},
		},
	})
	if req.Mode != "edit" {
		t.Fatalf("Mode = %q, want edit", req.Mode)
	}
	if len(req.SourceImages) != 1 || len(req.ReferenceImages) != 0 {
		t.Fatalf("SourceImages = %#v, ReferenceImages = %#v, want single edit source only", req.SourceImages, req.ReferenceImages)
	}
}

func TestNewTaskRejectsMoreThanMaxReferenceImages(t *testing.T) {
	server := &Server{cfg: config.New(t.TempDir())}
	server.cfg.ChatGPT.MaxReferenceImages = 2
	manager := &imageTaskManager{server: server}
	references := make([]imageTaskReferenceImagePayload, 0, 3)
	for index := 0; index < 3; index++ {
		references = append(references, imageTaskReferenceImagePayload{
			ID:  "reference",
			URL: "https://example.com/reference.png",
		})
	}

	_, err := manager.newTask(createImageTaskRequest{
		Mode:            "generate",
		Prompt:          "test",
		ReferenceImages: references,
	})
	if err == nil {
		t.Fatal("newTask() error = nil, want too_many_reference_images")
	}
	if code := requestErrorCode(err); code != "too_many_reference_images" {
		t.Fatalf("requestErrorCode() = %q, want too_many_reference_images", code)
	}
	if got := err.Error(); got != "参考图最多只能使用 2 张" {
		t.Fatalf("error = %q, want configured reference image limit", got)
	}
}
