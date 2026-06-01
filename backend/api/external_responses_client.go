package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"chatgpt2api/handler"
	"chatgpt2api/internal/config"
)

var claudeResponsesUserAgent = "Codex Desktop/0.131.0-alpha.9 (Windows 10.0.26200; x86_64) unknown (Codex Desktop; 26.513.40821)"

const maxExternalResponsesSSELineBytes = 128 << 20

type externalResponsesClient struct {
	providerID          string
	providerName        string
	baseURL             string
	apiKey              string
	model               string
	httpClient          *http.Client
	requestedImageModel string
	customInstructions  string
	lastRoute           string
	lastModel           string
	lastRequestBody     string
	lastFallbackReason  string
}

func newExternalResponsesClient(cfg config.ExternalResponsesConfig) *externalResponsesClient {
	providers := externalResponsesProvidersFromConfig(cfg)
	if len(providers) == 0 {
		providers = []config.ExternalResponsesProviderConfig{{
			ID:             "default",
			Name:           "Default",
			Enabled:        true,
			BaseURL:        cfg.BaseURL,
			APIKey:         cfg.APIKey,
			Model:          cfg.Model,
			RequestTimeout: cfg.RequestTimeout,
		}}
	}
	return newExternalResponsesClientForProvider(providers[0])
}

