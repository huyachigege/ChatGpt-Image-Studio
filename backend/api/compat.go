package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"chatgpt2api/internal/imagehistory"
	"chatgpt2api/internal/imaging"
)

type imageGenerationRequest struct {
	Model          string
	Prompt         string
	N              int
	Size           string
	Quality        string
	Background     string
	ResponseFormat string
}

type imageEditRequest struct {
	Model          string
	Prompt         string
	Images         [][]byte
	Mask           []byte
	Size           string
	Quality        string
	ResponseFormat string
}

type imageSelectionEditRequest struct {
	Model           string
	Prompt          string
	Mask            []byte
	OriginalFileID  string
	OriginalGenID   string
	ConversationID  string
	ParentMessageID string
	ResponseID      string
	SourceAccountID string
	ResponseFormat  string
}

type compatChatCompletionRequest struct {
	Model          string              `json:"model"`
	Messages       []compatChatMessage `json:"messages"`
	Stream         bool                `json:"stream"`
	N              int                 `json:"n"`
	Size           string              `json:"size"`
	Quality        string              `json:"quality"`
	Background     string              `json:"background"`
	ResponseFormat string              `json:"response_format"`
}

type compatChatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type compatResponseRequest struct {
	Model          string               `json:"model"`
	Input          any                  `json:"input"`
	Tools          []compatResponseTool `json:"tools"`
	Stream         bool                 `json:"stream"`
	N              int                  `json:"n"`
	Size           string               `json:"size"`
	Quality        string               `json:"quality"`
	Background     string               `json:"background"`
	ResponseFormat string               `json:"response_format"`
}

type compatResponseTool struct {
	Type string `json:"type"`
}

var compatImageFetchClient = &http.Client{Timeout: 30 * time.Second}

type compatInputError struct {
	code    string
	message string
}

func (e *compatInputError) Error() string {
	return firstNonEmpty(strings.TrimSpace(e.message), strings.TrimSpace(e.code))
}

func newCompatInputError(code, message string) error {
	return &compatInputError{
		code:    strings.TrimSpace(code),
		message: strings.TrimSpace(message),
	}
}

func (s *Server) executeImageGeneration(ctx context.Context, req imageGenerationRequest, r *http.Request) (map[string]any, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, newRequestError("prompt_required", "prompt is required")
	}
	if req.N < 1 {
		req.N = 1
	}
	size := normalizeGenerateImageSize(req.Size)
	quality := strings.TrimSpace(req.Quality)
	if quality == "" {
		quality = "high"
	}
	if quotaErr := s.ensureUserImageQuotaAvailable(ctx, "paid", req.N); requestErrorCode(quotaErr) == "user_paid_quota_exhausted" {
		if imaging.RequiresPaidGenerateAccount(size) {
			size = imaging.DefaultGenerateSize
		}
	} else if quotaErr != nil {
		return nil, quotaErr
	}
	policy, err := parseRequestImageAccountRoutingPolicy(r)
	if err != nil {
		return nil, err
	}

	identity := identityFromContext(ctx)
	task, err := s.imageTasks.createTask(createImageTaskRequest{
		UserID:         identity.UserID,
		Username:       identity.Username,
		ConversationID: "",
		TurnID:         fmt.Sprintf("compat-generate-%d", time.Now().UnixNano()),
		Source:         "compat",
		Mode:           "generate",
		Prompt:         prompt,
		Model:          normalizeRequestedImageModel(req.Model, s.cfg.ChatGPT.Model),
		Count:          req.N,
		Size:           size,
		Quality:        quality,
		Background:     strings.TrimSpace(req.Background),
		ResponseFormat: "url",
		Policy:         policy,
		RequestBaseURL: requestPublicBaseURL(r),
	})
	if err != nil {
		return nil, err
	}
	finalTask, err := s.imageTasks.waitForTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	s.saveCompatTaskHistory(ctx, identity, finalTask, "generate", prompt, normalizeRequestedImageModel(req.Model, s.cfg.ChatGPT.Model), req.N, size, strings.TrimSpace(req.Quality), nil)
	return compatTaskPayload(finalTask)
}

func normalizeGenerateImageSize(value string) string {
	return imaging.NormalizeGenerateSize(value)
}

