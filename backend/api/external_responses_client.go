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

const maxExternalResponsesSSELineBytes = 128 << 20

type externalResponsesClient struct {
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
	timeout := time.Duration(cfg.RequestTimeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &externalResponsesClient{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:  strings.TrimSpace(cfg.APIKey),
		model:   strings.TrimSpace(cfg.Model),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
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
	return c.generateViaResponses(ctx, handler.BuildResponsesPrompt(prompt), nil, nil, size, quality, background, "", "")
}

func (c *externalResponsesClient) GenerateImageWithPreviousResponse(ctx context.Context, prompt, model string, n int, size, quality, background, previousResponseID string) ([]handler.ImageResult, error) {
	return c.generateViaResponses(ctx, handler.BuildResponsesPrompt(prompt), nil, nil, size, quality, background, previousResponseID, "")
}

func (c *externalResponsesClient) GenerateImageWithReferenceImages(ctx context.Context, prompt, model string, n int, size, quality, background string, images [][]byte) ([]handler.ImageResult, error) {
	if len(images) == 0 {
		return c.GenerateImage(ctx, prompt, model, n, size, quality, background)
	}
	references, err := handler.PrepareResponsesReferenceImages(images)
	if err != nil {
		return nil, err
	}
	return c.generateViaResponses(ctx, handler.BuildResponsesReferencePrompt(prompt, len(references)), references, nil, size, quality, background, "", "generate")
}

func (c *externalResponsesClient) EditImageByUpload(ctx context.Context, prompt, model string, images [][]byte, mask []byte, size, quality string) ([]handler.ImageResult, error) {
	if len(mask) == 0 && len(images) > 0 && !handler.SupportsResponsesInlineEdit(images, nil) {
		references, err := handler.PrepareResponsesReferenceImages(images)
		if err != nil {
			return nil, err
		}
		return c.generateViaResponses(ctx, handler.BuildResponsesEditPrompt(prompt, len(references), false), references, nil, size, quality, "", "", "edit")
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
	return c.generateViaResponses(ctx, handler.BuildResponsesEditPrompt(prompt, len(images), len(mask) > 0), images, mask, size, quality, "", previousResponseID, "")
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

func (c *externalResponsesClient) generateViaResponses(ctx context.Context, prompt string, images [][]byte, mask []byte, size, quality, background string, previousResponseID string, action string) ([]handler.ImageResult, error) {
	payload, err := c.buildResponsesRequest(prompt, images, mask, size, quality, background, previousResponseID, action)
	if err != nil {
		return nil, err
	}
	return c.executeResponsesRequest(ctx, payload)
}

func (c *externalResponsesClient) buildResponsesRequest(prompt string, images [][]byte, mask []byte, size, quality, background string, previousResponseID string, action string) (map[string]any, error) {
	content := make([]map[string]any, 0, 1+len(images))
	content = append(content, map[string]any{
		"type": "input_text",
		"text": strings.TrimSpace(prompt),
	})
	for _, image := range images {
		if len(image) == 0 {
			continue
		}
		content = append(content, map[string]any{
			"type":      "input_image",
			"image_url": handler.EncodeResponsesImageDataURL(image),
		})
	}

	toolAction := strings.ToLower(strings.TrimSpace(action))
	if toolAction == "" {
		toolAction = "generate"
		if len(images) > 0 {
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
	})
	if err != nil {
		return nil, err
	}

	instructions := "You generate and edit images for the user."
	if strings.TrimSpace(c.customInstructions) != "" {
		instructions = strings.TrimSpace(c.customInstructions)
	}
	payload := map[string]any{
		"model":               c.model,
		"input":               []any{map[string]any{"role": "user", "content": content}},
		"tools":               []any{tool},
		"tool_choice":         map[string]any{"type": "image_generation"},
		"instructions":        instructions,
		"stream":              true,
		"store":               false,
		"parallel_tool_calls": true,
		"include":             []string{"reasoning.encrypted_content"},
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

	processFrame := func(frame string) error {
		frame = strings.TrimSpace(frame)
		if frame == "" || frame == "[DONE]" {
			return nil
		}
		var payload struct {
			Type   string `json:"type"`
			ItemID string `json:"item_id"`
			Error  *struct {
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
		case "error":
			if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
				return errors.New(payload.Error.Message)
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
		return nil, fmt.Errorf("external responses did not return image output")
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

func (c *fallbackResponsesClient) EditImageByUpload(ctx context.Context, prompt, model string, images [][]byte, mask []byte, size, quality string) ([]handler.ImageResult, error) {
	return c.run(ctx, func(client imageWorkflowClient) ([]handler.ImageResult, error) {
		return client.EditImageByUpload(ctx, prompt, model, images, mask, size, quality)
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