func newExternalResponsesClientForProvider(provider config.ExternalResponsesProviderConfig) *externalResponsesClient {
	timeout := time.Duration(provider.RequestTimeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &externalResponsesClient{
		providerID:   strings.TrimSpace(provider.ID),
		providerName: strings.TrimSpace(provider.Name),
		baseURL:      strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/"),
		apiKey:       strings.TrimSpace(provider.APIKey),
		model:        strings.TrimSpace(provider.Model),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func externalResponsesProvidersFromConfig(cfg config.ExternalResponsesConfig) []config.ExternalResponsesProviderConfig {
	providers := make([]config.ExternalResponsesProviderConfig, 0, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		provider.ID = strings.TrimSpace(provider.ID)
		provider.Name = strings.TrimSpace(provider.Name)
		provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
		provider.APIKey = strings.TrimSpace(provider.APIKey)
		provider.Model = strings.TrimSpace(provider.Model)
		if provider.RequestTimeout <= 0 {
			provider.RequestTimeout = cfg.RequestTimeout
		}
		if !provider.Enabled || provider.ID == "" || provider.BaseURL == "" || provider.APIKey == "" || provider.Model == "" {
			continue
		}
		providers = append(providers, provider)
	}
	if len(providers) > 0 {
		return providers
	}
	if cfg.Enabled && strings.TrimSpace(cfg.BaseURL) != "" && strings.TrimSpace(cfg.APIKey) != "" && strings.TrimSpace(cfg.Model) != "" {
		return []config.ExternalResponsesProviderConfig{{
			ID:             "default",
			Name:           "Default",
			Enabled:        true,
			BaseURL:        strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
			APIKey:         strings.TrimSpace(cfg.APIKey),
			Model:          strings.TrimSpace(cfg.Model),
			RequestTimeout: cfg.RequestTimeout,
		}}
	}
	return nil
}

func (c *externalResponsesClient) DownloadBytes(url string) ([]byte, error) {
	if payload, err := decodeCPAImageDataURL(url); err == nil {
		return payload, nil
	}
	return downloadExternalImage(context.Background(), c.httpClient, url)
}

func (c *externalResponsesClient) DownloadAsBase64(ctx context.Context, url string) (string, error) {
	if payload, err := decodeCPAImageDataURL(url); err == nil {
		return base64.StdEncoding.EncodeToString(payload), nil
	}
	data, err := downloadExternalImage(ctx, c.httpClient, url)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func (c *externalResponsesClient) GenerateImage(ctx context.Context, prompt, model string, n int, size, quality, background string) ([]handler.ImageResult, error) {
	return c.generateViaResponses(ctx, handler.BuildResponsesPrompt(prompt), nil, nil, nil, size, quality, background, "", "", "")
}

func (c *externalResponsesClient) GenerateImageWithPreviousResponse(ctx context.Context, prompt, model string, n int, size, quality, background, previousResponseID string) ([]handler.ImageResult, error) {
	return c.generateViaResponses(ctx, handler.BuildResponsesPrompt(prompt), nil, nil, nil, size, quality, background, previousResponseID, "", "")
}

func (c *externalResponsesClient) GenerateImageWithReferenceImages(ctx context.Context, prompt, model string, n int, size, quality, background string, images [][]byte) ([]handler.ImageResult, error) {
	if len(images) == 0 {
		return c.GenerateImage(ctx, prompt, model, n, size, quality, background)
	}
	references, err := handler.PrepareResponsesReferenceImages(images)
	if err != nil {
		return nil, err
	}
	return c.generateViaResponses(ctx, handler.BuildResponsesReferencePrompt(prompt, len(references)), references, nil, nil, size, quality, background, "", "generate", handler.ResponsesReferenceOutputFormat(len(references), size, quality))
}

func (c *externalResponsesClient) GenerateImageWithReferenceImageURLs(ctx context.Context, prompt, model string, n int, size, quality, background string, imageURLs []string) ([]handler.ImageResult, error) {
	if len(imageURLs) == 0 {
		return c.GenerateImage(ctx, prompt, model, n, size, quality, background)
	}
	return c.generateViaResponses(ctx, handler.BuildResponsesReferencePrompt(prompt, len(imageURLs)), nil, nil, imageURLs, size, quality, background, "", "generate", handler.ResponsesReferenceOutputFormat(len(imageURLs), size, quality))
}

func (c *externalResponsesClient) EditImageByUpload(ctx context.Context, prompt, model string, images [][]byte, mask []byte, size, quality string) ([]handler.ImageResult, error) {
	if len(mask) == 0 && len(images) > 0 && !handler.SupportsResponsesInlineEdit(images, nil) {
		references, err := handler.PrepareResponsesReferenceImages(images)
		if err != nil {
			return nil, err
		}
		return c.generateViaResponses(ctx, handler.BuildResponsesEditPrompt(prompt, len(references), false), references, nil, nil, size, quality, "", "", "edit", handler.ResponsesReferenceOutputFormat(len(references), size, quality))
	}
	return c.EditImageByUploadWithPreviousResponse(ctx, prompt, model, images, mask, size, quality, "")
}

func (c *externalResponsesClient) EditImageByUploadWithPreviousResponse(ctx context.Context, prompt, model string, images [][]byte, mask []byte, size, quality, previousResponseID string) ([]handler.ImageResult, error) {
	if len(images) == 0 {
		return nil, fmt.Errorf("at least one image is required")
	}
	if !handler.SupportsResponsesInlineEdit(images, mask) {
		return nil, fmt.Errorf("responses inline edit payload is too large")
	}
	return c.generateViaResponses(ctx, handler.BuildResponsesEditPrompt(prompt, len(images), len(mask) > 0), images, mask, nil, size, quality, "", previousResponseID, "", "")
}

func (c *externalResponsesClient) EditImageByUploadWithImageURLs(ctx context.Context, prompt, model string, imageURLs []string, mask []byte, size, quality string) ([]handler.ImageResult, error) {
	if len(imageURLs) == 0 {
		return nil, fmt.Errorf("at least one image is required")
	}
	return c.generateViaResponses(ctx, handler.BuildResponsesEditPrompt(prompt, len(imageURLs), len(mask) > 0), nil, mask, imageURLs, size, quality, "", "", "edit", handler.ResponsesReferenceOutputFormat(len(imageURLs), size, quality))
}

func (c *externalResponsesClient) InpaintImageByMask(ctx context.Context, prompt string, model string, originalFileID string, originalGenID string, conversationID string, parentMessageID string, mask []byte, size string, quality string) ([]handler.ImageResult, error) {
	return nil, fmt.Errorf("external responses does not support official image context inpaint")
}

func (c *externalResponsesClient) UsesResponsesAPI() bool {
	return c != nil
}

func (c *externalResponsesClient) LastRoute() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.lastRoute)
}

func (c *externalResponsesClient) ExternalProviderID() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.providerID)
}

func (c *externalResponsesClient) ExternalProviderLabel() string {
	if c == nil {
		return ""
	}
	return firstNonEmpty(strings.TrimSpace(c.providerName), strings.TrimSpace(c.providerID), strings.TrimSpace(c.baseURL))
}

func (c *externalResponsesClient) LastModelLabel() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.lastModel)
}

