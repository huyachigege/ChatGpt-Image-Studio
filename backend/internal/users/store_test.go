package users

import (
	"context"
	"path/filepath"
	"testing"

	"chatgpt2api/internal/config"
)

func newTestStore(t *testing.T, freeLimit, paidLimit int) *Store {
	t.Helper()
	cfg := config.New(t.TempDir())
	cfg.Storage.SQLitePath = filepath.Join(t.TempDir(), "users.db")
	cfg.Accounts.DailyFreeImageLimit = freeLimit
	cfg.Accounts.DailyPaidImageLimit = paidLimit
	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func insertTestUser(t *testing.T, store *Store, id, role string) {
	t.Helper()
	_, err := store.db.Exec(`INSERT INTO app_users (id,username,email,name,password_hash,role,image_api_key,created_at) VALUES (?,?,?,?,?,?,?,?)`, id, id, id+"@local.user", id, "hash", role, "key-"+id, "2026-05-21T00:00:00Z")
	if err != nil {
		t.Fatalf("insert user %s: %v", id, err)
	}
}

func TestDailyImageQuotaUsesConfiguredLimits(t *testing.T) {
	store := newTestStore(t, 7, 3)
	ctx := context.Background()
	insertTestUser(t, store, "user-a", RoleUser)

	quota, err := store.GetDailyImageQuota(ctx, "user-a")
	if err != nil {
		t.Fatalf("GetDailyImageQuota: %v", err)
	}
	if quota.FreeLimit != 7 || quota.FreeRemaining != 7 || quota.PaidLimit != 3 || quota.PaidRemaining != 3 {
		t.Fatalf("quota = %+v, want configured limits 7/3", quota)
	}

	for i := 0; i < 3; i++ {
		if _, err := store.ConsumeDailyImageQuota(ctx, "user-a", "paid"); err != nil {
			t.Fatalf("ConsumeDailyImageQuota paid #%d: %v", i+1, err)
		}
	}
	if _, err := store.ConsumeDailyImageQuota(ctx, "user-a", "paid"); err == nil {
		t.Fatalf("ConsumeDailyImageQuota paid over limit returned nil error")
	}
}

func TestAdjustAllUsersDailyImageQuota(t *testing.T) {
	store := newTestStore(t, 5, 2)
	ctx := context.Background()
	insertTestUser(t, store, "user-a", RoleUser)
	insertTestUser(t, store, "user-b", RoleUser)
	insertTestUser(t, store, "admin-user", RoleAdmin)

	if _, err := store.ConsumeDailyImageQuota(ctx, "user-a", "free"); err != nil {
		t.Fatalf("ConsumeDailyImageQuota user-a: %v", err)
	}
	affected, err := store.AdjustAllUsersDailyImageQuota(ctx, "free", 2)
	if err != nil {
		t.Fatalf("AdjustAllUsersDailyImageQuota +2: %v", err)
	}
	if affected != 2 {
		t.Fatalf("affected = %d, want 2", affected)
	}
	quotas, err := store.GetDailyImageQuotaBatch(ctx, []string{"user-a", "user-b", "admin-user"})
	if err != nil {
		t.Fatalf("GetDailyImageQuotaBatch: %v", err)
	}
	if quotas["user-a"].FreeRemaining != 5 || quotas["user-b"].FreeRemaining != 5 {
		t.Fatalf("free remaining after +2 = user-a:%d user-b:%d, want 5/5", quotas["user-a"].FreeRemaining, quotas["user-b"].FreeRemaining)
	}

	affected, err = store.AdjustAllUsersDailyImageQuota(ctx, "free", -3)
	if err != nil {
		t.Fatalf("AdjustAllUsersDailyImageQuota -3: %v", err)
	}
	if affected != 2 {
		t.Fatalf("affected = %d, want 2", affected)
	}
	quotas, err = store.GetDailyImageQuotaBatch(ctx, []string{"user-a", "user-b", "admin-user"})
	if err != nil {
		t.Fatalf("GetDailyImageQuotaBatch after -3: %v", err)
	}
	if quotas["user-a"].FreeRemaining != 2 || quotas["user-b"].FreeRemaining != 2 {
		t.Fatalf("free remaining after -3 = user-a:%d user-b:%d, want 2/2", quotas["user-a"].FreeRemaining, quotas["user-b"].FreeRemaining)
	}
	if quotas["admin-user"].FreeRemaining != 5 {
		t.Fatalf("admin remaining = %d, want unchanged 5", quotas["admin-user"].FreeRemaining)
	}
}
