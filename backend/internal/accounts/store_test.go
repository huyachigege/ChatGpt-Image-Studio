package accounts

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNeedsImageQuotaRefreshWithoutQuotaMessage(t *testing.T) {
	account := PublicAccount{
		Status:         "正常",
		Quota:          5,
		LimitsProgress: nil,
	}

	if !NeedsImageQuotaRefresh(account, time.Now()) {
		t.Fatal("expected refresh to be required when image_gen quota message is missing")
	}
}

func TestNeedsImageQuotaRefreshByResetTime(t *testing.T) {
	now := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)

	freshAccount := PublicAccount{
		Status: "正常",
		Quota:  3,
		LimitsProgress: []map[string]any{
			{
				"feature_name": "image_gen",
				"remaining":    3,
				"reset_after":  now.Add(2 * time.Hour).Format(time.RFC3339),
			},
		},
	}
	if NeedsImageQuotaRefresh(freshAccount, now) {
		t.Fatal("expected refresh to be skipped before reset_after")
	}

	expiredAccount := PublicAccount{
		Status: "限流",
		Quota:  0,
		LimitsProgress: []map[string]any{
			{
				"feature_name": "image_gen",
				"remaining":    0,
				"reset_after":  now.Add(-1 * time.Minute).Format(time.RFC3339),
			},
		},
	}
	if !NeedsImageQuotaRefresh(expiredAccount, now) {
		t.Fatal("expected refresh to be required after reset_after")
	}
}

func TestImportAuthFilesSkipsDuplicateToken(t *testing.T) {
	rootDir := t.TempDir()
	authDir := filepath.Join(rootDir, "auths")
	syncDir := filepath.Join(rootDir, "sync")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		t.Fatalf("mkdir sync dir: %v", err)
	}

	store := &Store{
		authDir:      authDir,
		syncStateDir: syncDir,
		stateFile:    filepath.Join(rootDir, "state.json"),
		defaultQuota: 5,
		providerType: "codex",
		states:       map[string]RuntimeState{},
	}

	existingToken := "token-1"
	if err := writeJSONFile(filepath.Join(authDir, "existing.json"), map[string]any{
		"type":         "codex",
		"access_token": existingToken,
		"email":        "existing@example.com",
	}); err != nil {
		t.Fatalf("seed existing auth file: %v", err)
	}

	imported, importedTokens, skipped, failures, err := store.ImportAuthFiles([]ImportedAuthFile{
		{
			Name: "duplicate.json",
			Data: []byte(`{"type":"codex","access_token":"token-1","email":"dup@example.com"}`),
		},
		{
			Name: "fresh.json",
			Data: []byte(`{"type":"codex","access_token":"token-2","email":"fresh@example.com"}`),
		},
	})
	if err != nil {
		t.Fatalf("ImportAuthFiles returned error: %v", err)
	}
	if imported != 1 {
		t.Fatalf("expected imported=1, got %d", imported)
	}
	if len(importedTokens) != 1 || importedTokens[0] != "token-2" {
		t.Fatalf("expected imported token token-2, got %#v", importedTokens)
	}
	if len(skipped) != 1 {
		t.Fatalf("expected 1 skipped file, got %d", len(skipped))
	}
	if len(failures) != 0 {
		t.Fatalf("expected no failures, got %#v", failures)
	}

	auths, err := store.loadAuths()
	if err != nil {
		t.Fatalf("loadAuths returned error: %v", err)
	}
	if len(auths) != 2 {
		t.Fatalf("expected 2 auth files after import, got %d", len(auths))
	}
}

