package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"chatgpt2api/handler"
	"chatgpt2api/internal/users"
)

func TestCreateImageTaskRunsToSuccessForWorkspaceUser(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	userStore, err := users.NewStore(server.cfg)
	if err != nil {
		t.Fatalf("new user store: %v", err)
	}
	defer userStore.Close()
	invite, err := userStore.CreateInvite(context.Background(), "admin")
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	user, err := userStore.RegisterWithInvite(context.Background(), "alice", "secret123", "Alice", invite.Code)
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	token, err := userStore.CreateSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/image/tasks", strings.NewReader(`{
		"conversationId":"conv-user-task-1",
		"turnId":"turn-user-task-1",
		"mode":"generate",
		"prompt":"draw a cat for user",
		"model":"gpt-image-2",
		"count":1,
		"size":"1248x1248",
		"quality":"high"
	}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Task struct {
			ID     string `json:"id"`
			UserID string `json:"userId"`
			Status string `json:"status"`
		} `json:"task"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Task.UserID != user.ID {
		t.Fatalf("task user id = %q, want %q", payload.Task.UserID, user.ID)
	}

	waitForTaskStatus(t, server, payload.Task.ID, imageTaskStatusSucceeded)
	if recorder.officialCalls != 1 {
		t.Fatalf("officialCalls = %d, want 1", recorder.officialCalls)
	}
}

func TestCreateLegacyImageTaskRunsMultipleImagesSeriallyForSameUser(t *testing.T) {
	started := make(chan string, 4)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()

	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{fileName: "parallel-1.json", accessToken: "token-parallel-1", accountType: "Free", priority: 0},
			{fileName: "parallel-2.json", accessToken: "token-parallel-2", accountType: "Free", priority: 0},
			{fileName: "parallel-3.json", accessToken: "token-parallel-3", accountType: "Free", priority: 0},
			{fileName: "parallel-4.json", accessToken: "token-parallel-4", accountType: "Free", priority: 0},
		},
		behavior: compatClientBehavior{
			generateStarted: started,
			generateRelease: release,
		},
	})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		UserID:         "user-serial-1",
		ConversationID: "conv-serial-1",
		TurnID:         "turn-serial-1",
		Mode:           "generate",
		Prompt:         "draw four cats",
		Model:          "gpt-image-2",
		Count:          4,
		Size:           "1248x1248",
		Quality:        "high",
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first image unit did not start")
	}
	select {
	case token := <-started:
		t.Fatalf("unexpected concurrent image unit start with token %q", token)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	released = true
	waitForTaskStatus(t, server, "turn-serial-1", imageTaskStatusSucceeded)
}

func TestCreateResponsesImageTaskRunsMultipleImagesConcurrentlyForSameUserOnBoundAccount(t *testing.T) {
	started := make(chan string, 4)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()

	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{fileName: "responses-bound-1.json", accessToken: "token-responses-bound-1", accountType: "Plus", priority: 20, quota: 5, status: "正常"},
			{fileName: "responses-bound-2.json", accessToken: "token-responses-bound-2", accountType: "Plus", priority: 10, quota: 5, status: "正常"},
		},
		behavior: compatClientBehavior{
			generateStarted: started,
			generateRelease: release,
		},
	})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		UserID:              "user-responses-bound",
		ConversationID:      "conv-responses-bound-a",
		TurnID:              "turn-responses-bound-a",
		Mode:                "generate",
		Prompt:              "draw concurrent responses cats",
		Model:               "gpt-image-2",
		Count:               3,
		Size:                "3840x2160",
		ResolutionAccess:    "paid",
		Quality:             "high",
		ConversationContext: "context a",
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	received := make([]string, 0, 3)
	deadline := time.After(2 * time.Second)
	for len(received) < 3 {
		select {
		case token := <-started:
			received = append(received, token)
		case <-deadline:
			t.Fatalf("started tokens = %v, want 3 concurrent responses starts", received)
		}
	}
	for _, token := range received {
		if token != received[0] {
			t.Fatalf("started tokens = %v, want same bound account", received)
		}
	}
	recorder.mu.Lock()
	recorderSessions := append([]string(nil), recorder.sessionIDs...)
	recorder.mu.Unlock()
	if len(recorderSessions) != 3 {
		t.Fatalf("responses session ids = %v, want 3", recorderSessions)
	}
	seenSessions := map[string]struct{}{}
	for _, sessionID := range recorderSessions {
		seenSessions[sessionID] = struct{}{}
	}
	if len(seenSessions) != 3 {
		t.Fatalf("responses session ids = %v, want unique session per concurrent unit", recorderSessions)
	}

	close(release)
	released = true
	waitForTaskStatus(t, server, "turn-responses-bound-a", imageTaskStatusSucceeded)
}

func TestCreateResponsesImageTasksUseSameBoundAccountAcrossConversations(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()

	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{fileName: "responses-cross-conv-1.json", accessToken: "token-responses-cross-conv-1", accountType: "Plus", priority: 20, quota: 5, status: "正常"},
			{fileName: "responses-cross-conv-2.json", accessToken: "token-responses-cross-conv-2", accountType: "Plus", priority: 10, quota: 5, status: "正常"},
		},
		behavior: compatClientBehavior{
			generateStarted: started,
			generateRelease: release,
		},
	})

	for _, req := range []createImageTaskRequest{
		{UserID: "user-responses-cross-conv", ConversationID: "conv-responses-cross-conv-a", TurnID: "turn-responses-cross-conv-a", Mode: "generate", Prompt: "draw cat a", Model: "gpt-image-2", Count: 1, Size: "3840x2160", ResolutionAccess: "paid", Quality: "high", ConversationContext: "context a"},
		{UserID: "user-responses-cross-conv", ConversationID: "conv-responses-cross-conv-b", TurnID: "turn-responses-cross-conv-b", Mode: "generate", Prompt: "draw cat b", Model: "gpt-image-2", Count: 1, Size: "3840x2160", ResolutionAccess: "paid", Quality: "high", ConversationContext: "context b"},
	} {
		if _, err := server.imageTasks.createTask(req); err != nil {
			t.Fatalf("createTask() returned error: %v", err)
		}
	}

	received := make([]string, 0, 2)
	deadline := time.After(2 * time.Second)
	for len(received) < 2 {
		select {
		case token := <-started:
			received = append(received, token)
		case <-deadline:
			t.Fatalf("started tokens = %v, want 2 concurrent responses starts", received)
		}
	}
	if received[0] != received[1] {
		t.Fatalf("started tokens = %v, want same bound account across conversations", received)
	}

	close(release)
	released = true
	waitForTaskStatus(t, server, "turn-responses-cross-conv-a", imageTaskStatusSucceeded)
	waitForTaskStatus(t, server, "turn-responses-cross-conv-b", imageTaskStatusSucceeded)
}

func TestCreateImageTasksRunConcurrentlyForDifferentUsers(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()

	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{fileName: "different-user-1.json", accessToken: "token-different-user-1", accountType: "Free", priority: 20, quota: 5, status: "正常"},
			{fileName: "different-user-2.json", accessToken: "token-different-user-2", accountType: "Free", priority: 10, quota: 5, status: "正常"},
		},
		behavior: compatClientBehavior{
			generateStarted: started,
			generateRelease: release,
		},
	})

	for _, req := range []createImageTaskRequest{
		{UserID: "user-a", ConversationID: "conv-user-a", TurnID: "turn-user-a", Mode: "generate", Prompt: "draw cat a", Model: "gpt-image-2", Count: 1, Size: "1248x1248", Quality: "high"},
		{UserID: "user-b", ConversationID: "conv-user-b", TurnID: "turn-user-b", Mode: "generate", Prompt: "draw cat b", Model: "gpt-image-2", Count: 1, Size: "1248x1248", Quality: "high"},
	} {
		if _, err := server.imageTasks.createTask(req); err != nil {
			t.Fatalf("createTask() returned error: %v", err)
		}
	}

	received := make([]string, 0, 2)
	deadline := time.After(2 * time.Second)
	for len(received) < 2 {
		select {
		case token := <-started:
			received = append(received, token)
		case <-deadline:
			t.Fatalf("started tokens = %v, want 2 concurrent starts for different users", received)
		}
	}

	close(release)
	released = true
	waitForTaskStatus(t, server, "turn-user-a", imageTaskStatusSucceeded)
	waitForTaskStatus(t, server, "turn-user-b", imageTaskStatusSucceeded)
}

func TestCreateResponsesGenerateAndEditTasksRunConcurrentlyForSameUserOnBoundAccount(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()

	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{fileName: "same-user-generate.json", accessToken: "token-same-user-generate", accountType: "Plus", priority: 20, quota: 5, status: "正常"},
			{fileName: "same-user-edit.json", accessToken: "token-same-user-edit", accountType: "Plus", priority: 10, quota: 5, status: "正常"},
		},
		behavior: compatClientBehavior{
			generateStarted: started,
			generateRelease: release,
		},
	})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		UserID:           "user-generate-edit",
		ConversationID:   "conv-generate-edit",
		TurnID:           "turn-generate-edit-generate",
		Mode:             "generate",
		Prompt:           "draw cat before edit",
		Model:            "gpt-image-2",
		Count:            1,
		Size:             "3840x2160",
		ResolutionAccess: "paid",
		Quality:          "high",
	}); err != nil {
		t.Fatalf("create generate task: %v", err)
	}
	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		UserID:         "user-generate-edit",
		ConversationID: "conv-generate-edit",
		TurnID:         "turn-generate-edit-edit",
		Mode:           "edit",
		Prompt:         "edit cat after generate",
		Model:          "gpt-image-2",
		Count:          1,
		Size:           "1248x1248",
		Quality:        "high",
		SourceImages: []imageTaskSourceImagePayload{{
			ID:      "source-edit-1",
			Role:    "image",
			Name:    "source.png",
			DataURL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC",
		}},
	}); err != nil {
		t.Fatalf("create edit task: %v", err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("generate task did not start")
	}
	received := []string{}
	select {
	case token := <-started:
		received = append(received, token)
	case <-time.After(2 * time.Second):
		t.Fatal("edit task did not start concurrently")
	}
	select {
	case token := <-started:
		received = append(received, token)
	default:
	}
	if len(received) == 0 || received[0] != "token-same-user-generate" {
		t.Fatalf("started tokens = %v, want edit to use same bound account", received)
	}

	close(release)
	released = true
	waitForTaskStatus(t, server, "turn-generate-edit-generate", imageTaskStatusSucceeded)
	waitForTaskStatus(t, server, "turn-generate-edit-edit", imageTaskStatusSucceeded)
}

func TestCreateImageTaskRunsToSuccess(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	req := httptest.NewRequest(http.MethodPost, "/api/image/tasks", strings.NewReader(`{
		"conversationId":"conv-task-1",
		"turnId":"turn-task-1",
		"mode":"generate",
		"prompt":"draw a cat",
		"model":"gpt-image-2",
		"count":1,
		"size":"1248x1248",
		"quality":"high"
	}`))
	req.Header.Set("Authorization", "Bearer "+server.cfg.App.AuthKey)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Task struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"task"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Task.ID != "turn-task-1" {
		t.Fatalf("task id = %q, want turn-task-1", payload.Task.ID)
	}

	waitForTaskStatus(t, server, payload.Task.ID, imageTaskStatusSucceeded)

	task, _, err := server.imageTasks.getTask(payload.Task.ID)
	if err != nil {
		t.Fatalf("getTask() returned error: %v", err)
	}
	if len(task.Images) != 1 {
		t.Fatalf("task images = %d, want 1", len(task.Images))
	}
	if task.Images[0].URL == "" {
		t.Fatalf("task image url = empty, want cached file url")
	}
	if !strings.HasPrefix(task.Images[0].URL, "/v1/files/image/") {
		t.Fatalf("task image url = %q, want relative cached file url", task.Images[0].URL)
	}
	if recorder.officialCalls != 1 {
		t.Fatalf("officialCalls = %d, want 1", recorder.officialCalls)
	}
}

func TestCreateImageTaskSelectionEditBypassesPolicySnapshot(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	accountID := compatPrimaryAccountID(t, server)
	req := httptest.NewRequest(http.MethodPost, "/api/image/tasks", strings.NewReader(`{
		"conversationId":"conv-selection-1",
		"turnId":"turn-selection-1",
		"mode":"edit",
		"prompt":"selection edit",
		"model":"gpt-image-2",
		"count":1,
		"sourceImages":[
			{
				"id":"mask-1",
				"role":"mask",
				"name":"mask.png",
				"dataUrl":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC"
			}
		],
		"sourceReference":{
			"original_file_id":"file-1",
			"original_gen_id":"gen-1",
			"conversation_id":"conv-1",
			"parent_message_id":"msg-1",
			"source_account_id":"`+accountID+`"
		},
		"policy":{
			"enabled":true,
			"sortMode":"imported_at",
			"groupSize":10,
			"enabledGroupIndexes":[999],
			"reserveMode":"daily_first_seen_percent",
			"reservePercent":20
		}
	}`))
	req.Header.Set("Authorization", "Bearer "+server.cfg.App.AuthKey)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	waitForTaskStatus(t, server, payload.Task.ID, imageTaskStatusSucceeded)
	if recorder.officialCalls != 1 {
		t.Fatalf("officialCalls = %d, want 1 source-bound official call", recorder.officialCalls)
	}
	if len(recorder.callSequence) != 1 {
		t.Fatalf("callSequence = %#v, want 1 entry", recorder.callSequence)
	}
	if !strings.Contains(recorder.callSequence[0], ":selection-edit") {
		t.Fatalf("callSequence[0] = %q, want selection-edit operation", recorder.callSequence[0])
	}
}

func TestCreateImageTaskSourceAccountRateLimitFallsBack(t *testing.T) {
	oldBackoffBase := imageTaskRetryBackoffBase
	oldBackoffMax := imageTaskRetryBackoffMax
	imageTaskRetryBackoffBase = 20 * time.Millisecond
	imageTaskRetryBackoffMax = 20 * time.Millisecond
	t.Cleanup(func() {
		imageTaskRetryBackoffBase = oldBackoffBase
		imageTaskRetryBackoffMax = oldBackoffMax
	})

	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{
				fileName:    "source-limited.json",
				accessToken: "token-source-limited",
				accountType: "Plus",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
			{
				fileName:    "fallback-healthy.json",
				accessToken: "token-fallback-healthy",
				accountType: "Plus",
				priority:    10,
				quota:       5,
				status:      "正常",
			},
		},
		behavior: compatClientBehavior{
			officialInpaintErrors: map[string]error{
				"token-source-limited": errors.New("chatgpt returned 429: rate limited"),
			},
		},
	})

	sourceAccount, err := server.getStore().GetAccountByToken("token-source-limited")
	if err != nil {
		t.Fatalf("GetAccountByToken(source) returned error: %v", err)
	}
	accountID := sourceAccount.ID
	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-source-fallback-1",
		TurnID:         "turn-source-fallback-1",
		Mode:           "edit",
		Prompt:         "selection edit fallback",
		Model:          "gpt-image-2",
		Count:          1,
		SourceImages: []imageTaskSourceImagePayload{
			{
				ID:      "mask-1",
				Role:    "mask",
				Name:    "mask.png",
				DataURL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC",
			},
		},
		SourceReference: &imageTaskSourceReferencePayload{
			OriginalFileID:  "file-1",
			OriginalGenID:   "gen-1",
			ConversationID:  "conv-1",
			ParentMessageID: "msg-1",
			SourceAccountID: accountID,
		},
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-source-fallback-1", imageTaskStatusSucceeded)
	if len(recorder.callSequence) < 2 {
		t.Fatalf("callSequence = %#v, want source then fallback attempt", recorder.callSequence)
	}
	if !strings.Contains(recorder.callSequence[0], "token-source-limited") {
		t.Fatalf("callSequence[0] = %q, want source account first", recorder.callSequence[0])
	}
	if !strings.Contains(recorder.callSequence[len(recorder.callSequence)-1], "token-fallback-healthy") {
		t.Fatalf("callSequence = %#v, want fallback healthy account", recorder.callSequence)
	}
}

func TestCreateImageTaskSelectionEditFallsBackToSourceMaskEdit(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "legacy",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		behavior: compatClientBehavior{
			officialInpaintErrors: map[string]error{
				"token-compat": errors.New("conversation_not_found"),
			},
		},
	})

	accountID := compatPrimaryAccountID(t, server)
	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-selection-fallback-1",
		TurnID:         "turn-selection-fallback-1",
		Mode:           "edit",
		Prompt:         "selection edit fallback",
		Model:          "gpt-image-2",
		Count:          1,
		SourceImages: []imageTaskSourceImagePayload{
			{
				ID:      "source-1",
				Role:    "image",
				Name:    "source.png",
				DataURL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC",
			},
			{
				ID:      "mask-1",
				Role:    "mask",
				Name:    "mask.png",
				DataURL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC",
			},
		},
		SourceReference: &imageTaskSourceReferencePayload{
			OriginalFileID:  "file-1",
			OriginalGenID:   "gen-1",
			ConversationID:  "conv-missing",
			ParentMessageID: "msg-1",
			SourceAccountID: accountID,
		},
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-selection-fallback-1", imageTaskStatusSucceeded)
	if len(recorder.callSequence) != 2 {
		t.Fatalf("callSequence = %#v, want selection edit then source-mask edit", recorder.callSequence)
	}
	if !strings.Contains(recorder.callSequence[0], ":selection-edit") || !strings.Contains(recorder.callSequence[1], ":edit") {
		t.Fatalf("callSequence = %#v, want selection edit fallback to edit", recorder.callSequence)
	}
}

func TestCreateImageTaskDoesNotSwitchAccountForGenericEditError(t *testing.T) {
	oldBackoffBase := imageTaskRetryBackoffBase
	oldBackoffMax := imageTaskRetryBackoffMax
	imageTaskRetryBackoffBase = 20 * time.Millisecond
	imageTaskRetryBackoffMax = 20 * time.Millisecond
	t.Cleanup(func() {
		imageTaskRetryBackoffBase = oldBackoffBase
		imageTaskRetryBackoffMax = oldBackoffMax
	})

	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{
				fileName:    "edit-transient.json",
				accessToken: "token-edit-transient",
				accountType: "Plus",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
			{
				fileName:    "edit-healthy.json",
				accessToken: "token-edit-healthy",
				accountType: "Plus",
				priority:    10,
				quota:       5,
				status:      "正常",
			},
		},
		behavior: compatClientBehavior{
			responsesEditErrors: map[string]error{
				"token-edit-transient": errors.New("responses returned 500: temporary upstream failure"),
			},
		},
	})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-edit-retry-1",
		TurnID:         "turn-edit-retry-1",
		Mode:           "edit",
		Prompt:         "edit retry",
		Model:          "gpt-image-2",
		Count:          1,
		Size:           "2048x2048",
		Quality:        "high",
		SourceImages: []imageTaskSourceImagePayload{
			{
				ID:      "source-1",
				Role:    "image",
				Name:    "source.png",
				DataURL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC",
			},
		},
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-edit-retry-1", imageTaskStatusFailed)
	if len(recorder.callSequence) != 1 {
		t.Fatalf("callSequence = %#v, want only first account", recorder.callSequence)
	}
	if !strings.Contains(recorder.callSequence[0], "token-edit-transient") {
		t.Fatalf("callSequence[0] = %q, want first account", recorder.callSequence[0])
	}
	if strings.Contains(strings.Join(recorder.callSequence, ","), "token-edit-healthy") {
		t.Fatalf("callSequence = %#v, generic edit error must not switch account", recorder.callSequence)
	}
}

func TestCreateImageEditTaskHighResolutionUsesPaidAccount(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{
				fileName:    "free-priority.json",
				accessToken: "token-free-priority",
				accountType: "Free",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
			{
				fileName:    "paid-priority.json",
				accessToken: "token-paid-priority",
				accountType: "Plus",
				priority:    10,
				quota:       5,
				status:      "正常",
			},
		},
	})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-edit-paid-1",
		TurnID:         "turn-edit-paid-1",
		Mode:           "edit",
		Prompt:         "high resolution edit",
		Model:          "gpt-image-2",
		Count:          1,
		Size:           "3840x2160",
		Quality:        "high",
		SourceImages: []imageTaskSourceImagePayload{
			{
				ID:      "source-1",
				Role:    "image",
				Name:    "source.png",
				DataURL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC",
			},
		},
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-edit-paid-1", imageTaskStatusSucceeded)
	if len(recorder.callSequence) == 0 {
		t.Fatal("callSequence = empty, want paid edit execution")
	}
	lastCall := recorder.callSequence[len(recorder.callSequence)-1]
	if !strings.Contains(lastCall, "token-paid-priority") {
		t.Fatalf("callSequence = %#v, want paid account selected for high-resolution edit", recorder.callSequence)
	}
}

func TestCreateImageEditTaskWithMultipleImagesUsesResponsesThumbnails(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	sources := make([]imageTaskSourceImagePayload, 0, 7)
	for index := 0; index < 7; index++ {
		sources = append(sources, imageTaskSourceImagePayload{
			ID:      fmt.Sprintf("source-%d", index+1),
			Role:    "image",
			Name:    fmt.Sprintf("source-%d.png", index+1),
			DataURL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC",
		})
	}

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-edit-multi-responses-1",
		TurnID:         "turn-edit-multi-responses-1",
		Mode:           "edit",
		Prompt:         "multi image edit",
		Model:          "gpt-image-2",
		Count:          1,
		SourceImages:   sources,
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-edit-multi-responses-1", imageTaskStatusSucceeded)
	if len(recorder.callSequence) == 0 {
		t.Fatal("callSequence = empty, want responses edit execution")
	}
	lastCall := recorder.callSequence[len(recorder.callSequence)-1]
	if !strings.HasPrefix(lastCall, "responses:") || !strings.Contains(lastCall, ":edit") {
		t.Fatalf("callSequence = %#v, want responses edit", recorder.callSequence)
	}
}

func TestCreateImageGenerateTaskAutoFreeUsesFreeAccount(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{
				fileName:    "free-auto.json",
				accessToken: "token-free-auto",
				accountType: "Free",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
			{
				fileName:    "paid-auto.json",
				accessToken: "token-paid-auto",
				accountType: "Plus",
				priority:    10,
				quota:       5,
				status:      "正常",
			},
		},
	})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID:   "conv-generate-auto-free-1",
		TurnID:           "turn-generate-auto-free-1",
		Mode:             "generate",
		Prompt:           "auto free generate",
		Model:            "gpt-image-2",
		Count:            1,
		Size:             "",
		ResolutionAccess: "free",
		Quality:          "high",
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-generate-auto-free-1", imageTaskStatusSucceeded)
	if len(recorder.callSequence) == 0 {
		t.Fatal("callSequence = empty, want free auto execution")
	}
	lastCall := recorder.callSequence[len(recorder.callSequence)-1]
	if !strings.Contains(lastCall, "token-free-auto") {
		t.Fatalf("callSequence = %#v, want free account selected for auto-free generate", recorder.callSequence)
	}
}

func TestCreateImageGenerateTaskAutoPaidUsesPaidAccount(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{
				fileName:    "free-priority-auto-paid.json",
				accessToken: "token-free-priority-auto-paid",
				accountType: "Free",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
			{
				fileName:    "paid-priority-auto-paid.json",
				accessToken: "token-paid-priority-auto-paid",
				accountType: "Plus",
				priority:    10,
				quota:       5,
				status:      "正常",
			},
		},
	})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID:   "conv-generate-auto-paid-1",
		TurnID:           "turn-generate-auto-paid-1",
		Mode:             "generate",
		Prompt:           "auto paid generate",
		Model:            "gpt-image-2",
		Count:            1,
		Size:             "",
		ResolutionAccess: "paid",
		Quality:          "high",
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-generate-auto-paid-1", imageTaskStatusSucceeded)
	if len(recorder.callSequence) == 0 {
		t.Fatal("callSequence = empty, want paid auto execution")
	}
	lastCall := recorder.callSequence[len(recorder.callSequence)-1]
	if !strings.Contains(lastCall, "token-paid-priority-auto-paid") {
		t.Fatalf("callSequence = %#v, want paid account selected for auto-paid generate", recorder.callSequence)
	}
}

func TestCreateImageGenerateTaskDeletesAccountOnResponsesUnauthorized(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{
				fileName:    "paid-unauthorized-first.json",
				accessToken: "token-paid-unauthorized-first",
				accountType: "Plus",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
			{
				fileName:    "paid-unauthorized-second.json",
				accessToken: "token-paid-unauthorized-second",
				accountType: "Plus",
				priority:    10,
				quota:       5,
				status:      "正常",
			},
		},
		behavior: compatClientBehavior{
			responsesGenerateErrors: map[string]error{
				"token-paid-unauthorized-first": errors.New("responses returned 401: unauthorized"),
			},
		},
	})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID:   "conv-generate-unauthorized-delete-1",
		TurnID:           "turn-generate-unauthorized-delete-1",
		Mode:             "generate",
		Prompt:           "unauthorized delete generate",
		Model:            "gpt-image-2",
		Count:            1,
		Size:             "",
		ResolutionAccess: "paid",
		Quality:          "high",
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-generate-unauthorized-delete-1", imageTaskStatusSucceeded)
	if _, err := server.store.GetAccountByToken("token-paid-unauthorized-first"); err == nil {
		t.Fatal("unauthorized account still exists, want deleted")
	}
	if _, err := server.store.GetAccountByToken("token-paid-unauthorized-second"); err != nil {
		t.Fatalf("fallback account missing: %v", err)
	}
	if len(recorder.callSequence) != 2 || !strings.HasPrefix(recorder.callSequence[0], "responses:token-paid-unauthorized-first:generate") || !strings.HasPrefix(recorder.callSequence[1], "responses:token-paid-unauthorized-second:generate") {
		t.Fatalf("callSequence = %#v, want first account 401 then second account responses", recorder.callSequence)
	}
}

func TestCreateImageGenerateTaskUsesExternalResponsesWhenResponsesAccountBusy(t *testing.T) {
	started := make(chan string, 1)
	release := make(chan struct{})
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		externalResponsesEnabled: true,
		accounts: []compatSeedAccount{
			{
				fileName:    "paid-busy-external.json",
				accessToken: "token-paid-busy-external",
				accountType: "Plus",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
		},
		behavior: compatClientBehavior{
			generateStarted: started,
			generateRelease: release,
		},
	})
	server.cfg.Server.MaxImageConcurrency = 2

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		UserID:           "user-a",
		ConversationID:   "conv-generate-busy-holder-1",
		TurnID:           "turn-generate-busy-holder-1",
		Mode:             "generate",
		Prompt:           "hold responses account",
		Model:            "gpt-image-2",
		Count:            1,
		ResolutionAccess: "paid",
		Quality:          "high",
	}); err != nil {
		t.Fatalf("createTask(holder) returned error: %v", err)
	}
	select {
	case token := <-started:
		if token != "token-paid-busy-external" {
			t.Fatalf("started token = %q, want token-paid-busy-external", token)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for holder task to occupy responses account")
	}

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID:   "conv-generate-busy-external-1",
		TurnID:           "turn-generate-busy-external-1",
		Mode:             "generate",
		Prompt:           "use external while responses busy",
		Model:            "gpt-image-2",
		Count:            1,
		ResolutionAccess: "paid",
		Quality:          "high",
	}); err != nil {
		t.Fatalf("createTask(external) returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-generate-busy-external-1", imageTaskStatusSucceeded)
	close(release)
	waitForTaskStatus(t, server, "turn-generate-busy-holder-1", imageTaskStatusSucceeded)
	if recorder.externalCalls != 1 {
		t.Fatalf("externalCalls = %d, want 1; sequence = %#v", recorder.externalCalls, recorder.callSequence)
	}
	if len(recorder.callSequence) < 2 || !strings.HasPrefix(recorder.callSequence[1], "external_responses:external_responses:generate") {
		t.Fatalf("callSequence = %#v, want second task to use external_responses", recorder.callSequence)
	}
}

func TestCreateImageGenerateTaskFallsBackToExternalResponsesOnResponsesLimit(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		externalResponsesEnabled: true,
		accounts: []compatSeedAccount{
			{
				fileName:    "paid-external-fallback.json",
				accessToken: "token-paid-external-fallback",
				accountType: "Plus",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
		},
		behavior: compatClientBehavior{
			responsesGenerateErrors: map[string]error{
				"token-paid-external-fallback": errors.New("responses returned 429: 5h limit reached"),
			},
		},
	})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID:   "conv-generate-external-fallback-1",
		TurnID:           "turn-generate-external-fallback-1",
		Mode:             "generate",
		Prompt:           "external fallback generate",
		Model:            "gpt-image-2",
		Count:            1,
		Size:             "",
		ResolutionAccess: "paid",
		Quality:          "high",
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-generate-external-fallback-1", imageTaskStatusSucceeded)
	if _, err := server.store.GetAccountByToken("token-paid-external-fallback"); err != nil {
		t.Fatalf("limited account was deleted, want responses route disabled only: %v", err)
	}
	if recorder.responsesCalls != 1 || recorder.externalCalls != 1 {
		t.Fatalf("responsesCalls/externalCalls = %d/%d, want 1/1; sequence = %#v", recorder.responsesCalls, recorder.externalCalls, recorder.callSequence)
	}
	if len(recorder.callSequence) != 2 || !strings.HasPrefix(recorder.callSequence[0], "responses:token-paid-external-fallback:generate") || !strings.HasPrefix(recorder.callSequence[1], "external_responses:external_responses:generate") {
		t.Fatalf("callSequence = %#v, want responses then external_responses", recorder.callSequence)
	}
}

func TestCreateImageGenerateTaskRetriesExternalResponses5xxThreeTimes(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "external_responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		externalResponsesEnabled: true,
		behavior: compatClientBehavior{
			externalGenerateErr: errors.New("external responses returned 504: Upstream request timeout"),
		},
	})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID:   "conv-external-504-retry-1",
		TurnID:           "turn-external-504-retry-1",
		Mode:             "generate",
		Prompt:           "external 504 retry generate",
		Model:            "gpt-image-2",
		Count:            1,
		ResolutionAccess: "paid",
		Quality:          "high",
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-external-504-retry-1", imageTaskStatusFailed)
	if recorder.externalCalls != 3 {
		t.Fatalf("externalCalls = %d, want 3; sequence = %#v", recorder.externalCalls, recorder.callSequence)
	}
	if len(recorder.callSequence) != 3 {
		t.Fatalf("callSequence = %#v, want 3 external retry attempts", recorder.callSequence)
	}
}

func TestCreateImageGenerateTaskRetriesExternalResponsesSSEContextCanceledThreeTimes(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "external_responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		externalResponsesEnabled: true,
		behavior: compatClientBehavior{
			externalGenerateErr: fmt.Errorf("external responses SSE read error: %w", context.Canceled),
		},
	})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID:   "conv-external-sse-canceled-retry-1",
		TurnID:           "turn-external-sse-canceled-retry-1",
		Mode:             "generate",
		Prompt:           "external sse canceled retry generate",
		Model:            "gpt-image-2",
		Count:            1,
		ResolutionAccess: "paid",
		Quality:          "high",
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-external-sse-canceled-retry-1", imageTaskStatusFailed)
	if recorder.externalCalls != 3 {
		t.Fatalf("externalCalls = %d, want 3; sequence = %#v", recorder.externalCalls, recorder.callSequence)
	}
	if len(recorder.callSequence) != 3 {
		t.Fatalf("callSequence = %#v, want 3 external retry attempts", recorder.callSequence)
	}
}

func TestCreateImageGenerateUsesPreviousResponseOnlyForSameResponsesAccount(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})
	accountID := compatPrimaryAccountID(t, server)

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-prev-response-1",
		TurnID:         "turn-prev-response-1",
		Mode:           "generate",
		Prompt:         "continue image",
		Model:          "gpt-image-2",
		Count:          1,
		ContextReference: &imageTaskContextReferencePayload{
			ResponseID:      "resp_123",
			SourceAccountID: accountID,
		},
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-prev-response-1", imageTaskStatusSucceeded)
	if len(recorder.callSequence) == 0 {
		t.Fatal("callSequence = empty, want responses previous-response execution")
	}
	if !strings.Contains(recorder.callSequence[0], ":previous-response") {
		t.Fatalf("callSequence = %#v, want previous-response execution", recorder.callSequence)
	}
}

func TestCreateImageGenerateFallsBackToReferenceImageWhenPreviousResponseMissing(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		behavior: compatClientBehavior{
			responsesPreviousErrors: map[string]error{
				"token-compat": errors.New("previous_response_id response not found"),
			},
		},
	})
	accountID := compatPrimaryAccountID(t, server)

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-prev-response-missing-1",
		TurnID:         "turn-prev-response-missing-1",
		Mode:           "generate",
		Prompt:         "continue after missing previous response",
		Model:          "gpt-image-2",
		Count:          1,
		ReferenceImages: []imageTaskReferenceImagePayload{
			{
				ID:      "reference-1",
				Name:    "reference.png",
				DataURL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC",
			},
		},
		ContextReference: &imageTaskContextReferencePayload{
			ResponseID:      "resp_missing",
			SourceAccountID: accountID,
		},
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-prev-response-missing-1", imageTaskStatusSucceeded)
	if len(recorder.callSequence) != 2 {
		t.Fatalf("callSequence = %#v, want previous-response then reference fallback", recorder.callSequence)
	}
	if recorder.callSequence[0] != "responses:token-compat:previous-response" || recorder.callSequence[1] != "responses:token-compat:reference-generate" {
		t.Fatalf("callSequence = %#v, want previous-response then reference fallback", recorder.callSequence)
	}
}

func TestCreateImageGenerateFallsBackToLegacyWhenResponsesReferenceFails(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		behavior: compatClientBehavior{
			responsesPreviousErrors: map[string]error{
				"token-compat": errors.New("responses returned 400: previous_response_id response not found"),
			},
			responsesReferenceErrors: map[string]error{
				"token-compat": errors.New("responses returned 500: upstream unavailable"),
			},
		},
	})
	accountID := compatPrimaryAccountID(t, server)

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-prev-response-legacy-fallback-1",
		TurnID:         "turn-prev-response-legacy-fallback-1",
		Mode:           "generate",
		Prompt:         "continue after responses reference failure",
		Model:          "gpt-image-2",
		Count:          1,
		ReferenceImages: []imageTaskReferenceImagePayload{
			{
				ID:      "reference-1",
				Name:    "reference.png",
				DataURL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC",
			},
		},
		ContextReference: &imageTaskContextReferencePayload{
			ConversationID:  "conv-legacy-1",
			ParentMessageID: "msg-legacy-1",
			ResponseID:      "resp_missing",
			SourceAccountID: accountID,
		},
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-prev-response-legacy-fallback-1", imageTaskStatusSucceeded)
	want := []string{
		"responses:token-compat:previous-response",
		"responses:token-compat:reference-generate",
		"official:token-compat:reference-generate",
	}
	if len(recorder.callSequence) != len(want) {
		t.Fatalf("callSequence = %#v, want %#v", recorder.callSequence, want)
	}
	for index := range want {
		if recorder.callSequence[index] != want[index] {
			t.Fatalf("callSequence = %#v, want %#v", recorder.callSequence, want)
		}
	}
}

func TestResolveTaskSourceImageBytesRejectsNonImageDataURL(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	_, err := server.resolveTaskSourceImageBytes(imageTaskSourceImage{
		DataURL: "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte("<html></html>")),
	})
	if err == nil || !strings.Contains(err.Error(), "only image data urls are supported") {
		t.Fatalf("resolveTaskSourceImageBytes() error = %v, want image data url error", err)
	}
}

func TestResolveTaskSourceImageBytesRejectsInvalidImageDataURL(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	_, err := server.resolveTaskSourceImageBytes(imageTaskSourceImage{
		DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not an image")),
	})
	if err == nil || !strings.Contains(err.Error(), "data url image is not a valid image") {
		t.Fatalf("resolveTaskSourceImageBytes() error = %v, want invalid image error", err)
	}
}

func TestResolveTaskSourceImageBytesRejectsNonImageURL(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer upstream.Close()

	_, err := server.resolveTaskSourceImageBytes(imageTaskSourceImage{URL: upstream.URL})
	if err == nil || !strings.Contains(err.Error(), "non-image content-type text/html") {
		t.Fatalf("resolveTaskSourceImageBytes() error = %v, want non-image content-type error", err)
	}
}

func TestResolveTaskSourceImageBytesAcceptsImageURL(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\b\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\rIDATx\xdac\xf8\xcf\xc0\x00\x00\x03\x01\x01\x00\xc9\xfe\x92\xef\x00\x00\x00\x00IEND\xaeB`\x82")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer upstream.Close()

	data, err := server.resolveTaskSourceImageBytes(imageTaskSourceImage{URL: upstream.URL})
	if err != nil {
		t.Fatalf("resolveTaskSourceImageBytes() returned error: %v", err)
	}
	if string(data) != string(png) {
		t.Fatalf("resolveTaskSourceImageBytes() returned %d bytes, want %d", len(data), len(png))
	}
}

