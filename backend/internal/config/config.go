package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"chatgpt2api/internal/outboundproxy"

	"github.com/BurntSushi/toml"
)

const (
	exampleConfigFile = "config.example.toml"
	userConfigFile    = "config.toml"
	dataDirName       = "data"
)

var (
	osGetwd      = os.Getwd
	osExecutable = os.Executable
)

type Paths struct {
	Root     string
	Defaults string
	Override string
}

type AppConfig struct {
	Name            string `toml:"name"`
	Version         string `toml:"version"`
	APIKey          string `toml:"api_key"`
	AuthKey         string `toml:"auth_key"`
	ImageFormat     string `toml:"image_format"`
	MaxUploadSizeMB int    `toml:"max_upload_size_mb"`
}

type ServerConfig struct {
	Host                         string `toml:"host"`
	Port                         int    `toml:"port"`
	StaticDir                    string `toml:"static_dir"`
	MaxImageConcurrency          int    `toml:"max_image_concurrency"`
	ImageQueueLimit              int    `toml:"image_queue_limit"`
	ImageQueueTimeoutSeconds     int    `toml:"image_queue_timeout_seconds"`
	ImageTaskQueueTTLSeconds     int    `toml:"image_task_queue_ttl_seconds"`
	ImageDownloadRateKBPerSecond int    `toml:"image_download_rate_kbps"`
}

type ChatGPTConfig struct {
	Model                            string `toml:"model"`
	SSETimeout                       int    `toml:"sse_timeout"`
	PollInterval                     int    `toml:"poll_interval"`
	PollMaxWait                      int    `toml:"poll_max_wait"`
	RequestTimeout                   int    `toml:"request_timeout"`
	ImageMode                        string `toml:"image_mode"`
	FreeImageRoute                   string `toml:"free_image_route"`
	FreeImageModel                   string `toml:"free_image_model"`
	PaidImageRoute                   string `toml:"paid_image_route"`
	PaidImageModel                   string `toml:"paid_image_model"`
	StudioAllowDisabledImageAccounts bool   `toml:"studio_allow_disabled_image_accounts"`
	ImageAccountRetryTimes           int    `toml:"image_account_retry_times"`
	MaxReferenceImages               int    `toml:"max_reference_images"`
	ImageCommonSystemHint            string `toml:"image_common_system_hint"`
	ImagePrivateSystemHint           string `toml:"image_private_system_hint"`
	ImageSystemHint                  string `toml:"image_system_hint"`
}

type AccountsConfig struct {
	DefaultQuota                int  `toml:"default_quota"`
	PreferRemoteRefresh         bool `toml:"prefer_remote_refresh"`
	RefreshWorkers              int  `toml:"refresh_workers"`
	ImageQuotaRefreshTTLSeconds int  `toml:"image_quota_refresh_ttl_seconds"`
	DailyFreeImageLimit         int  `toml:"daily_free_image_limit"`
	DailyPaidImageLimit         int  `toml:"daily_paid_image_limit"`
}

type StorageConfig struct {
	Backend                  string `toml:"backend"`
	ConfigBackend            string `toml:"config_backend"`
	AuthDir                  string `toml:"auth_dir"`
	StateFile                string `toml:"state_file"`
	SyncStateDir             string `toml:"sync_state_dir"`
	ImageDir                 string `toml:"image_dir"`
	ImageStorage             string `toml:"image_storage"`
	ImageConversationStorage string `toml:"image_conversation_storage"`
	ImageDataStorage         string `toml:"image_data_storage"`
	SQLitePath               string `toml:"sqlite_path"`
	RedisAddr                string `toml:"redis_addr"`
	RedisPassword            string `toml:"redis_password"`
	RedisDB                  int    `toml:"redis_db"`
	RedisPrefix              string `toml:"redis_prefix"`
}

type SyncConfig struct {
	Enabled        bool   `toml:"enabled"`
	BaseURL        string `toml:"base_url"`
	ManagementKey  string `toml:"management_key"`
	RequestTimeout int    `toml:"request_timeout"`
	Concurrency    int    `toml:"concurrency"`
	ProviderType   string `toml:"provider_type"`
}

type LogConfig struct {
	LogAllRequests bool `toml:"log_all_requests"`
}