func TestImportAuthFilesKeepsNewerByIdentityAndType(t *testing.T) {
	rootDir := t.TempDir()
	authDir := filepath.Join(rootDir, "auths")
	syncDir := filepath.Join(rootDir, "sync")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		t.Fatalf("mkdir sync dir: %v", err)
	}

	store := &Store{
		authDir:      authDir,
		syncStateDir: syncDir,
		stateFile:    filepath.Join(rootDir, "state.json"),
		defaultQuota: 5,
		providerType: "codex",
		states:       map[string]RuntimeState{},
	}

	existingName := "existing.json"
	existingData := map[string]any{
		"type":         "codex",
		"access_token": "token-old",
		"account_id":   "acct-1",
		"email":        "user@example.com",
		"last_refresh": "2026-04-20T01:00:00+08:00",
	}
	if err := writeJSONFile(filepath.Join(authDir, existingName), existingData); err != nil {
		t.Fatalf("seed existing auth file: %v", err)
	}

	imported, importedTokens, skipped, failures, err := store.ImportAuthFiles([]ImportedAuthFile{
		{
			Name: "newer.json",
			Data: []byte(`{"type":"codex","access_token":"token-new","account_id":"acct-1","email":"user@example.com","last_refresh":"2026-04-21T01:00:00+08:00"}`),
		},
	})
	if err != nil {
		t.Fatalf("ImportAuthFiles returned error: %v", err)
	}
	if imported != 1 {
		t.Fatalf("expected imported=1, got %d", imported)
	}
	if len(importedTokens) != 1 || importedTokens[0] != "token-new" {
		t.Fatalf("expected imported token token-new, got %#v", importedTokens)
	}
	if len(skipped) != 0 {
		t.Fatalf("expected no skipped files, got %#v", skipped)
	}
	if len(failures) != 0 {
		t.Fatalf("expected no failures, got %#v", failures)
	}

	var stored map[string]any
	if err := readJSONFile(filepath.Join(authDir, existingName), &stored); err != nil {
		t.Fatalf("read overwritten auth file: %v", err)
	}
	if got := stringValue(stored["access_token"]); got != "token-new" {
		t.Fatalf("expected overwritten access_token token-new, got %s", got)
	}
}

func TestBuildPublicAccountFallsBackProImageQuotaWhenLimitMissing(t *testing.T) {
	store := &Store{
		defaultQuota: 5,
		providerType: "codex",
		states: map[string]RuntimeState{
			"pro.json": {
				Type:           "Pro",
				Status:         "限流",
				Quota:          0,
				QuotaKnown:     true,
				LimitsProgress: nil,
			},
		},
	}

	got := store.buildPublicAccount(
		LocalAuth{
			Name:        "pro.json",
			AccessToken: "token-1",
			Provider:    "codex",
			Data:        map[string]any{},
		},
		SyncState{},
		nil,
	)

	if got.Type != "Pro" {
		t.Fatalf("expected type Pro, got %q", got.Type)
	}
	if got.Quota != proFallbackImageGenQuota {
		t.Fatalf("expected quota %d, got %d", proFallbackImageGenQuota, got.Quota)
	}
	if got.Status != "正常" {
		t.Fatalf("expected status 正常, got %q", got.Status)
	}

	hasImageGen := false
	for _, item := range got.LimitsProgress {
		if strings.TrimSpace(strings.ToLower(stringValue(item["feature_name"]))) != "image_gen" {
			continue
		}
		hasImageGen = true
		if gotRemaining := intValue(item["remaining"]); gotRemaining != proFallbackImageGenQuota {
			t.Fatalf("expected image_gen remaining %d, got %d", proFallbackImageGenQuota, gotRemaining)
		}
	}
	if !hasImageGen {
		t.Fatal("expected limits_progress to include image_gen")
	}
}