func (s *Server) executeImageEdit(ctx context.Context, req imageEditRequest, r *http.Request) (map[string]any, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, newRequestError("prompt_required", "prompt is required")
	}
	if len(req.Images) == 0 {
		return nil, newRequestError("image_required", "at least one image is required")
	}
	sourceImages := make([]imageTaskSourceImagePayload, 0, len(req.Images)+1)
	for index, image := range req.Images {
		sourceImages = append(sourceImages, imageTaskSourceImagePayload{
			ID:      fmt.Sprintf("compat-image-%d", index),
			Role:    "image",
			Name:    fmt.Sprintf("image-%d.png", index+1),
			DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(image),
		})
	}
	if len(req.Mask) > 0 {
		sourceImages = append(sourceImages, imageTaskSourceImagePayload{
			ID:      "compat-mask",
			Role:    "mask",
			Name:    "mask.png",
			DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(req.Mask),
		})
	}
	policy, err := parseRequestImageAccountRoutingPolicy(r)
	if err != nil {
		return nil, err
	}

	identity := identityFromContext(ctx)
	task, err := s.imageTasks.createTask(createImageTaskRequest{
		UserID:         identity.UserID,
		Username:       identity.Username,
		ConversationID: "",
		TurnID:         fmt.Sprintf("compat-edit-%d", time.Now().UnixNano()),
		Source:         "compat",
		Mode:           "edit",
		Prompt:         prompt,
		Model:          normalizeRequestedImageModel(req.Model, s.cfg.ChatGPT.Model),
		Count:          1,
		Size:           strings.TrimSpace(req.Size),
		Quality:        strings.TrimSpace(req.Quality),
		ResponseFormat: "url",
		SourceImages:   sourceImages,
		Policy:         policy,
		RequestBaseURL: requestPublicBaseURL(r),
	})
	if err != nil {
		return nil, err
	}
	finalTask, err := s.imageTasks.waitForTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	s.saveCompatTaskHistory(ctx, identity, finalTask, "edit", prompt, normalizeRequestedImageModel(req.Model, s.cfg.ChatGPT.Model), 1, strings.TrimSpace(req.Size), strings.TrimSpace(req.Quality), sourceImages)
	return compatTaskPayload(finalTask)
}

func (s *Server) executeImageSelectionEdit(ctx context.Context, req imageSelectionEditRequest, r *http.Request) (map[string]any, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, newRequestError("prompt_required", "prompt is required")
	}
	if len(req.Mask) == 0 {
		return nil, newRequestError("mask_required", "mask is required for selection edit")
	}
	if strings.TrimSpace(req.OriginalFileID) == "" || strings.TrimSpace(req.OriginalGenID) == "" {
		return nil, newRequestError("source_context_required", "original_file_id and original_gen_id are required for selection edit")
	}
	sourceAccountID := strings.TrimSpace(req.SourceAccountID)
	if sourceAccountID == "" {
		return nil, newRequestError("source_account_id_required", "source_account_id is required for selection edit")
	}

	identity := identityFromContext(ctx)
	sourceImages := []imageTaskSourceImagePayload{
		{
			ID:      "compat-mask",
			Role:    "mask",
			Name:    "mask.png",
			DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(req.Mask),
		},
	}
	task, err := s.imageTasks.createTask(createImageTaskRequest{
		UserID:         identity.UserID,
		Username:       identity.Username,
		ConversationID: "",
		TurnID:         fmt.Sprintf("compat-selection-edit-%d", time.Now().UnixNano()),
		Source:         "compat",
		Mode:           "edit",
		Prompt:         prompt,
		Model:          normalizeRequestedImageModel(req.Model, s.cfg.ChatGPT.Model),
		Count:          1,
		ResponseFormat: "url",
		SourceImages:   sourceImages,
		RequestBaseURL: requestPublicBaseURL(r),
		SourceReference: &imageTaskSourceReferencePayload{
			OriginalFileID:  strings.TrimSpace(req.OriginalFileID),
			OriginalGenID:   strings.TrimSpace(req.OriginalGenID),
			ConversationID:  strings.TrimSpace(req.ConversationID),
			ParentMessageID: strings.TrimSpace(req.ParentMessageID),
			ResponseID:      strings.TrimSpace(req.ResponseID),
			SourceAccountID: sourceAccountID,
		},
	})
	if err != nil {
		return nil, err
	}
	finalTask, err := s.imageTasks.waitForTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	s.saveCompatTaskHistory(ctx, identity, finalTask, "edit", prompt, normalizeRequestedImageModel(req.Model, s.cfg.ChatGPT.Model), 1, "", "", sourceImages)
	return compatTaskPayload(finalTask)
}

