package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"chatgpt2api/internal/config"
)

type externalResponsesReferenceTestConfig struct {
	Enabled               bool                               `json:"enabled"`
	BaseURL               string                             `json:"baseUrl"`
	APIKey                string                             `json:"apiKey"`
	Model                 string                             `json:"model"`
	RequestTimeout        int                                `json:"requestTimeout"`
	RetryTimes            int                                `json:"retryTimes"`
	ReferenceImageMode    string                             `json:"referenceImageMode"`
	ReferenceImageBaseURL string                             `json:"referenceImageBaseUrl"`
	Providers             []externalResponsesProviderPayload `json:"providers"`
}

type externalResponsesReferenceTestRequest struct {
	ExternalResponses externalResponsesReferenceTestConfig `json:"externalResponses"`
}

type externalResponsesReferenceTestResponse struct {
	OK                    bool   `json:"ok"`
	Message               string `json:"message"`
	Latency               int64  `json:"latency"`
	ReferenceImageMode    string `json:"referenceImageMode"`
	ReferenceImagePreview string `json:"referenceImagePreview,omitempty"`
	Prompt                string `json:"prompt,omitempty"`
}

func (s *Server) handleExternalResponsesReferenceTest(w http.ResponseWriter, r *http.Request) {
	var req externalResponsesReferenceTestRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	provider, ok := firstExternalResponsesProviderFromTestConfig(req.ExternalResponses)
	if !ok {
		provider, ok = s.firstExternalResponsesProvider()
	}
	if !ok {
		writeJSON(w, http.StatusBadRequest, externalResponsesReferenceTestResponse{OK: false, Message: "external_responses 还未配置可用 provider", Latency: -1})
		return
	}

	mode := strings.TrimSpace(req.ExternalResponses.ReferenceImageMode)
	if mode == "" {
		mode = s.cfg.ExternalResponsesImageReferenceMode()
	}
	if !strings.EqualFold(mode, "base64") {
		mode = "url"
	} else {
		mode = "base64"
	}
	baseURL := strings.TrimSpace(req.ExternalResponses.ReferenceImageBaseURL)
	if baseURL == "" {
		baseURL = s.cfg.ExternalResponsesImageReferenceBaseURL()
	}

	imageRefs, err := s.buildExternalResponsesReferenceTestImages(mode, baseURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, externalResponsesReferenceTestResponse{OK: false, Message: err.Error(), Latency: -1, ReferenceImageMode: mode})
		return
	}

	started := time.Now()
	prompts, err := runImagePromptEnhanceModel(r, provider, imagePromptEnhanceRequest{
		Prompt:  externalResponsesReferenceTestPrompt,
		Mode:    "reference-test",
		Size:    "3840x2160",
		Quality: "high",
		Auto:    true,
	}, imageRefs)
	latency := time.Since(started).Milliseconds()
	preview := firstNonEmptyString(imageRefs)
	if strings.HasPrefix(preview, "data:image/") && len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	if err != nil {
		writeJSON(w, http.StatusOK, externalResponsesReferenceTestResponse{OK: false, Message: err.Error(), Latency: latency, ReferenceImageMode: mode, ReferenceImagePreview: preview})
		return
	}
	prompts = normalizeEnhancedPrompts(prompts)
	prompt := ""
	if len(prompts) > 0 {
		prompt = prompts[0]
	}
	writeJSON(w, http.StatusOK, externalResponsesReferenceTestResponse{OK: true, Message: "图片引用测试通过", Latency: latency, ReferenceImageMode: mode, ReferenceImagePreview: preview, Prompt: prompt})
}

func firstExternalResponsesProviderFromTestConfig(cfg externalResponsesReferenceTestConfig) (config.ExternalResponsesProviderConfig, bool) {
	for _, provider := range cfg.Providers {
		candidate := config.ExternalResponsesProviderConfig{
			ID:             strings.TrimSpace(provider.ID),
			Name:           strings.TrimSpace(provider.Name),
			Enabled:        provider.Enabled,
			BaseURL:        strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/"),
			APIKey:         strings.TrimSpace(provider.APIKey),
			Model:          strings.TrimSpace(provider.Model),
			RequestTimeout: provider.RequestTimeout,
		}
		if candidate.RequestTimeout <= 0 {
			candidate.RequestTimeout = cfg.RequestTimeout
		}
		if candidate.Enabled && candidate.BaseURL != "" && candidate.APIKey != "" && candidate.Model != "" {
			return candidate, true
		}
	}
	if cfg.Enabled && strings.TrimSpace(cfg.BaseURL) != "" && strings.TrimSpace(cfg.APIKey) != "" && strings.TrimSpace(cfg.Model) != "" {
		requestTimeout := cfg.RequestTimeout
		if requestTimeout <= 0 {
			requestTimeout = 300
		}
		return config.ExternalResponsesProviderConfig{
			ID:             "default",
			Name:           "Default",
			Enabled:        true,
			BaseURL:        strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
			APIKey:         strings.TrimSpace(cfg.APIKey),
			Model:          strings.TrimSpace(cfg.Model),
			RequestTimeout: requestTimeout,
		}, true
	}
	return config.ExternalResponsesProviderConfig{}, false
}