type ProxyConfig struct {
	Enabled     bool   `toml:"enabled"`
	URL         string `toml:"url"`
	Mode        string `toml:"mode"`
	SyncEnabled bool   `toml:"sync_enabled"`
}

type CPAConfig struct {
	BaseURL        string `toml:"base_url"`
	APIKey         string `toml:"api_key"`
	RequestTimeout int    `toml:"request_timeout"`
	RouteStrategy  string `toml:"route_strategy"`
}

type ExternalResponsesProviderConfig struct {
	ID             string `toml:"id"`
	Name           string `toml:"name"`
	Enabled        bool   `toml:"enabled"`
	BaseURL        string `toml:"base_url"`
	APIKey         string `toml:"api_key"`
	Model          string `toml:"model"`
	RequestTimeout int    `toml:"request_timeout"`
}

type ExternalResponsesConfig struct {
	Enabled               bool                              `toml:"enabled"`
	BaseURL               string                            `toml:"base_url"`
	APIKey                string                            `toml:"api_key"`
	Model                 string                            `toml:"model"`
	RequestTimeout        int                               `toml:"request_timeout"`
	RetryTimes            int                               `toml:"retry_times"`
	ReferenceImageMode    string                            `toml:"reference_image_mode"`
	ReferenceImageBaseURL string                            `toml:"reference_image_base_url"`
	Providers             []ExternalResponsesProviderConfig `toml:"providers"`
}

type NewAPIConfig struct {
	BaseURL        string `toml:"base_url"`
	Username       string `toml:"username"`
	Password       string `toml:"password"`
	AccessToken    string `toml:"access_token"`
	UserID         int    `toml:"user_id"`
	SessionCookie  string `toml:"session_cookie"`
	RequestTimeout int    `toml:"request_timeout"`
}

type Sub2APIConfig struct {
	BaseURL        string `toml:"base_url"`
	Email          string `toml:"email"`
	Password       string `toml:"password"`
	APIKey         string `toml:"api_key"`
	GroupID        string `toml:"group_id"`
	RequestTimeout int    `toml:"request_timeout"`
}

type Config struct {
	mu     sync.RWMutex `toml:"-"`
	loadMu sync.Mutex   `toml:"-"`
	loaded bool         `toml:"-"`
	paths  Paths        `toml:"-"`

	App               AppConfig               `toml:"app"`
	Server            ServerConfig            `toml:"server"`
	ChatGPT           ChatGPTConfig           `toml:"chatgpt"`
	Accounts          AccountsConfig          `toml:"accounts"`
	Storage           StorageConfig           `toml:"storage"`
	Sync              SyncConfig              `toml:"sync"`
	Log               LogConfig               `toml:"log"`
	Proxy             ProxyConfig             `toml:"proxy"`
	CPA               CPAConfig               `toml:"cpa"`
	ExternalResponses ExternalResponsesConfig `toml:"external_responses"`
	NewAPI            NewAPIConfig            `toml:"newapi"`
	Sub2API           Sub2APIConfig           `toml:"sub2api"`
}

func New(rootDir string) *Config {
	return &Config{paths: resolvePaths(rootDir)}
}

func (c *Config) Load() error {
	c.loadMu.Lock()
	defer c.loadMu.Unlock()

	next := &Config{paths: c.paths}

	if err := decodeDefaultTemplate(next); err != nil {
		return fmt.Errorf("decode embedded defaults: %w", err)
	}
	if fileExists(c.paths.Override) {
		_, _ = migrateLegacyOverrideFile(c.paths.Override)
		if err := decodeOverrideFile(c.paths.Override, next); err != nil {
			return fmt.Errorf("decode override: %w", err)
		}
	}
	if err := next.validate(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.copyFrom(next)
	c.loaded = true
	return nil
}

func (c *Config) EnsureLoaded() error {
	c.mu.RLock()
	loaded := c.loaded
	c.mu.RUnlock()
	if loaded {
		return nil
	}
	return c.Load()
}

func (c *Config) GetString(key string, fallback ...string) string {
	value, ok := c.lookup(key)
	if !ok {
		return stringFallback(fallback)
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return stringFallback(fallback)
	}
}

func (c *Config) GetInt(key string, fallback ...int) int {
	value, ok := c.lookup(key)
	if !ok {
		return intFallback(fallback)
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	default:
		return intFallback(fallback)
	}
}

func (c *Config) GetBool(key string, fallback ...bool) bool {
	value, ok := c.lookup(key)
	if !ok {
		return boolFallback(fallback)
	}
	typed, ok := value.(bool)
	if !ok {
		return boolFallback(fallback)
	}
	return typed
}

func (c *Config) Paths() Paths {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.paths
}

func (c *Config) RootDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.paths.Root
}

