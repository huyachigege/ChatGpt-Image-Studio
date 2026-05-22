package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"io"
	"net/http/httptest"
	"os"
	"strings"

	"chatgpt2api/handler"
	"chatgpt2api/internal/accounts"
	"chatgpt2api/internal/imagehistory"
)

const imageTaskFakeHost = "workspace.local"

type imageTaskDeferredError struct {
	cause        error
	accessToken  string
	accountEmail string
	accountFile  string
}

func (e *imageTaskDeferredError) Error() string {
	if e == nil || e.cause == nil {
		return "image task deferred"
	}
	return e.cause.Error()
}

func (e *imageTaskDeferredError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

type imageAttemptErrorClass int

const (
	imageAttemptFatal imageAttemptErrorClass = iota
	imageAttemptRetrySameOnce
	imageAttemptRetryableDisableResponses
	imageAttemptRetryable
)

func classifyImageAttemptError(err error) imageAttemptErrorClass {
	if err == nil || isImageModelRefusalError(err) || isResponsesPreviousResponseContextError(err) {
		return imageAttemptFatal
	}
	if code := requestErrorCode(err); code != "" {
		switch code {
		case "source_account_rate_limited", "external_responses_unavailable":
			return imageAttemptRetryableDisableResponses
		case "source_account_unavailable", "image_account_route_unavailable":
			return imageAttemptRetryable
		default:
			return imageAttemptFatal
		}
	}
	if isImageHTTPStatusError(err, 401) || isImageHTTPStatusError(err, 403) || isImageHTTPStatusError(err, 429) || isImageFiveHourLimitError(err) {
		return imageAttemptRetryableDisableResponses
	}
	if isImageHTTPStatusError(err, 502) || isImageHTTPStatusError(err, 503) {
		return imageAttemptRetrySameOnce
	}
	if isImageRateLimitError(err) || isTransientImageStreamError(err) || isInvalidImageTokenError(err) {
		return imageAttemptRetryable
	}
	return imageAttemptFatal
}

func shouldImmediateExternalResponsesFallback(attemptClass imageAttemptErrorClass) bool {
	return attemptClass != imageAttemptFatal
}

func shouldTryImageFallback(err error, attemptClass imageAttemptErrorClass) bool {
	return err != nil && attemptClass != imageAttemptFatal
}

func imageTaskAttemptAllowAccount(task *imageTask) func(accounts.PublicAccount) bool {
	if task == nil {
		return nil
	}
	if task.Requirement.NeedPaid {
		return func(account accounts.PublicAccount) bool {
			return isPaidImageAccountType(account.Type)
		}
	}
	if strings.EqualFold(strings.TrimSpace(task.ResolutionAccess), "free") {
		return func(account accounts.PublicAccount) bool {
			return !isPaidImageAccountType(account.Type)
		}
	}
	return nil
}

func (s *Server) acquireImageTaskAttemptLease(task *imageTask, excluded map[string]struct{}, route string) (*imageTaskLease, error) {
	store := s.getStore()
	allowDisabled := s.allowDisabledStudioImageAccounts()
	allowAccount := imageTaskAttemptAllowAccount(task)
	if task.Requirement.SourceAccountID != "" {
		auth, account, release, err := store.FindImageAuthByIDWithLeaseForUserRoute(task.Requirement.SourceAccountID, task.UserID, route)
		if err != nil {
			return nil, err
		}
		return &imageTaskLease{auth: auth, account: account, release: release}, nil
	}
	if task.Requirement.PolicySnapshot != nil && task.Requirement.PolicySnapshot.Enabled {
		auth, account, decision, release, err := store.AcquireImageAuthLeaseForUserConversationWithPolicyRouteFilteredWithDisabledOption(excluded, allowAccount, allowDisabled, task.Requirement.PolicySnapshot, task.UserID, task.ConversationID, route)
		if err != nil {
			return nil, err
		}
		return &imageTaskLease{auth: auth, account: account, decision: decision, release: release}, nil
	}
	auth, account, release, err := store.AcquireImageAuthLeaseForUserConversationRouteFilteredWithDisabledOption(excluded, allowAccount, allowDisabled, task.UserID, task.ConversationID, route)
	if err != nil {
		return nil, err
	}
	return &imageTaskLease{auth: auth, account: account, release: release}, nil
}

func (s *Server) executeImageTaskUnit(ctx context.Context, taskID string, unitIndex int, lease *imageTaskLease) ([]imagehistory.Image, error) {
	if lease == nil || lease.auth == nil {
		return nil, fmt.Errorf("task lease is required")
	}
	task := s.imageTasks.copyTask(taskID)
	if task == nil {
		return nil, fmt.Errorf("task not found")
	}

	fakeRole := "user"
	if strings.TrimSpace(task.UserID) == "admin" {
		fakeRole = "admin"
	}
	fakeIdentity := authIdentity{UserID: task.UserID, Username: firstNonEmpty(task.Username, task.UserID), Role: fakeRole}
	taskCtx := context.WithValue(ctx, authIdentityContextKey{}, fakeIdentity)
	taskCtx = withResponsesSessionScope(taskCtx, task.UserID, task.ID+"#"+fmt.Sprintf("%d", unitIndex))
	taskCtx = withUserImageQuotaKind(taskCtx, imageTaskQuotaKind(task.Mode, task.ResolutionAccess, task.Size))
	fakeReq := httptest.NewRequest("POST", "http://"+imageTaskFakeHost+"/api/image/tasks", nil).WithContext(taskCtx)
	metadata := newImageRequestMetadata(task.Prompt, task.Size, task.Quality)
	requestedModel := normalizeRequestedImageModel(task.Model, s.cfg.ChatGPT.Model)

	effectivePrompt := buildImageTaskEffectivePrompt(task.Prompt, task.ConversationContext)
	instructions := buildImageTaskInstructions(task)

	applySystemHint := func(client imageWorkflowClient) {
		if instructions == "" {
			return
		}
		if setter, ok := client.(interface{ SetInstructions(string) }); ok {
			setter.SetInstructions(instructions)
		}
	}
	holdLease := func() {}

	var (
		items []map[string]any
		err   error
	)

	switch {
	case task.SourceReference != nil:
		var mask []byte
		var imageFiles [][]byte
		mask, imageFiles, err = s.resolveTaskEditInputs(task)
		if err != nil {
			return nil, err
		}
		if len(mask) == 0 {
			return nil, fmt.Errorf("selection edit mask is required")
		}
		selectionEditModel := "gpt-image-2"
		responsesEligible := false
		selectionEditRun := func(client imageWorkflowClient, upstreamModel string) ([]handler.ImageResult, error) {
			applySystemHint(client)
			prompt := effectivePrompt
			if instructions != "" {
				if _, ok := client.(interface{ SetInstructions(string) }); !ok {
					prompt = instructions + "\n\n" + prompt
				}
			}
			if responses, ok := client.(interface{ UsesResponsesAPI() bool }); ok && responses.UsesResponsesAPI() && responsesEligible {
				if editor, ok := client.(interface {
					EditImageByUploadWithPreviousResponse(context.Context, string, string, [][]byte, []byte, string, string, string) ([]handler.ImageResult, error)
				}); ok {
					results, err := editor.EditImageByUploadWithPreviousResponse(taskCtx, prompt, upstreamModel, imageFiles, mask, task.Size, task.Quality, task.SourceReference.ResponseID)
					if err != nil && len(imageFiles) > 0 && isSelectionEditContextFallbackError(err) {
						return client.EditImageByUpload(taskCtx, prompt, upstreamModel, imageFiles, mask, task.Size, task.Quality)
					}
					return results, err
				}
				return client.EditImageByUpload(taskCtx, prompt, upstreamModel, imageFiles, mask, task.Size, task.Quality)
			}
			results, err := client.InpaintImageByMask(
				taskCtx,
				prompt,
				upstreamModel,
				task.SourceReference.OriginalFileID,
				task.SourceReference.OriginalGenID,
				task.SourceReference.ConversationID,
				task.SourceReference.ParentMessageID,
				mask,
				task.Size,
				task.Quality,
			)
			if err != nil && len(imageFiles) > 0 && isSelectionEditContextFallbackError(err) {
				return client.EditImageByUpload(taskCtx, prompt, upstreamModel, imageFiles, mask, task.Size, task.Quality)
			}
			return results, err
		}
		items, _, err = s.runImageRequestWithAdmissionRoute(
			taskCtx,
			lease.auth,
			lease.account,
			holdLease,
			lease.decision,
			"selection-edit",
			task.ResponseFormat,
			true,
			"gpt-image-2",
			false,
			metadata,
			selectionEditRun,
			fakeReq,
			false,
			"legacy",
		)
		attemptClass := classifyImageAttemptError(err)
		if shouldTryImageFallback(err, attemptClass) && len(imageFiles) > 0 {
			items, _, err = s.runImageRequestWithAdmissionRoute(
				taskCtx,
				lease.auth,
				lease.account,
				func() {},
				lease.decision,
				"edit",
				task.ResponseFormat,
				false,
				selectionEditModel,
				false,
				metadata,
				func(client imageWorkflowClient, upstreamModel string) ([]handler.ImageResult, error) {
					applySystemHint(client)
					prompt := effectivePrompt
					if instructions != "" {
						if _, ok := client.(interface{ SetInstructions(string) }); !ok {
							prompt = instructions + "\n\n" + prompt
						}
					}
					return client.EditImageByUpload(taskCtx, prompt, upstreamModel, imageFiles, nil, task.Size, task.Quality)
				},
				fakeReq,
				false,
				"legacy",
			)
		}
	case task.Mode == "edit" || len(task.SourceImages) > 0:
		var mask []byte
		var imageFiles [][]byte
		mask, imageFiles, err = s.resolveTaskEditInputs(task)
		if err != nil {
			return nil, err
		}
		responsesEligible := handler.SupportsResponsesInlineEdit(imageFiles, mask) || (len(mask) == 0 && handler.SupportsResponsesReferenceImages(imageFiles))
		editRun := func(client imageWorkflowClient, upstreamModel string) ([]handler.ImageResult, error) {
			applySystemHint(client)
			prompt := effectivePrompt
			if instructions != "" {
				if _, ok := client.(interface{ SetInstructions(string) }); !ok {
					prompt = instructions + "\n\n" + prompt
				}
			}
			return client.EditImageByUpload(taskCtx, prompt, upstreamModel, imageFiles, mask, task.Size, task.Quality)
		}
		items, _, err = s.runImageRequestWithAdmission(
			taskCtx,
			lease.auth,
			lease.account,
			holdLease,
			lease.decision,
			"edit",
			task.ResponseFormat,
			false,
			requestedModel,
			responsesEligible,
			metadata,
			editRun,
			fakeReq,
			false,
		)
		attemptClass := classifyImageAttemptError(err)
		if err != nil && responsesEligible && accounts.AccountSupportsImageRoute(lease.account, "responses") && shouldImmediateExternalResponsesFallback(attemptClass) && s.externalResponsesConfigured() {
			items, _, err = s.runImageRequestWithAdmissionRoute(
				taskCtx,
				lease.auth,
				lease.account,
				func() {},
				lease.decision,
				"edit",
				task.ResponseFormat,
				false,
				requestedModel,
				true,
				metadata,
				editRun,
				fakeReq,
				false,
				"external_responses",
			)
			attemptClass = classifyImageAttemptError(err)
		}
		if shouldTryImageFallback(err, attemptClass) {
			items, _, err = s.runImageRequestWithAdmissionRoute(
				taskCtx,
				lease.auth,
				lease.account,
				func() {},
				lease.decision,
				"edit",
				task.ResponseFormat,
				false,
				requestedModel,
				false,
				metadata,
				editRun,
				fakeReq,
				false,
				"legacy",
			)
		}
	default:
		var referenceImageFiles [][]byte
		referenceImageFiles, err = s.resolveTaskReferenceImageInputs(task)
		if err != nil {
			return nil, err
		}
		buildPrompt := func(client imageWorkflowClient) string {
			applySystemHint(client)
			prompt := effectivePrompt
			if instructions != "" {
				if _, ok := client.(interface{ SetInstructions(string) }); !ok {
					prompt = instructions + "\n\n" + prompt
				}
			}
			return prompt
		}
		sameSourceAccount := task.ContextReference != nil && strings.TrimSpace(task.ContextReference.SourceAccountID) != "" && strings.TrimSpace(task.ContextReference.SourceAccountID) == strings.TrimSpace(lease.account.ID)
		runGenerate := func(client imageWorkflowClient, upstreamModel string) ([]handler.ImageResult, error) {
			prompt := buildPrompt(client)
			if task.ContextReference != nil && len(referenceImageFiles) == 0 {
				if sameSourceAccount && task.ContextReference.ResponseID != "" {
					if responses, ok := client.(interface{ UsesResponsesAPI() bool }); ok && responses.UsesResponsesAPI() {
						if generator, ok := client.(interface {
							GenerateImageWithPreviousResponse(context.Context, string, string, int, string, string, string, string) ([]handler.ImageResult, error)
						}); ok {
							results, err := generator.GenerateImageWithPreviousResponse(taskCtx, prompt, upstreamModel, 1, task.Size, task.Quality, task.Background, task.ContextReference.ResponseID)
							if err == nil || !isResponsesPreviousResponseContextError(err) {
								return results, err
							}
						}
					}
				}
				if sameSourceAccount && task.ContextReference.ConversationID != "" && task.ContextReference.ParentMessageID != "" {
					if responses, ok := client.(interface{ UsesResponsesAPI() bool }); !ok || !responses.UsesResponsesAPI() {
						if generator, ok := client.(interface {
							GenerateImageWithContext(context.Context, string, string, int, string, string, string, string, string) ([]handler.ImageResult, error)
						}); ok {
							return generator.GenerateImageWithContext(taskCtx, prompt, upstreamModel, 1, task.Size, task.Quality, task.Background, task.ContextReference.ConversationID, task.ContextReference.ParentMessageID)
						}
					}
				}
			}
			if handler.SupportsResponsesReferenceImages(referenceImageFiles) {
				if generator, ok := client.(interface {
					GenerateImageWithReferenceImages(context.Context, string, string, int, string, string, string, [][]byte) ([]handler.ImageResult, error)
				}); ok {
					return generator.GenerateImageWithReferenceImages(taskCtx, prompt, upstreamModel, 1, task.Size, task.Quality, task.Background, referenceImageFiles)
				}
			}
			return client.GenerateImage(taskCtx, prompt, upstreamModel, 1, task.Size, task.Quality, task.Background)
		}
		store := s.getStore()
		excluded := imageTaskUnitAttemptedTokens(task, unitIndex)
		if excluded == nil {
			excluded = map[string]struct{}{}
		}
		rememberAttempt := func(attemptLease *imageTaskLease) {
			if attemptLease != nil && attemptLease.auth != nil && strings.TrimSpace(attemptLease.auth.AccessToken) != "" {
				excluded[attemptLease.auth.AccessToken] = struct{}{}
			}
		}
		tryRoute := func(attemptLease *imageTaskLease, route string) ([]map[string]any, imageAttemptErrorClass, error) {
			if attemptLease == nil || attemptLease.auth == nil {
				return nil, imageAttemptFatal, fmt.Errorf("task lease is required")
			}
			if route == "responses" && !accounts.AccountSupportsImageRoute(attemptLease.account, "responses") {
				return nil, imageAttemptRetryable, newRequestError("image_account_route_unavailable", "当前账号不支持 responses 链路")
			}
			runOnce := func() ([]map[string]any, bool, error) {
				return s.runImageRequestWithAdmissionRoute(
					taskCtx,
					attemptLease.auth,
					attemptLease.account,
					holdLease,
					attemptLease.decision,
					"generate",
					task.ResponseFormat,
					false,
					requestedModel,
					route == "responses",
					metadata,
					runGenerate,
					fakeReq,
					false,
					route,
				)
			}
			attemptItems, _, attemptErr := runOnce()
			attemptClass := classifyImageAttemptError(attemptErr)
			if attemptErr == nil {
				return attemptItems, attemptClass, attemptErr
			}
			if attemptClass == imageAttemptRetryableDisableResponses && attemptLease.auth != nil {
				store.RemoveImageRouteCapability(attemptLease.auth.AccessToken, "responses")
			}
			if route == "responses" {
				if attemptClass == imageAttemptRetrySameOnce {
					attemptClass = imageAttemptRetryable
				}
				return attemptItems, attemptClass, attemptErr
			}
			if attemptClass != imageAttemptRetrySameOnce {
				return attemptItems, attemptClass, attemptErr
			}
			attemptItems, _, attemptErr = runOnce()
			attemptClass = classifyImageAttemptError(attemptErr)
			if attemptClass == imageAttemptRetrySameOnce {
				attemptClass = imageAttemptRetryable
			}
			if attemptClass == imageAttemptRetryableDisableResponses && attemptLease.auth != nil {
				store.RemoveImageRouteCapability(attemptLease.auth.AccessToken, "responses")
			}
			return attemptItems, attemptClass, attemptErr
		}
		acquireNext := func(route string) (*imageTaskLease, error) {
			return s.acquireImageTaskAttemptLease(task, excluded, route)
		}
		tryNextResponses := func() ([]map[string]any, imageAttemptErrorClass, error) {
			nextLease, acquireErr := acquireNext("responses")
			if acquireErr != nil {
				return nil, imageAttemptRetryable, acquireErr
			}
			if nextLease.release != nil {
				defer nextLease.release()
			}
			attemptItems, attemptClass, attemptErr := tryRoute(nextLease, "responses")
			if attemptErr != nil {
				rememberAttempt(nextLease)
			}
			return attemptItems, attemptClass, attemptErr
		}
		tryNextLegacy := func() ([]map[string]any, imageAttemptErrorClass, error) {
			nextLease, acquireErr := acquireNext("legacy")
			if acquireErr != nil {
				return nil, imageAttemptRetryable, acquireErr
			}
			if nextLease.release != nil {
				defer nextLease.release()
			}
			attemptItems, attemptClass, attemptErr := tryRoute(nextLease, "legacy")
			if attemptErr != nil {
				rememberAttempt(nextLease)
			}
			return attemptItems, attemptClass, attemptErr
		}
		tryExternal := func() ([]map[string]any, imageAttemptErrorClass, error) {
			if !s.externalResponsesConfigured() {
				return nil, imageAttemptRetryable, fmt.Errorf("external responses route is not configured")
			}
			attemptItems, attemptClass, attemptErr := tryRoute(lease, "external_responses")
			if attemptErr != nil {
				rememberAttempt(lease)
			}
			return attemptItems, attemptClass, attemptErr
		}
		trySameLegacy := func() ([]map[string]any, imageAttemptErrorClass, error) {
			attemptItems, attemptClass, attemptErr := tryRoute(lease, "legacy")
			if attemptErr != nil {
				rememberAttempt(lease)
			}
			return attemptItems, attemptClass, attemptErr
		}

		var attemptClass imageAttemptErrorClass
		startRoute := "legacy"
		if task.Requirement.NeedPaid || accounts.AccountSupportsImageRoute(lease.account, "responses") {
			startRoute = "responses"
		}
		items, attemptClass, err = tryRoute(lease, startRoute)
		if err != nil {
			rememberAttempt(lease)
		}
		if err != nil && attemptClass != imageAttemptFatal && startRoute == "legacy" {
			if s.externalResponsesConfigured() {
				items, attemptClass, err = tryExternal()
			}
			for i := 0; i < 3 && err != nil && attemptClass != imageAttemptFatal; i++ {
				items, attemptClass, err = tryNextLegacy()
			}
		} else if err != nil && attemptClass != imageAttemptFatal {
			if s.externalResponsesConfigured() {
				items, attemptClass, err = tryExternal()
			} else {
				for i := 0; i < 2 && err != nil && attemptClass != imageAttemptFatal; i++ {
					items, attemptClass, err = tryNextResponses()
				}
			}
			if err != nil && attemptClass != imageAttemptFatal && !task.Requirement.NeedPaid {
				items, attemptClass, err = trySameLegacy()
			}
		}
		if err != nil && attemptClass != imageAttemptFatal && !task.Requirement.NeedPaid && !strings.EqualFold(strings.TrimSpace(task.ResolutionAccess), "legacy") {
			for i := 0; i < 3 && err != nil && attemptClass != imageAttemptFatal; i++ {
				items, attemptClass, err = tryNextLegacy()
			}
		}
		if err != nil && attemptClass != imageAttemptFatal && !task.Requirement.NeedPaid && strings.EqualFold(strings.TrimSpace(task.ResolutionAccess), "legacy") {
			items, attemptClass, err = trySameLegacy()
			for i := 0; i < 3 && err != nil && attemptClass != imageAttemptFatal; i++ {
				items, attemptClass, err = tryNextLegacy()
			}
		}
		_ = attemptClass
	}

	if err != nil {
		if shouldRetryImageRequestWithNextAccount(err) || shouldRetryImageTaskOnSameAccount(lease.account, err) {
			return nil, &imageTaskDeferredError{cause: err, accessToken: lease.auth.AccessToken, accountEmail: lease.account.Email, accountFile: lease.auth.Name}
		}
		return nil, err
	}
	images := historyImagesFromResponseItems(items)
	if len(images) == 0 {
		return nil, fmt.Errorf("image task returned no images from account %s", lease.auth.AccessToken)
	}
	return images, nil
}

func buildImageTaskEffectivePrompt(prompt string, conversationContext string) string {
	prompt = strings.TrimSpace(prompt)
	conversationContext = strings.TrimSpace(conversationContext)
	if conversationContext == "" {
		return prompt
	}
	return conversationContext + "\n\n本轮用户请求：\n" + prompt
}

func taskHasMask(task *imageTask) bool {
	if task == nil {
		return false
	}
	for _, source := range task.SourceImages {
		if strings.EqualFold(strings.TrimSpace(source.Role), "mask") {
			return true
		}
	}
	return false
}

func buildImageTaskInstructions(task *imageTask) string {
	if task == nil {
		return ""
	}
	if task.PrivatePhotoMode {
		return strings.TrimSpace(firstNonEmpty(task.PrivateSystemHint, task.SystemHint))
	}
	return strings.TrimSpace(task.CommonSystemHint)
}

func isSelectionEditContextFallbackError(err error) bool {
	return requestErrorCode(err) == "source_context_missing" || isConversationContextError(err) || isResponsesPreviousResponseContextError(err)
}

func historyImagesFromResponseItems(items []map[string]any) []imagehistory.Image {
	images := make([]imagehistory.Image, 0, len(items))
	for index, item := range items {
		url := strings.TrimSpace(stringValue(item["url"]))
		if strings.Contains(url, imageTaskFakeHost) {
			if parts := strings.SplitN(url, imageTaskFakeHost, 2); len(parts) == 2 {
				url = parts[1]
				if strings.HasPrefix(url, ":") {
					if slash := strings.Index(url, "/"); slash >= 0 {
						url = url[slash:]
					}
				}
			}
		}
		if !strings.HasPrefix(url, "/") && strings.Contains(url, "/v1/files/image/") {
			if slash := strings.Index(url, "/v1/files/image/"); slash >= 0 {
				url = url[slash:]
			}
		}
		images = append(images, imagehistory.Image{
			ID:              firstNonEmpty(stringValue(item["id"]), fmt.Sprintf("image-%d", index)),
			Status:          firstNonEmpty(stringValue(item["status"]), "success"),
			B64JSON:         stringValue(item["b64_json"]),
			URL:             url,
			RevisedPrompt:   stringValue(item["revised_prompt"]),
			FileID:          stringValue(item["file_id"]),
			GenID:           stringValue(item["gen_id"]),
			ConversationID:  stringValue(item["conversation_id"]),
			ParentMessageID: stringValue(item["parent_message_id"]),
			ResponseID:      stringValue(item["response_id"]),
			SourceAccountID: stringValue(item["source_account_id"]),
			Error:           stringValue(item["error"]),
		})
	}
	return images
}

func shouldRetryImageTaskOnSameAccount(account accounts.PublicAccount, err error) bool {
	if err == nil || isImageModelRefusalError(err) || shouldRetryImageRequestWithNextAccount(err) {
		return false
	}
	return !isPaidImageAccountType(account.Type)
}

func (s *Server) resolveTaskEditInputs(task *imageTask) ([]byte, [][]byte, error) {
	imageFiles := make([][]byte, 0)
	var mask []byte
	for _, source := range task.SourceImages {
		data, err := s.resolveTaskSourceImageBytes(source)
		if err != nil {
			return nil, nil, err
		}
		if source.Role == "mask" {
			mask = data
			continue
		}
		imageFiles = append(imageFiles, data)
	}
	return mask, imageFiles, nil
}

func (s *Server) resolveTaskReferenceImageInputs(task *imageTask) ([][]byte, error) {
	imageFiles := make([][]byte, 0, len(task.ReferenceImages))
	for _, source := range task.ReferenceImages {
		data, err := s.resolveTaskSourceImageBytes(source)
		if err != nil {
			return nil, err
		}
		imageFiles = append(imageFiles, data)
	}
	return imageFiles, nil
}

func (s *Server) resolveTaskSourceImageBytes(source imageTaskSourceImage) ([]byte, error) {
	if strings.TrimSpace(source.DataURL) != "" {
		payload, err := decodeTaskDataURL(strings.TrimSpace(source.DataURL))
		if err != nil {
			return nil, err
		}
		if err := validateTaskImageBytes(payload, "data url image"); err != nil {
			return nil, err
		}
		return payload, nil
	}
	rawURL := strings.TrimSpace(source.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("source image is empty")
	}
	if index := strings.Index(rawURL, "/v1/files/image/"); index >= 0 {
		name := rawURL[index+len("/v1/files/image/"):]
		path := s.resolveImageFilePath(name)
		if path == "" {
			return nil, fmt.Errorf("image not found")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := validateTaskImageBytes(data, "local image"); err != nil {
			return nil, err
		}
		return data, nil
	}
	resp, err := compatImageFetchClient.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch image returned %d", resp.StatusCode)
	}
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if contentType != "" && !strings.HasPrefix(contentType, "image/") {
		return nil, fmt.Errorf("fetch image returned non-image content-type %s", contentType)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := validateTaskImageBytes(data, "url image"); err != nil {
		return nil, err
	}
	return data, nil
}

func decodeTaskDataURL(raw string) ([]byte, error) {
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return nil, fmt.Errorf("invalid data url")
	}
	meta := strings.ToLower(strings.TrimSpace(raw[:comma]))
	if !strings.HasPrefix(meta, "data:image/") {
		return nil, fmt.Errorf("only image data urls are supported")
	}
	if !strings.Contains(meta, ";base64") {
		return nil, fmt.Errorf("only base64 data urls are supported")
	}
	payload, err := base64.StdEncoding.DecodeString(raw[comma+1:])
	if err != nil {
		return nil, fmt.Errorf("decode data url: %w", err)
	}
	return payload, nil
}

func validateTaskImageBytes(data []byte, source string) error {
	if len(data) == 0 {
		return fmt.Errorf("%s is empty", source)
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("%s is not a valid image: %w", source, err)
	}
	return nil
}
