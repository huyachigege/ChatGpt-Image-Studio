package api

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"chatgpt2api/internal/config"

	_ "modernc.org/sqlite"
)

const maxImageRequestLogEntries = 5000

type imageRequestLogEntry struct {
	ID                    string   `json:"id"`
	StartedAt             string   `json:"startedAt"`
	FinishedAt            string   `json:"finishedAt"`
	Endpoint              string   `json:"endpoint"`
	Operation             string   `json:"operation"`
	ImageMode             string   `json:"imageMode"`
	Direction             string   `json:"direction"`
	Route                 string   `json:"route"`
	CPASubroute           string   `json:"cpaSubroute,omitempty"`
	CPAFallbackReason     string   `json:"cpaFallbackReason,omitempty"`
	QueueWaitMS           int64    `json:"queueWaitMs,omitempty"`
	InflightCountAtStart  int      `json:"inflightCountAtStart,omitempty"`
	LeaseAcquired         bool     `json:"leaseAcquired,omitempty"`
	ErrorCode             string   `json:"errorCode,omitempty"`
	RoutingPolicyApplied  bool     `json:"routingPolicyApplied,omitempty"`
	RoutingGroupIndex     int      `json:"routingGroupIndex,omitempty"`
	RoutingSortMode       string   `json:"routingSortMode,omitempty"`
	RoutingReservePercent int      `json:"routingReservePercent,omitempty"`
	AccountType           string   `json:"accountType,omitempty"`
	AccountEmail          string   `json:"accountEmail,omitempty"`
	AccountFile           string   `json:"accountFile,omitempty"`
	RequestedModel        string   `json:"requestedModel,omitempty"`
	UpstreamModel         string   `json:"upstreamModel,omitempty"`
	ImageToolModel        string   `json:"imageToolModel,omitempty"`
	UserID                string   `json:"userId,omitempty"`
	Username              string   `json:"username,omitempty"`
	UserRole              string   `json:"userRole,omitempty"`
	Size                  string   `json:"size,omitempty"`
	Quality               string   `json:"quality,omitempty"`
	Prompt                string   `json:"prompt,omitempty"`
	PromptLength          int      `json:"promptLength,omitempty"`
	ImageURLs             []string `json:"imageUrls,omitempty"`
	ImageNames            []string `json:"imageNames,omitempty"`
	Preferred             bool     `json:"preferred"`
	Success               bool     `json:"success"`
	Error                 string   `json:"error,omitempty"`
	UpstreamRequest       string   `json:"upstreamRequest,omitempty"`
}

type imageRequestLogSummary struct {
	ID                string `json:"id"`
	StartedAt         string `json:"startedAt"`
	FinishedAt        string `json:"finishedAt"`
	Operation         string `json:"operation"`
	Route             string `json:"route"`
	CPASubroute       string `json:"cpaSubroute,omitempty"`
	CPAFallbackReason string `json:"cpaFallbackReason,omitempty"`
	Size              string `json:"size,omitempty"`
	Quality           string `json:"quality,omitempty"`
	PromptLength      int    `json:"promptLength,omitempty"`
	UserID            string `json:"userId,omitempty"`
	Username          string `json:"username,omitempty"`
	UserRole          string `json:"userRole,omitempty"`
	AccountEmail      string `json:"accountEmail,omitempty"`
	AccountType       string `json:"accountType,omitempty"`
	AccountFile       string `json:"accountFile,omitempty"`
	Success           bool   `json:"success"`
	ErrorCode         string `json:"errorCode,omitempty"`
	Error             string `json:"error,omitempty"`
}

func (e *imageRequestLogEntry) summary() imageRequestLogSummary {
	return imageRequestLogSummary{
		ID:                e.ID,
		StartedAt:         e.StartedAt,
		FinishedAt:        e.FinishedAt,
		Operation:         e.Operation,
		Route:             e.Route,
		CPASubroute:       e.CPASubroute,
		CPAFallbackReason: e.CPAFallbackReason,
		Size:              e.Size,
		Quality:           e.Quality,
		PromptLength:      e.PromptLength,
		UserID:            e.UserID,
		Username:          e.Username,
		UserRole:          e.UserRole,
		AccountEmail:      e.AccountEmail,
		AccountType:       e.AccountType,
		AccountFile:       e.AccountFile,
		Success:           e.Success,
		ErrorCode:         e.ErrorCode,
		Error:             e.Error,
	}
}