func (c *Config) ResolvePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return c.RootDir()
	}
	if filepath.IsAbs(trimmed) {
		return trimmed
	}
	return filepath.Join(c.RootDir(), trimmed)
}

func (c *Config) SaveOverride(section, key string, value any) error {
	return c.SaveOverrides(map[string]map[string]any{
		section: {
			key: value,
		},
	})
}

func (c *Config) SaveOverrides(values map[string]map[string]any) error {
	c.loadMu.Lock()
	defer c.loadMu.Unlock()

	raw, err := mergeOverrideValues(c.paths.Override, values)
	if err != nil {
		return err
	}

	if err := writeOverrideMap(c.paths.Override, raw); err != nil {
		return err
	}

	next := &Config{paths: c.paths}
	if err := decodeDefaultTemplate(next); err != nil {
		return fmt.Errorf("reload embedded defaults: %w", err)
	}
	if fileExists(c.paths.Override) {
		if err := decodeOverrideFile(c.paths.Override, next); err != nil {
			return fmt.Errorf("reload override: %w", err)
		}
	}
	if err := next.validate(); err != nil {
		return err
	}

	c.mu.Lock()
	c.copyFrom(next)
	c.loaded = true
	c.mu.Unlock()
	return nil
}

func (c *Config) PersistOverrideFile(values map[string]map[string]any) error {
	c.loadMu.Lock()
	defer c.loadMu.Unlock()

	raw, err := mergeOverrideValues(c.paths.Override, values)
	if err != nil {
		return err
	}
	return writeOverrideMap(c.paths.Override, raw)
}

func (c *Config) ApplyOverrides(values map[string]map[string]any) error {
	c.loadMu.Lock()
	defer c.loadMu.Unlock()

	raw := map[string]any{}
	for section, entries := range values {
		sectionMap := map[string]any{}
		for key, value := range entries {
			sectionMap[key] = sanitizeOverrideEntry(section, key, value)
		}
		raw[section] = sectionMap
	}
	sanitizeOverrideMap(raw)

	next := &Config{paths: c.paths}
	c.mu.RLock()
	next.copyFrom(c)
	next.paths = c.paths
	c.mu.RUnlock()

	if err := applyOverrideMap(reflect.ValueOf(next).Elem(), raw); err != nil {
		return err
	}
	if err := next.validate(); err != nil {
		return err
	}

	c.mu.Lock()
	c.copyFrom(next)
	c.loaded = true
	c.mu.Unlock()
	return nil
}

func mergeOverrideValues(path string, values map[string]map[string]any) (map[string]any, error) {
	raw := map[string]any{}
	if fileExists(path) {
		loaded, err := loadOverrideMap(path)
		if err != nil {
			return nil, fmt.Errorf("read override: %w", err)
		}
		raw = loaded
	}
	sanitizeOverrideMap(raw)

	for section, entries := range values {
		sec, ok := raw[section].(map[string]any)
		if !ok {
			sec = map[string]any{}
		}
		for key, value := range entries {
			sec[key] = sanitizeOverrideEntry(section, key, value)
		}
		raw[section] = sec
	}
	return raw, nil
}

func LoadDefaults(paths Paths) (*Config, error) {
	next := &Config{paths: paths}
	if err := decodeDefaultTemplate(next); err != nil {
		return nil, fmt.Errorf("decode embedded defaults: %w", err)
	}
	if err := next.validate(); err != nil {
		return nil, err
	}
	next.loaded = true
	return next, nil
}