func TestAcquireImageAuthFilteredReturnsSentinelWhenNoEligibleAccount(t *testing.T) {
	rootDir := t.TempDir()
	authDir := filepath.Join(rootDir, "auths")
	syncDir := filepath.Join(rootDir, "sync")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		t.Fatalf("mkdir sync dir: %v", err)
	}

	store := &Store{
		authDir:      authDir,
		syncStateDir: syncDir,
		stateFile:    filepath.Join(rootDir, "state.json"),
		defaultQuota: 5,
		providerType: "codex",
		states: map[string]RuntimeState{
			"free.json": {
				Type:       "Free",
				Status:     "正常",
				Quota:      1,
				QuotaKnown: true,
			},
		},
	}

	if err := writeJSONFile(filepath.Join(authDir, "free.json"), map[string]any{
		"type":         "codex",
		"access_token": "token-free",
		"email":        "free@example.com",
	}); err != nil {
		t.Fatalf("seed free auth file: %v", err)
	}

	_, _, err := store.AcquireImageAuthFiltered(nil, func(account PublicAccount) bool {
		return account.Type != "Free"
	})
	if !errors.Is(err, ErrNoAvailableImageAuth) {
		t.Fatalf("AcquireImageAuthFiltered() error = %v, want ErrNoAvailableImageAuth", err)
	}
}

func TestAcquireImageAuthFilteredAcceptsPaidAccountInferredFromAccessToken(t *testing.T) {
	rootDir := t.TempDir()
	authDir := filepath.Join(rootDir, "auths")
	syncDir := filepath.Join(rootDir, "sync")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		t.Fatalf("mkdir sync dir: %v", err)
	}

	store := &Store{
		authDir:      authDir,
		syncStateDir: syncDir,
		stateFile:    filepath.Join(rootDir, "state.json"),
		defaultQuota: 5,
		providerType: "codex",
		states:       map[string]RuntimeState{},
	}

	token := mustTestJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type": "plus",
		},
	})

	if err := writeJSONFile(filepath.Join(authDir, "paid.json"), map[string]any{
		"type":         "codex",
		"access_token": token,
		"email":        "paid@example.com",
	}); err != nil {
		t.Fatalf("seed paid auth file: %v", err)
	}

	_, account, err := store.AcquireImageAuthFiltered(nil, func(account PublicAccount) bool {
		return account.Type == "Plus" || account.Type == "Pro" || account.Type == "Team"
	})
	if err != nil {
		t.Fatalf("AcquireImageAuthFiltered() returned error: %v", err)
	}
	if account.Type != "Plus" {
		t.Fatalf("AcquireImageAuthFiltered() type = %q, want %q", account.Type, "Plus")
	}
}

func TestAcquireImageAuthLeaseForUserPrefersAccountNotUsedByOtherUser(t *testing.T) {
	rootDir := t.TempDir()
	authDir := filepath.Join(rootDir, "auths")
	syncDir := filepath.Join(rootDir, "sync")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		t.Fatalf("mkdir sync dir: %v", err)
	}

	store := &Store{
		authDir:      authDir,
		syncStateDir: syncDir,
		stateFile:    filepath.Join(rootDir, "state.json"),
		defaultQuota: 5,
		providerType: "codex",
		states: map[string]RuntimeState{
			"first.json":  {Type: "Free", Status: "正常", Quota: 5, QuotaKnown: true},
			"second.json": {Type: "Free", Status: "正常", Quota: 5, QuotaKnown: true},
		},
	}

	if err := writeJSONFile(filepath.Join(authDir, "first.json"), map[string]any{
		"type":         "codex",
		"access_token": "token-first",
		"email":        "first@example.com",
	}); err != nil {
		t.Fatalf("seed first auth file: %v", err)
	}
	if err := writeJSONFile(filepath.Join(authDir, "second.json"), map[string]any{
		"type":         "codex",
		"access_token": "token-second",
		"email":        "second@example.com",
	}); err != nil {
		t.Fatalf("seed second auth file: %v", err)
	}

	authA, _, releaseA, err := store.AcquireImageAuthLeaseForUserFilteredWithDisabledOption(nil, nil, false, "user-a")
	if err != nil {
		t.Fatalf("AcquireImageAuthLeaseForUserFilteredWithDisabledOption(user-a) error = %v", err)
	}
	if authA.AccessToken != "token-first" {
		t.Fatalf("user-a token = %q, want token-first", authA.AccessToken)
	}
	releaseA()

	authB, _, releaseB, err := store.AcquireImageAuthLeaseForUserFilteredWithDisabledOption(nil, nil, false, "user-b")
	if err != nil {
		t.Fatalf("AcquireImageAuthLeaseForUserFilteredWithDisabledOption(user-b) error = %v", err)
	}
	if authB.AccessToken != "token-second" {
		t.Fatalf("user-b token = %q, want token-second", authB.AccessToken)
	}
	releaseB()

	authAAgain, _, releaseAAgain, err := store.AcquireImageAuthLeaseForUserFilteredWithDisabledOption(nil, nil, false, "user-a")
	if err != nil {
		t.Fatalf("AcquireImageAuthLeaseForUserFilteredWithDisabledOption(user-a again) error = %v", err)
	}
	defer releaseAAgain()
	if authAAgain.AccessToken != "token-first" {
		t.Fatalf("user-a again token = %q, want token-first", authAAgain.AccessToken)
	}

	_, _, _, err = store.AcquireImageAuthLeaseForUserFilteredWithDisabledOption(nil, nil, false, "user-c")
	if !errors.Is(err, ErrNoAvailableImageAuth) {
		t.Fatalf("AcquireImageAuthLeaseForUserFilteredWithDisabledOption(user-c) error = %v, want ErrNoAvailableImageAuth", err)
	}
}