func TestCreateImageGenerateFallsBackWhenContextAccountChanges(t *testing.T) {
	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-prev-response-account-change-1",
		TurnID:         "turn-prev-response-account-change-1",
		Mode:           "generate",
		Prompt:         "continue image after account change",
		Model:          "gpt-image-2",
		Count:          1,
		ContextReference: &imageTaskContextReferencePayload{
			ResponseID:      "resp_123",
			SourceAccountID: "different-account",
		},
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-prev-response-account-change-1", imageTaskStatusSucceeded)
	if len(recorder.callSequence) == 0 {
		t.Fatal("callSequence = empty, want generate fallback execution")
	}
	lastCall := recorder.callSequence[len(recorder.callSequence)-1]
	if strings.Contains(lastCall, ":previous-response") {
		t.Fatalf("callSequence = %#v, want plain generate fallback", recorder.callSequence)
	}
	if !strings.Contains(lastCall, ":generate") {
		t.Fatalf("callSequence = %#v, want generate fallback", recorder.callSequence)
	}
}

func TestCreateImageGenerateTaskAutoPaidRequiresPaidAccount(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{
				fileName:    "free-only-auto-paid.json",
				accessToken: "token-free-only-auto-paid",
				accountType: "Free",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
		},
	})

	_, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID:   "conv-generate-auto-paid-no-paid",
		TurnID:           "turn-generate-auto-paid-no-paid",
		Mode:             "generate",
		Prompt:           "auto paid without paid account",
		Model:            "gpt-image-2",
		Count:            1,
		Size:             "",
		ResolutionAccess: "paid",
		Quality:          "high",
	})
	if err == nil {
		t.Fatal("createTask() returned nil error, want paid account validation failure")
	}
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		t.Fatalf("createTask() error = %T, want *requestError", err)
	}
	if reqErr.code != "paid_resolution_requires_paid_account" {
		t.Fatalf("request error code = %q, want paid_resolution_requires_paid_account", reqErr.code)
	}
}