func (c *Config) lookup(key string) (any, bool) {
	if err := c.EnsureLoaded(); err != nil {
		return nil, false
	}

	parts := strings.Split(key, ".")
	if len(parts) == 0 {
		return nil, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	current := reflect.ValueOf(c).Elem()
	for _, part := range parts {
		current = indirectValue(current)
		if !current.IsValid() || current.Kind() != reflect.Struct {
			return nil, false
		}
		next, ok := structFieldByTOMLTag(current, part)
		if !ok {
			return nil, false
		}
		current = next
	}

	current = indirectValue(current)
	if !current.IsValid() {
		return nil, false
	}
	return current.Interface(), true
}

func (c *Config) copyFrom(other *Config) {
	c.App = other.App
	c.Server = other.Server
	c.ChatGPT = other.ChatGPT
	c.Accounts = other.Accounts
	c.Storage = other.Storage
	c.Sync = other.Sync
	c.Log = other.Log
	c.Proxy = other.Proxy
	c.CPA = other.CPA
	c.ExternalResponses = other.ExternalResponses
	c.NewAPI = other.NewAPI
	c.Sub2API = other.Sub2API
	c.paths = other.paths
}

func (c *Config) ChatGPTProxyURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.proxyURLLocked(false)
}

func (c *Config) SyncProxyURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.proxyURLLocked(true)
}

func (c *Config) CPAImageBaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if trimmed := strings.TrimSpace(c.CPA.BaseURL); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(c.Sync.BaseURL)
}

func (c *Config) CPAImageAPIKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return strings.TrimSpace(c.CPA.APIKey)
}

func (c *Config) CPAImageConfigured() bool {
	return c.CPAImageBaseURL() != "" && c.CPAImageAPIKey() != ""
}

func (c *Config) CPAImageRequestTimeout() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.CPA.RequestTimeout > 0 {
		return c.CPA.RequestTimeout
	}
	return 60
}

func (c *Config) CPAImageRouteStrategy() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return normalizeCPAImageRouteStrategy(c.CPA.RouteStrategy)
}

func (c *Config) ExternalResponsesConfig() ExternalResponsesConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ExternalResponses
}

func (c *Config) ExternalResponsesProviders() []ExternalResponsesProviderConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return externalResponsesProviders(c.ExternalResponses)
}

func (c *Config) ExternalResponsesRetryTimes() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	retries := c.ExternalResponses.RetryTimes
	if retries <= 0 {
		retries = 3
	}
	if retries > 10 {
		retries = 10
	}
	return retries
}

func (c *Config) ExternalResponsesImageReferenceMode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return normalizeExternalResponsesReferenceImageMode(c.ExternalResponses.ReferenceImageMode)
}

func (c *Config) ExternalResponsesImageReferenceBaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return strings.TrimRight(strings.TrimSpace(c.ExternalResponses.ReferenceImageBaseURL), "/")
}

func (c *Config) ImageQueueConfig() (int, int, time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	maxImageConcurrency := c.Server.MaxImageConcurrency
	if maxImageConcurrency <= 0 {
		maxImageConcurrency = 8
	}
	imageQueueLimit := c.Server.ImageQueueLimit
	if imageQueueLimit <= 0 {
		imageQueueLimit = 32
	}
	timeoutSeconds := c.Server.ImageQueueTimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 20
	}
	return maxImageConcurrency, imageQueueLimit, time.Duration(timeoutSeconds) * time.Second
}

func (c *Config) ImageTaskQueueTTL() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ttlSeconds := c.Server.ImageTaskQueueTTLSeconds
	if ttlSeconds <= 0 {
		ttlSeconds = 600
	}
	return time.Duration(ttlSeconds) * time.Second
}

func (c *Config) ImageQuotaRefreshTTL() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ttlSeconds := c.Accounts.ImageQuotaRefreshTTLSeconds
	if ttlSeconds <= 0 {
		ttlSeconds = 120
	}
	return time.Duration(ttlSeconds) * time.Second
}

func (c *Config) ImageAccountRetryTimes() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	retries := c.ChatGPT.ImageAccountRetryTimes
	if retries < 0 {
		retries = 0
	}
	if retries > 10 {
		retries = 10
	}
	return retries
}

func (c *Config) MaxReferenceImages() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return normalizeMaxReferenceImages(c.ChatGPT.MaxReferenceImages)
}

func (c *Config) ImageCommonSystemHint() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return strings.TrimSpace(c.ChatGPT.ImageCommonSystemHint)
}

func (c *Config) ImagePrivateSystemHint() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if hint := strings.TrimSpace(c.ChatGPT.ImagePrivateSystemHint); hint != "" {
		return hint
	}
	return strings.TrimSpace(c.ChatGPT.ImageSystemHint)
}