func TestAcquireImageAuthLeaseForUserConversationKeepsRouteBindingsSeparate(t *testing.T) {
	rootDir := t.TempDir()
	authDir := filepath.Join(rootDir, "auths")
	syncDir := filepath.Join(rootDir, "sync")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		t.Fatalf("mkdir sync dir: %v", err)
	}

	store := &Store{
		authDir:      authDir,
		syncStateDir: syncDir,
		stateFile:    filepath.Join(rootDir, "state.json"),
		defaultQuota: 5,
		providerType: "codex",
		states: map[string]RuntimeState{
			"first.json":  {Type: "Plus", Status: "正常", Quota: 5, QuotaKnown: true, ImageRoutes: []string{"legacy", "responses"}},
			"second.json": {Type: "Plus", Status: "正常", Quota: 5, QuotaKnown: true, ImageRoutes: []string{"legacy", "responses"}},
		},
	}

	if err := writeJSONFile(filepath.Join(authDir, "first.json"), map[string]any{
		"type":         "codex",
		"access_token": "token-first",
		"email":        "first@example.com",
	}); err != nil {
		t.Fatalf("seed first auth file: %v", err)
	}
	if err := writeJSONFile(filepath.Join(authDir, "second.json"), map[string]any{
		"type":         "codex",
		"access_token": "token-second",
		"email":        "second@example.com",
	}); err != nil {
		t.Fatalf("seed second auth file: %v", err)
	}

	authR, _, releaseR, err := store.AcquireImageAuthLeaseForUserConversationRouteFilteredWithDisabledOption(nil, nil, false, "user-a", "conv-1", "responses")
	if err != nil {
		t.Fatalf("Acquire responses route error = %v", err)
	}
	if authR.AccessToken != "token-first" {
		t.Fatalf("responses token = %q, want token-first", authR.AccessToken)
	}
	releaseR()

	authL, _, releaseL, err := store.AcquireImageAuthLeaseForUserConversationRouteFilteredWithDisabledOption(nil, nil, false, "user-a", "conv-1", "legacy")
	if err != nil {
		t.Fatalf("Acquire legacy route error = %v", err)
	}
	defer releaseL()
	if authL.AccessToken != "token-second" {
		t.Fatalf("legacy token = %q, want token-second", authL.AccessToken)
	}
}