func waitForTaskStatus(t *testing.T, server *Server, taskID string, want imageTaskStatus) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, _, err := server.imageTasks.getTask(taskID)
		if err == nil && task != nil && imageTaskStatus(task.Status) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	task, _, err := server.imageTasks.getTask(taskID)
	if err != nil {
		t.Fatalf("getTask(%s) returned error: %v", taskID, err)
	}
	t.Fatalf("task %s status = %q, want %q", taskID, task.Status, want)
}

func TestImageTaskStreamWritesInitPayload(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-stream-1",
		TurnID:         "turn-stream-1",
		Mode:           "generate",
		Prompt:         "stream init",
		Model:          "gpt-image-2",
		Count:          1,
		Size:           "1248x1248",
		Quality:        "high",
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/image/tasks/stream", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+server.cfg.App.AuthKey)
	rec := httptest.NewRecorder()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		server.Handler().ServeHTTP(rec, req)
	}()

	time.Sleep(80 * time.Millisecond)
	cancel()
	wg.Wait()

	body := rec.Body.String()
	if !strings.Contains(body, "event: init") {
		t.Fatalf("stream body = %q, want init event", body)
	}
	if !strings.Contains(body, `"turnId":"turn-stream-1"`) {
		t.Fatalf("stream body = %q, want queued task in init payload", body)
	}
	if !strings.Contains(body, `"snapshot"`) {
		t.Fatalf("stream body = %q, want snapshot payload", body)
	}
}