func (c *Config) ImageSystemHint() string {
	return c.ImagePrivateSystemHint()
}

func (c *Config) proxyURLLocked(forSync bool) string {
	if !c.Proxy.Enabled {
		return ""
	}
	if forSync && !c.Proxy.SyncEnabled {
		return ""
	}
	if normalizeProxyMode(c.Proxy.Mode) != "fixed" {
		return ""
	}
	return strings.TrimSpace(c.Proxy.URL)
}

func (c *Config) validate() error {
	if c.Server.MaxImageConcurrency <= 0 {
		c.Server.MaxImageConcurrency = 8
	}
	if c.Server.ImageQueueLimit <= 0 {
		c.Server.ImageQueueLimit = 32
	}
	if c.Server.ImageQueueTimeoutSeconds <= 0 {
		c.Server.ImageQueueTimeoutSeconds = 20
	}
	if c.Server.ImageTaskQueueTTLSeconds <= 0 {
		c.Server.ImageTaskQueueTTLSeconds = 600
	}
	if c.Accounts.ImageQuotaRefreshTTLSeconds <= 0 {
		c.Accounts.ImageQuotaRefreshTTLSeconds = 120
	}
	if c.Accounts.DailyFreeImageLimit <= 0 {
		c.Accounts.DailyFreeImageLimit = 120
	}
	if c.Accounts.DailyPaidImageLimit <= 0 {
		c.Accounts.DailyPaidImageLimit = 30
	}
	if c.ChatGPT.ImageAccountRetryTimes < 0 {
		c.ChatGPT.ImageAccountRetryTimes = 0
	}
	if c.ChatGPT.ImageAccountRetryTimes > 10 {
		c.ChatGPT.ImageAccountRetryTimes = 10
	}
	c.ChatGPT.MaxReferenceImages = normalizeMaxReferenceImages(c.ChatGPT.MaxReferenceImages)

	if normalized, ok := normalizeImageMode(c.ChatGPT.ImageMode); !ok {
		return fmt.Errorf("invalid chatgpt.image_mode %q: only studio or cpa are supported", strings.TrimSpace(c.ChatGPT.ImageMode))
	} else {
		c.ChatGPT.ImageMode = normalized
	}

	if normalized, ok := normalizeImageRoute(c.ChatGPT.FreeImageRoute); !ok {
		return fmt.Errorf("invalid chatgpt.free_image_route %q: only responses is supported", strings.TrimSpace(c.ChatGPT.FreeImageRoute))
	} else if normalized == "" {
		return fmt.Errorf("invalid chatgpt.free_image_route %q", strings.TrimSpace(c.ChatGPT.FreeImageRoute))
	} else {
		c.ChatGPT.FreeImageRoute = normalized
	}
	if normalized, ok := normalizeImageRoute(c.ChatGPT.PaidImageRoute); !ok {
		return fmt.Errorf("invalid chatgpt.paid_image_route %q: only responses is supported", strings.TrimSpace(c.ChatGPT.PaidImageRoute))
	} else if normalized == "" {
		return fmt.Errorf("invalid chatgpt.paid_image_route %q", strings.TrimSpace(c.ChatGPT.PaidImageRoute))
	} else {
		c.ChatGPT.PaidImageRoute = normalized
	}
	c.ChatGPT.FreeImageModel = normalizeConfiguredImageRouteModel(c.ChatGPT.FreeImageRoute, c.ChatGPT.FreeImageModel, "auto", true)
	c.ChatGPT.PaidImageModel = normalizeConfiguredImageRouteModel(c.ChatGPT.PaidImageRoute, c.ChatGPT.PaidImageModel, "gpt-5.4-mini", false)

	c.CPA.RouteStrategy = normalizeCPAImageRouteStrategy(c.CPA.RouteStrategy)
	if err := normalizeExternalResponsesConfig(&c.ExternalResponses); err != nil {
		return err
	}
	c.Storage.Backend = normalizeStorageBackend(c.Storage.Backend)
	c.Storage.ConfigBackend = normalizeConfigBackend(c.Storage.ConfigBackend)
	legacyImageStorage := strings.ToLower(strings.TrimSpace(c.Storage.ImageStorage))
	if strings.TrimSpace(c.Storage.ImageConversationStorage) == "" && legacyImageStorage != "" {
		c.Storage.ImageConversationStorage = legacyImageStorage
	}
	if strings.TrimSpace(c.Storage.ImageDataStorage) == "" && legacyImageStorage != "" {
		c.Storage.ImageDataStorage = legacyImageStorage
	}
	c.Storage.ImageConversationStorage = normalizeImageStorage(c.Storage.ImageConversationStorage)
	c.Storage.ImageDataStorage = normalizeImageStorage(c.Storage.ImageDataStorage)
	if c.Storage.ImageConversationStorage != c.Storage.ImageDataStorage {
		c.Storage.ImageDataStorage = c.Storage.ImageConversationStorage
	}
	c.Storage.ImageStorage = c.Storage.ImageConversationStorage

	if !c.Proxy.Enabled {
		return nil
	}

	if normalizeProxyMode(c.Proxy.Mode) != "fixed" {
		return fmt.Errorf("unsupported proxy.mode %q: only fixed is supported", strings.TrimSpace(c.Proxy.Mode))
	}

	if strings.TrimSpace(c.Proxy.URL) == "" {
		return fmt.Errorf("proxy.url is required when proxy.enabled = true")
	}

	if err := outboundproxy.Validate(c.Proxy.URL); err != nil {
		return fmt.Errorf("invalid proxy.url: %w", err)
	}

	return nil
}

