package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"chatgpt2api/handler"
	"chatgpt2api/internal/accounts"
	"chatgpt2api/internal/imagehistory"
)

const (
	imageTaskFakeHost                   = "workspace.local"
	externalResponsesPublicImageBaseURL = "https://images-bak.chigege.cn:8888"
)

type imageTaskDeferredError struct {
	cause           error
	accessToken     string
	attemptedTokens []string
	accountEmail    string
	accountFile     string
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
	if err == nil || isResponsesPreviousResponseContextError(err) {
		return imageAttemptFatal
	}
	if isFreePlanImageLimitError(err) {
		return imageAttemptRetryableDisableResponses
	}
	if isImageModelRefusalError(err) {
		return imageAttemptFatal
	}
	if code := requestErrorCode(err); code != "" {
		switch code {
		case "source_account_rate_limited", "external_responses_unavailable":
			return imageAttemptRetryableDisableResponses
		case "source_account_unavailable", "image_account_route_unavailable", "external_responses_not_configured":
			return imageAttemptRetryable
		default:
			return imageAttemptFatal
		}
	}
	if isImageHTTPStatusError(err, 403) {
		return imageAttemptFatal
	}
	if isImageHTTPStatusError(err, 401) || isImageHTTPStatusError(err, 429) || isImageFiveHourLimitError(err) {
		return imageAttemptRetryableDisableResponses
	}
	if isImageGatewayRetryStatusError(err) {
		return imageAttemptRetrySameOnce
	}
	if isImageRateLimitError(err) || isTransientImageStreamError(err) || isInvalidImageTokenError(err) {
		return imageAttemptRetryable
	}
	return imageAttemptRetryable
}

func shouldImmediateExternalResponsesFallback(attemptClass imageAttemptErrorClass) bool {
	return attemptClass != imageAttemptFatal
}

func shouldTryImageFallback(err error, attemptClass imageAttemptErrorClass) bool {
	return err != nil && attemptClass != imageAttemptFatal
}

func shouldRetryExternalResponsesSameRoute(ctx context.Context, err error, attemptClass imageAttemptErrorClass) bool {
	return err != nil && ctx.Err() == nil && attemptClass != imageAttemptFatal
}

