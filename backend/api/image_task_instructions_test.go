package api

import "testing"

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
