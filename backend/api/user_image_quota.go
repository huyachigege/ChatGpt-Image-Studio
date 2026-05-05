package api

import (
	"context"
	"fmt"
	"strings"

	"chatgpt2api/internal/users"
)

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

func (s *Server) ensureUserImageQuotaAvailable(ctx context.Context, quotaKind string) error {
	identity := identityFromContext(ctx)
	if identity.Role == users.RoleAdmin || strings.TrimSpace(identity.UserID) == "" {
		return nil
	}
	store, err := users.NewStore(s.cfg)
	if err != nil {
		return err
	}
	defer store.Close()
	status, err := store.GetDailyImageQuota(ctx, identity.UserID)
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(quotaKind), "paid") {
		if status.PaidRemaining <= 0 {
			return newRequestError("user_paid_quota_exhausted", fmt.Sprintf("今日 paid 图片额度已用完（%d/%d）", status.PaidUsed, status.PaidLimit))
		}
		return nil
	}
	if status.FreeRemaining <= 0 {
		return newRequestError("user_free_quota_exhausted", fmt.Sprintf("今日 free 图片额度已用完（%d/%d）", status.FreeUsed, status.FreeLimit))
	}
	return nil
}

func (s *Server) consumeUserImageQuota(ctx context.Context, quotaKind string) error {
	identity := identityFromContext(ctx)
	if identity.Role == users.RoleAdmin || strings.TrimSpace(identity.UserID) == "" {
		return nil
	}
	store, err := users.NewStore(s.cfg)
	if err != nil {
		return err
	}
	defer store.Close()
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