func TestSchedulePublishesQueuedBlockerUpdatesWhenConcurrencyIsFull(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	task, err := server.imageTasks.newTask(createImageTaskRequest{
		ConversationID: "conv-blocker-1",
		TurnID:         "turn-blocker-1",
		Mode:           "generate",
		Prompt:         "blocked by concurrency",
		Model:          "gpt-image-2",
		Count:          1,
	})
	if err != nil {
		t.Fatalf("newTask() returned error: %v", err)
	}

	server.imageTasks.mu.Lock()
	server.imageTasks.tasks[task.ID] = task
	server.imageTasks.order = append(server.imageTasks.order, task.ID)
	server.imageTasks.runningUnits = server.imageTasks.maxRunningLocked()
	server.imageTasks.mu.Unlock()

	subID, ch := server.imageTasks.subscribe()
	defer server.imageTasks.unsubscribe(subID)

	if server.imageTasks.tryScheduleOne() {
		t.Fatal("tryScheduleOne() = true, want no work scheduled when concurrency is full")
	}

	timeout := time.After(2 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for task.upsert blocker update")
		case event := <-ch:
			if event.Type != "task.upsert" || event.Task == nil {
				continue
			}
			if event.Task.ID != task.ID {
				continue
			}
			if event.Task.WaitingReason != imageTaskWaitingReasonGlobalConcurrency {
				t.Fatalf("WaitingReason = %q, want %q", event.Task.WaitingReason, imageTaskWaitingReasonGlobalConcurrency)
			}
			if len(event.Task.Blockers) == 0 || event.Task.Blockers[0].Code != string(imageTaskWaitingReasonGlobalConcurrency) {
				t.Fatalf("Blockers = %#v, want global concurrency blocker", event.Task.Blockers)
			}
			return
		}
	}
}

