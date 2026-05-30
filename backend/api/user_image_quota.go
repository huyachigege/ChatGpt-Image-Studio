package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"chatgpt2api/internal/imaging"
	"chatgpt2api/internal/users"
)

type userImageQuotaKindContextKey struct{}

func (s *Server) handleGetUserImageQuota(w http.ResponseWriter, r *http.Request) {
	identity := identityFromContext(r.Context())
	store, err := s.userDB()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	status, err := store.GetDailyImageQuota(r.Context(), identity.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": status})
}

func imageQuotaKind(accountType, direction, route string) string {
	switch strings.TrimSpace(accountType) {
	case "Plus", "Pro", "Team":
		return "paid"
	}
	if strings.EqualFold(strings.TrimSpace(direction), "cpa") || strings.EqualFold(strings.TrimSpace(route), "responses") {
		return "paid"
	}
	return "free"
}

func imageTaskQuotaCount(count int) int {
	if count <= 0 {
		return 1
	}
	if count > 8 {
		return 8
	}
	return count
}

func imageTaskQuotaKind(mode, resolutionAccess, size string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" || mode == "generate" {
		if strings.EqualFold(strings.TrimSpace(resolutionAccess), "paid") || requiresPaidGenerateTask(size) {
			return "paid"
		}
		return "free"
	}
	if strings.EqualFold(strings.TrimSpace(resolutionAccess), "paid") {
		return "paid"
	}
	return "free"
}

func countCreateTaskImageSources(sources []imageTaskSourceImagePayload) int {
	count := 0
	for _, source := range sources {
		role := strings.ToLower(strings.TrimSpace(source.Role))
		if role == "mask" {
			continue
		}
		if strings.TrimSpace(source.DataURL) == "" && strings.TrimSpace(source.URL) == "" {
			continue
		}
		count++
	}
	return count
}

func moveCreateTaskSourceImagesToReferences(req createImageTaskRequest) createImageTaskRequest {
	if len(req.SourceImages) == 0 {
		return req
	}
	references := append([]imageTaskReferenceImagePayload(nil), req.ReferenceImages...)
	for _, source := range req.SourceImages {
		role := strings.ToLower(strings.TrimSpace(source.Role))
		if role == "mask" {
			continue
		}
		if strings.TrimSpace(source.DataURL) == "" && strings.TrimSpace(source.URL) == "" {
			continue
		}
		references = append(references, imageTaskReferenceImagePayload{
			ID:      strings.TrimSpace(source.ID),
			Name:    strings.TrimSpace(source.Name),
			DataURL: strings.TrimSpace(source.DataURL),
			URL:     strings.TrimSpace(source.URL),
		})
	}
	req.SourceImages = nil
	req.ReferenceImages = references
	return req
}

func (s *Server) normalizeCreateImageTaskRequest(ctx context.Context, req createImageTaskRequest) createImageTaskRequest {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "generate"
	}
	req.Mode = mode
	if strings.TrimSpace(req.Quality) == "" {
		req.Quality = "high"
	}
	if mode == "generate" && countCreateTaskImageSources(req.SourceImages) > 0 {
		req = moveCreateTaskSourceImagesToReferences(req)
	}
	if mode == "edit" && countCreateTaskImageSources(req.SourceImages) > 1 && req.SourceReference == nil {
		mode = "generate"
		req.Mode = mode
		req = moveCreateTaskSourceImagesToReferences(req)
	}
	if mode != "generate" || req.SourceReference != nil || req.ContextReference != nil {
		return req
	}
	if strings.TrimSpace(req.ResolutionAccess) == "" {
		req.ResolutionAccess = "free"
	}
	req.Size = normalizeGenerateImageSize(req.Size)
	if imageTaskQuotaKind(mode, req.ResolutionAccess, req.Size) == "paid" {
		if err := s.ensureUserImageQuotaAvailable(ctx, "paid", imageTaskQuotaCount(req.Count)); requestErrorCode(err) == "user_paid_quota_exhausted" {
			req.ResolutionAccess = "free"
			if requiresPaidGenerateTask(req.Size) {
				req.Size = imaging.DefaultGenerateSize
			}
		}
	}
	if imageTaskQuotaKind(mode, req.ResolutionAccess, req.Size) == "free" {
		req.Quality = "low"
	}
	return req
}

func withUserImageQuotaKind(ctx context.Context, quotaKind string) context.Context {
	quotaKind = strings.ToLower(strings.TrimSpace(quotaKind))
	if quotaKind != "paid" {
		quotaKind = "free"
	}
	return context.WithValue(ctx, userImageQuotaKindContextKey{}, quotaKind)
}

func userImageQuotaKindFromContext(ctx context.Context, fallback string) string {
	if value, ok := ctx.Value(userImageQuotaKindContextKey{}).(string); ok && strings.TrimSpace(value) != "" {
		return strings.ToLower(strings.TrimSpace(value))
	}
	fallback = strings.ToLower(strings.TrimSpace(fallback))
	if fallback == "paid" {
		return "paid"
	}
	return "free"
}

func (s *Server) ensureUserImageQuotaAvailable(ctx context.Context, quotaKind string, count int) error {
	identity := identityFromContext(ctx)
	if identity.Role == users.RoleAdmin || strings.TrimSpace(identity.UserID) == "" {
		return nil
	}
	store, err := s.userDB()
	if err != nil {
		return err
	}
	status, err := store.GetDailyImageQuota(ctx, identity.UserID)
	if err != nil {
		return err
	}
	if count <= 0 {
		count = 1
	}
	quotaKind = userImageQuotaKindFromContext(ctx, quotaKind)
	if quotaKind == "paid" {
		if status.PaidRemaining < count {
			return newRequestError("user_paid_quota_exhausted", fmt.Sprintf("今日 paid 图片额度不足（剩余 %d，需要 %d，已用 %d/%d）", status.PaidRemaining, count, status.PaidUsed, status.PaidLimit))
		}
		return nil
	}
	if status.FreeRemaining < count {
		return newRequestError("user_free_quota_exhausted", fmt.Sprintf("今日 free 图片额度不足（剩余 %d，需要 %d，已用 %d/%d）", status.FreeRemaining, count, status.FreeUsed, status.FreeLimit))
	}
	return nil
}

func (s *Server) consumeUserImageQuota(ctx context.Context, quotaKind string) error {
	identity := identityFromContext(ctx)
	if identity.Role == users.RoleAdmin || strings.TrimSpace(identity.UserID) == "" {
		return nil
	}
	store, err := s.userDB()
	if err != nil {
		return err
	}
	quotaKind = userImageQuotaKindFromContext(ctx, quotaKind)
	_, err = store.ConsumeDailyImageQuota(ctx, identity.UserID, quotaKind)
	if err == nil {
		return nil
	}
	message := err.Error()
	if strings.Contains(message, "paid") {
		return newRequestError("user_paid_quota_exhausted", message)
	}
	if strings.Contains(message, "free") {
		return newRequestError("user_free_quota_exhausted", message)
	}
	return err
}
