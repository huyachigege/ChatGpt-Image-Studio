package api

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestPreferredRouteForFreeGenerateUsesResponsesAfterDeferredRetry(t *testing.T) {
	manager := &imageTaskManager{server: &Server{}}
	task := &imageTask{
		Mode:  "generate",
		Units: []imageTaskUnit{{DeferredCount: 1}},
	}

	if got := manager.preferredRouteForTask(task, 0); got != "responses" {
		t.Fatalf("preferredRouteForTask() = %q, want responses", got)
	}
}

func TestPreferredRouteForFreeReferenceGenerateKeepsResponsesBeforeDeferredRetry(t *testing.T) {
	manager := &imageTaskManager{server: &Server{}}
	task := &imageTask{
		Mode:            "generate",
		ReferenceImages: []imageTaskSourceImage{{ID: "reference-1"}},
		Units:           []imageTaskUnit{{}},
	}

	if got := manager.preferredRouteForTask(task, 0); got != "responses" {
		t.Fatalf("preferredRouteForTask() = %q, want responses", got)
	}
}

func TestPreferredRouteForFreeReferenceGenerateKeepsResponsesAfterDeferredRetry(t *testing.T) {
	manager := &imageTaskManager{server: &Server{}}
	task := &imageTask{
		Mode:            "generate",
		ReferenceImages: []imageTaskSourceImage{{ID: "reference-1"}},
		Units:           []imageTaskUnit{{DeferredCount: 1}},
	}

	if got := manager.preferredRouteForTask(task, 0); got != "responses" {
		t.Fatalf("preferredRouteForTask() = %q, want responses", got)
	}
}

func TestPreferredRouteForPaidGenerateKeepsResponsesAfterDeferredRetry(t *testing.T) {
	manager := &imageTaskManager{server: &Server{}}
	task := &imageTask{
		Mode:        "generate",
		Requirement: imageTaskRequirement{NeedPaid: true},
		Units:       []imageTaskUnit{{DeferredCount: 1}},
	}

	if got := manager.preferredRouteForTask(task, 0); got != "responses" {
		t.Fatalf("preferredRouteForTask() = %q, want responses", got)
	}
}

func TestNormalizeTaskEditMaskToFirstImageResizesMismatch(t *testing.T) {
	mask := testPNGImageBytes(t, 1, 1, color.NRGBA{A: 255})
	source := testPNGImageBytes(t, 3, 2, color.NRGBA{R: 255, A: 255})

	resized, err := normalizeTaskEditMaskToFirstImage(mask, [][]byte{source})
	if err != nil {
		t.Fatalf("normalizeTaskEditMaskToFirstImage() returned error: %v", err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("DecodeConfig(resized) returned error: %v", err)
	}
	if config.Width != 3 || config.Height != 2 {
		t.Fatalf("resized mask size = %dx%d, want 3x2", config.Width, config.Height)
	}
}

func testPNGImageBytes(t *testing.T, width, height int, fill color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return buffer.Bytes()
}

func TestBuildImageTaskInstructionsUsesCommonHintInNormalMode(t *testing.T) {
	got := buildImageTaskInstructions(&imageTask{
		CommonSystemHint:  " common hint ",
		PrivateSystemHint: " private hint ",
	})
	want := "common hint"
	if got != want {
		t.Fatalf("buildImageTaskInstructions() = %q, want %q", got, want)
	}
}

func TestBuildImageTaskInstructionsUsesPrivateHintInPrivateMode(t *testing.T) {
	got := buildImageTaskInstructions(&imageTask{
		PrivatePhotoMode:  true,
		CommonSystemHint:  "common hint",
		PrivateSystemHint: " private hint ",
	})
	want := "private hint"
	if got != want {
		t.Fatalf("buildImageTaskInstructions() = %q, want %q", got, want)
	}
}

func TestBuildImageTaskInstructionsFallsBackToLegacySystemHintInPrivateMode(t *testing.T) {
	got := buildImageTaskInstructions(&imageTask{
		PrivatePhotoMode: true,
		CommonSystemHint: "common hint",
		SystemHint:       " legacy private hint ",
	})
	want := "legacy private hint"
	if got != want {
		t.Fatalf("buildImageTaskInstructions() = %q, want %q", got, want)
	}
}