func normalizeProxyMode(mode string) string {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" {
		return "fixed"
	}
	return normalized
}

func normalizeImageRoute(route string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(route)) {
	case "", "legacy", "conversation", "responses", "external_responses":
		return "responses", true
	default:
		return "", false
	}
}

func normalizeConfiguredImageRouteModel(route, value, fallback string, allowAuto bool) string {
	model := strings.ToLower(strings.TrimSpace(value))
	if model == "" {
		return fallback
	}
	if allowAuto && model == "auto" {
		return "auto"
	}
	switch model {
	case "gpt-5.4-mini", "gpt-5.4", "gpt-5.5", "gpt-5-5-thinking":
		return model
	case "gpt-image-1", "gpt-image-2":
		if normalizedRoute, _ := normalizeImageRoute(route); normalizedRoute != "responses" {
			return model
		}
	}
	return fallback
}

func normalizeImageMode(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "studio", "mix":
		return "studio", true
	case "cpa":
		return "cpa", true
	default:
		return "", false
	}
}

func normalizeMaxReferenceImages(value int) int {
	if value <= 0 {
		return 4
	}
	if value > 20 {
		return 20
	}
	return value
}

func normalizeStorageBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "current", "local":
		return "current"
	case "sqlite":
		return "sqlite"
	case "redis":
		return "redis"
	default:
		return "current"
	}
}

func normalizeConfigBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "redis":
		return "redis"
	default:
		return "file"
	}
}

func normalizeImageStorage(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "server":
		return "server"
	default:
		return "browser"
	}
}

func normalizeCPAImageRouteStrategy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "images_api"
	case "images_api":
		return "images_api"
	case "codex_responses":
		return "codex_responses"
	case "auto":
		return "auto"
	default:
		return "images_api"
	}
}

func NormalizeImageModeForAPI(mode string) (string, bool) {
	return normalizeImageMode(mode)
}