func (s *Server) saveCompatTaskHistory(ctx context.Context, identity authIdentity, task *imageTaskView, mode, prompt, model string, count int, size, quality string, sources []imageTaskSourceImagePayload) {
	if task == nil || len(task.Images) == 0 || strings.TrimSpace(identity.UserID) == "" {
		return
	}
	images := append([]imagehistory.Image(nil), task.Images...)
	for index := range images {
		if strings.TrimSpace(images[index].Prompt) == "" {
			images[index].Prompt = prompt
		}
	}
	sourceImages := make([]imagehistory.SourceImage, 0, len(sources))
	for _, source := range sources {
		sourceImages = append(sourceImages, imagehistory.SourceImage{
			ID:      strings.TrimSpace(source.ID),
			Role:    strings.TrimSpace(source.Role),
			Name:    strings.TrimSpace(source.Name),
			DataURL: strings.TrimSpace(source.DataURL),
			URL:     strings.TrimSpace(source.URL),
		})
	}
	createdAt := task.CreatedAt
	if strings.TrimSpace(createdAt) == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	status := "success"
	if task.Status != imageTaskStatusSucceeded {
		status = string(task.Status)
	}
	title := firstNonEmpty(strings.TrimSpace(prompt), mode)
	if len([]rune(title)) > 24 {
		title = string([]rune(title)[:24]) + "..."
	}
	store, err := imagehistory.NewStoreForUser(s.cfg, identity.UserID)
	if err != nil {
		return
	}
	defer store.Close()
	_, _ = store.Save(ctx, imagehistory.Conversation{
		ID:           firstNonEmpty(task.ConversationID, task.ID),
		UserID:       identity.UserID,
		Title:        title,
		Mode:         mode,
		Prompt:       prompt,
		Model:        model,
		Count:        count,
		Size:         size,
		Quality:      quality,
		SourceImages: sourceImages,
		Images:       images,
		CreatedAt:    createdAt,
		Status:       status,
		Turns: []imagehistory.Turn{
			{
				ID:           firstNonEmpty(task.TurnID, task.ID+"-turn"),
				Title:        title,
				Mode:         mode,
				Prompt:       prompt,
				Model:        model,
				Count:        count,
				Size:         size,
				Quality:      quality,
				SourceImages: sourceImages,
				Images:       images,
				CreatedAt:    createdAt,
				Status:       status,
			},
		},
	})
}

func (s *Server) handleImageChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req compatChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "", "invalid request body")
		return
	}
	if req.Stream {
		writeAPIError(w, http.StatusBadRequest, "stream_not_supported", "stream is not supported for image generation")
		return
	}

	prompt, imageURLs := extractCompatPromptAndImagesFromMessages(req.Messages)
	if strings.TrimSpace(prompt) == "" {
		writeAPIError(w, http.StatusBadRequest, "prompt_required", "prompt is required")
		return
	}

	payload, err := s.runCompatImageRequest(r.Context(), r, compatRunRequest{
		DisplayModel:   firstNonEmpty(strings.TrimSpace(req.Model), "gpt-image-2"),
		RequestedModel: normalizeCompatRequestedModel(req.Model, s.cfg.ChatGPT.Model),
		Prompt:         prompt,
		ImageURLs:      imageURLs,
		N:              req.N,
		Size:           req.Size,
		Quality:        req.Quality,
		Background:     req.Background,
	})
	if err != nil {
		writeCompatImageError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, buildCompatChatCompletionResponse(firstNonEmpty(strings.TrimSpace(req.Model), "gpt-image-2"), payload))
}