type imageRequestLogQuery struct {
	Page     int
	PageSize int
	User     string
	Account  string
	Prompt   string
}

type imageRequestLogFilterOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type imageRequestLogFilterOptions struct {
	Users    []imageRequestLogFilterOption `json:"users"`
	Accounts []imageRequestLogFilterOption `json:"accounts"`
}

type imageRequestLogStore struct {
	mu         sync.Mutex
	path       string
	legacyPath string
	db         *sql.DB
	items      []imageRequestLogEntry
}

func newImageRequestLogStore(cfg *config.Config) *imageRequestLogStore {
	dbPath := cfg.ResolvePath(cfg.Storage.SQLitePath)
	if dbPath == "" {
		dbPath = cfg.ResolvePath("data/chatgpt-image-studio.db")
	}
	store := &imageRequestLogStore{
		path:       strings.TrimSpace(dbPath),
		legacyPath: cfg.ResolvePath("data/image_request_logs.jsonl"),
		items:      make([]imageRequestLogEntry, 0, maxImageRequestLogEntries),
	}
	store.initDB()
	return store
}

func (s *imageRequestLogStore) initDB() {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return
	}
	if err := s.openSQLite(); err != nil {
		return
	}
	_ = s.importLegacyJSONL()
	s.loadFromDB()
}

func (s *imageRequestLogStore) close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *imageRequestLogStore) add(entry imageRequestLogEntry) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(entry.ID) == "" {
		entry.ID = fmt.Sprintf("req_%d", time.Now().UnixNano())
	}
	s.items = append([]imageRequestLogEntry{entry}, s.items...)
	if len(s.items) > maxImageRequestLogEntries {
		s.items = s.items[:maxImageRequestLogEntries]
	}
	_ = s.append(entry)
}

func (s *imageRequestLogStore) list(limit int) []imageRequestLogEntry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 || limit > len(s.items) {
		limit = len(s.items)
	}
	out := make([]imageRequestLogEntry, limit)
	copy(out, s.items[:limit])
	return out
}

func (s *imageRequestLogStore) imagePromptMetadata() map[string]imageGalleryPromptMetadata {
	metadataByName := make(map[string]imageGalleryPromptMetadata)
	if s == nil {
		return metadataByName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.items {
		prompt := strings.TrimSpace(entry.Prompt)
		if prompt == "" || !entry.Success {
			continue
		}
		metadata := imageGalleryPromptMetadata{Prompt: prompt}
		for _, imageURL := range entry.ImageURLs {
			addImageGalleryPromptMetadata(metadataByName, imageURL, metadata)
		}
		for _, imageName := range entry.ImageNames {
			addImageGalleryPromptMetadata(metadataByName, imageName, metadata)
		}
	}
	return metadataByName
}

func (s *imageRequestLogStore) latestStartedAtByUser() map[string]string {
	latestByUser := map[string]string{}
	if s == nil {
		return latestByUser
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.items {
		userID := strings.TrimSpace(entry.UserID)
		startedAt := strings.TrimSpace(entry.StartedAt)
		if userID == "" || startedAt == "" {
			continue
		}
		if latestByUser[userID] == "" || startedAt > latestByUser[userID] {
			latestByUser[userID] = startedAt
		}
	}
	return latestByUser
}

func (s *imageRequestLogStore) listPage(query imageRequestLogQuery) ([]imageRequestLogEntry, int) {
	if s == nil || s.db == nil {
		return nil, 0
	}

	page := query.Page
	if page <= 0 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	where, args := s.buildWhereClause(query)

	var total int
	countSQL := `SELECT COUNT(*) FROM image_request_logs` + where
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0
	}

	offset := (page - 1) * pageSize
	if offset >= total {
		return []imageRequestLogEntry{}, total
	}

	dataSQL := `SELECT raw_json FROM image_request_logs` + where + ` ORDER BY started_at DESC, id DESC LIMIT ? OFFSET ?`
	dataArgs := append(args, pageSize, offset)
	rows, err := s.db.Query(dataSQL, dataArgs...)
	if err != nil {
		return nil, total
	}
	defer rows.Close()

	items := make([]imageRequestLogEntry, 0, pageSize)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var entry imageRequestLogEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		items = append(items, entry)
	}
	return items, total
}

