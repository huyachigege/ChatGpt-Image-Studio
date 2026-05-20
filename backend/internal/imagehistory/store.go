package imagehistory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"chatgpt2api/internal/config"

	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

const (
	defaultHistoryDir = "data/image_history"
	defaultAssetMIME  = "image/png"
)

type SourceImage struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Name    string `json:"name"`
	DataURL string `json:"dataUrl,omitempty"`
	URL     string `json:"url,omitempty"`
}

type Image struct {
	ID              string `json:"id"`
	Status          string `json:"status,omitempty"`
	B64JSON         string `json:"b64_json,omitempty"`
	URL             string `json:"url,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
	RevisedPrompt   string `json:"revised_prompt,omitempty"`
	FileID          string `json:"file_id,omitempty"`
	GenID           string `json:"gen_id,omitempty"`
	ConversationID  string `json:"conversation_id,omitempty"`
	ParentMessageID string `json:"parent_message_id,omitempty"`
	ResponseID      string `json:"response_id,omitempty"`
	SourceAccountID string `json:"source_account_id,omitempty"`
	Error           string `json:"error,omitempty"`
}

type Turn struct {
	ID                  string          `json:"id"`
	Title               string          `json:"title"`
	Mode                string          `json:"mode"`
	Prompt              string          `json:"prompt"`
	Model               string          `json:"model"`
	Count               int             `json:"count"`
	Size                string          `json:"size,omitempty"`
	Quality             string          `json:"quality,omitempty"`
	Scale               string          `json:"scale,omitempty"`
	SourceImages        []SourceImage   `json:"sourceImages,omitempty"`
	Images              []Image         `json:"images"`
	CreatedAt           string          `json:"createdAt"`
	Status              string          `json:"status"`
	Error               string          `json:"error,omitempty"`
	Diagnostic          json.RawMessage `json:"diagnostic,omitempty"`
	DiagnosticStatus    string          `json:"diagnosticStatus,omitempty"`
	DiagnosticError     string          `json:"diagnosticError,omitempty"`
	DiagnosticUpdatedAt string          `json:"diagnosticUpdatedAt,omitempty"`
}

type Conversation struct {
	ID           string        `json:"id"`
	UserID       string        `json:"userId,omitempty"`
	Title        string        `json:"title"`
	Mode         string        `json:"mode"`
	Prompt       string        `json:"prompt"`
	Model        string        `json:"model"`
	Count        int           `json:"count"`
	Size         string        `json:"size,omitempty"`
	Quality      string        `json:"quality,omitempty"`
	Scale        string        `json:"scale,omitempty"`
	SourceImages []SourceImage `json:"sourceImages,omitempty"`
	Images       []Image       `json:"images"`
	CreatedAt    string        `json:"createdAt"`
	Status       string        `json:"status"`
	Error        string        `json:"error,omitempty"`
	Turns        []Turn        `json:"turns,omitempty"`
}

type Store struct {
	backend  backend
	imageDir string
	userID   string
}

type backend interface {
	Init() error
	Close() error
	List(ctx context.Context) ([]Conversation, error)
	Get(ctx context.Context, id string) (*Conversation, error)
	Save(ctx context.Context, conversation Conversation) error
	Delete(ctx context.Context, id string) error
	Clear(ctx context.Context) error
}

func NewStore(cfg *config.Config) (*Store, error) {
	return NewStoreForUser(cfg, "")
}

func NewStoreForUser(cfg *config.Config, userID string) (*Store, error) {
	cleanedUserID := sanitizeUserID(userID)
	imageDir := cfg.ResolvePath(cfg.Storage.ImageDir)
	if cleanedUserID != "" {
		imageDir = filepath.Join(imageDir, cleanedUserID)
	}
	var storage backend
	switch strings.ToLower(strings.TrimSpace(cfg.Storage.Backend)) {
	case "sqlite":
		storage = &sqliteBackend{path: cfg.ResolvePath(cfg.Storage.SQLitePath)}
	case "redis":
		storage = &redisBackend{
			client: redis.NewClient(&redis.Options{
				Addr:     strings.TrimSpace(cfg.Storage.RedisAddr),
				Password: cfg.Storage.RedisPassword,
				DB:       cfg.Storage.RedisDB,
			}),
			prefix: firstNonEmpty(cfg.Storage.RedisPrefix, "chatgpt2api:studio") + ":image_history",
		}
	default:
		storage = &fileBackend{dir: cfg.ResolvePath(defaultHistoryDir)}
	}
	if err := storage.Init(); err != nil {
		_ = storage.Close()
		return nil, err
	}
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		_ = storage.Close()
		return nil, err
	}
	return &Store{backend: storage, imageDir: imageDir, userID: cleanedUserID}, nil
}

func (s *Store) Close() error {
	if s == nil || s.backend == nil {
		return nil
	}
	return s.backend.Close()
}

func (s *Store) List(ctx context.Context) ([]Conversation, error) {
	items, err := s.backend.List(ctx)
	if err != nil {
		return nil, err
	}
	if s.userID != "" {
		filtered := make([]Conversation, 0, len(items))
		for _, item := range items {
			if sanitizeUserID(item.UserID) == s.userID {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	sortConversations(items)
	return items, nil
}

func (s *Store) Get(ctx context.Context, id string) (*Conversation, error) {
	item, err := s.backend.Get(ctx, cleanID(id))
	if err != nil || item == nil {
		return item, err
	}
	if s.userID != "" && sanitizeUserID(item.UserID) != s.userID {
		return nil, nil
	}
	return item, nil
}

func (s *Store) Save(ctx context.Context, conversation Conversation) (*Conversation, error) {
	normalized, err := s.normalizeConversation(conversation)
	if err != nil {
		return nil, err
	}
	if err := s.backend.Save(ctx, normalized); err != nil {
		return nil, err
	}
	return &normalized, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	current, err := s.backend.Get(ctx, cleanID(id))
	if err != nil || current == nil {
		return err
	}
	candidateFiles := collectConversationImageFiles(*current)
	if err := s.backend.Delete(ctx, cleanID(id)); err != nil {
		return err
	}
	return s.cleanupCandidateFiles(ctx, candidateFiles)
}

func (s *Store) Clear(ctx context.Context) error {
	items, err := s.backend.List(ctx)
	if err != nil {
		return err
	}
	candidateFiles := map[string]struct{}{}
	for _, item := range items {
		mergeFileSets(candidateFiles, collectConversationImageFiles(item))
	}
	if err := s.backend.Clear(ctx); err != nil {
		return err
	}
	return s.cleanupCandidateFiles(ctx, candidateFiles)
}

func (s *Store) normalizeConversation(conversation Conversation) (Conversation, error) {
	conversation.ID = cleanID(conversation.ID)
	if s.userID != "" {
		conversation.UserID = s.userID
	}
	if conversation.ID == "" {
		return Conversation{}, fmt.Errorf("conversation id is required")
	}
	if conversation.CreatedAt == "" {
		conversation.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if len(conversation.Turns) == 0 {
		conversation.Turns = []Turn{{
			ID:           conversation.ID + "-legacy",
			Title:        conversation.Title,
			Mode:         conversation.Mode,
			Prompt:       conversation.Prompt,
			Model:        conversation.Model,
			Count:        conversation.Count,
			Size:         conversation.Size,
			Quality:      conversation.Quality,
			Scale:        conversation.Scale,
			SourceImages: conversation.SourceImages,
			Images:       conversation.Images,
			CreatedAt:    conversation.CreatedAt,
			Status:       conversation.Status,
			Error:        conversation.Error,
		}}
	}
	for turnIndex := range conversation.Turns {
		turn := &conversation.Turns[turnIndex]
		if turn.ID == "" {
			turn.ID = fmt.Sprintf("%s-%d", conversation.ID, turnIndex)
		}
		if turn.CreatedAt == "" {
			turn.CreatedAt = conversation.CreatedAt
		}
		for sourceIndex := range turn.SourceImages {
			source := &turn.SourceImages[sourceIndex]
			if source.ID == "" {
				source.ID = fmt.Sprintf("%s-source-%d", turn.ID, sourceIndex)
			}
			if source.URL == "" && strings.TrimSpace(source.DataURL) != "" {
				url, err := s.saveDataURLAsset(source.DataURL, "source", source.Name)
				if err != nil {
					return Conversation{}, err
				}
				source.URL = url
				source.DataURL = ""
			}
		}
		for imageIndex := range turn.Images {
			image := &turn.Images[imageIndex]
			if image.Prompt == "" {
				image.Prompt = turn.Prompt
			}
			if image.ID == "" {
				image.ID = fmt.Sprintf("%s-image-%d", turn.ID, imageIndex)
			}
			if image.URL == "" && strings.TrimSpace(image.B64JSON) != "" {
				url, err := s.saveBase64Asset(image.B64JSON, "result", defaultAssetMIME)
				if err != nil {
					return Conversation{}, err
				}
				image.URL = url
				image.B64JSON = ""
			}
			if image.Status == "" {
				if image.URL != "" {
					image.Status = "success"
				} else {
					image.Status = "loading"
				}
			}
		}
	}
	latest := conversation.Turns[len(conversation.Turns)-1]
	conversation.Title = latest.Title
	conversation.Mode = latest.Mode
	conversation.Prompt = latest.Prompt
	conversation.Model = latest.Model
	conversation.Count = latest.Count
	conversation.Size = latest.Size
	conversation.Quality = latest.Quality
	conversation.Scale = latest.Scale
	conversation.SourceImages = latest.SourceImages
	conversation.Images = latest.Images
	conversation.CreatedAt = latest.CreatedAt
	conversation.Status = latest.Status
	conversation.Error = latest.Error
	return conversation, nil
}

func (s *Store) saveDataURLAsset(raw, kind, name string) (string, error) {
	payload, mimeType, err := decodeDataURL(raw)
	if err != nil {
		return "", err
	}
	return s.saveAsset(payload, kind, firstNonEmpty(mimeType, mime.TypeByExtension(filepath.Ext(name)), defaultAssetMIME))
}

func (s *Store) saveBase64Asset(raw, kind, mimeType string) (string, error) {
	payload, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("decode image: %w", err)
	}
	return s.saveAsset(payload, kind, firstNonEmpty(mimeType, defaultAssetMIME))
}

func (s *Store) saveAsset(payload []byte, kind, mimeType string) (string, error) {
	if len(payload) == 0 {
		return "", fmt.Errorf("image is empty")
	}
	sum := sha256.Sum256(payload)
	ext := extensionForMIME(mimeType)
	filename := fmt.Sprintf("%s-%x%s", sanitizeKind(kind), sum[:16], ext)
	path := filepath.Join(s.imageDir, filename)
	if _, err := os.Stat(path); err == nil {
		return "/v1/files/image/" + filename, nil
	}
	if err := os.MkdirAll(s.imageDir, 0o755); err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if s.userID != "" {
		return "/v1/files/image/" + s.userID + "/" + filename, nil
	}
	return "/v1/files/image/" + filename, nil
}

func decodeDataURL(raw string) ([]byte, string, error) {
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return nil, "", fmt.Errorf("invalid data url")
	}
	meta := raw[:comma]
	if !strings.Contains(strings.ToLower(meta), ";base64") {
		return nil, "", fmt.Errorf("only base64 data urls are supported")
	}
	mimeType := strings.TrimPrefix(strings.Split(meta, ";")[0], "data:")
	payload, err := base64.StdEncoding.DecodeString(raw[comma+1:])
	if err != nil {
		return nil, "", fmt.Errorf("decode data url: %w", err)
	}
	return payload, mimeType, nil
}

func extensionForMIME(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func sanitizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "source", "mask", "result":
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return "image"
	}
}

func collectConversationImageFiles(conversation Conversation) map[string]struct{} {
	files := map[string]struct{}{}
	collectSourceFiles := func(items []SourceImage) {
		for _, item := range items {
			if filename := filenameFromImageURL(item.URL); filename != "" {
				files[filename] = struct{}{}
			}
		}
	}
	collectResultFiles := func(items []Image) {
		for _, item := range items {
			if filename := filenameFromImageURL(item.URL); filename != "" {
				files[filename] = struct{}{}
			}
		}
	}
	collectSourceFiles(conversation.SourceImages)
	collectResultFiles(conversation.Images)
	for _, turn := range conversation.Turns {
		collectSourceFiles(turn.SourceImages)
		collectResultFiles(turn.Images)
	}
	return files
}

func filenameFromImageURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	index := strings.LastIndex(trimmed, "/v1/files/image/")
	if index >= 0 {
		return filepath.Base(trimmed[index+len("/v1/files/image/"):])
	}
	return filepath.Base(trimmed)
}

func mergeFileSets(target map[string]struct{}, source map[string]struct{}) {
	for key := range source {
		target[key] = struct{}{}
	}
}

func (s *Store) cleanupCandidateFiles(ctx context.Context, candidates map[string]struct{}) error {
	if len(candidates) == 0 {
		return nil
	}
	remainingItems, err := s.backend.List(ctx)
	if err != nil {
		return err
	}
	stillReferenced := map[string]struct{}{}
	for _, item := range remainingItems {
		mergeFileSets(stillReferenced, collectConversationImageFiles(item))
	}
	for filename := range candidates {
		if _, exists := stillReferenced[filename]; exists {
			continue
		}
		path := filepath.Join(s.imageDir, filepath.Base(filename))
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func sortConversations(items []Conversation) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
}

func cleanID(id string) string {
	return strings.ReplaceAll(strings.TrimSpace(id), "/", "-")
}

func sanitizeUserID(userID string) string {
	cleaned := strings.TrimSpace(userID)
	cleaned = strings.ReplaceAll(cleaned, "/", "-")
	cleaned = strings.ReplaceAll(cleaned, "\\", "-")
	return cleaned
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

type fileBackend struct {
	dir string
}

func (b *fileBackend) Init() error {
	return os.MkdirAll(b.dir, 0o755)
}

func (b *fileBackend) Close() error {
	return nil
}

func (b *fileBackend) List(ctx context.Context) ([]Conversation, error) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Conversation{}, nil
		}
		return nil, err
	}
	result := make([]Conversation, 0, len(entries))
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		conversation, err := b.read(filepath.Join(b.dir, entry.Name()))
		if err != nil {
			continue
		}
		result = append(result, conversation)
	}
	return result, nil
}

func (b *fileBackend) Get(_ context.Context, id string) (*Conversation, error) {
	conversation, err := b.read(b.path(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return &conversation, nil
}

func (b *fileBackend) Save(_ context.Context, conversation Conversation) error {
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(conversation, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := b.path(conversation.ID) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, b.path(conversation.ID))
}

func (b *fileBackend) Delete(_ context.Context, id string) error {
	err := os.Remove(b.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (b *fileBackend) Clear(_ context.Context) error {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if err := os.Remove(filepath.Join(b.dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (b *fileBackend) read(path string) (Conversation, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Conversation{}, err
	}
	var conversation Conversation
	if err := json.Unmarshal(raw, &conversation); err != nil {
		return Conversation{}, err
	}
	return conversation, nil
}

func (b *fileBackend) path(id string) string {
	return filepath.Join(b.dir, cleanID(id)+".json")
}

type sqliteBackend struct {
	path string
	db   *sql.DB
}

func (b *sqliteBackend) Init() error {
	if err := os.MkdirAll(filepath.Dir(b.path), 0o755); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", b.path)
	if err != nil {
		return err
	}
	if err := configureSQLiteDB(db); err != nil {
		_ = db.Close()
		return err
	}
	b.db = db
	_, err = b.db.Exec(`CREATE TABLE IF NOT EXISTS image_conversations (id TEXT PRIMARY KEY, raw_json BLOB NOT NULL, updated_at TEXT NOT NULL);`)
	return err
}

func configureSQLiteDB(db *sql.DB) error {
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 10000`); err != nil {
		return err
	}
	_, err := db.Exec(`PRAGMA journal_mode = WAL`)
	return err
}