func TestMarkImageAccountLimitedKeepsLegacyRoute(t *testing.T) {
	rootDir := t.TempDir()
	authDir := filepath.Join(rootDir, "auths")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := writeJSONFile(filepath.Join(authDir, "plus.json"), map[string]any{"access_token": "token-plus"}); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	store := &Store{
		authDir:   authDir,
		stateFile: filepath.Join(rootDir, "state.json"),
		states: map[string]RuntimeState{
			"plus.json": {
				Type:        "Plus",
				Status:      "正常",
				Quota:       5,
				QuotaKnown:  true,
				ImageRoutes: []string{"legacy", "responses"},
			},
		},
	}

	store.MarkImageAccountLimited("token-plus")
	account := store.buildPublicAccount(LocalAuth{Name: "plus.json", AccessToken: "token-plus"}, SyncState{}, nil)

	if !AccountSupportsImageRoute(account, "legacy") {
		t.Fatalf("ImageRoutes = %v, want legacy retained", account.ImageRoutes)
	}
	if AccountSupportsImageRoute(account, "responses") {
		t.Fatalf("ImageRoutes = %v, want responses removed", account.ImageRoutes)
	}
	if account.Status != "正常" {
		t.Fatalf("Status = %q, want 正常", account.Status)
	}
}

func TestDisableImageAccountByTokenMarksAccountDisabled(t *testing.T) {
	rootDir := t.TempDir()
	authDir := filepath.Join(rootDir, "auths")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := writeJSONFile(filepath.Join(authDir, "plus.json"), map[string]any{"access_token": "token-plus"}); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	store := &Store{
		authDir:   authDir,
		stateFile: filepath.Join(rootDir, "state.json"),
		states: map[string]RuntimeState{
			"plus.json": {
				Type:        "Plus",
				Status:      "正常",
				Quota:       5,
				QuotaKnown:  true,
				ImageRoutes: []string{"legacy", "responses"},
			},
		},
	}

	store.DisableImageAccountByToken("token-plus", "限流")
	account := store.buildPublicAccount(LocalAuth{Name: "plus.json", AccessToken: "token-plus", Disabled: true}, SyncState{}, nil)

	if account.Status != "限流" {
		t.Fatalf("Status = %q, want 限流", account.Status)
	}
	if len(account.ImageRoutes) != 0 {
		t.Fatalf("ImageRoutes = %v, want empty", account.ImageRoutes)
	}
	if !account.Disabled {
		t.Fatal("Disabled = false, want true")
	}
}

func TestBuildPublicAccountDoesNotInferResponsesRouteFromHighQuota(t *testing.T) {
	store := &Store{
		states: map[string]RuntimeState{
			"plus.json": {
				Type:        "Plus",
				Status:      "正常",
				Quota:       120,
				QuotaKnown:  true,
				ImageRoutes: []string{"legacy"},
			},
		},
	}

	account := store.buildPublicAccount(LocalAuth{Name: "plus.json", AccessToken: "token-plus"}, SyncState{}, nil)

	if AccountSupportsImageRoute(account, "responses") {
		t.Fatalf("ImageRoutes = %v, want no responses inference from quota only", account.ImageRoutes)
	}
}

func TestAcquireImageAuthFilteredWithDisabledOptionAllowsDisabledAccount(t *testing.T) {
	rootDir := t.TempDir()
	authDir := filepath.Join(rootDir, "auths")
	syncDir := filepath.Join(rootDir, "sync")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		t.Fatalf("mkdir sync dir: %v", err)
	}

	store := &Store{
		authDir:      authDir,
		syncStateDir: syncDir,
		stateFile:    filepath.Join(rootDir, "state.json"),
		defaultQuota: 5,
		providerType: "codex",
		states: map[string]RuntimeState{
			"disabled.json": {
				Type:       "Plus",
				Status:     "禁用",
				Quota:      3,
				QuotaKnown: true,
			},
		},
	}

	if err := writeJSONFile(filepath.Join(authDir, "disabled.json"), map[string]any{
		"type":         "codex",
		"access_token": "token-disabled",
		"email":        "disabled@example.com",
		"disabled":     true,
	}); err != nil {
		t.Fatalf("seed disabled auth file: %v", err)
	}

	if _, _, err := store.AcquireImageAuthFiltered(nil, nil); !errors.Is(err, ErrNoAvailableImageAuth) {
		t.Fatalf("AcquireImageAuthFiltered() error = %v, want ErrNoAvailableImageAuth", err)
	}

	_, account, err := store.AcquireImageAuthFilteredWithDisabledOption(nil, nil, true)
	if err != nil {
		t.Fatalf("AcquireImageAuthFilteredWithDisabledOption() returned error: %v", err)
	}
	if account.Email != "disabled@example.com" {
		t.Fatalf("AcquireImageAuthFilteredWithDisabledOption() email = %q, want %q", account.Email, "disabled@example.com")
	}
}