func (s *Server) handleImageResponses(w http.ResponseWriter, r *http.Request) {
	var req compatResponseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "", "invalid request body")
		return
	}
	if req.Stream {
		writeAPIError(w, http.StatusBadRequest, "stream_not_supported", "stream is not supported for image generation")
		return
	}
	if !hasCompatImageGenerationTool(req.Tools) {
		writeAPIError(w, http.StatusBadRequest, "image_generation_tool_required", "only image_generation tool requests are supported on this endpoint")
		return
	}

	prompt, imageURLs := extractCompatPromptAndImages(req.Input)
	if strings.TrimSpace(prompt) == "" {
		writeAPIError(w, http.StatusBadRequest, "prompt_required", "input text is required")
		return
	}

	payload, err := s.runCompatImageRequest(r.Context(), r, compatRunRequest{
		DisplayModel:   firstNonEmpty(strings.TrimSpace(req.Model), "gpt-image-2"),
		RequestedModel: normalizeCompatRequestedModel(req.Model, s.cfg.ChatGPT.Model),
		Prompt:         prompt,
		ImageURLs:      imageURLs,
		N:              req.N,
		Size:           req.Size,
		Quality:        req.Quality,
		Background:     req.Background,
	})
	if err != nil {
		writeCompatImageError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, buildCompatResponsesResponse(firstNonEmpty(strings.TrimSpace(req.Model), "gpt-image-2"), payload))
}

type compatRunRequest struct {
	DisplayModel   string
	RequestedModel string
	Prompt         string
	ImageURLs      []string
	N              int
	Size           string
	Quality        string
	Background     string
}

func (s *Server) runCompatImageRequest(ctx context.Context, r *http.Request, req compatRunRequest) (map[string]any, error) {
	if len(req.ImageURLs) == 0 {
		return s.executeImageGeneration(ctx, imageGenerationRequest{
			Model:          req.RequestedModel,
			Prompt:         req.Prompt,
			N:              max(1, req.N),
			Size:           req.Size,
			Quality:        req.Quality,
			Background:     req.Background,
			ResponseFormat: "url",
		}, r)
	}

	images := make([][]byte, 0, len(req.ImageURLs))
	for _, rawURL := range req.ImageURLs {
		data, err := readCompatImageURL(ctx, r, rawURL)
		if err != nil {
			return nil, newCompatInputError("invalid_image_input", err.Error())
		}
		images = append(images, data)
	}
	return s.executeImageEdit(ctx, imageEditRequest{
		Model:          req.RequestedModel,
		Prompt:         req.Prompt,
		Images:         images,
		Size:           req.Size,
		Quality:        req.Quality,
		ResponseFormat: "url",
	}, r)
}

func compatTaskPayload(task *imageTaskView) (map[string]any, error) {
	if task == nil {
		return nil, fmt.Errorf("image task not found")
	}

	created := time.Now().Unix()
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(task.CreatedAt)); err == nil {
		created = parsed.Unix()
	}

	data := make([]map[string]any, 0, len(task.Images))
	taskErrors := make([]map[string]any, 0)
	firstFailure := ""

	for index, image := range task.Images {
		item := map[string]any{
			"id":                firstNonEmpty(strings.TrimSpace(image.ID), fmt.Sprintf("%s-%d", task.ID, index)),
			"revised_prompt":    strings.TrimSpace(image.RevisedPrompt),
			"file_id":           strings.TrimSpace(image.FileID),
			"gen_id":            strings.TrimSpace(image.GenID),
			"conversation_id":   strings.TrimSpace(image.ConversationID),
			"parent_message_id": strings.TrimSpace(image.ParentMessageID),
			"response_id":       strings.TrimSpace(image.ResponseID),
		}
		if sourceAccountID := strings.TrimSpace(image.SourceAccountID); sourceAccountID != "" {
			item["source_account_id"] = sourceAccountID
		}

		switch {
		case strings.TrimSpace(image.B64JSON) != "":
			item["b64_json"] = strings.TrimSpace(image.B64JSON)
			data = append(data, item)
		case strings.TrimSpace(image.URL) != "":
			item["url"] = strings.TrimSpace(image.URL)
			data = append(data, item)
		default:
			message := firstNonEmpty(strings.TrimSpace(image.Error), strings.TrimSpace(task.Error), "image generation failed")
			if firstFailure == "" {
				firstFailure = message
			}
			taskErrors = append(taskErrors, map[string]any{
				"index":   index,
				"id":      item["id"],
				"status":  firstNonEmpty(strings.TrimSpace(image.Status), "error"),
				"error":   message,
				"file_id": item["file_id"],
				"gen_id":  item["gen_id"],
			})
		}
	}

	if len(data) == 0 {
		switch task.Status {
		case imageTaskStatusCancelled:
			return nil, errors.New("image task was cancelled")
		case imageTaskStatusExpired:
			return nil, errors.New("image task expired")
		default:
			message := firstNonEmpty(firstFailure, strings.TrimSpace(task.Error), "image generation failed")
			return nil, errors.New(sanitizeImageUserFacingMessage(errors.New(message)))
		}
	}

	payload := map[string]any{
		"created": created,
		"data":    data,
	}
	if len(taskErrors) > 0 {
		payload["errors"] = taskErrors
	}
	return payload, nil
}

