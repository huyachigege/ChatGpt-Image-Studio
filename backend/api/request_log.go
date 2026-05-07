package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxImageRequestLogEntries = 5000

type imageRequestLogEntry struct {
	ID                    string `json:"id"`
	StartedAt             string `json:"startedAt"`
	FinishedAt            string `json:"finishedAt"`
	Endpoint              string `json:"endpoint"`
	Operation             string `json:"operation"`
	ImageMode             string `json:"imageMode"`
	Direction             string `json:"direction"`
	Route                 string `json:"route"`
	CPASubroute           string `json:"cpaSubroute,omitempty"`
	QueueWaitMS           int64  `json:"queueWaitMs,omitempty"`
	InflightCountAtStart  int    `json:"inflightCountAtStart,omitempty"`
	LeaseAcquired         bool   `json:"leaseAcquired,omitempty"`
	ErrorCode             string `json:"errorCode,omitempty"`
	RoutingPolicyApplied  bool   `json:"routingPolicyApplied,omitempty"`
	RoutingGroupIndex     int    `json:"routingGroupIndex,omitempty"`
	RoutingSortMode       string `json:"routingSortMode,omitempty"`
	RoutingReservePercent int    `json:"routingReservePercent,omitempty"`
	AccountType           string `json:"accountType,omitempty"`
	AccountEmail          string `json:"accountEmail,omitempty"`
	AccountFile           string `json:"accountFile,omitempty"`
	RequestedModel        string `json:"requestedModel,omitempty"`
	UpstreamModel         string `json:"upstreamModel,omitempty"`
	ImageToolModel        string `json:"imageToolModel,omitempty"`
	UserID                string `json:"userId,omitempty"`
	Username              string `json:"username,omitempty"`
	UserRole              string `json:"userRole,omitempty"`
	Size                  string `json:"size,omitempty"`
	Quality               string `json:"quality,omitempty"`
	Prompt                string `json:"prompt,omitempty"`
	PromptLength          int    `json:"promptLength,omitempty"`
	Preferred             bool   `json:"preferred"`
	Success               bool   `json:"success"`
	Error                 string `json:"error,omitempty"`
}

type imageRequestLogQuery struct {
	Page     int
	PageSize int
	User     string
	Account  string
	Prompt   string
}

type imageRequestLogStore struct {
	mu    sync.Mutex
	path  string
	items []imageRequestLogEntry
}

func newImageRequestLogStore(path string) *imageRequestLogStore {
	store := &imageRequestLogStore{
		path:  strings.TrimSpace(path),
		items: make([]imageRequestLogEntry, 0, maxImageRequestLogEntries),
	}
	store.load()
	return store
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

func (s *imageRequestLogStore) listPage(query imageRequestLogQuery) ([]imageRequestLogEntry, int) {
	if s == nil {
		return nil, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()

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

	filtered := make([]imageRequestLogEntry, 0, len(s.items))
	for _, item := range s.items {
		if !requestLogEntryMatches(item, query) {
			continue
		}
		filtered = append(filtered, item)
	}
	total := len(filtered)
	start := (page - 1) * pageSize
	if start >= total {
		return []imageRequestLogEntry{}, total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	out := make([]imageRequestLogEntry, end-start)
	copy(out, filtered[start:end])
	return out, total
}

func (s *imageRequestLogStore) load() {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return
	}
	file, err := os.Open(s.path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	items := make([]imageRequestLogEntry, 0, maxImageRequestLogEntries)
	for scanner.Scan() {
		var entry imageRequestLogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		items = append([]imageRequestLogEntry{entry}, items...)
		if len(items) > maxImageRequestLogEntries {
			items = items[:maxImageRequestLogEntries]
		}
	}
	s.items = items
}

func (s *imageRequestLogStore) append(entry imageRequestLogEntry) error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = file.Write(append(payload, '\n'))
	return err
}

func requestLogEntryMatches(item imageRequestLogEntry, query imageRequestLogQuery) bool {
	user := strings.ToLower(strings.TrimSpace(query.User))
	if user != "" && !strings.Contains(strings.ToLower(item.UserID+" "+item.Username+" "+item.UserRole), user) {
		return false
	}
	account := strings.ToLower(strings.TrimSpace(query.Account))
	if account != "" && !strings.Contains(strings.ToLower(item.AccountEmail+" "+item.AccountFile+" "+item.AccountType), account) {
		return false
	}
	prompt := strings.ToLower(strings.TrimSpace(query.Prompt))
	if prompt != "" && !strings.Contains(strings.ToLower(item.Prompt), prompt) {
		return false
	}
	return true
}