func normalizeExternalResponsesConfig(cfg *ExternalResponsesConfig) error {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.ReferenceImageMode = normalizeExternalResponsesReferenceImageMode(cfg.ReferenceImageMode)
	cfg.ReferenceImageBaseURL = strings.TrimRight(strings.TrimSpace(cfg.ReferenceImageBaseURL), "/")
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 300
	}
	if cfg.RetryTimes <= 0 {
		cfg.RetryTimes = 3
	}
	if cfg.RetryTimes > 10 {
		cfg.RetryTimes = 10
	}

	seen := map[string]struct{}{}
	for index := range cfg.Providers {
		provider := &cfg.Providers[index]
		provider.ID = strings.TrimSpace(provider.ID)
		provider.Name = strings.TrimSpace(provider.Name)
		provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
		provider.APIKey = strings.TrimSpace(provider.APIKey)
		provider.Model = strings.TrimSpace(provider.Model)
		if provider.RequestTimeout <= 0 {
			provider.RequestTimeout = cfg.RequestTimeout
		}
		if provider.ID == "" {
			return fmt.Errorf("external_responses.providers[%d].id is required", index)
		}
		if _, ok := seen[provider.ID]; ok {
			return fmt.Errorf("external_responses.providers[%d].id %q is duplicated", index, provider.ID)
		}
		seen[provider.ID] = struct{}{}
		if provider.Enabled {
			if provider.BaseURL == "" {
				return fmt.Errorf("external_responses.providers[%d].base_url is required when provider is enabled", index)
			}
			if provider.APIKey == "" {
				return fmt.Errorf("external_responses.providers[%d].api_key is required when provider is enabled", index)
			}
			if provider.Model == "" {
				return fmt.Errorf("external_responses.providers[%d].model is required when provider is enabled", index)
			}
		}
	}

	if cfg.Enabled && len(cfg.Providers) == 0 {
		if cfg.BaseURL == "" {
			return fmt.Errorf("external_responses.base_url is required when external_responses.enabled = true")
		}
		if cfg.APIKey == "" {
			return fmt.Errorf("external_responses.api_key is required when external_responses.enabled = true")
		}
		if cfg.Model == "" {
			return fmt.Errorf("external_responses.model is required when external_responses.enabled = true")
		}
	}
	return nil
}

func normalizeExternalResponsesReferenceImageMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "base64", "data_url", "data-url", "dataurl":
		return "base64"
	default:
		return "url"
	}
}