func (s *imageRequestLogStore) buildWhereClause(query imageRequestLogQuery) (string, []any) {
	var conditions []string
	var args []any
	user := strings.ToLower(strings.TrimSpace(query.User))
	if user != "" {
		conditions = append(conditions, `user_key LIKE ?`)
		args = append(args, "%"+user+"%")
	}
	account := strings.ToLower(strings.TrimSpace(query.Account))
	if account != "" {
		conditions = append(conditions, `account_key LIKE ?`)
		args = append(args, "%"+account+"%")
	}
	prompt := strings.ToLower(strings.TrimSpace(query.Prompt))
	if prompt != "" {
		conditions = append(conditions, `prompt LIKE ?`)
		args = append(args, "%"+prompt+"%")
	}
	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func (s *imageRequestLogStore) getByID(id string) (*imageRequestLogEntry, bool) {
	if s == nil || s.db == nil {
		return nil, false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, false
	}
	var raw []byte
	if err := s.db.QueryRow(`SELECT raw_json FROM image_request_logs WHERE id = ?`, id).Scan(&raw); err != nil {
		return nil, false
	}
	var entry imageRequestLogEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, false
	}
	return &entry, true
}

func (s *imageRequestLogStore) delete(ids []string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			wanted[id] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	placeholders := make([]string, 0, len(wanted))
	args := make([]any, 0, len(wanted))
	for id := range wanted {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	query := `DELETE FROM image_request_logs WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return []string{}, nil
	}

	deleted := make([]string, 0, len(wanted))
	next := make([]imageRequestLogEntry, 0, len(s.items))
	for _, item := range s.items {
		if _, ok := wanted[item.ID]; ok {
			deleted = append(deleted, item.ID)
			continue
		}
		next = append(next, item)
	}
	s.items = next
	return deleted, nil
}

func (s *imageRequestLogStore) filterOptions() imageRequestLogFilterOptions {
	options := imageRequestLogFilterOptions{}
	if s == nil {
		return options
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	users := map[string]string{}
	accounts := map[string]string{}
	for _, item := range s.items {
		if value, label := requestLogUserOption(item); value != "" {
			users[value] = label
		}
		if value, label := requestLogAccountOption(item); value != "" {
			accounts[value] = label
		}
	}
	options.Users = requestLogOptionsFromMap(users)
	options.Accounts = requestLogOptionsFromMap(accounts)
	return options
}

func (s *imageRequestLogStore) loadFromDB() {
	if s == nil || s.db == nil {
		return
	}
	rows, err := s.db.Query(`SELECT raw_json FROM image_request_logs ORDER BY started_at DESC, id DESC LIMIT ?`, maxImageRequestLogEntries)
	if err != nil {
		return
	}
	defer rows.Close()

	items := make([]imageRequestLogEntry, 0, maxImageRequestLogEntries)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var entry imageRequestLogEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		items = append(items, entry)
	}
	s.items = items
}

func (s *imageRequestLogStore) append(entry imageRequestLogEntry) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.insertSQLite(entry)
}

func (s *imageRequestLogStore) openSQLite() error {
	if s.db != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 10000`); err != nil {
		_ = db.Close()
		return err
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		_ = db.Close()
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS image_request_logs (
		id TEXT PRIMARY KEY,
		started_at TEXT NOT NULL DEFAULT '',
		user_key TEXT NOT NULL DEFAULT '',
		account_key TEXT NOT NULL DEFAULT '',
		prompt TEXT NOT NULL DEFAULT '',
		raw_json BLOB NOT NULL
	)`); err != nil {
		_ = db.Close()
		return err
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_image_request_logs_started_at ON image_request_logs(started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_image_request_logs_user_key ON image_request_logs(user_key)`,
		`CREATE INDEX IF NOT EXISTS idx_image_request_logs_account_key ON image_request_logs(account_key)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return err
		}
	}
	s.db = db
	return nil
}

func (s *imageRequestLogStore) insertSQLite(entry imageRequestLogEntry) error {
	if strings.TrimSpace(entry.ID) == "" {
		return nil
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO image_request_logs (id, started_at, user_key, account_key, prompt, raw_json) VALUES (?, ?, ?, ?, ?, ?)`,
		entry.ID,
		entry.StartedAt,
		strings.ToLower(strings.TrimSpace(entry.UserID+" "+entry.Username+" "+entry.UserRole)),
		strings.ToLower(strings.TrimSpace(entry.AccountEmail+" "+entry.AccountFile+" "+entry.AccountType)),
		strings.ToLower(strings.TrimSpace(entry.Prompt)),
		payload,
	)
	return err
}