const externalResponsesReferenceTestPrompt = "图一为场景和角色站位图，图二为阿诚，图三为赵员外，图四为家丁甲，图五为家丁乙，图六为秋娘，图七为8个村民。一份用于生成专业影视导演示意图的描述，包括有叙事文字，是一张完整的电影导演分镜设计板。镜头主题包含16张镜号，必须严格按照提供的参考去出图，保持角色、场景一致性，全部画面统一色调，8K写实电影大片质感。主题是：说罢，赵员外拂袖和家丁甲而去，家丁乙扛着太师椅紧随其后，空地上8个村民有些人摇着头也渐渐远去，只留下在辣椒田里秋娘抱着死去的阿诚、碎了一地的油坛、滚烫的油锅、只剩下一些辣椒的箩筐、一片死寂的椒田，火红的辣椒在秋风中摇摆着，像一地未干的血。 Use the provided image inputs as source references."

var externalResponsesReferenceTestImageURLs = []string{
	"https://images-bak.chigege.cn:8888/v1/files/image/.thumbs/source-1-0d2d9ca928d3bd21afab4e81.jpg",
	"https://images-bak.chigege.cn:8888/v1/files/image/.thumbs/source-2-5bc2cd727b6fd09789f1d367.jpg",
	"https://images-bak.chigege.cn:8888/v1/files/image/.thumbs/source-3-5ded124e4523e0f4f76bb4e0.jpg",
	"https://images-bak.chigege.cn:8888/v1/files/image/.thumbs/source-4-cf6df921d72fa9745abefb96.jpg",
	"https://images-bak.chigege.cn:8888/v1/files/image/.thumbs/source-5-9876c3a7680c81c8c3ba301f.jpg",
	"https://images-bak.chigege.cn:8888/v1/files/image/.thumbs/source-6-6536b29b1a61c6b757b23610.jpg",
	"https://images-bak.chigege.cn:8888/v1/files/image/.thumbs/source-7-cc3347351b847dea37135bb7.jpg",
}

func (s *Server) buildExternalResponsesReferenceTestImages(mode, baseURL string) ([]string, error) {
	if strings.EqualFold(strings.TrimSpace(mode), "base64") {
		refs := make([]string, 0, len(externalResponsesReferenceTestImageURLs))
		for index, imageURL := range externalResponsesReferenceTestImageURLs {
			data, err := fetchExternalResponsesReferenceTestImage(imageURL)
			if err != nil {
				return nil, fmt.Errorf("fetch test image %d: %w", index+1, err)
			}
			ref, err := encodeJPEGReferenceImageDataURL(data)
			if err != nil {
				return nil, fmt.Errorf("encode test image %d: %w", index+1, err)
			}
			refs = append(refs, ref)
		}
		return refs, nil
	}

	baseURL = firstNonEmpty(strings.TrimRight(strings.TrimSpace(baseURL), "/"), externalResponsesPublicImageBaseURL)
	refs := make([]string, 0, len(externalResponsesReferenceTestImageURLs))
	for _, imageURL := range externalResponsesReferenceTestImageURLs {
		name, err := externalResponsesReferenceTestImageName(imageURL)
		if err != nil {
			return nil, err
		}
		refs = append(refs, absoluteImageFileURL(baseURL, name))
	}
	return refs, nil
}

func externalResponsesReferenceTestImageName(rawURL string) (string, error) {
	index := strings.Index(rawURL, "/v1/files/image/")
	if index < 0 {
		return "", fmt.Errorf("test image url missing /v1/files/image/: %s", rawURL)
	}
	name := rawURL[index+len("/v1/files/image/"):]
	if qmark := strings.Index(name, "?"); qmark >= 0 {
		name = name[:qmark]
	}
	name = strings.TrimLeft(strings.TrimSpace(name), "/")
	if name == "" {
		return "", fmt.Errorf("test image url has empty image name")
	}
	return name, nil
}

func fetchExternalResponsesReferenceTestImage(rawURL string) ([]byte, error) {
	resp, err := compatImageFetchClient.Get(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("returned %d", resp.StatusCode)
	}
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if contentType != "" && !strings.HasPrefix(contentType, "image/") {
		return nil, fmt.Errorf("returned non-image content-type %s", contentType)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, err
	}
	if err := validateTaskImageBytes(data, "reference test image"); err != nil {
		return nil, err
	}
	return data, nil
}

func firstNonEmptyString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
