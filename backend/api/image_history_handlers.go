package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"chatgpt2api/internal/config"
	"chatgpt2api/internal/imagehistory"
)

func (s *Server) handleListImageConversations(w http.ResponseWriter, r *http.Request) {
	if !s.serverImageConversationStorageEnabled() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "server image storage is disabled"})
		return
	}
	store, err := imagehistory.NewStoreForUser(s.cfg, identityFromContext(r.Context()).UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	items, err := store.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleGetImageConversation(w http.ResponseWriter, r *http.Request) {
	if !s.serverImageConversationStorageEnabled() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "server image storage is disabled"})
		return
	}
	store, err := imagehistory.NewStoreForUser(s.cfg, identityFromContext(r.Context()).UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	item, err := store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if item == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "conversation not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func (s *Server) handleSaveImageConversation(w http.ResponseWriter, r *http.Request) {
	if !s.serverImageConversationStorageEnabled() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "server image storage is disabled"})
		return
	}
	var body imagehistory.Conversation
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	if pathID := strings.TrimSpace(r.PathValue("id")); pathID != "" {
		body.ID = pathID
	}

	identity := identityFromContext(r.Context())
	store, err := imagehistory.NewStoreForUser(s.cfg, identity.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	item, err := store.Save(r.Context(), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if item != nil {
		s.addImageConversationPromptMetadataCache(identity.UserID, *item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func (s *Server) handleDeleteImageConversation(w http.ResponseWriter, r *http.Request) {
	if !s.serverImageConversationStorageEnabled() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "server image storage is disabled"})
		return
	}
	identity := identityFromContext(r.Context())
	store, err := imagehistory.NewStoreForUser(s.cfg, identity.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	if err := store.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.invalidateImageConversationPromptMetadataCache(identity.UserID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleClearImageConversations(w http.ResponseWriter, r *http.Request) {
	if !s.serverImageConversationStorageEnabled() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "server image storage is disabled"})
		return
	}
	identity := identityFromContext(r.Context())
	store, err := imagehistory.NewStoreForUser(s.cfg, identity.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	if err := store.Clear(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.invalidateImageConversationPromptMetadataCache(identity.UserID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleImportImageConversations(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items   []imagehistory.Conversation `json:"items"`
		Storage struct {
			Backend                  string `json:"backend"`
			ImageDir                 string `json:"imageDir"`
			SQLitePath               string `json:"sqlitePath"`
			RedisAddr                string `json:"redisAddr"`
			RedisPassword            string `json:"redisPassword"`
			RedisDB                  int    `json:"redisDb"`
			RedisPrefix              string `json:"redisPrefix"`
			ImageConversationStorage string `json:"imageConversationStorage"`
			ImageDataStorage         string `json:"imageDataStorage"`
		} `json:"storage"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}

	tempCfg := config.New(s.cfg.RootDir())
	tempCfg.Storage.Backend = body.Storage.Backend
	tempCfg.Storage.ImageDir = firstNonEmpty(body.Storage.ImageDir, s.cfg.Storage.ImageDir)
	tempCfg.Storage.SQLitePath = firstNonEmpty(body.Storage.SQLitePath, s.cfg.Storage.SQLitePath)
	tempCfg.Storage.RedisAddr = firstNonEmpty(body.Storage.RedisAddr, s.cfg.Storage.RedisAddr)
	tempCfg.Storage.RedisPassword = body.Storage.RedisPassword
	tempCfg.Storage.RedisDB = body.Storage.RedisDB
	tempCfg.Storage.RedisPrefix = firstNonEmpty(body.Storage.RedisPrefix, s.cfg.Storage.RedisPrefix)
	tempCfg.Storage.ImageConversationStorage = firstNonEmpty(body.Storage.ImageConversationStorage, "server")
	tempCfg.Storage.ImageDataStorage = firstNonEmpty(body.Storage.ImageDataStorage, tempCfg.Storage.ImageConversationStorage)

	identity := identityFromContext(r.Context())
	store, err := imagehistory.NewStoreForUser(tempCfg, identity.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()

	if err := store.Clear(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	for _, item := range body.Items {
		if _, err := store.Save(r.Context(), item); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	s.invalidateImageConversationPromptMetadataCache(identity.UserID)
	writeJSON(w, http.StatusOK, map[string]any{"imported": len(body.Items)})
}

func (s *Server) serverImageConversationStorageEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(s.cfg.Storage.ImageConversationStorage), "server")
}