func TestCreateImageTaskRetriesRateLimitedAccount(t *testing.T) {
	oldBackoffBase := imageTaskRetryBackoffBase
	oldBackoffMax := imageTaskRetryBackoffMax
	imageTaskRetryBackoffBase = 20 * time.Millisecond
	imageTaskRetryBackoffMax = 20 * time.Millisecond
	t.Cleanup(func() {
		imageTaskRetryBackoffBase = oldBackoffBase
		imageTaskRetryBackoffMax = oldBackoffMax
	})

	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{
				fileName:    "limited.json",
				accessToken: "token-limited",
				accountType: "Free",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
			{
				fileName:    "healthy.json",
				accessToken: "token-healthy",
				accountType: "Free",
				priority:    10,
				quota:       5,
				status:      "正常",
			},
		},
		behavior: compatClientBehavior{
			officialGenerateErrors: map[string]error{
				"token-limited": errors.New("cpa returned 429: quota reached"),
			},
		},
	})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-retry-1",
		TurnID:         "turn-retry-1",
		Mode:           "generate",
		Prompt:         "retry please",
		Model:          "gpt-image-2",
		Count:          1,
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-retry-1", imageTaskStatusSucceeded)

	task, _, err := server.imageTasks.getTask("turn-retry-1")
	if err != nil {
		t.Fatalf("getTask() returned error: %v", err)
	}
	if len(task.Images) != 1 || task.Images[0].URL == "" {
		t.Fatalf("task images = %#v, want successful cached image", task.Images)
	}
	if len(recorder.callSequence) < 2 {
		t.Fatalf("callSequence = %#v, want limited then fallback attempt", recorder.callSequence)
	}
	if !strings.Contains(recorder.callSequence[0], "token-limited") {
		t.Fatalf("callSequence[0] = %q, want first limited account", recorder.callSequence[0])
	}
	if !strings.Contains(recorder.callSequence[len(recorder.callSequence)-1], "token-healthy") {
		t.Fatalf("callSequence = %#v, want fallback healthy account", recorder.callSequence)
	}
}