func externalResponsesProviders(cfg ExternalResponsesConfig) []ExternalResponsesProviderConfig {
	providers := make([]ExternalResponsesProviderConfig, 0, len(cfg.Providers))
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
		return []ExternalResponsesProviderConfig{{
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

func decodeOverrideFile(path string, target *Config) error {
	raw, err := loadOverrideMap(path)
	if err != nil {
		return err
	}
	sanitizeOverrideMap(raw)
	return applyOverrideMap(reflect.ValueOf(target).Elem(), raw)
}

func loadOverrideMap(path string) (map[string]any, error) {
	raw := map[string]any{}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func migrateLegacyOverrideFile(path string) (bool, error) {
	if !fileExists(path) {
		return false, nil
	}

	raw, err := loadOverrideMap(path)
	if err != nil {
		return false, err
	}
	if !sanitizeOverrideMap(raw) {
		return false, nil
	}
	if err := writeOverrideMap(path, raw); err != nil {
		return false, err
	}
	return true, nil
}

func sanitizeOverrideMap(raw map[string]any) bool {
	if raw == nil {
		return false
	}

	changed := false
	chatgptSection, ok := raw["chatgpt"].(map[string]any)
	if !ok {
		return false
	}
	if value, ok := chatgptSection["image_mode"]; ok {
		sanitized := sanitizeOverrideEntry("chatgpt", "image_mode", value)
		if !reflect.DeepEqual(sanitized, value) {
			chatgptSection["image_mode"] = sanitized
			changed = true
		}
	}
	return changed
}

func sanitizeOverrideEntry(section, key string, value any) any {
	if section != "chatgpt" || key != "image_mode" {
		return value
	}
	text, ok := value.(string)
	if !ok {
		return value
	}
	if normalized, ok := normalizeImageMode(text); ok {
		return normalized
	}
	return value
}

func writeOverrideMap(path string, raw map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create override file: %w", err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(raw); err != nil {
		return fmt.Errorf("encode override: %w", err)
	}
	return nil
}

func decodeDefaultTemplate(target *Config) error {
	_, err := toml.Decode(defaultConfigTemplate, target)
	return err
}

func applyOverrideMap(dst reflect.Value, raw map[string]any) error {
	for key, value := range raw {
		field, ok := structFieldByTOMLTag(dst, key)
		if !ok {
			continue
		}
		if err := setOverrideValue(field, value); err != nil {
			return err
		}
	}
	return nil
}

func setOverrideValue(dst reflect.Value, raw any) error {
	if !dst.CanSet() {
		return nil
	}
	dst = indirectValue(dst)
	if !dst.IsValid() {
		return nil
	}

	switch dst.Kind() {
	case reflect.Struct:
		nested, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("expected table, got %T", raw)
		}
		return applyOverrideMap(dst, nested)
	case reflect.Slice:
		value := reflect.ValueOf(raw)
		if value.IsValid() && value.Type().AssignableTo(dst.Type()) {
			dst.Set(value)
			return nil
		}
		items, ok := raw.([]map[string]any)
		if !ok {
			return fmt.Errorf("expected array table, got %T", raw)
		}
		slice := reflect.MakeSlice(dst.Type(), 0, len(items))
		for _, item := range items {
			elem := reflect.New(dst.Type().Elem()).Elem()
			if err := setOverrideValue(elem, item); err != nil {
				return err
			}
			slice = reflect.Append(slice, elem)
		}
		dst.Set(slice)
	case reflect.String:
		text, ok := raw.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", raw)
		}
		dst.SetString(text)
	case reflect.Bool:
		flag, ok := raw.(bool)
		if !ok {
			return fmt.Errorf("expected bool, got %T", raw)
		}
		dst.SetBool(flag)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch n := raw.(type) {
		case int64:
			dst.SetInt(n)
		case int:
			dst.SetInt(int64(n))
		case float64:
			dst.SetInt(int64(n))
		default:
			return fmt.Errorf("expected int, got %T", raw)
		}
	default:
		value := reflect.ValueOf(raw)
		if value.IsValid() && value.Type().AssignableTo(dst.Type()) {
			dst.Set(value)
			return nil
		}
		return fmt.Errorf("unsupported type %s", dst.Type())
	}
	return nil
}

func structFieldByTOMLTag(value reflect.Value, part string) (reflect.Value, bool) {
	valueType := value.Type()
	for i := 0; i < value.NumField(); i++ {
		fieldType := valueType.Field(i)
		if !fieldType.IsExported() {
			continue
		}
		tag := strings.Split(fieldType.Tag.Get("toml"), ",")[0]
		if tag == "-" {
			continue
		}
		if tag == "" {
			tag = strings.ToLower(fieldType.Name)
		}
		if tag == part {
			return value.Field(i), true
		}
	}
	return reflect.Value{}, false
}

func indirectValue(value reflect.Value) reflect.Value {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

func resolvePaths(rootDir string) Paths {
	root := normalizeRoot(rootDir)
	return Paths{
		Root:     root,
		Defaults: filepath.Join(root, dataDirName, exampleConfigFile),
		Override: filepath.Join(root, dataDirName, userConfigFile),
	}
}

func normalizeRoot(rootDir string) string {
	if rootDir != "" {
		return rootDir
	}
	if exePath, err := osExecutable(); err == nil {
		exeDir := filepath.Dir(exePath)
		if detected := detectConfigRoot(exeDir); detected != "" {
			return detected
		}
	}
	if cwd, err := osGetwd(); err == nil {
		if detected := detectConfigRoot(cwd); detected != "" {
			return detected
		}
	}
	if exePath, err := osExecutable(); err == nil {
		if exeDir := filepath.Dir(exePath); exeDir != "" {
			return exeDir
		}
	}
	if cwd, err := osGetwd(); err == nil {
		return cwd
	}
	return "."
}

func detectConfigRoot(startDir string) string {
	dir := startDir
	for {
		// Prefer a local config root when running from backend itself or from a release package.
		if hasConfigMarker(dir) {
			return dir
		}
		// Backward compatibility: older layout placed defaults in backend/config.defaults.toml.
		if fileExists(filepath.Join(dir, "config.defaults.toml")) {
			return dir
		}
		// Support running from repo root (or any subdir) by locating backend/data config files.
		backendDir := filepath.Join(dir, "backend")
		if hasConfigMarker(backendDir) {
			return backendDir
		}
		// Backward compatibility: older layout placed defaults in backend/config.defaults.toml.
		if fileExists(filepath.Join(backendDir, "config.defaults.toml")) {
			return backendDir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func hasConfigMarker(root string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	dataDir := filepath.Join(root, dataDirName)
	return fileExists(filepath.Join(dataDir, userConfigFile)) ||
		fileExists(filepath.Join(dataDir, exampleConfigFile)) ||
		fileExists(filepath.Join(dataDir, "config.defaults.toml")) ||
		fileExists(filepath.Join(root, "internal", "config", "config.defaults.toml"))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func stringFallback(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func intFallback(values []int) int {
	if len(values) > 0 {
		return values[0]
	}
	return 0
}

func boolFallback(values []bool) bool {
	if len(values) > 0 {
		return values[0]
	}
	return false
}