func (c *externalResponsesClient) ImageToolModel() string {
	if c == nil {
		return ""
	}
	tool, err := handler.BuildResponsesImageGenerationTool(handler.ResponsesImageToolOptions{RequestedModel: c.requestedImageModel})
	if err != nil {
		return ""
	}
	if model, _ := tool["model"].(string); strings.TrimSpace(model) != "" {
		return strings.TrimSpace(model)
	}
	return ""
}

func (c *externalResponsesClient) LastRequestBody() string {
	if c == nil {
		return ""
	}
	return c.lastRequestBody
}

func (c *externalResponsesClient) LastFallbackReason() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.lastFallbackReason)
}

func (c *externalResponsesClient) SetRequestedImageModel(model string) {
	if c == nil {
		return
	}
	c.requestedImageModel = strings.TrimSpace(model)
}

func (c *externalResponsesClient) SetInstructions(instructions string) {
	if c == nil {
		return
	}
	c.customInstructions = strings.TrimSpace(instructions)
}

func (c *externalResponsesClient) SetSessionID(sessionID string) {
	_ = sessionID
}

func (c *externalResponsesClient) generateViaResponses(ctx context.Context, prompt string, images [][]byte, mask []byte, imageURLs []string, size, quality, background string, previousResponseID string, action string, outputFormat string) ([]handler.ImageResult, error) {
	format := handler.NormalizeResponsesImageOutputFormat(outputFormat)
	payload, err := c.buildResponsesRequest(prompt, images, mask, imageURLs, size, quality, background, previousResponseID, action, format)
	if err != nil {
		return nil, err
	}
	results, err := c.executeResponsesRequest(ctx, payload)
	if err != nil && ctx.Err() == nil && format != "jpeg" && isResponsesURLReferenceFallbackError(err) {
		payload, buildErr := c.buildResponsesRequest(prompt, images, mask, imageURLs, size, quality, background, previousResponseID, action, "jpeg")
		if buildErr != nil {
			return nil, buildErr
		}
		return c.executeResponsesRequest(ctx, payload)
	}
	return results, err
}

func (c *externalResponsesClient) buildResponsesRequest(prompt string, images [][]byte, mask []byte, imageURLs []string, size, quality, background string, previousResponseID string, action string, outputFormat string) (map[string]any, error) {
	imageParts := make([]map[string]any, 0, len(images)+len(imageURLs))
	for _, image := range images {
		if len(image) == 0 {
			continue
		}
		imageParts = append(imageParts, map[string]any{
			"type":      "input_image",
			"image_url": handler.EncodeResponsesImageDataURL(image),
			"detail":    "high",
		})
	}
	for _, imageURL := range imageURLs {
		if imageURL = strings.TrimSpace(imageURL); imageURL == "" {
			continue
		}
		imageParts = append(imageParts, map[string]any{
			"type":      "input_image",
			"image_url": imageURL,
			"detail":    "high",
		})
	}

	toolAction := strings.ToLower(strings.TrimSpace(action))
	if toolAction == "" {
		toolAction = "generate"
		if len(images) > 0 || len(imageURLs) > 0 {
			toolAction = "edit"
		}
	}
	tool, err := handler.BuildResponsesImageGenerationTool(handler.ResponsesImageToolOptions{
		RequestedModel: c.requestedImageModel,
		Action:         toolAction,
		Size:           size,
		Quality:        quality,
		Background:     background,
		Mask:           mask,
		OutputFormat:   outputFormat,
	})
	if err != nil {
		return nil, err
	}

	instructions := "You generate and edit images for the user."
	if strings.TrimSpace(c.customInstructions) != "" {
		instructions = strings.TrimSpace(c.customInstructions)
	}
	payload := map[string]any{
		"model":        c.model,
		"input":        buildResponsesInputItemsFromPrompt(prompt, imageParts),
		"tools":        []any{tool},
		"tool_choice":  map[string]any{"type": "image_generation"},
		"instructions": instructions,
		"stream":       true,
		"store":        false,
	}
	if previousResponseID = strings.TrimSpace(previousResponseID); previousResponseID != "" {
		payload["previous_response_id"] = previousResponseID
	}
	return payload, nil
}