func TestCreateImageTaskDoesNotRetryPaidTransientResponsesSSEError(t *testing.T) {
	oldBackoffBase := imageTaskRetryBackoffBase
	oldBackoffMax := imageTaskRetryBackoffMax
	imageTaskRetryBackoffBase = 20 * time.Millisecond
	imageTaskRetryBackoffMax = 20 * time.Millisecond
	t.Cleanup(func() {
		imageTaskRetryBackoffBase = oldBackoffBase
		imageTaskRetryBackoffMax = oldBackoffMax
	})

	server, recorder := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Plus",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{
		accounts: []compatSeedAccount{
			{
				fileName:    "transient-paid.json",
				accessToken: "token-transient-paid",
				accountType: "Plus",
				priority:    100,
				quota:       5,
				status:      "正常",
			},
		},
	})

	var transientCalls int32
	server.responsesClientFactory = func(accessToken, proxyURL string, authData map[string]any, requestConfig handler.ImageRequestConfig) imageWorkflowClient {
		_ = proxyURL
		_ = authData
		_ = requestConfig
		recorder.responsesCalls++
		errOnce := error(nil)
		if accessToken == "token-transient-paid" && atomic.AddInt32(&transientCalls, 1) == 1 {
			errOnce = errors.New("responses SSE read error: stream error: stream ID 1; INTERNAL_ERROR; received from peer")
		}
		return &compatStubWorkflowClient{
			factory:     "responses",
			token:       accessToken,
			recorder:    recorder,
			generateErr: errOnce,
		}
	}

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-transient-1",
		TurnID:         "turn-transient-1",
		Mode:           "generate",
		Prompt:         "retry transient stream",
		Model:          "gpt-image-2",
		Count:          1,
		Size:           "2048x2048",
		Quality:        "high",
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-transient-1", imageTaskStatusFailed)
	if got := atomic.LoadInt32(&transientCalls); got != 1 {
		t.Fatalf("transientCalls = %d, want no paid same-account retry", got)
	}
}

