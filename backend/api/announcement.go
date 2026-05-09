package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type announcement struct {
	Content   string `json:"content"`
	ExpiresAt string `json:"expiresAt"`
	CreatedAt string `json:"createdAt"`
}

func (s *Server) initAnnouncementTable() {
	db := s.reqLogs.db
	if db == nil {
		return
	}
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS announcements (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		content TEXT NOT NULL DEFAULT '',
		expires_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT ''
	)`)
}

func (s *Server) handleGetAnnouncement(w http.ResponseWriter, r *http.Request) {
	db := s.reqLogs.db
	if db == nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	var content, expiresAt, createdAt string
	err := db.QueryRow(`SELECT content, expires_at, created_at FROM announcements WHERE id = 1`).Scan(&content, &expiresAt, &createdAt)
	if err == sql.ErrNoRows || err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	if content == "" {
		writeJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	if expiresAt != "" {
		expiry, parseErr := time.Parse(time.RFC3339, expiresAt)
		if parseErr == nil && time.Now().After(expiry) {
			writeJSON(w, http.StatusOK, map[string]any{"active": false})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active":    true,
		"content":   content,
		"expiresAt": expiresAt,
		"createdAt": createdAt,
	})
}

func (s *Server) handleSetAnnouncement(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content   string `json:"content"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	db := s.reqLogs.db
	if db == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "database not available"})
		return
	}
	content := strings.TrimSpace(body.Content)
	expiresAt := strings.TrimSpace(body.ExpiresAt)
	createdAt := time.Now().UTC().Format(time.RFC3339)

	_, err := db.Exec(
		`INSERT OR REPLACE INTO announcements (id, content, expires_at, created_at) VALUES (1, ?, ?, ?)`,
		content, expiresAt, createdAt,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteAnnouncement(w http.ResponseWriter, r *http.Request) {
	db := s.reqLogs.db
	if db == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	_, _ = db.Exec(`DELETE FROM announcements WHERE id = 1`)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