func (c *externalResponsesClient) executeResponsesRequest(ctx context.Context, body map[string]any) ([]handler.ImageResult, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal external responses request: %w", err)
	}
	c.lastRequestBody = string(raw)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/responses", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("create external responses request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", claudeResponsesUserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("external responses request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		if readErr != nil {
			return nil, fmt.Errorf("read external responses error: %w", readErr)
		}
		return nil, fmt.Errorf("external responses returned %d: %s", resp.StatusCode, summarizeCPAError(body))
	}

	c.lastRoute = "external_responses"
	c.lastModel = c.model
	return parseExternalResponsesSSE(resp.Body)
}

func parseExternalResponsesSSE(reader io.Reader) ([]handler.ImageResult, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), maxExternalResponsesSSELineBytes)

	var dataLines []string
	responseID := ""
	results := make([]handler.ImageResult, 0)
	partialImages := make(map[string]string)
	responseSummaries := make([]string, 0, 8)

	processFrame := func(frame string) error {
		frame = strings.TrimSpace(frame)
		if frame == "" || frame == "[DONE]" {
			return nil
		}
		if summary := extractExternalResponsesModelMessage(frame); summary != "" {
			responseSummaries = append(responseSummaries, summary)
			if len(responseSummaries) > 8 {
				responseSummaries = responseSummaries[len(responseSummaries)-8:]
			}
		}
		var payload struct {
			Type   string `json:"type"`
			ItemID string `json:"item_id"`
			Error  *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			PartialImageB64 string `json:"partial_image_b64"`
			Response        *struct {
				ID     string `json:"id"`
				Output []struct {
					Type          string `json:"type"`
					Result        string `json:"result"`
					RevisedPrompt string `json:"revised_prompt"`
					OutputFormat  string `json:"output_format"`
				} `json:"output"`
			} `json:"response"`
			Item *struct {
				Type          string `json:"type"`
				Result        string `json:"result"`
				RevisedPrompt string `json:"revised_prompt"`
				OutputFormat  string `json:"output_format"`
			} `json:"item"`
		}
		if err := json.Unmarshal([]byte(frame), &payload); err != nil {
			return nil
		}
		switch payload.Type {
		case "error", "response.failed":
			if payload.Error != nil {
				message := strings.TrimSpace(payload.Error.Message)
				code := strings.TrimSpace(payload.Error.Code)
				if code != "" && message != "" {
					return fmt.Errorf("%s: %s", code, message)
				}
				if message != "" {
					return errors.New(message)
				}
				if code != "" {
					return errors.New(code)
				}
			}
			return errors.New("external responses stream returned an error")
		case "response.image_generation_call.partial_image":
			if payload.ItemID != "" && payload.PartialImageB64 != "" {
				partialImages[payload.ItemID] = payload.PartialImageB64
			}
		case "response.output_item.done":
			if payload.Item != nil && payload.Item.Type == "image_generation_call" {
				appendExternalResponseImage(&results, payload.Item.Result, payload.Item.OutputFormat, payload.Item.RevisedPrompt, responseID)
			}
		case "response.completed":
			if payload.Response == nil {
				return nil
			}
			if id := strings.TrimSpace(payload.Response.ID); id != "" {
				responseID = id
			}
			for _, item := range payload.Response.Output {
				if item.Type != "image_generation_call" {
					continue
				}
				appendExternalResponseImage(&results, item.Result, item.OutputFormat, item.RevisedPrompt, responseID)
			}
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if err := processFrame(strings.Join(dataLines, "\n")); err != nil {
				return nil, err
			}
			dataLines = dataLines[:0]
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("external responses SSE read error: %w", err)
	}
	if err := processFrame(strings.Join(dataLines, "\n")); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		for _, b64 := range partialImages {
			appendExternalResponseImage(&results, b64, "png", "", responseID)
		}
	}
	if len(results) == 0 {
		if summary := strings.TrimSpace(strings.Join(responseSummaries, "\n")); summary != "" {
			return nil, fmt.Errorf("external responses did not return image output; model response: %s", summary)
		}
		return nil, fmt.Errorf("external responses did not return image output; model response: empty SSE stream")
	}
	if responseID != "" {
		for index := range results {
			if strings.TrimSpace(results[index].ResponseID) == "" {
				results[index].ResponseID = responseID
			}
		}
	}
	return results, nil
}