func TestCancelRunningImageTaskCancelsQueuedUnits(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	var active int32
	var maxActive int32
	server.officialClientFactory = func(accessToken, proxyURL string, authData map[string]any, requestConfig handler.ImageRequestConfig) imageWorkflowClient {
		_ = proxyURL
		_ = authData
		_ = requestConfig
		return &parallelGenerateWorkflowClient{
			token:     accessToken,
			active:    &active,
			maxActive: &maxActive,
			delay:     180 * time.Millisecond,
		}
	}

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-cancel-1",
		TurnID:         "turn-cancel-1",
		Mode:           "generate",
		Prompt:         "cancel me",
		Model:          "gpt-image-2",
		Count:          3,
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskPredicate(t, server, "turn-cancel-1", func(task *imageTaskView) bool {
		return task.Status == imageTaskStatusRunning
	})

	if _, err := server.imageTasks.cancelTask("turn-cancel-1"); err != nil {
		t.Fatalf("cancelTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-cancel-1", imageTaskStatusCancelled)

	task, _, err := server.imageTasks.getTask("turn-cancel-1")
	if err != nil {
		t.Fatalf("getTask() returned error: %v", err)
	}
	cancelledUnits := 0
	for _, image := range task.Images {
		if image.Error == "任务已取消" {
			cancelledUnits++
		}
	}
	if cancelledUnits < 1 {
		t.Fatalf("task images = %#v, want at least one queued unit cancelled", task.Images)
	}
}

func TestCancelRunningImageTaskInterruptsUpstreamRequest(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	server.officialClientFactory = func(accessToken, proxyURL string, authData map[string]any, requestConfig handler.ImageRequestConfig) imageWorkflowClient {
		_ = proxyURL
		_ = authData
		_ = requestConfig
		return &parallelGenerateWorkflowClient{
			token:     accessToken,
			active:    new(int32),
			maxActive: new(int32),
			delay:     5 * time.Second,
		}
	}

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-cancel-fast-1",
		TurnID:         "turn-cancel-fast-1",
		Mode:           "generate",
		Prompt:         "cancel fast",
		Model:          "gpt-image-2",
		Count:          1,
	}); err != nil {
		t.Fatalf("createTask() returned error: %v", err)
	}

	waitForTaskPredicate(t, server, "turn-cancel-fast-1", func(task *imageTaskView) bool {
		return task.Status == imageTaskStatusRunning
	})

	startedAt := time.Now()
	if _, err := server.imageTasks.cancelTask("turn-cancel-fast-1"); err != nil {
		t.Fatalf("cancelTask() returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-cancel-fast-1", imageTaskStatusCancelled)

	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("cancel finished in %s, want running request to stop promptly", elapsed)
	}
}

func TestDeferredQueuedUnitSchedulesRetryWakeup(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	task, err := server.imageTasks.newTask(createImageTaskRequest{
		ConversationID: "conv-backoff-1",
		TurnID:         "turn-backoff-1",
		Mode:           "generate",
		Prompt:         "backoff",
		Model:          "gpt-image-2",
		Count:          1,
	})
	if err != nil {
		t.Fatalf("newTask() returned error: %v", err)
	}

	server.imageTasks.mu.Lock()
	task.Units[0].NextAttemptAt = time.Now().Add(3 * time.Second)
	server.imageTasks.tasks[task.ID] = task
	server.imageTasks.order = append(server.imageTasks.order, task.ID)
	server.imageTasks.mu.Unlock()

	if server.imageTasks.tryScheduleOne() {
		t.Fatal("tryScheduleOne() = true, want deferred task to stay queued")
	}

	server.imageTasks.mu.Lock()
	defer server.imageTasks.mu.Unlock()
	if server.imageTasks.scheduleAt.IsZero() {
		t.Fatal("scheduleAt = zero, want retry wakeup to be scheduled")
	}
	if !server.imageTasks.scheduleAt.After(time.Now()) {
		t.Fatalf("scheduleAt = %s, want future retry wakeup", server.imageTasks.scheduleAt)
	}
}