func (b *sqliteBackend) Close() error {
	if b.db == nil {
		return nil
	}
	return b.db.Close()
}

func (b *sqliteBackend) List(_ context.Context) ([]Conversation, error) {
	rows, err := b.db.Query(`SELECT raw_json FROM image_conversations ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Conversation{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var conversation Conversation
		if err := json.Unmarshal(raw, &conversation); err != nil {
			continue
		}
		result = append(result, conversation)
	}
	return result, rows.Err()
}

func (b *sqliteBackend) Get(_ context.Context, id string) (*Conversation, error) {
	var raw []byte
	err := b.db.QueryRow(`SELECT raw_json FROM image_conversations WHERE id = ?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var conversation Conversation
	if err := json.Unmarshal(raw, &conversation); err != nil {
		return nil, err
	}
	return &conversation, nil
}

func (b *sqliteBackend) Save(_ context.Context, conversation Conversation) error {
	raw, err := json.Marshal(conversation)
	if err != nil {
		return err
	}
	_, err = b.db.Exec(
		`INSERT INTO image_conversations(id, raw_json, updated_at) VALUES(?, ?, ?) ON CONFLICT(id) DO UPDATE SET raw_json = excluded.raw_json, updated_at = excluded.updated_at`,
		conversation.ID,
		raw,
		conversation.CreatedAt,
	)
	return err
}

func (b *sqliteBackend) Delete(_ context.Context, id string) error {
	_, err := b.db.Exec(`DELETE FROM image_conversations WHERE id = ?`, id)
	return err
}

func (b *sqliteBackend) Clear(_ context.Context) error {
	_, err := b.db.Exec(`DELETE FROM image_conversations`)
	return err
}

type redisBackend struct {
	client *redis.Client
	prefix string
}

func (b *redisBackend) Init() error {
	return b.client.Ping(context.Background()).Err()
}

func (b *redisBackend) Close() error {
	if b.client == nil {
		return nil
	}
	return b.client.Close()
}

func (b *redisBackend) List(ctx context.Context) ([]Conversation, error) {
	values, err := b.client.HGetAll(ctx, b.key("conversations")).Result()
	if err != nil {
		return nil, err
	}
	result := make([]Conversation, 0, len(values))
	for _, raw := range values {
		var conversation Conversation
		if err := json.Unmarshal([]byte(raw), &conversation); err != nil {
			continue
		}
		result = append(result, conversation)
	}
	return result, nil
}

func (b *redisBackend) Get(ctx context.Context, id string) (*Conversation, error) {
	raw, err := b.client.HGet(ctx, b.key("conversations"), id).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var conversation Conversation
	if err := json.Unmarshal([]byte(raw), &conversation); err != nil {
		return nil, err
	}
	return &conversation, nil
}

func (b *redisBackend) Save(ctx context.Context, conversation Conversation) error {
	raw, err := json.Marshal(conversation)
	if err != nil {
		return err
	}
	return b.client.HSet(ctx, b.key("conversations"), conversation.ID, raw).Err()
}

func (b *redisBackend) Delete(ctx context.Context, id string) error {
	return b.client.HDel(ctx, b.key("conversations"), id).Err()
}

func (b *redisBackend) Clear(ctx context.Context) error {
	return b.client.Del(ctx, b.key("conversations")).Err()
}

func (b *redisBackend) key(name string) string {
	return b.prefix + ":" + name
}
