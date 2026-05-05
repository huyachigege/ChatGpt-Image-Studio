package api

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type imageGalleryItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"createdAt"`
}

func (s *Server) handleListImageGallery(w http.ResponseWriter, r *http.Request) {
	identity := identityFromContext(r.Context())
	items, err := s.listImageGalleryItems(identity.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleDeleteImageGalleryItem(w http.ResponseWriter, r *http.Request) {
	identity := identityFromContext(r.Context())
	name := strings.TrimSpace(r.PathValue("name"))
	path, relName, err := s.resolveScopedImageGalleryPath(identity.UserID, name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "image not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": relName})
}

func (s *Server) listImageGalleryItems(userID string) ([]imageGalleryItem, error) {
	dir := s.imageGalleryDirForUser(userID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []imageGalleryItem{}, nil
		}
		return nil, err
	}
	items := make([]imageGalleryItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		name := entry.Name()
		items = append(items, imageGalleryItem{
			ID:        name,
			Name:      name,
			URL:       "/v1/files/image/" + imageURLName(userID, name),
			Size:      info.Size(),
			CreatedAt: info.ModTime().UTC().Format(time.RFC3339Nano),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
	return items, nil
}

func (s *Server) imageGalleryDirForUser(userID string) string {
	root := s.cfg.ResolvePath(s.cfg.Storage.ImageDir)
	cleaned := sanitizeGalleryUserID(userID)
	if cleaned == "" || cleaned == "admin" {
		return root
	}
	return filepath.Join(root, cleaned)
}

func (s *Server) resolveScopedImageGalleryPath(userID, name string) (string, string, error) {
	cleanedName := filepath.Base(strings.Trim(strings.TrimSpace(name), "/"))
	if cleanedName == "" || cleanedName == "." {
		return "", "", errBadRequest("image name is required")
	}
	dir := s.imageGalleryDirForUser(userID)
	path := filepath.Join(dir, cleanedName)
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(dir)+string(filepath.Separator)) && filepath.Clean(path) != filepath.Clean(filepath.Join(dir, cleanedName)) {
		return "", "", errBadRequest("invalid image name")
	}
	return path, cleanedName, nil
}

func sanitizeGalleryUserID(userID string) string {
	cleaned := strings.TrimSpace(userID)
	cleaned = strings.ReplaceAll(cleaned, "/", "-")
	cleaned = strings.ReplaceAll(cleaned, "\\", "-")
	return cleaned
}

func errBadRequest(message string) error {
	return &requestError{code: "bad_request", message: message}
}