func TestQueuedImageTaskExpiresBeforeFirstRun(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	server.cfg.Server.MaxImageConcurrency = 1
	server.cfg.Server.ImageTaskQueueTTLSeconds = 1
	server.officialClientFactory = func(accessToken, proxyURL string, authData map[string]any, requestConfig handler.ImageRequestConfig) imageWorkflowClient {
		_ = proxyURL
		_ = authData
		_ = requestConfig
		return &parallelGenerateWorkflowClient{
			token:     accessToken,
			active:    new(int32),
			maxActive: new(int32),
			delay:     1500 * time.Millisecond,
		}
	}

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-expire-runner",
		TurnID:         "turn-expire-runner",
		Mode:           "generate",
		Prompt:         "occupy slot",
		Model:          "gpt-image-2",
		Count:          1,
	}); err != nil {
		t.Fatalf("createTask(runner) returned error: %v", err)
	}
	waitForTaskPredicate(t, server, "turn-expire-runner", func(task *imageTaskView) bool {
		return task.Status == imageTaskStatusRunning
	})

	if _, err := server.imageTasks.createTask(createImageTaskRequest{
		ConversationID: "conv-expire-queued",
		TurnID:         "turn-expire-queued",
		Mode:           "generate",
		Prompt:         "should expire",
		Model:          "gpt-image-2",
		Count:          1,
	}); err != nil {
		t.Fatalf("createTask(queued) returned error: %v", err)
	}

	waitForTaskStatus(t, server, "turn-expire-queued", imageTaskStatusExpired)

	task, _, err := server.imageTasks.getTask("turn-expire-queued")
	if err != nil {
		t.Fatalf("getTask() returned error: %v", err)
	}
	if task.Error == "" {
		t.Fatal("expired task error = empty, want timeout message")
	}
	if len(task.Images) != 1 || task.Images[0].Error == "" {
		t.Fatalf("task images = %#v, want queued image marked as error", task.Images)
	}
}

func TestCompletedImageTaskIsPrunedAfterRetention(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	task, err := server.imageTasks.newTask(createImageTaskRequest{
		ConversationID: "conv-prune-1",
		TurnID:         "turn-prune-1",
		Mode:           "generate",
		Prompt:         "prune me",
		Model:          "gpt-image-2",
		Count:          1,
	})
	if err != nil {
		t.Fatalf("newTask() returned error: %v", err)
	}

	server.imageTasks.mu.Lock()
	task.Status = imageTaskStatusSucceeded
	task.FinishedAt = time.Now().Add(-imageTaskRetentionAfterFinish - time.Minute)
	server.imageTasks.tasks[task.ID] = task
	server.imageTasks.order = append(server.imageTasks.order, task.ID)
	server.imageTasks.mu.Unlock()

	if !server.imageTasks.tryScheduleOne() {
		t.Fatal("tryScheduleOne() = false, want prune cycle to run")
	}

	if _, _, err := server.imageTasks.getTask(task.ID); err == nil {
		t.Fatalf("getTask(%s) error = nil, want pruned task to be removed", task.ID)
	}
	items, snapshot := server.imageTasks.listTasks()
	if len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0 after pruning", len(items))
	}
	if snapshot.Total != 0 {
		t.Fatalf("snapshot.Total = %d, want 0 after pruning", snapshot.Total)
	}
}

func TestTaskSnapshotCountsQueuedUnitsWithinRunningParentTask(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	task, err := server.imageTasks.newTask(createImageTaskRequest{
		ConversationID: "conv-parent-1",
		TurnID:         "turn-parent-1",
		Mode:           "generate",
		Prompt:         "multi image",
		Model:          "gpt-image-2",
		Count:          6,
	})
	if err != nil {
		t.Fatalf("newTask() returned error: %v", err)
	}

	server.imageTasks.mu.Lock()
	task.Status = imageTaskStatusRunning
	task.ActiveUnits = 2
	task.Units[0].Status = imageTaskStatusRunning
	task.Units[1].Status = imageTaskStatusRunning
	server.imageTasks.tasks[task.ID] = task
	server.imageTasks.order = append(server.imageTasks.order, task.ID)
	server.imageTasks.runningUnits = 2
	snapshot := server.imageTasks.snapshotLocked()
	server.imageTasks.mu.Unlock()

	if snapshot.Running != 2 {
		t.Fatalf("snapshot.Running = %d, want 2", snapshot.Running)
	}
	if snapshot.Queued != 4 {
		t.Fatalf("snapshot.Queued = %d, want 4 queued units", snapshot.Queued)
	}
	if snapshot.ActiveSources.Workspace != 6 {
		t.Fatalf("snapshot.ActiveSources.Workspace = %d, want 6 active units", snapshot.ActiveSources.Workspace)
	}
}

func TestTaskQueuePositionCountsQueuedUnits(t *testing.T) {
	server, _ := newImageModeCompatTestServerWithOptions(t, imageModeCompatScenario{
		imageMode:   "studio",
		accountType: "Free",
		freeRoute:   "legacy",
		freeModel:   "auto",
		paidRoute:   "responses",
		paidModel:   "gpt-5.4-mini",
	}, compatTestServerOptions{})

	firstTask, err := server.imageTasks.newTask(createImageTaskRequest{
		ConversationID: "conv-queue-a",
		TurnID:         "turn-queue-a",
		Mode:           "generate",
		Prompt:         "first queued parent",
		Model:          "gpt-image-2",
		Count:          3,
	})
	if err != nil {
		t.Fatalf("newTask(first) returned error: %v", err)
	}
	secondTask, err := server.imageTasks.newTask(createImageTaskRequest{
		ConversationID: "conv-queue-b",
		TurnID:         "turn-queue-b",
		Mode:           "generate",
		Prompt:         "second queued parent",
		Model:          "gpt-image-2",
		Count:          2,
	})
	if err != nil {
		t.Fatalf("newTask(second) returned error: %v", err)
	}

	server.imageTasks.mu.Lock()
	server.imageTasks.tasks[firstTask.ID] = firstTask
	server.imageTasks.tasks[secondTask.ID] = secondTask
	server.imageTasks.order = append(server.imageTasks.order, firstTask.ID, secondTask.ID)
	view := server.imageTasks.buildTaskViewLocked(secondTask)
	server.imageTasks.mu.Unlock()

	if view.QueuePosition != 4 {
		t.Fatalf("QueuePosition = %d, want 4 because 3 queued units are ahead", view.QueuePosition)
	}
}

func waitForTaskPredicate(t *testing.T, server *Server, taskID string, predicate func(*imageTaskView) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, _, err := server.imageTasks.getTask(taskID)
		if err == nil && task != nil && predicate(task) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	task, _, err := server.imageTasks.getTask(taskID)
	if err != nil {
		t.Fatalf("getTask(%s) returned error: %v", taskID, err)
	}
	t.Fatalf("task %s did not satisfy predicate, current status = %q", taskID, task.Status)
}