func (s *imageRequestLogStore) importLegacyJSONL() error {
	if strings.TrimSpace(s.legacyPath) == "" {
		return nil
	}
	var existing int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM image_request_logs`).Scan(&existing); err != nil || existing > 0 {
		return err
	}
	file, err := os.Open(s.legacyPath)
	if err != nil {
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var entry imageRequestLogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if strings.TrimSpace(entry.ID) == "" {
			entry.ID = fmt.Sprintf("req_%d", time.Now().UnixNano())
		}
		if err := s.insertSQLite(entry); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func applyImageResponseLogFields(entry *imageRequestLogEntry, items []map[string]any) {
	if entry == nil || len(items) == 0 {
		return
	}
	imageURLs := make([]string, 0, len(items))
	imageNames := make([]string, 0, len(items)*3)
	seenURLs := make(map[string]struct{})
	seenNames := make(map[string]struct{})
	for _, item := range items {
		url := strings.TrimSpace(stringValue(item["url"]))
		if isImageDataURL(url) {
			url = ""
		}
		if url != "" {
			if _, ok := seenURLs[url]; !ok {
				seenURLs[url] = struct{}{}
				imageURLs = append(imageURLs, url)
			}
			withoutQuery := strings.Split(url, "?")[0]
			imagePath := extractImagePathFromURL(withoutQuery)
			for _, name := range []string{imagePath, path.Base(withoutQuery)} {
				name = strings.Trim(strings.TrimSpace(name), "/")
				if name == "" || name == "." {
					continue
				}
				if _, ok := seenNames[name]; ok {
					continue
				}
				seenNames[name] = struct{}{}
				imageNames = append(imageNames, name)
			}
		}
		for _, key := range []string{"id", "file_id", "gen_id"} {
			name := strings.TrimSpace(stringValue(item[key]))
			if name == "" {
				continue
			}
			if _, ok := seenNames[name]; ok {
				continue
			}
			seenNames[name] = struct{}{}
			imageNames = append(imageNames, name)
		}
	}
	entry.ImageURLs = imageURLs
	entry.ImageNames = imageNames
}

const imagePathMarker = "/v1/files/image/"

func extractImagePathFromURL(rawURL string) string {
	if idx := strings.Index(rawURL, imagePathMarker); idx >= 0 {
		return rawURL[idx+len(imagePathMarker):]
	}
	return strings.TrimPrefix(rawURL, imagePathMarker)
}

func requestLogUserOption(item imageRequestLogEntry) (string, string) {
	value := strings.TrimSpace(item.UserID)
	label := strings.TrimSpace(item.Username)
	if value == "" {
		value = label
	}
	if label == "" {
		label = value
	}
	if item.UserRole != "" && label != "" {
		label += " · " + item.UserRole
	}
	return value, label
}

func requestLogAccountOption(item imageRequestLogEntry) (string, string) {
	value := strings.TrimSpace(item.AccountEmail)
	if value == "" {
		value = strings.TrimSpace(item.AccountFile)
	}
	if value == "" {
		value = strings.TrimSpace(item.AccountType)
	}
	label := strings.TrimSpace(item.AccountEmail)
	if label == "" {
		label = strings.TrimSpace(item.AccountFile)
	}
	if item.AccountType != "" && label != "" {
		label += " · " + item.AccountType
	}
	return value, label
}

func requestLogOptionsFromMap(values map[string]string) []imageRequestLogFilterOption {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return strings.ToLower(values[keys[i]]) < strings.ToLower(values[keys[j]])
	})
	options := make([]imageRequestLogFilterOption, 0, len(keys))
	for _, key := range keys {
		options = append(options, imageRequestLogFilterOption{Value: key, Label: values[key]})
	}
	return options
}
