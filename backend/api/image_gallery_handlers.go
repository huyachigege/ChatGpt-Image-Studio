package api

import (
	"context"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"chatgpt2api/internal/imagehistory"
	"chatgpt2api/internal/users"
)

type imageGalleryItem struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Folder         string `json:"folder,omitempty"`
	URL            string `json:"url"`
	ThumbURL       string `json:"thumbUrl"`
	Size           int64  `json:"size"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
	CreatedAt      string `json:"createdAt"`
	Prompt         string `json:"prompt,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
	TurnID         string `json:"turnId,omitempty"`
}

type imageGalleryListResponse struct {
	Items    []imageGalleryItem `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
}

type imageGalleryPromptMetadata struct {
	Prompt         string
	ConversationID string
	TurnID         string
}

func (s *Server) handleListImageGallery(w http.ResponseWriter, r *http.Request) {
	identity := identityFromContext(r.Context())
	page := parsePositiveQueryInt(r, "page", 1)
	pageSize := parsePositiveQueryInt(r, "pageSize", 24)
	if pageSize > 100 {
		pageSize = 100
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	items, err := s.listImageGalleryItems(r.Context(), identity)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if query != "" {
		items = filterImageGalleryItemsByPrompt(items, query)
	}
	total := len(items)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	writeJSON(w, http.StatusOK, imageGalleryListResponse{Items: items[start:end], Total: total, Page: page, PageSize: pageSize})
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

func parsePositiveQueryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func filterImageGalleryItemsByPrompt(items []imageGalleryItem, query string) []imageGalleryItem {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return items
	}
	filtered := make([]imageGalleryItem, 0, len(items))
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Prompt), query) {
			filtered = append(filtered, item)
		}
	}
	return filtered
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

func (s *Server) listImageGalleryItems(ctx context.Context, identity authIdentity) ([]imageGalleryItem, error) {
	var items []imageGalleryItem
	var err error
	if identity.Role == users.RoleAdmin {
		items, err = s.listAdminImageGalleryItems(ctx)
	} else {
		root := s.cfg.ResolvePath(s.cfg.Storage.ImageDir)
		folder := sanitizeGallerySegment(identity.UserID)
		if folder == "" {
			items, err = s.listImageGalleryDir(root, "", "")
		} else {
			items, err = s.listImageGalleryDir(filepath.Join(root, folder), folder, "")
		}
	}
	if err != nil {
		return nil, err
	}
	return s.attachImageGalleryPrompts(ctx, identity, items), nil
}

func (s *Server) listAdminImageGalleryItems(ctx context.Context) ([]imageGalleryItem, error) {
	root := s.cfg.ResolvePath(s.cfg.Storage.ImageDir)
	items, err := s.listImageGalleryDir(root, "", "管理员")
	if err != nil {
		return nil, err
	}
	usernames := s.imageGalleryUsernames(ctx)
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
		folderItems, err := s.listImageGalleryDir(filepath.Join(root, folder), folder, firstNonEmpty(usernames[folder], folder))
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

func (s *Server) imageGalleryUsernames(ctx context.Context) map[string]string {
	store, err := users.NewStore(s.cfg)
	if err != nil {
		return map[string]string{}
	}
	defer store.Close()
	items, err := store.ListUsers(ctx)
	if err != nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(items))
	for _, item := range items {
		result[sanitizeGallerySegment(item.ID)] = firstNonEmpty(item.Username, item.Name, item.ID)
	}
	return result
}

func (s *Server) listImageGalleryDir(dir, folder, displayFolder string) ([]imageGalleryItem, error) {
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
		width, height := imageGalleryDimensions(filepath.Join(dir, name))
		items = append(items, imageGalleryItem{
			ID:        relName,
			Name:      relName,
			Folder:    displayFolder,
			URL:       "/v1/files/image/" + urlName,
			ThumbURL:  "/v1/files/image-thumb/" + urlName,
			Size:      info.Size(),
			Width:     width,
			Height:    height,
			CreatedAt: info.ModTime().UTC().Format(time.RFC3339Nano),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
	return items, nil
}

func imageGalleryDimensions(filePath string) (int, int) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0
	}
	return config.Width, config.Height
}

func (s *Server) attachImageGalleryPrompts(ctx context.Context, identity authIdentity, items []imageGalleryItem) []imageGalleryItem {
	if len(items) == 0 || !s.serverImageConversationStorageEnabled() {
		return items
	}
	userID := identity.UserID
	if identity.Role == users.RoleAdmin {
		userID = ""
	}
	store, err := imagehistory.NewStoreForUser(s.cfg, userID)
	if err != nil {
		return items
	}
	defer store.Close()
	conversations, err := store.List(ctx)
	if err != nil {
		return items
	}
	metadataByName := buildImageGalleryPromptMetadata(conversations)
	for index := range items {
		metadata, ok := lookupImageGalleryPromptMetadata(metadataByName, items[index])
		if !ok {
			continue
		}
		items[index].Prompt = metadata.Prompt
		items[index].ConversationID = metadata.ConversationID
		items[index].TurnID = metadata.TurnID
	}
	return items
}

func buildImageGalleryPromptMetadata(conversations []imagehistory.Conversation) map[string]imageGalleryPromptMetadata {
	metadataByName := make(map[string]imageGalleryPromptMetadata)
	for _, conversation := range conversations {
		conversationPrompt := strings.TrimSpace(conversation.Prompt)
		for _, image := range conversation.Images {
			addImageGalleryPromptMetadata(metadataByName, image.URL, imageGalleryPromptMetadata{
				Prompt:         strings.TrimSpace(firstNonEmpty(image.Prompt, conversationPrompt)),
				ConversationID: conversation.ID,
			})
		}
		for _, turn := range conversation.Turns {
			turnPrompt := strings.TrimSpace(firstNonEmpty(turn.Prompt, conversationPrompt))
			for _, image := range turn.Images {
				addImageGalleryPromptMetadata(metadataByName, image.URL, imageGalleryPromptMetadata{
					Prompt:         strings.TrimSpace(firstNonEmpty(image.Prompt, turnPrompt)),
					ConversationID: conversation.ID,
					TurnID:         turn.ID,
				})
			}
		}
	}
	return metadataByName
}

func addImageGalleryPromptMetadata(metadataByName map[string]imageGalleryPromptMetadata, imageURL string, metadata imageGalleryPromptMetadata) {
	imageURL = strings.TrimSpace(imageURL)
	if imageURL == "" || metadata.Prompt == "" {
		return
	}
	withoutQuery := strings.Split(imageURL, "?")[0]
	keys := []string{
		withoutQuery,
		strings.TrimPrefix(withoutQuery, "/v1/files/image/"),
		path.Base(withoutQuery),
	}
	for _, key := range keys {
		key = strings.Trim(strings.TrimSpace(key), "/")
		if key == "" || key == "." {
			continue
		}
		metadataByName[key] = metadata
	}
}

func lookupImageGalleryPromptMetadata(metadataByName map[string]imageGalleryPromptMetadata, item imageGalleryItem) (imageGalleryPromptMetadata, bool) {
	keys := []string{
		item.Name,
		strings.TrimPrefix(item.URL, "/v1/files/image/"),
		path.Base(item.Name),
		path.Base(item.URL),
	}
	for _, key := range keys {
		key = strings.Trim(strings.TrimSpace(key), "/")
		if metadata, ok := metadataByName[key]; ok {
			return metadata, true
		}
	}
	return imageGalleryPromptMetadata{}, false
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
