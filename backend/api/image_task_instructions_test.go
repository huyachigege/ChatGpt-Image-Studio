package api

import "testing"

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