func extractExternalResponsesModelMessage(frame string) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(frame), &payload); err != nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if responseID := externalResponsesNestedString(payload, "response", "id"); responseID != "" {
		parts = append(parts, "response_id="+responseID)
	}
	if code := externalResponsesNestedString(payload, "error", "code"); code != "" {
		parts = append(parts, "error_code="+code)
	}
	if message := externalResponsesNestedString(payload, "error", "message"); message != "" {
		parts = append(parts, "error_message="+message)
	}
	texts := collectExternalResponsesModelTexts(payload)
	if len(texts) > 0 {
		parts = append(parts, "text="+strings.Join(texts, " | "))
	}
	if len(parts) == 0 {
		outputTypes := collectExternalResponsesOutputTypes(payload)
		if len(outputTypes) > 0 {
			parts = append(parts, "output_types="+strings.Join(outputTypes, ","))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return truncateExternalResponsesString(strings.Join(parts, "; "), 4000)
}

func externalResponsesNestedString(payload map[string]any, keys ...string) string {
	var current any = payload
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[key]
	}
	value, _ := current.(string)
	return truncateExternalResponsesString(strings.TrimSpace(value), 2000)
}

func collectExternalResponsesModelTexts(value any) []string {
	texts := make([]string, 0, 4)
	var walk func(any)
	walk = func(current any) {
		if len(texts) >= 8 {
			return
		}
		switch typed := current.(type) {
		case map[string]any:
			itemType, _ := typed["type"].(string)
			itemType = strings.ToLower(strings.TrimSpace(itemType))
			if itemType == "output_text" || itemType == "refusal" {
				for _, key := range []string{"text", "refusal"} {
					if text, _ := typed[key].(string); strings.TrimSpace(text) != "" {
						texts = append(texts, truncateExternalResponsesString(strings.TrimSpace(text), 2000))
						return
					}
				}
				return
			}
			if itemType == "message" {
				for _, key := range []string{"content", "text", "refusal"} {
					if next, ok := typed[key]; ok {
						walk(next)
					}
				}
				return
			}
			for _, key := range []string{"response", "output", "item", "content", "message"} {
				if next, ok := typed[key]; ok {
					walk(next)
				}
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case string:
			if strings.TrimSpace(typed) != "" {
				texts = append(texts, truncateExternalResponsesString(strings.TrimSpace(typed), 2000))
			}
		}
	}
	walk(value)
	return texts
}

func collectExternalResponsesOutputTypes(payload map[string]any) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, 4)
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			if itemType, _ := typed["type"].(string); strings.TrimSpace(itemType) != "" {
				itemType = strings.TrimSpace(itemType)
				if _, ok := seen[itemType]; !ok {
					seen[itemType] = struct{}{}
					result = append(result, itemType)
				}
			}
			for _, key := range []string{"response", "output", "item", "content"} {
				if next, ok := typed[key]; ok {
					walk(next)
				}
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(payload)
	return result
}

func truncateExternalResponsesString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + fmt.Sprintf("...[truncated %d bytes]", len(value)-limit)
}

func appendExternalResponseImage(results *[]handler.ImageResult, result string, outputFormat string, revisedPrompt string, responseID string) {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		return
	}
	imageURL := trimmed
	if !strings.HasPrefix(strings.ToLower(imageURL), "data:image/") {
		imageURL = encodeCPAImageDataURLFromBase64(trimmed, mimeTypeFromOutputFormat(outputFormat))
	}
	*results = append(*results, handler.ImageResult{
		URL:           imageURL,
		RevisedPrompt: strings.TrimSpace(revisedPrompt),
		ResponseID:    strings.TrimSpace(responseID),
	})
}