func TestAddAccountsMarksTokenSourceKind(t *testing.T) {
	rootDir := t.TempDir()
	authDir := filepath.Join(rootDir, "auths")
	syncDir := filepath.Join(rootDir, "sync")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		t.Fatalf("mkdir sync dir: %v", err)
	}

	store := &Store{
		authDir:      authDir,
		syncStateDir: syncDir,
		stateFile:    filepath.Join(rootDir, "state.json"),
		defaultQuota: 5,
		providerType: "codex",
		states:       map[string]RuntimeState{},
	}

	if _, _, err := store.AddAccounts([]string{"token-import-1"}); err != nil {
		t.Fatalf("AddAccounts() returned error: %v", err)
	}

	auths, err := store.loadAuths()
	if err != nil {
		t.Fatalf("loadAuths() returned error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("loadAuths() count = %d, want 1", len(auths))
	}
	if auths[0].SourceKind != AccountSourceKindToken {
		t.Fatalf("SourceKind = %q, want %q", auths[0].SourceKind, AccountSourceKindToken)
	}

	items, err := store.ListAccounts()
	if err != nil {
		t.Fatalf("ListAccounts() returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListAccounts() count = %d, want 1", len(items))
	}
	if items[0].SourceKind != AccountSourceKindToken {
		t.Fatalf("public SourceKind = %q, want %q", items[0].SourceKind, AccountSourceKindToken)
	}
	if items[0].SyncStatus != "" {
		t.Fatalf("token account SyncStatus = %q, want empty", items[0].SyncStatus)
	}
}

func TestImportAuthFilesPreservesExplicitTokenSourceKind(t *testing.T) {
	rootDir := t.TempDir()
	authDir := filepath.Join(rootDir, "auths")
	syncDir := filepath.Join(rootDir, "sync")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		t.Fatalf("mkdir sync dir: %v", err)
	}

	store := &Store{
		authDir:      authDir,
		syncStateDir: syncDir,
		stateFile:    filepath.Join(rootDir, "state.json"),
		defaultQuota: 5,
		providerType: "codex",
		states:       map[string]RuntimeState{},
	}

	imported, _, skipped, failures, err := store.ImportAuthFiles([]ImportedAuthFile{{
		Name: "demo.json",
		Data: []byte(`{"type":"codex","access_token":"token-auth-file","email":"demo@example.com","source_kind":"token"}`),
	}})
	if err != nil {
		t.Fatalf("ImportAuthFiles() returned error: %v", err)
	}
	if imported != 1 || len(skipped) != 0 || len(failures) != 0 {
		t.Fatalf("ImportAuthFiles() = imported %d skipped %d failures %d", imported, len(skipped), len(failures))
	}

	auths, err := store.loadAuths()
	if err != nil {
		t.Fatalf("loadAuths() returned error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("loadAuths() count = %d, want 1", len(auths))
	}
	if auths[0].SourceKind != AccountSourceKindToken {
		t.Fatalf("SourceKind = %q, want %q", auths[0].SourceKind, AccountSourceKindToken)
	}
}

