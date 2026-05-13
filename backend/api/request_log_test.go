package api

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"chatgpt2api/internal/config"
)

func TestImageRequestLogStoreMigratesSummariesAndDeletesFailed(t *testing.T) {
	rootDir := t.TempDir()
	cfg := config.New(rootDir)
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	dbPath := cfg.ResolvePath(cfg.Storage.SQLitePath)
	if dbPath == "" {
		dbPath = cfg.ResolvePath("data/chatgpt-image-studio.db")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE image_request_logs (
		id TEXT PRIMARY KEY,
		started_at TEXT NOT NULL DEFAULT '',
		user_key TEXT NOT NULL DEFAULT '',
		account_key TEXT NOT NULL DEFAULT '',
		prompt TEXT NOT NULL DEFAULT '',
		raw_json BLOB NOT NULL
	)`); err != nil {
		t.Fatalf("create old table: %v", err)
	}
	seed := []imageRequestLogEntry{
		{ID: "failed-1", StartedAt: "2026-05-13T01:00:00Z", Operation: "edit", Route: "responses", UserID: "u1", Username: "user", AccountEmail: "a@example.com", Success: false, Error: "boom", UpstreamRequest: string(make([]byte, 1024*1024))},
		{ID: "success-1", StartedAt: "2026-05-13T01:01:00Z", Operation: "generate", Route: "responses", UserID: "u1", Username: "user", AccountEmail: "a@example.com", Success: true, UpstreamRequest: string(make([]byte, 1024*1024))},
	}
	for _, entry := range seed {
		raw, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal seed: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO image_request_logs (id, started_at, user_key, account_key, prompt, raw_json) VALUES (?, ?, ?, ?, ?, ?)`, entry.ID, entry.StartedAt, entry.UserID, entry.AccountEmail, entry.Prompt, raw); err != nil {
			t.Fatalf("insert seed: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	store := newImageRequestLogStore(cfg)
	defer store.close()
	var missingBeforeList int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM image_request_logs WHERE summary_json IS NULL OR length(summary_json) = 0`).Scan(&missingBeforeList); err != nil {
		t.Fatalf("count missing summaries before list: %v", err)
	}
	if missingBeforeList != 2 {
		t.Fatalf("missing summaries before list = %d, want 2", missingBeforeList)
	}
	if len(store.items) != 0 {
		t.Fatalf("startup loaded %d request logs, want 0 before lazy summary backfill", len(store.items))
	}

	items, total := store.listPage(imageRequestLogQuery{Page: 1, PageSize: 20})
	if total != 2 || len(items) != 2 {
		t.Fatalf("listPage total=%d len=%d, want 2/2", total, len(items))
	}
	if len(items[0].UpstreamRequest) != 0 || len(items[1].UpstreamRequest) != 0 {
		t.Fatalf("listPage loaded full upstream requests: %d/%d", len(items[0].UpstreamRequest), len(items[1].UpstreamRequest))
	}
	var missing int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM image_request_logs WHERE summary_json IS NULL OR length(summary_json) = 0`).Scan(&missing); err != nil {
		t.Fatalf("count missing summaries: %v", err)
	}
	if missing != 0 {
		t.Fatalf("missing summaries = %d, want 0", missing)
	}

	deleted, err := store.deleteFailed()
	if err != nil {
		t.Fatalf("deleteFailed() returned error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleteFailed() = %d, want 1", deleted)
	}
	items, total = store.listPage(imageRequestLogQuery{Page: 1, PageSize: 20})
	if total != 1 || len(items) != 1 || items[0].ID != "success-1" {
		t.Fatalf("after delete total=%d items=%#v, want only success-1", total, items)
	}
}