func downloadExternalImage(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(url), nil)
	if err != nil {
		return nil, fmt.Errorf("create image request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download image returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	return data, nil
}

type fallbackResponsesClient struct {
	primary            imageWorkflowClient
	fallback           imageWorkflowClient
	primaryLabel       string
	fallbackLabel      string
	lastRoute          string
	lastModelLabel     string
	lastFallbackReason string
}

func newFallbackResponsesClient(primary imageWorkflowClient, fallback imageWorkflowClient) *fallbackResponsesClient {
	return newNamedFallbackResponsesClient(primary, fallback, "primary", "fallback")
}

func newNamedFallbackResponsesClient(primary imageWorkflowClient, fallback imageWorkflowClient, primaryLabel string, fallbackLabel string) *fallbackResponsesClient {
	return &fallbackResponsesClient{
		primary:       primary,
		fallback:      fallback,
		primaryLabel:  strings.TrimSpace(primaryLabel),
		fallbackLabel: strings.TrimSpace(fallbackLabel),
	}
}

func (c *fallbackResponsesClient) DownloadBytes(url string) ([]byte, error) {
	if c == nil || c.primary == nil || c.fallback == nil {
		return nil, fmt.Errorf("responses fallback client is required")
	}
	data, err := c.primary.DownloadBytes(url)
	if err == nil {
		return data, nil
	}
	return c.fallback.DownloadBytes(url)
}

func (c *fallbackResponsesClient) DownloadAsBase64(ctx context.Context, url string) (string, error) {
	if c == nil || c.primary == nil || c.fallback == nil {
		return "", fmt.Errorf("responses fallback client is required")
	}
	data, err := c.primary.DownloadAsBase64(ctx, url)
	if err == nil {
		return data, nil
	}
	return c.fallback.DownloadAsBase64(ctx, url)
}

func (c *fallbackResponsesClient) GenerateImage(ctx context.Context, prompt, model string, n int, size, quality, background string) ([]handler.ImageResult, error) {
	return c.run(ctx, func(client imageWorkflowClient) ([]handler.ImageResult, error) {
		return client.GenerateImage(ctx, prompt, model, n, size, quality, background)
	})
}

func (c *fallbackResponsesClient) GenerateImageWithPreviousResponse(ctx context.Context, prompt, model string, n int, size, quality, background, previousResponseID string) ([]handler.ImageResult, error) {
	if c == nil || c.primary == nil || c.fallback == nil {
		return nil, fmt.Errorf("responses fallback client is required")
	}
	call := func(client imageWorkflowClient) ([]handler.ImageResult, error) {
		if generator, ok := client.(interface {
			GenerateImageWithPreviousResponse(context.Context, string, string, int, string, string, string, string) ([]handler.ImageResult, error)
		}); ok {
			return generator.GenerateImageWithPreviousResponse(ctx, prompt, model, n, size, quality, background, previousResponseID)
		}
		return client.GenerateImage(ctx, prompt, model, n, size, quality, background)
	}
	results, err := call(c.primary)
	if err == nil {
		c.lastFallbackReason = ""
		c.captureRoute(c.primary)
		return results, nil
	}
	if errors.Is(err, context.Canceled) || isImageModelRefusalError(err) {
		return nil, err
	}
	if (isResponsesPreviousResponseContextError(err) || isTransientImageStreamError(err)) && !isImageRateLimitError(err) {
		return nil, markResponsesPreviousResponseContextError(err)
	}
	c.lastFallbackReason = err.Error()
	fallbackResults, fallbackErr := call(c.fallback)
	if fallbackErr == nil {
		c.captureRoute(c.fallback)
		return fallbackResults, nil
	}
	return nil, fmt.Errorf("%s failed: %v; %s fallback failed: %w", c.primaryName(), err, c.fallbackName(), fallbackErr)
}

func (c *fallbackResponsesClient) GenerateImageWithReferenceImages(ctx context.Context, prompt, model string, n int, size, quality, background string, images [][]byte) ([]handler.ImageResult, error) {
	return c.run(ctx, func(client imageWorkflowClient) ([]handler.ImageResult, error) {
		if generator, ok := client.(interface {
			GenerateImageWithReferenceImages(context.Context, string, string, int, string, string, string, [][]byte) ([]handler.ImageResult, error)
		}); ok {
			return generator.GenerateImageWithReferenceImages(ctx, prompt, model, n, size, quality, background, images)
		}
		return client.GenerateImage(ctx, prompt, model, n, size, quality, background)
	})
}

func (c *fallbackResponsesClient) GenerateImageWithReferenceImageURLs(ctx context.Context, prompt, model string, n int, size, quality, background string, imageURLs []string) ([]handler.ImageResult, error) {
	return c.run(ctx, func(client imageWorkflowClient) ([]handler.ImageResult, error) {
		if generator, ok := client.(interface {
			GenerateImageWithReferenceImageURLs(context.Context, string, string, int, string, string, string, []string) ([]handler.ImageResult, error)
		}); ok {
			return generator.GenerateImageWithReferenceImageURLs(ctx, prompt, model, n, size, quality, background, imageURLs)
		}
		return client.GenerateImage(ctx, prompt, model, n, size, quality, background)
	})
}

func (c *fallbackResponsesClient) EditImageByUpload(ctx context.Context, prompt, model string, images [][]byte, mask []byte, size, quality string) ([]handler.ImageResult, error) {
	return c.run(ctx, func(client imageWorkflowClient) ([]handler.ImageResult, error) {
		return client.EditImageByUpload(ctx, prompt, model, images, mask, size, quality)
	})
}

func (c *fallbackResponsesClient) EditImageByUploadWithImageURLs(ctx context.Context, prompt, model string, imageURLs []string, mask []byte, size, quality string) ([]handler.ImageResult, error) {
	return c.run(ctx, func(client imageWorkflowClient) ([]handler.ImageResult, error) {
		if editor, ok := client.(interface {
			EditImageByUploadWithImageURLs(context.Context, string, string, []string, []byte, string, string) ([]handler.ImageResult, error)
		}); ok {
			return editor.EditImageByUploadWithImageURLs(ctx, prompt, model, imageURLs, mask, size, quality)
		}
		return client.EditImageByUpload(ctx, prompt, model, nil, mask, size, quality)
	})
}

func (c *fallbackResponsesClient) EditImageByUploadWithPreviousResponse(ctx context.Context, prompt, model string, images [][]byte, mask []byte, size, quality, previousResponseID string) ([]handler.ImageResult, error) {
	if c == nil || c.primary == nil || c.fallback == nil {
		return nil, fmt.Errorf("responses fallback client is required")
	}
	call := func(client imageWorkflowClient) ([]handler.ImageResult, error) {
		if editor, ok := client.(interface {
			EditImageByUploadWithPreviousResponse(context.Context, string, string, [][]byte, []byte, string, string, string) ([]handler.ImageResult, error)
		}); ok {
			return editor.EditImageByUploadWithPreviousResponse(ctx, prompt, model, images, mask, size, quality, previousResponseID)
		}
		return client.EditImageByUpload(ctx, prompt, model, images, mask, size, quality)
	}
	results, err := call(c.primary)
	if err == nil {
		c.lastFallbackReason = ""
		c.captureRoute(c.primary)
		return results, nil
	}
	if errors.Is(err, context.Canceled) || isImageModelRefusalError(err) {
		return nil, err
	}
	if (isResponsesPreviousResponseContextError(err) || isTransientImageStreamError(err)) && !isImageRateLimitError(err) {
		return nil, markResponsesPreviousResponseContextError(err)
	}
	c.lastFallbackReason = err.Error()
	fallbackResults, fallbackErr := call(c.fallback)
	if fallbackErr == nil {
		c.captureRoute(c.fallback)
		return fallbackResults, nil
	}
	return nil, fmt.Errorf("%s failed: %v; %s fallback failed: %w", c.primaryName(), err, c.fallbackName(), fallbackErr)
}

func (c *fallbackResponsesClient) InpaintImageByMask(ctx context.Context, prompt string, model string, originalFileID string, originalGenID string, conversationID string, parentMessageID string, mask []byte, size string, quality string) ([]handler.ImageResult, error) {
	if c == nil || c.fallback == nil {
		return nil, fmt.Errorf("responses fallback client is required")
	}
	results, err := c.fallback.InpaintImageByMask(ctx, prompt, model, originalFileID, originalGenID, conversationID, parentMessageID, mask, size, quality)
	if err == nil {
		c.captureRoute(c.fallback)
	}
	return results, err
}

func (c *fallbackResponsesClient) UsesResponsesAPI() bool {
	return c != nil
}

func (c *fallbackResponsesClient) SetRequestedImageModel(model string) {
	setRequestedImageModel(c.primary, model)
	setRequestedImageModel(c.fallback, model)
}

func (c *fallbackResponsesClient) SetInstructions(instructions string) {
	setInstructions(c.primary, instructions)
	setInstructions(c.fallback, instructions)
}

func (c *fallbackResponsesClient) SetSessionID(sessionID string) {
	setSessionID(c.primary, sessionID)
	setSessionID(c.fallback, sessionID)
}

func (c *fallbackResponsesClient) LastRequestBody() string {
	if c == nil {
		return ""
	}
	if c.lastRouteMatches(c.primary) {
		return lastRequestBody(c.primary)
	}
	if c.lastRouteMatches(c.fallback) {
		return lastRequestBody(c.fallback)
	}
	if body := lastRequestBody(c.primary); body != "" {
		return body
	}
	return lastRequestBody(c.fallback)
}

func (c *fallbackResponsesClient) LastRoute() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.lastRoute)
}

