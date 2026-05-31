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
	UserID         string `json:"userId,omitempty"`
	UserLabel      string `json:"userLabel,omitempty"`
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
	Items    []imageGalleryItem         `json:"items"`
	Total    int                        `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"pageSize"`
	Folders  []imageGalleryFolderOption `json:"folders,omitempty"`
}

type imageGalleryFolderOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type imageGalleryPromptMetadata struct {
	Prompt         string
	ConversationID string
	TurnID         string
}

type imageConversationPromptMetadataCacheEntry struct {
	loaded                       bool
	metadata                     map[string]imageGalleryPromptMetadata
	metadataKeysByConversationID map[string][]string
}

func (s *Server) handleListImageGallery(w http.ResponseWriter, r *http.Request) {
	identity := identityFromContext(r.Context())
	page := parsePositiveQueryInt(r, "page", 1)
	pageSize := parsePositiveQueryInt(r, "pageSize", 24)
	if pageSize > 100 {
		pageSize = 100
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	folderFilter := strings.TrimSpace(r.URL.Query().Get("folder"))
	groupMode := strings.TrimSpace(r.URL.Query().Get("group"))
	favoriteOnly := strings.TrimSpace(r.URL.Query().Get("favorite")) == "1" || strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("favorite")), "true")
	items, err := s.listImageGalleryItems(r.Context(), identity)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if favoriteOnly {
		items, err = s.filterFavoriteImageGalleryItems(r.Context(), identity, items)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
	}
	var folders []imageGalleryFolderOption
	if identity.Role == users.RoleAdmin {
		folders = collectImageGalleryFolders(items)
	}
	if folderFilter != "" {
		items = filterImageGalleryItemsByFolder(items, folderFilter)
	}
	if query != "" {
		items = filterImageGalleryItemsByPrompt(items, query)
	}
	if groupMode == "month" || groupMode == "day" {
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].CreatedAt > items[j].CreatedAt
		})
	}
	total := len(items)
	pageCount := 1
	if pageSize > 0 {
		pageCount = (total + pageSize - 1) / pageSize
		if pageCount <= 0 {
			pageCount = 1
		}
	}
	if page > pageCount {
		page = pageCount
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	writeJSON(w, http.StatusOK, imageGalleryListResponse{Items: items[start:end], Total: total, Page: page, PageSize: pageSize, Folders: folders})
}

func (s *Server) handleDeleteImageGalleryItem(w http.ResponseWriter, r *http.Request) {
	identity := identityFromContext(r.Context())
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "image names are required"})
		return
	}
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
	if len(body.Names) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "image names are required"})
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

func (s *Server) handleDeleteImageGalleryBefore(w http.ResponseWriter, r *http.Request) {
	months, ok := parseRetentionMonths(w, r)
	if !ok {
		return
	}
	identity := identityFromContext(r.Context())
	items, err := s.listImageGalleryItems(r.Context(), identity)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	cutoff := time.Now().AddDate(0, -months, 0).UTC()
	names := make([]string, 0)
	for _, item := range items {
		createdAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(item.CreatedAt))
		if err != nil || createdAt.IsZero() {
			continue
		}
		if createdAt.UTC().Before(cutoff) {
			names = append(names, item.Name)
		}
	}
	deleted, err := s.deleteImageGalleryNames(identity, names)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted, "deletedCount": len(deleted)})
}

func parsePositiveQueryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func (s *Server) filterFavoriteImageGalleryItems(ctx context.Context, identity authIdentity, items []imageGalleryItem) ([]imageGalleryItem, error) {
	store, err := s.userDB()
	if err != nil {
		return nil, err
	}
	favoriteKeys, err := store.ListFavorites(ctx, identity.UserID, favoriteTypeImage)
	if err != nil {
		return nil, err
	}
	favoriteSet := make(map[string]struct{}, len(favoriteKeys))
	for _, key := range favoriteKeys {
		favoriteSet[strings.TrimSpace(key)] = struct{}{}
	}
	filtered := make([]imageGalleryItem, 0, len(items))
	for _, item := range items {
		if _, ok := favoriteSet[item.Name]; ok {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
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

func filterImageGalleryItemsByFolder(items []imageGalleryItem, folder string) []imageGalleryItem {
	filtered := make([]imageGalleryItem, 0, len(items))
	for _, item := range items {
		if item.Folder == folder {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func collectImageGalleryFolders(items []imageGalleryItem) []imageGalleryFolderOption {
	seen := map[string]struct{}{}
	var folders []imageGalleryFolderOption
	for _, item := range items {
		if item.Folder == "" {
			continue
		}
		if _, ok := seen[item.Folder]; ok {
			continue
		}
		seen[item.Folder] = struct{}{}
		folders = append(folders, imageGalleryFolderOption{Value: item.Folder, Label: item.Folder})
	}
	return folders
}

func (s *Server) deleteImageGalleryNames(identity authIdentity, names []string) ([]string, error) {
	if len(names) == 0 {
		return []string{}, nil
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
		for index := range items {
			items[index].UserID = identity.UserID
			items[index].UserLabel = firstNonEmpty(identity.Username, identity.Name, identity.UserID)
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
	for index := range items {
		items[index].UserID = "admin"
		items[index].UserLabel = "管理员"
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
		userLabel := firstNonEmpty(usernames[folder], folder)
		folderItems, err := s.listImageGalleryDir(filepath.Join(root, folder), folder, userLabel)
		if err != nil {
			return nil, err
		}
		for index := range folderItems {
			folderItems[index].UserID = folder
			folderItems[index].UserLabel = userLabel
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
	store, err := s.userDB()
	if err != nil {
		return map[string]string{}
	}
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
	if len(items) == 0 {
		return items
	}
	metadataByName := s.reqLogs.imagePromptMetadata()
	if s.serverImageConversationStorageEnabled() {
		userID := identity.UserID
		if identity.Role == users.RoleAdmin {
			userID = ""
		}
		for key, metadata := range s.imageConversationPromptMetadata(ctx, userID) {
			metadataByName[key] = metadata
		}
	}
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

func (s *Server) imageConversationPromptMetadata(ctx context.Context, userID string) map[string]imageGalleryPromptMetadata {
	scope := imageConversationPromptMetadataScope(userID)
	s.imageConversationPromptMetadataCacheMu.RLock()
	if entry, ok := s.imageConversationPromptMetadataCache[scope]; ok && entry.loaded {
		metadata := cloneImageGalleryPromptMetadata(entry.metadata)
		s.imageConversationPromptMetadataCacheMu.RUnlock()
		return metadata
	}
	s.imageConversationPromptMetadataCacheMu.RUnlock()

	metadataByName := make(map[string]imageGalleryPromptMetadata)
	store, err := imagehistory.NewStoreForUser(s.cfg, userID)
	if err != nil {
		return metadataByName
	}
	conversations, err := store.List(ctx)
	if err != nil {
		return metadataByName
	}
	metadataByName, keysByConversationID := buildImageGalleryPromptMetadata(conversations)

	s.imageConversationPromptMetadataCacheMu.Lock()
	if s.imageConversationPromptMetadataCache == nil {
		s.imageConversationPromptMetadataCache = make(map[string]imageConversationPromptMetadataCacheEntry)
	}
	s.imageConversationPromptMetadataCache[scope] = imageConversationPromptMetadataCacheEntry{loaded: true, metadata: metadataByName, metadataKeysByConversationID: keysByConversationID}
	metadata := cloneImageGalleryPromptMetadata(metadataByName)
	s.imageConversationPromptMetadataCacheMu.Unlock()
	return metadata
}

func (s *Server) addImageConversationPromptMetadataCache(userID string, conversation imagehistory.Conversation) {
	metadataByName, keysByConversationID := buildImageGalleryPromptMetadata([]imagehistory.Conversation{conversation})
	if len(metadataByName) == 0 {
		return
	}
	s.imageConversationPromptMetadataCacheMu.Lock()
	defer s.imageConversationPromptMetadataCacheMu.Unlock()
	for _, scope := range imageConversationPromptMetadataAffectedScopes(userID) {
		entry, ok := s.imageConversationPromptMetadataCache[scope]
		if !ok || !entry.loaded {
			continue
		}
		if entry.metadata == nil {
			entry.metadata = make(map[string]imageGalleryPromptMetadata)
		}
		if entry.metadataKeysByConversationID == nil {
			entry.metadataKeysByConversationID = make(map[string][]string)
		}
		for conversationID, keys := range keysByConversationID {
			for _, key := range entry.metadataKeysByConversationID[conversationID] {
				delete(entry.metadata, key)
			}
			entry.metadataKeysByConversationID[conversationID] = keys
		}
		for key, metadata := range metadataByName {
			entry.metadata[key] = metadata
		}
		s.imageConversationPromptMetadataCache[scope] = entry
	}
}

func (s *Server) invalidateImageConversationPromptMetadataCache(userID string) {
	s.imageConversationPromptMetadataCacheMu.Lock()
	defer s.imageConversationPromptMetadataCacheMu.Unlock()
	for _, scope := range imageConversationPromptMetadataAffectedScopes(userID) {
		delete(s.imageConversationPromptMetadataCache, scope)
	}
}

func (s *Server) removeImageConversationPromptMetadataCache(userID, conversationID string) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return
	}
	s.imageConversationPromptMetadataCacheMu.Lock()
	defer s.imageConversationPromptMetadataCacheMu.Unlock()
	for _, scope := range imageConversationPromptMetadataAffectedScopes(userID) {
		entry, ok := s.imageConversationPromptMetadataCache[scope]
		if !ok || !entry.loaded {
			continue
		}
		for _, key := range entry.metadataKeysByConversationID[conversationID] {
			delete(entry.metadata, key)
		}
		delete(entry.metadataKeysByConversationID, conversationID)
		s.imageConversationPromptMetadataCache[scope] = entry
	}
}

func (s *Server) clearImageConversationPromptMetadataCache(userID string) {
	s.imageConversationPromptMetadataCacheMu.Lock()
	defer s.imageConversationPromptMetadataCacheMu.Unlock()
	userScope := imageConversationPromptMetadataScope(userID)
	for _, scope := range imageConversationPromptMetadataAffectedScopes(userID) {
		if scope != userScope {
			delete(s.imageConversationPromptMetadataCache, scope)
			continue
		}
		entry, ok := s.imageConversationPromptMetadataCache[scope]
		if !ok || !entry.loaded {
			continue
		}
		entry.metadata = make(map[string]imageGalleryPromptMetadata)
		entry.metadataKeysByConversationID = make(map[string][]string)
		s.imageConversationPromptMetadataCache[scope] = entry
	}
}

func (s *Server) invalidateAllImageConversationPromptMetadataCache() {
	s.imageConversationPromptMetadataCacheMu.Lock()
	defer s.imageConversationPromptMetadataCacheMu.Unlock()
	s.imageConversationPromptMetadataCache = make(map[string]imageConversationPromptMetadataCacheEntry)
}

func imageConversationPromptMetadataScope(userID string) string {
	return sanitizeGallerySegment(userID)
}

func imageConversationPromptMetadataAffectedScopes(userID string) []string {
	scope := imageConversationPromptMetadataScope(userID)
	if scope == "" {
		return []string{""}
	}
	return []string{scope, ""}
}

func cloneImageGalleryPromptMetadata(source map[string]imageGalleryPromptMetadata) map[string]imageGalleryPromptMetadata {
	clone := make(map[string]imageGalleryPromptMetadata, len(source))
	for key, metadata := range source {
		clone[key] = metadata
	}
	return clone
}

func buildImageGalleryPromptMetadata(conversations []imagehistory.Conversation) (map[string]imageGalleryPromptMetadata, map[string][]string) {
	metadataByName := make(map[string]imageGalleryPromptMetadata)
	keysByConversationID := make(map[string][]string)
	for _, conversation := range conversations {
		conversationID := strings.TrimSpace(conversation.ID)
		conversationPrompt := strings.TrimSpace(conversation.Prompt)
		for _, image := range conversation.Images {
			keys := addImageGalleryPromptMetadata(metadataByName, image.URL, imageGalleryPromptMetadata{
				Prompt:         strings.TrimSpace(firstNonEmpty(image.Prompt, conversationPrompt)),
				ConversationID: conversation.ID,
			})
			if conversationID != "" {
				keysByConversationID[conversationID] = append(keysByConversationID[conversationID], keys...)
			}
		}
		for _, turn := range conversation.Turns {
			turnPrompt := strings.TrimSpace(firstNonEmpty(turn.Prompt, conversationPrompt))
			for _, image := range turn.Images {
				keys := addImageGalleryPromptMetadata(metadataByName, image.URL, imageGalleryPromptMetadata{
					Prompt:         strings.TrimSpace(firstNonEmpty(image.Prompt, turnPrompt)),
					ConversationID: conversation.ID,
					TurnID:         turn.ID,
				})
				if conversationID != "" {
					keysByConversationID[conversationID] = append(keysByConversationID[conversationID], keys...)
				}
			}
		}
	}
	return metadataByName, keysByConversationID
}

func addImageGalleryPromptMetadata(metadataByName map[string]imageGalleryPromptMetadata, imageURL string, metadata imageGalleryPromptMetadata) []string {
	if metadata.Prompt == "" {
		return nil
	}
	keys := imageGalleryPromptKeys(imageURL)
	for _, key := range keys {
		metadataByName[key] = metadata
	}
	return keys
}

func lookupImageGalleryPromptMetadata(metadataByName map[string]imageGalleryPromptMetadata, item imageGalleryItem) (imageGalleryPromptMetadata, bool) {
	keys := append(imageGalleryPromptKeys(item.Name), imageGalleryPromptKeys(item.URL)...)
	for _, key := range keys {
		if metadata, ok := metadataByName[key]; ok {
			return metadata, true
		}
	}
	return imageGalleryPromptMetadata{}, false
}

func imageGalleryPromptKeys(value string) []string {
	value = strings.TrimSpace(strings.Split(value, "?")[0])
	if value == "" {
		return nil
	}
	pathValue := extractImagePathFromURL(value)
	candidates := []string{
		value,
		pathValue,
		strings.TrimPrefix(value, imagePathMarker),
		path.Base(value),
		path.Base(pathValue),
	}
	keys := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		key := strings.Trim(strings.TrimSpace(candidate), "/")
		if key == "" || key == "." {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
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
