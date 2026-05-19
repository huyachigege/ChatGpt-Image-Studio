package api

import "testing"

func TestBuildImageTaskInstructionsCombinesCommonAndPrivateHints(t *testing.T) {
	got := buildImageTaskInstructions(&imageTask{
		CommonSystemHint:  " common hint ",
		PrivateSystemHint: " private hint ",
	})
	want := "common hint\n\nprivate hint"
	if got != want {
		t.Fatalf("buildImageTaskInstructions() = %q, want %q", got, want)
	}
}

func TestBuildImageTaskInstructionsFallsBackToLegacySystemHint(t *testing.T) {
	got := buildImageTaskInstructions(&imageTask{
		CommonSystemHint: "common hint",
		SystemHint:       "legacy private hint",
	})
	want := "common hint\n\nlegacy private hint"
	if got != want {
		t.Fatalf("buildImageTaskInstructions() = %q, want %q", got, want)
	}
}