func (c *fallbackResponsesClient) LastModelLabel() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.lastModelLabel)
}

func (c *fallbackResponsesClient) ImageToolModel() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.lastModelLabel)
}

func (c *fallbackResponsesClient) LastFallbackReason() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.lastFallbackReason)
}

func (c *fallbackResponsesClient) run(ctx context.Context, call func(imageWorkflowClient) ([]handler.ImageResult, error)) ([]handler.ImageResult, error) {
	if c == nil || c.primary == nil || c.fallback == nil {
		return nil, fmt.Errorf("responses fallback client is required")
	}
	results, err := call(c.primary)
	if err == nil {
		c.lastFallbackReason = ""
		c.captureRoute(c.primary)
		return results, nil
	}
	if errors.Is(err, context.Canceled) || isResponsesPreviousResponseContextError(err) || isImageModelRefusalError(err) {
		return nil, err
	}
	c.lastFallbackReason = err.Error()
	fallbackResults, fallbackErr := call(c.fallback)
	if fallbackErr == nil {
		c.captureRoute(c.fallback)
		return fallbackResults, nil
	}
	return nil, fmt.Errorf("%s failed: %v; %s fallback failed: %w", c.primaryName(), err, c.fallbackName(), fallbackErr)
}

func (c *fallbackResponsesClient) lastRouteMatches(client imageWorkflowClient) bool {
	if c == nil || client == nil || strings.TrimSpace(c.lastRoute) == "" {
		return false
	}
	clientRoute := lastRoute(client)
	if clientRoute == "" {
		if _, ok := client.(*externalResponsesClient); ok {
			clientRoute = "external_responses"
		} else if _, ok := client.(*fallbackResponsesClient); ok {
			clientRoute = c.lastRoute
		} else {
			clientRoute = "responses"
		}
	}
	return strings.EqualFold(strings.TrimSpace(c.lastRoute), strings.TrimSpace(clientRoute))
}