func TestImportAuthFilesInfersLegacyTokenSourceKindWhenMissingField(t *testing.T) {
	rootDir := t.TempDir()
	authDir := filepath.Join(rootDir, "auths")
	syncDir := filepath.Join(rootDir, "sync")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		t.Fatalf("mkdir sync dir: %v", err)
	}

	store := &Store{
		authDir:      authDir,
		syncStateDir: syncDir,
		stateFile:    filepath.Join(rootDir, "state.json"),
		defaultQuota: 5,
		providerType: "codex",
		states:       map[string]RuntimeState{},
	}

	imported, _, skipped, failures, err := store.ImportAuthFiles([]ImportedAuthFile{{
		Name: "legacy-token.json",
		Data: []byte(`{"type":"codex","access_token":"legacy-token","email":"legacy@example.com","created_at":"2026-04-01T00:00:00Z"}`),
	}})
	if err != nil {
		t.Fatalf("ImportAuthFiles() returned error: %v", err)
	}
	if imported != 1 || len(skipped) != 0 || len(failures) != 0 {
		t.Fatalf("ImportAuthFiles() = imported %d skipped %d failures %d", imported, len(skipped), len(failures))
	}

	auths, err := store.loadAuths()
	if err != nil {
		t.Fatalf("loadAuths() returned error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("loadAuths() count = %d, want 1", len(auths))
	}
	if auths[0].SourceKind != AccountSourceKindToken {
		t.Fatalf("SourceKind = %q, want %q", auths[0].SourceKind, AccountSourceKindToken)
	}
}

func TestParseLocalAuthInfersLegacyTokenSourceKindWhenMissingField(t *testing.T) {
	auth, err := parseLocalAuth(
		"legacy.json",
		"legacy.json",
		[]byte(`{"type":"codex","access_token":"legacy-token","email":"legacy@example.com","chatgpt_plan_type":"plus"}`),
		"codex",
	)
	if err != nil {
		t.Fatalf("parseLocalAuth() returned error: %v", err)
	}
	if auth.SourceKind != AccountSourceKindToken {
		t.Fatalf("SourceKind = %q, want %q", auth.SourceKind, AccountSourceKindToken)
	}
	if got := stringValue(auth.Data["source_kind"]); got != AccountSourceKindToken {
		t.Fatalf("persisted source_kind = %q, want %q", got, AccountSourceKindToken)
	}
}

func TestGuessPlanFromPayloadDecodesIDToken(t *testing.T) {
	payload := map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type": "plus",
		},
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	idToken := "e30." + base64.RawURLEncoding.EncodeToString(rawPayload) + ".sig"

	if got := guessPlanFromPayload(map[string]any{"id_token": idToken}); got != "Plus" {
		t.Fatalf("expected Plus from id_token, got %q", got)
	}
	if got := guessPlanFromPayload(map[string]any{"id_token": payload}); got != "Plus" {
		t.Fatalf("expected Plus from decoded id_token object, got %q", got)
	}
}

func TestBuildPublicAccountLetsPayloadPlusOverrideStaleFreeState(t *testing.T) {
	idToken := mustTestJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type": "plus",
		},
	})
	store := &Store{
		defaultQuota: 5,
		states: map[string]RuntimeState{
			"acct.json": {Type: "Free"},
		},
	}

	got := store.buildPublicAccount(
		LocalAuth{Name: "acct.json", AccessToken: "token-1", Data: map[string]any{"id_token": idToken}},
		SyncState{},
		nil,
	)

	if got.Type != "Plus" {
		t.Fatalf("expected stale Free state to be corrected to Plus, got %q", got.Type)
	}
}

func TestParseLocalAuthKeepsFullAuthFilesAsAuthFileSourceKind(t *testing.T) {
	auth, err := parseLocalAuth(
		"full.json",
		"full.json",
		[]byte(`{"type":"codex","access_token":"full-token","email":"full@example.com","cookies":"a=b","account_id":"acct-1"}`),
		"codex",
	)
	if err != nil {
		t.Fatalf("parseLocalAuth() returned error: %v", err)
	}
	if auth.SourceKind != AccountSourceKindAuthFile {
		t.Fatalf("SourceKind = %q, want %q", auth.SourceKind, AccountSourceKindAuthFile)
	}
}

func mustTestJWT(t *testing.T, payload map[string]any) string {
	t.Helper()

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(raw) + ".signature"
}
