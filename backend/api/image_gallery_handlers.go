package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"chatgpt2api/internal/users"
)

type imageGalleryItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Folder    string `json:"folder,omitempty"`
	URL       string `json:"url"`
	ThumbURL  string `json:"thumbUrl"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"createdAt"`
}

func (s *Server) handleListImageGallery(w http.ResponseWriter, r *http.Request) {
	identity := identityFromContext(r.Context())
	items, err := s.listImageGalleryItems(identity)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleDeleteImageGalleryItem(w http.ResponseWriter, r *http.Request) {
	identity := identityFromContext(r.Context())
	name := strings.TrimSpace(r.PathValue("name"))
	deleted, err := s.deleteImageGalleryNames(identity, []string{name})
	if err != nil {
		status := http.StatusBadRequest
		if os.IsNotExist(err) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name, "deleted": deleted})
}

func (s *Server) handleBatchDeleteImageGallery(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Names []string `json:"names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	identity := identityFromContext(r.Context())
	deleted, err := s.deleteImageGalleryNames(identity, body.Names)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted})
}

func (s *Server) deleteImageGalleryNames(identity authIdentity, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, errBadRequest("image names are required")
	}
	deleted := make([]string, 0, len(names))
	for _, name := range names {
		path, relName, err := s.resolveScopedImageGalleryPath(identity, name)
		if err != nil {
			return deleted, err
		}
		s.removeImageThumbnail(path)
		if err := os.Remove(path); err != nil {
			return deleted, err
		}
		deleted = append(deleted, relName)
	}
	return deleted, nil
}

func (s *Server) listImageGalleryItems(identity authIdentity) ([]imageGalleryItem, error) {
	if identity.Role == users.RoleAdmin {
		return s.listAdminImageGalleryItems()
	}
	root := s.cfg.ResolvePath(s.cfg.Storage.ImageDir)
	folder := sanitizeGallerySegment(identity.UserID)
	if folder == "" {
		return s.listImageGalleryDir(root, "")
	}
	return s.listImageGalleryDir(filepath.Join(root, folder), folder)
}

func (s *Server) listAdminImageGalleryItems() ([]imageGalleryItem, error) {
	root := s.cfg.ResolvePath(s.cfg.Storage.ImageDir)
	items, err := s.listImageGalleryDir(root, "")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []imageGalleryItem{}, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == ".thumbs" {
			continue
		}
		folder := sanitizeGallerySegment(entry.Name())
		if folder == "" {
			continue
		}
		folderItems, err := s.listImageGalleryDir(filepath.Join(root, folder), folder)
		if err != nil {
			return nil, err
		}
		items = append(items, folderItems...)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Folder == items[j].Folder {
			return items[i].CreatedAt > items[j].CreatedAt
		}
		return items[i].Folder < items[j].Folder
	})
	return items, nil
}

func (s *Server) listImageGalleryDir(dir, folder string) ([]imageGalleryItem, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []imageGalleryItem{}, nil
		}
		return nil, err
	}
	items := make([]imageGalleryItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == ".thumbs" {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		name := entry.Name()
		relName := name
		urlName := name
		if folder != "" {
			relName = folder + "/" + name
			urlName = relName
		}
		items = append(items, imageGalleryItem{
			ID:        relName,
			Name:      relName,
			Folder:    firstNonEmpty(folder, "根目录"),
			URL:       "/v1/files/image/" + urlName,
			ThumbURL:  "/v1/files/image-thumb/" + urlName,
			Size:      info.Size(),
			CreatedAt: info.ModTime().UTC().Format(time.RFC3339Nano),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
	return items, nil
}

func (s *Server) resolveScopedImageGalleryPath(identity authIdentity, name string) (string, string, error) {
	name = strings.Trim(strings.TrimSpace(name), "/")
	if name == "" || name == "." {
		return "", "", errBadRequest("image name is required")
	}
	root := s.cfg.ResolvePath(s.cfg.Storage.ImageDir)
	var relName string
	if identity.Role == users.RoleAdmin {
		parts := strings.Split(name, "/")
		if len(parts) == 1 {
			relName = filepath.Base(parts[0])
		} else {
			folder := sanitizeGallerySegment(parts[0])
			file := filepath.Base(parts[len(parts)-1])
			if folder == "" || file == "" || file == "." {
				return "", "", errBadRequest("invalid image name")
			}
			relName = folder + "/" + file
		}
	} else {
		file := filepath.Base(name)
		if file == "" || file == "." {
			return "", "", errBadRequest("invalid image name")
		}
		folder := sanitizeGallerySegment(identity.UserID)
		if folder != "" {
			relName = folder + "/" + file
		} else {
			relName = file
		}
	}
	path := filepath.Join(root, filepath.FromSlash(relName))
	cleanRoot := filepath.Clean(root)
	cleanPath := filepath.Clean(path)
	if cleanPath != cleanRoot && !strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) {
		return "", "", errBadRequest("invalid image name")
	}
	return path, relName, nil
}

func sanitizeGallerySegment(value string) string {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.ReplaceAll(cleaned, "/", "-")
	cleaned = strings.ReplaceAll(cleaned, "\\", "-")
	return cleaned
}

func errBadRequest(message string) error {
	return &requestError{code: "bad_request", message: message}
}