func (c *fallbackResponsesClient) primaryName() string {
	if c == nil || strings.TrimSpace(c.primaryLabel) == "" {
		return "primary"
	}
	return strings.TrimSpace(c.primaryLabel)
}

func (c *fallbackResponsesClient) fallbackName() string {
	if c == nil || strings.TrimSpace(c.fallbackLabel) == "" {
		return "fallback"
	}
	return strings.TrimSpace(c.fallbackLabel)
}

func (c *fallbackResponsesClient) captureRoute(client imageWorkflowClient) {
	if c == nil {
		return
	}
	c.lastRoute = lastRoute(client)
	if c.lastRoute == "" {
		if _, ok := client.(*externalResponsesClient); ok {
			c.lastRoute = "external_responses"
		} else {
			c.lastRoute = "responses"
		}
	}
	c.lastModelLabel = firstNonEmpty(imageToolModel(client), lastModelLabel(client))
}

func setRequestedImageModel(client imageWorkflowClient, model string) {
	if setter, ok := client.(interface{ SetRequestedImageModel(string) }); ok {
		setter.SetRequestedImageModel(model)
	}
}

func setInstructions(client imageWorkflowClient, instructions string) {
	if setter, ok := client.(interface{ SetInstructions(string) }); ok {
		setter.SetInstructions(instructions)
	}
}

func setSessionID(client imageWorkflowClient, sessionID string) {
	if setter, ok := client.(interface{ SetSessionID(string) }); ok {
		setter.SetSessionID(sessionID)
	}
}

func lastRequestBody(client imageWorkflowClient) string {
	if provider, ok := client.(interface{ LastRequestBody() string }); ok {
		return provider.LastRequestBody()
	}
	return ""
}

func lastRoute(client imageWorkflowClient) string {
	if provider, ok := client.(interface{ LastRoute() string }); ok {
		return strings.TrimSpace(provider.LastRoute())
	}
	return ""
}

func lastModelLabel(client imageWorkflowClient) string {
	if provider, ok := client.(interface{ LastModelLabel() string }); ok {
		return strings.TrimSpace(provider.LastModelLabel())
	}
	return ""
}

func imageToolModel(client imageWorkflowClient) string {
	if provider, ok := client.(interface{ ImageToolModel() string }); ok {
		return strings.TrimSpace(provider.ImageToolModel())
	}
	return ""
}