func imageTaskAttemptAllowAccount(task *imageTask) func(accounts.PublicAccount) bool {
	if task == nil {
		return nil
	}
	if task.Requirement.NeedPaid || task.Requirement.SourceAccountID != "" {
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

func imageTaskAttemptAllowAnyAccount(task *imageTask) func(accounts.PublicAccount) bool {
	if task == nil {
		return nil
	}
	if task.Requirement.NeedPaid || task.Requirement.SourceAccountID != "" {
		return func(account accounts.PublicAccount) bool {
			return isPaidImageAccountType(account.Type)
		}
	}
	return nil
}

func (s *Server) acquireImageTaskAttemptLease(task *imageTask, excluded map[string]struct{}, route string) (*imageTaskLease, error) {
	store := s.getStore()
	allowDisabled := s.allowDisabledStudioImageAccounts()
	allowAccount := imageTaskAttemptAllowAccount(task)
	if task.Requirement.PolicySnapshot != nil && task.Requirement.PolicySnapshot.Enabled {
		auth, account, decision, release, err := store.AcquireRandomImageAuthLeaseForUserConversationWithPolicyRouteFilteredWithDisabledOption(excluded, allowAccount, allowDisabled, task.Requirement.PolicySnapshot, task.UserID, task.ConversationID, route)
		if err != nil {
			return nil, err
		}
		return &imageTaskLease{auth: auth, account: account, decision: decision, release: release}, nil
	}
	auth, account, release, err := store.AcquireRandomImageAuthLeaseForUserConversationRouteFilteredWithDisabledOption(excluded, allowAccount, allowDisabled, task.UserID, task.ConversationID, route)
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

	effectivePrompt := buildImageTaskEffectivePrompt(task.Prompt, task.ConversationContext, task.ConversationInput)
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
		items           []map[string]any
		err             error
		attemptedTokens func() []string
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
		imageFileURLs, urlErr := s.buildTaskImageURLsWithCompression(imageFiles, task.UserID, task.RequestBaseURL, "source", false)
		if urlErr != nil {
			return nil, urlErr
		}
		selectionEditModel := "gpt-image-2"
		responsesEligible := true
		selectionEditRun := func(client imageWorkflowClient, upstreamModel string) ([]handler.ImageResult, error) {
			applySystemHint(client)
			prompt := effectivePrompt
			if instructions != "" {
				if _, ok := client.(interface{ SetInstructions(string) }); !ok {
					prompt = instructions + "\n\n" + prompt
				}
			}
			if responses, ok := client.(interface{ UsesResponsesAPI() bool }); ok && responses.UsesResponsesAPI() && responsesEligible {
				if len(imageFileURLs) == 0 {
					return nil, newRequestError("source_reference_url_required", "external_responses 选区编辑需要可公开访问的源图 URL")
				}
				if editor, ok := client.(interface {
					EditImageByUploadWithImageURLs(context.Context, string, string, []string, []byte, string, string) ([]handler.ImageResult, error)
				}); ok {
					return editor.EditImageByUploadWithImageURLs(taskCtx, prompt, upstreamModel, imageFileURLs, mask, task.Size, task.Quality)
				}
			}
			if len(imageFiles) > 0 {
				return client.EditImageByUpload(taskCtx, prompt, upstreamModel, imageFiles, mask, task.Size, task.Quality)
			}
			return nil, newRequestError("source_reference_upload_required", "legacy 图片链路已停用，请重新上传原图后再编辑")
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
			"external_responses",
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
					if len(imageFileURLs) > 0 {
						if editor, ok := client.(interface {
							EditImageByUploadWithImageURLs(context.Context, string, string, []string, []byte, string, string) ([]handler.ImageResult, error)
						}); ok {
							return editor.EditImageByUploadWithImageURLs(taskCtx, prompt, upstreamModel, imageFileURLs, nil, task.Size, task.Quality)
						}
					}
					return client.EditImageByUpload(taskCtx, prompt, upstreamModel, imageFiles, nil, task.Size, task.Quality)
				},
				fakeReq,
				false,
				"external_responses",
			)
		}
	case task.Mode == "edit":
		var mask []byte
		var imageFiles [][]byte
		mask, imageFiles, err = s.resolveTaskEditInputs(task)
		if err != nil {
			return nil, err
		}
		if len(imageFiles) > 1 {
			return nil, newRequestError("too_many_edit_source_images", "编辑模式最多只能使用 1 张源图；多张参考图请使用生成模式")
		}
		imageFileURLs, urlErr := s.buildTaskImageURLsWithCompression(imageFiles, task.UserID, task.RequestBaseURL, "source", false)
		if urlErr != nil {
			return nil, urlErr
		}
		responsesEligible := len(imageFileURLs) > 0 || handler.SupportsResponsesInlineEdit(imageFiles, mask) || (len(mask) == 0 && handler.SupportsResponsesReferenceImages(imageFiles))
		editRun := func(client imageWorkflowClient, upstreamModel string) ([]handler.ImageResult, error) {
			applySystemHint(client)
			prompt := effectivePrompt
			if instructions != "" {
				if _, ok := client.(interface{ SetInstructions(string) }); !ok {
					prompt = instructions + "\n\n" + prompt
				}
			}
			if len(imageFileURLs) > 0 {
				if responses, ok := client.(interface{ UsesResponsesAPI() bool }); ok && responses.UsesResponsesAPI() {
					if editor, ok := client.(interface {
						EditImageByUploadWithImageURLs(context.Context, string, string, []string, []byte, string, string) ([]handler.ImageResult, error)
					}); ok {
						return editor.EditImageByUploadWithImageURLs(taskCtx, prompt, upstreamModel, imageFileURLs, mask, task.Size, task.Quality)
					}
				}
			}
			return client.EditImageByUpload(taskCtx, prompt, upstreamModel, imageFiles, mask, task.Size, task.Quality)
		}
		startRoute := firstNonEmpty(strings.TrimSpace(lease.forceRoute), "external_responses")
		items, _, err = s.runImageRequestWithAdmissionRoute(
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
			startRoute,
		)
		attemptClass := classifyImageAttemptError(err)
		if err != nil && responsesEligible && startRoute == "responses" && !isExternalResponsesRoute(lease.forceRoute) && shouldImmediateExternalResponsesFallback(attemptClass) && s.externalResponsesConfigured() {
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
				"external_responses",
			)
		}
	default:
		var referenceImageFiles [][]byte
		referenceImageFiles, err = s.resolveTaskReferenceImageInputs(task)
		if err != nil {
			return nil, err
		}
		referenceImageURLs, urlErr := s.buildTaskImageURLsWithCompression(referenceImageFiles, task.UserID, task.RequestBaseURL, "reference", len(referenceImageFiles) > 1)
		if urlErr != nil {
			return nil, urlErr
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
		sameSourceAccount := false
		if task.ContextReference != nil {
			sourceAccountID := strings.TrimSpace(task.ContextReference.SourceAccountID)
			sameSourceAccount = sourceAccountID == "" || sourceAccountID == strings.TrimSpace(lease.account.ID) || isExternalResponsesAttemptToken(lease.auth.AccessToken)
		}
		runGenerate := func(client imageWorkflowClient, upstreamModel string) ([]handler.ImageResult, error) {
			prompt := buildPrompt(client)
			if task.ContextReference != nil && sameSourceAccount && len(referenceImageFiles) == 0 {
				if previousResponseID := providerScopedPreviousResponseID(client, task.ContextReference.ResponseProviderID, task.ContextReference.ResponseID); previousResponseID != "" {
					if responses, ok := client.(interface{ UsesResponsesAPI() bool }); ok && responses.UsesResponsesAPI() {
						if generator, ok := client.(interface {
							GenerateImageWithPreviousResponse(context.Context, string, string, int, string, string, string, string) ([]handler.ImageResult, error)
						}); ok {
							results, err := generator.GenerateImageWithPreviousResponse(taskCtx, prompt, upstreamModel, 1, task.Size, task.Quality, task.Background, previousResponseID)
							if err == nil || !isResponsesPreviousResponseContextError(err) {
								return results, err
							}
						}
					}
				}
				if len(referenceImageFiles) == 0 && sameSourceAccount && task.ContextReference.ConversationID != "" && task.ContextReference.ParentMessageID != "" {
					if responses, ok := client.(interface{ UsesResponsesAPI() bool }); !ok || !responses.UsesResponsesAPI() {
						if generator, ok := client.(interface {
							GenerateImageWithContext(context.Context, string, string, int, string, string, string, string, string) ([]handler.ImageResult, error)
						}); ok {
							return generator.GenerateImageWithContext(taskCtx, prompt, upstreamModel, 1, task.Size, task.Quality, task.Background, task.ContextReference.ConversationID, task.ContextReference.ParentMessageID)
						}
					}
				}
			}
			if len(referenceImageURLs) > 0 {
				if responses, ok := client.(interface{ UsesResponsesAPI() bool }); ok && responses.UsesResponsesAPI() {
					if generator, ok := client.(interface {
						GenerateImageWithReferenceImageURLs(context.Context, string, string, int, string, string, string, []string) ([]handler.ImageResult, error)
					}); ok {
						return generator.GenerateImageWithReferenceImageURLs(taskCtx, prompt, upstreamModel, 1, task.Size, task.Quality, task.Background, referenceImageURLs)
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
				excluded[strings.TrimSpace(attemptLease.auth.AccessToken)] = struct{}{}
			}
		}
		attemptedTokens = func() []string {
			tokens := make([]string, 0, len(excluded))
			for token := range excluded {
				if strings.TrimSpace(token) != "" && !isExternalResponsesAttemptToken(token) {
					tokens = append(tokens, strings.TrimSpace(token))
				}
			}
			return tokens
		}
		tryRoute := func(attemptLease *imageTaskLease, route string) ([]map[string]any, imageAttemptErrorClass, error) {
			if attemptLease == nil || attemptLease.auth == nil {
				return nil, imageAttemptFatal, fmt.Errorf("task lease is required")
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
					route == "responses" || isExternalResponsesRoute(route),
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
			if route == "responses" || route == "external_responses" {
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
		tryExternal := func() ([]map[string]any, imageAttemptErrorClass, error) {
			if !s.externalResponsesConfigured() {
				return nil, imageAttemptRetryable, fmt.Errorf("external responses route is not configured")
			}
			retries := s.cfg.ExternalResponsesRetryTimes()
			providerCount := len(s.externalResponsesProviders())
			attemptLimit := max(1, retries) * max(1, providerCount)
			var attemptItems []map[string]any
			var attemptClass imageAttemptErrorClass
			var attemptErr error
			for attempt := 0; attempt < attemptLimit; attempt++ {
				nextLease, _, acquireErr := s.imageTasks.externalResponsesLeaseForTask(task, unitIndex)
				if acquireErr != nil {
					return nil, imageAttemptRetryable, acquireErr
				}
				attemptItems, attemptClass, attemptErr = tryRoute(nextLease, "external_responses")
				if attemptErr != nil {
					rememberAttempt(nextLease)
				}
				if !shouldRetryExternalResponsesSameRoute(taskCtx, attemptErr, attemptClass) {
					break
				}
			}
			return attemptItems, attemptClass, attemptErr
		}
		var attemptClass imageAttemptErrorClass
		startRoute := firstNonEmpty(strings.TrimSpace(lease.forceRoute), "external_responses")
		if isExternalResponsesRoute(startRoute) || startRoute == "responses" {
			items, attemptClass, err = tryExternal()
		} else {
			items, attemptClass, err = tryRoute(lease, "external_responses")
			if err != nil {
				rememberAttempt(lease)
			}
		}
		_ = attemptClass
	}

	if err != nil {
		if shouldRetryImageRequestWithNextAccount(err) || shouldRetryImageTaskOnSameAccount(lease.account, err) {
			deferred := &imageTaskDeferredError{cause: err, accessToken: lease.auth.AccessToken, accountEmail: lease.account.Email, accountFile: lease.auth.Name}
			if attemptedTokens != nil {
				deferred.attemptedTokens = attemptedTokens()
			}
			return nil, deferred
		}
		return nil, err
	}
	images := historyImagesFromResponseItems(items)
	if len(images) == 0 {
		return nil, fmt.Errorf("image task returned no images from account %s", lease.auth.AccessToken)
	}
	return images, nil
}

func (s *Server) runImageTaskGenerateExternalResponses(ctx context.Context, task *imageTask, lease *imageTaskLease, effectivePrompt, instructions, requestedModel string, metadata imageRequestMetadata, r *http.Request) ([]map[string]any, bool, error) {
	applyPrompt := func(client imageWorkflowClient) string {
		if instructions != "" {
			if setter, ok := client.(interface{ SetInstructions(string) }); ok {
				setter.SetInstructions(instructions)
				return effectivePrompt
			}
			return instructions + "\n\n" + effectivePrompt
		}
		return effectivePrompt
	}
	return s.runImageRequestWithAdmissionRoute(
		ctx,
		lease.auth,
		lease.account,
		func() {},
		lease.decision,
		"generate",
		task.ResponseFormat,
		false,
		requestedModel,
		true,
		metadata,
		func(client imageWorkflowClient, upstreamModel string) ([]handler.ImageResult, error) {
			prompt := applyPrompt(client)
			return client.GenerateImage(ctx, prompt, upstreamModel, 1, task.Size, task.Quality, task.Background)
		},
		r,
		false,
		"external_responses",
	)
}

func buildImageTaskEffectivePrompt(prompt string, conversationContext string, conversationInput []imageConversationInputItem) string {
	prompt = strings.TrimSpace(prompt)
	items := normalizeImageConversationInput(conversationInput)
	if len(items) > 0 {
		items = append(items, imageConversationInputItem{
			Role: "user",
			Content: []imageConversationInputContent{{
				Type: "input_text",
				Text: prompt,
			}},
		})
		if encoded := encodeImageConversationInput(items); encoded != "" {
			return encoded
		}
	}
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

func providerScopedPreviousResponseID(client imageWorkflowClient, sourceProviderID, responseID string) string {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return ""
	}
	currentProviderID := ""
	if provider, ok := client.(interface{ ExternalProviderID() string }); ok {
		currentProviderID = strings.TrimSpace(provider.ExternalProviderID())
	}
	if currentProviderID == "" {
		return responseID
	}
	if strings.EqualFold(currentProviderID, strings.TrimSpace(sourceProviderID)) {
		return responseID
	}
	return ""
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
			ID:                 firstNonEmpty(stringValue(item["id"]), fmt.Sprintf("image-%d", index)),
			Status:             firstNonEmpty(stringValue(item["status"]), "success"),
			B64JSON:            stringValue(item["b64_json"]),
			URL:                url,
			RevisedPrompt:      stringValue(item["revised_prompt"]),
			FileID:             stringValue(item["file_id"]),
			GenID:              stringValue(item["gen_id"]),
			ConversationID:     stringValue(item["conversation_id"]),
			ParentMessageID:    stringValue(item["parent_message_id"]),
			ResponseID:         stringValue(item["response_id"]),
			ResponseProviderID: stringValue(item["response_provider_id"]),
			SourceAccountID:    stringValue(item["source_account_id"]),
			Error:              stringValue(item["error"]),
		})
	}
	return images
}

func shouldRetryImageTaskOnSameAccount(account accounts.PublicAccount, err error) bool {
	_ = account
	_ = err
	return false
}

func isResponsesURLReferenceFallbackError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "server_error") ||
		strings.Contains(message, "response.failed") ||
		strings.Contains(message, "an error occurred while processing your request") ||
		strings.Contains(message, "help.openai.com")
}

