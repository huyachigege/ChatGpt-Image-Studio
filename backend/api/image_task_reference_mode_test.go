package api

import (
	"context"
	"testing"
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
