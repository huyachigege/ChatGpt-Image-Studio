package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"chatgpt2api/internal/users"
)

const (
	favoriteTypeTemplate = "template"
	favoriteTypeImage    = "image"
)

type favoritesResponse struct {
	Items []string `json:"items"`
}

func normalizeFavoriteType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case favoriteTypeTemplate, "prompt-template", "prompt_template":
		return favoriteTypeTemplate
	case favoriteTypeImage, "gallery-image", "gallery_image":
		return favoriteTypeImage
	default:
		return ""
	}
}

func (s *Server) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	favoriteType := normalizeFavoriteType(r.PathValue("type"))
	if favoriteType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid favorite type"})
		return
	}
	identity := identityFromContext(r.Context())
	store, err := users.NewStore(s.cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()
	items, err := store.ListFavorites(r.Context(), identity.UserID, favoriteType)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, favoritesResponse{Items: items})
}

func (s *Server) handleSetFavorite(w http.ResponseWriter, r *http.Request) {
	favoriteType := normalizeFavoriteType(r.PathValue("type"))
	if favoriteType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid favorite type"})
		return
	}
	var body struct {
		Key      string `json:"key"`
		Favorite bool   `json:"favorite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	key := strings.TrimSpace(body.Key)
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "favorite key is required"})
		return
	}
	identity := identityFromContext(r.Context())
	store, err := users.NewStore(s.cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer store.Close()
	if err := store.SetFavorite(r.Context(), identity.UserID, favoriteType, key, body.Favorite); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "key": key, "favorite": body.Favorite})
}