func normalizeCompatRequestedModel(model, fallback string) string {
	switch strings.TrimSpace(model) {
	case "gpt-image-1", "gpt-image-2":
		return strings.TrimSpace(model)
	default:
		return normalizeRequestedImageModel("", fallback)
	}
}

func hasCompatImageGenerationTool(tools []compatResponseTool) bool {
	for _, tool := range tools {
		if strings.EqualFold(strings.TrimSpace(tool.Type), "image_generation") {
			return true
		}
	}
	return false
}

func extractCompatPromptAndImagesFromMessages(messages []compatChatMessage) (string, []string) {
	texts := make([]string, 0, len(messages))
	images := make([]string, 0)
	for _, message := range messages {
		role := strings.TrimSpace(strings.ToLower(message.Role))
		if role == "assistant" || role == "tool" {
			continue
		}
		appendCompatPromptAndImages(message.Content, &texts, &images)
	}
	return strings.Join(texts, "\n\n"), images
}

func extractCompatPromptAndImages(input any) (string, []string) {
	texts := make([]string, 0, 2)
	images := make([]string, 0)
	appendCompatPromptAndImages(input, &texts, &images)
	return strings.Join(texts, "\n\n"), images
}

func appendCompatPromptAndImages(value any, texts *[]string, images *[]string) {
	switch typed := value.(type) {
	case nil:
		return
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			*texts = append(*texts, trimmed)
		}
	case []any:
		for _, item := range typed {
			appendCompatPromptAndImages(item, texts, images)
		}
	case map[string]any:
		if content, ok := typed["content"]; ok {
			role := strings.TrimSpace(strings.ToLower(stringValue(typed["role"])))
			if role == "assistant" || role == "tool" {
				return
			}
			appendCompatPromptAndImages(content, texts, images)
			return
		}

		itemType := strings.TrimSpace(stringValue(typed["type"]))
		switch itemType {
		case "text", "input_text":
			if text := strings.TrimSpace(stringValue(typed["text"])); text != "" {
				*texts = append(*texts, text)
			}
			return
		case "image_url", "input_image":
			if imageURL := strings.TrimSpace(extractCompatImageURL(typed["image_url"])); imageURL != "" {
				*images = append(*images, imageURL)
			}
			return
		}
		if imageURL := strings.TrimSpace(extractCompatImageURL(typed["image_url"])); imageURL != "" {
			*images = append(*images, imageURL)
		}
	case []map[string]any:
		for _, item := range typed {
			appendCompatPromptAndImages(item, texts, images)
		}
	}
}

func extractCompatImageURL(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return firstNonEmpty(
			strings.TrimSpace(stringValue(typed["url"])),
			strings.TrimSpace(stringValue(typed["image_url"])),
		)
	default:
		return ""
	}
}

func readCompatImageURL(ctx context.Context, r *http.Request, rawURL string) ([]byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("image url is required")
	}
	if strings.HasPrefix(strings.ToLower(rawURL), "data:") {
		return decodeCompatDataURL(rawURL)
	}

	resolvedURL, err := resolveCompatRemoteURL(rawURL, r)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolvedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create image request failed")
	}
	resp, err := compatImageFetchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download image failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download image failed")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("read image failed")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("image is empty")
	}
	return data, nil
}