func (s *Server) buildTaskImageURLs(images [][]byte, userID, baseURL, prefix string) ([]string, error) {
	return s.buildTaskImageURLsWithCompression(images, userID, baseURL, prefix, true)
}

func (s *Server) buildTaskImageURLsWithCompression(images [][]byte, userID, baseURL, prefix string, compress bool) ([]string, error) {
	_ = baseURL
	if len(images) == 0 {
		return nil, nil
	}
	urls := make([]string, 0, len(images))
	for index, image := range images {
		if len(image) == 0 {
			continue
		}
		var (
			url string
			err error
		)
		if compress {
			url, err = s.buildExternalResponsesImageReference(image, userID, fmt.Sprintf("%s-%d", prefix, index+1))
		} else {
			url, err = s.buildExternalResponsesOriginalImageReference(image, userID, fmt.Sprintf("%s-%d", prefix, index+1))
		}
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(url) != "" {
			urls = append(urls, url)
		}
	}
	return urls, nil
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
	var err error
	mask, err = normalizeTaskEditMaskToFirstImage(mask, imageFiles)
	if err != nil {
		return nil, nil, err
	}
	return mask, imageFiles, nil
}

func normalizeTaskEditMaskToFirstImage(mask []byte, imageFiles [][]byte) ([]byte, error) {
	if len(mask) == 0 || len(imageFiles) == 0 || len(imageFiles[0]) == 0 {
		return mask, nil
	}
	maskConfig, _, err := image.DecodeConfig(bytes.NewReader(mask))
	if err != nil {
		return nil, fmt.Errorf("mask image is not a valid image: %w", err)
	}
	imageConfig, _, err := image.DecodeConfig(bytes.NewReader(imageFiles[0]))
	if err != nil {
		return nil, fmt.Errorf("source image is not a valid image: %w", err)
	}
	if maskConfig.Width == imageConfig.Width && maskConfig.Height == imageConfig.Height {
		return mask, nil
	}
	if imageConfig.Width <= 0 || imageConfig.Height <= 0 {
		return mask, nil
	}
	decodedMask, _, err := image.Decode(bytes.NewReader(mask))
	if err != nil {
		return nil, fmt.Errorf("decode mask image: %w", err)
	}
	resized := resizeImageNearestTo(decodedMask, imageConfig.Width, imageConfig.Height)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, resized); err != nil {
		return nil, fmt.Errorf("encode resized mask image: %w", err)
	}
	return buffer.Bytes(), nil
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
		if qmark := strings.Index(name, "?"); qmark >= 0 {
			name = name[:qmark]
		}
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