func resolveCompatRemoteURL(rawURL string, r *http.Request) (string, error) {
	if strings.HasPrefix(rawURL, "/") {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
			scheme = "https"
		}
		return fmt.Sprintf("%s://%s%s", scheme, r.Host, rawURL), nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid image url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("only data urls and same-origin image urls are supported")
	}

	requestAuthority := normalizeCompatAuthority(r.Host)
	if forwardedHost := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); forwardedHost != "" {
		requestAuthority = normalizeCompatAuthority(forwardedHost)
	}
	targetAuthority := normalizeCompatAuthority(parsed.Host)
	if targetAuthority == "" {
		return "", fmt.Errorf("invalid image url")
	}
	if requestAuthority != "" && requestAuthority == targetAuthority {
		return parsed.String(), nil
	}
	return "", fmt.Errorf("only data urls and same-origin image urls are supported")
}

func normalizeCompatAuthority(rawHost string) string {
	authority := strings.TrimSpace(rawHost)
	if authority == "" {
		return ""
	}
	if parsedHost, parsedPort, err := net.SplitHostPort(authority); err == nil {
		return strings.ToLower(net.JoinHostPort(strings.Trim(parsedHost, "[]"), parsedPort))
	}
	return strings.ToLower(strings.Trim(authority, "[]"))
}

func decodeCompatDataURL(raw string) ([]byte, error) {
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return nil, fmt.Errorf("invalid data url")
	}
	meta := strings.ToLower(raw[:comma])
	if !strings.Contains(meta, ";base64") {
		return nil, fmt.Errorf("only base64 data urls are supported")
	}
	payload, err := base64.StdEncoding.DecodeString(raw[comma+1:])
	if err != nil {
		return nil, fmt.Errorf("invalid base64 image")
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("image is empty")
	}
	return payload, nil
}

func buildCompatChatCompletionResponse(model string, payload map[string]any) map[string]any {
	created := int64Value(payload["created"])
	imageItems := compatResponseDataItems(payload)
	messageImages := make([]map[string]any, 0, len(imageItems))
	markdownImages := make([]string, 0, len(imageItems))
	for index, item := range imageItems {
		if b64 := strings.TrimSpace(stringValue(item["b64_json"])); b64 != "" {
			messageImages = append(messageImages, map[string]any{
				"b64_json":       b64,
				"revised_prompt": stringValue(item["revised_prompt"]),
			})
			markdownImages = append(markdownImages, fmt.Sprintf("![image_%d](data:image/png;base64,%s)", index+1, b64))
		}
	}
	content := "Image generation completed."
	if len(markdownImages) > 0 {
		content = strings.Join(markdownImages, "\n\n")
	}
	return map[string]any{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
					"images":  messageImages,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}
}

func buildCompatResponsesResponse(model string, payload map[string]any) map[string]any {
	created := int64Value(payload["created"])
	imageItems := compatResponseDataItems(payload)
	output := make([]map[string]any, 0, len(imageItems))
	for index, item := range imageItems {
		if b64 := strings.TrimSpace(stringValue(item["b64_json"])); b64 != "" {
			output = append(output, map[string]any{
				"id":             fmt.Sprintf("ig_%d", index+1),
				"type":           "image_generation_call",
				"status":         "completed",
				"result":         b64,
				"revised_prompt": strings.TrimSpace(stringValue(item["revised_prompt"])),
			})
		}
	}
	return map[string]any{
		"id":                  fmt.Sprintf("resp_%d", created),
		"object":              "response",
		"created_at":          created,
		"status":              "completed",
		"error":               nil,
		"incomplete_details":  nil,
		"model":               model,
		"output":              output,
		"parallel_tool_calls": false,
	}
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	default:
		return 0
	}
}

func compatResponseDataItems(payload map[string]any) []map[string]any {
	rawItems, _ := payload["data"].([]map[string]any)
	if rawItems != nil {
		return rawItems
	}
	itemsAny, _ := payload["data"].([]any)
	items := make([]map[string]any, 0, len(itemsAny))
	for _, item := range itemsAny {
		typed, _ := item.(map[string]any)
		if typed != nil {
			items = append(items, typed)
		}
	}
	return items
}

func writeCompatImageError(w http.ResponseWriter, err error) {
	if err == nil {
		writeAPIError(w, http.StatusBadGateway, "", "image generation failed")
		return
	}
	var inputErr *compatInputError
	if errors.As(err, &inputErr) {
		writeAPIError(w, http.StatusBadRequest, inputErr.code, inputErr.message)
		return
	}
	writeAPIError(w, http.StatusBadGateway, requestErrorCode(err), sanitizeImageUserFacingMessage(err))
}
