package users

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"chatgpt2api/internal/config"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	DailyFreeImageLimit = 120
	DailyPaidImageLimit = 30
)

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email,omitempty"`
	Name        string `json:"name,omitempty"`
	Role        string `json:"role"`
	ImageAPIKey string `json:"imageApiKey,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
}

type Invite struct {
	Code              string `json:"code"`
	CreatedBy         string `json:"createdBy,omitempty"`
	CreatedAt         string `json:"createdAt,omitempty"`
	UsedByUserID      string `json:"usedByUserId,omitempty"`
	UsedByUsername    string `json:"usedByUsername,omitempty"`
	UsedByDisplayName string `json:"usedByDisplayName,omitempty"`
	UsedAt            string `json:"usedAt,omitempty"`
}

type DailyImageQuota struct {
	DateKey       string `json:"dateKey"`
	FreeLimit     int    `json:"freeLimit"`
	FreeUsed      int    `json:"freeUsed"`
	FreeRemaining int    `json:"freeRemaining"`
	PaidLimit     int    `json:"paidLimit"`
	PaidUsed      int    `json:"paidUsed"`
	PaidRemaining int    `json:"paidRemaining"`
}

type Store struct {
	db *sql.DB
}

func NewStore(cfg *config.Config) (*Store, error) {
	path := cfg.ResolvePath(cfg.Storage.SQLitePath)
	if path == "" {
		path = cfg.ResolvePath("data/chatgpt-image-studio.db")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := configureSQLiteDB(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &Store{db: db}
	if err := store.Init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func configureSQLiteDB(db *sql.DB) error {
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 10000`); err != nil {
		return err
	}
	_, err := db.Exec(`PRAGMA journal_mode = WAL`)
	return err
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Init(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS app_users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'user',
			image_api_key TEXT NOT NULL DEFAULT '',
			disabled_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS app_user_sessions (
			token_hash TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			FOREIGN KEY(user_id) REFERENCES app_users(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS app_invites (
			code TEXT PRIMARY KEY,
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			used_by_user_id TEXT NOT NULL DEFAULT '',
			used_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS app_user_daily_image_quotas (
			user_id TEXT NOT NULL,
			quota_date TEXT NOT NULL,
			free_used INTEGER NOT NULL DEFAULT 0,
			paid_used INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(user_id, quota_date)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_app_user_sessions_user_id ON app_user_sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_app_invites_created_at ON app_invites(created_at DESC)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE app_users ADD COLUMN username TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE app_users ADD COLUMN image_api_key TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE app_users ADD COLUMN disabled_at TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	if err := s.ensureUsernames(ctx); err != nil {
		return err
	}
	if err := s.ensureImageAPIKeys(ctx); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_app_users_username ON app_users(username)`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_app_users_image_api_key ON app_users(image_api_key)`); err != nil {
		return err
	}
	return nil
}

func (s *Store) RegisterWithInvite(ctx context.Context, username, password, name, inviteCode string) (*User, error) {
	username = normalizeUsername(username)
	name = strings.TrimSpace(name)
	inviteCode = normalizeInviteCode(inviteCode)
	if username == "" {
		return nil, fmt.Errorf("请输入用户名")
	}
	if !isValidUsername(username) {
		return nil, fmt.Errorf("用户名仅支持 3-32 位小写字母、数字、下划线和中横线")
	}
	if username == "admin" {
		return nil, fmt.Errorf("admin 是内置管理员用户名，不能注册")
	}
	if inviteCode == "" {
		return nil, fmt.Errorf("请输入邀请码")
	}
	if len([]rune(password)) < 6 {
		return nil, fmt.Errorf("密码至少 6 位")
	}
	if name == "" {
		name = username
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	user := &User{ID: newUserID(), Username: username, Email: "", Name: name, Role: RoleUser, ImageAPIKey: newImageAPIKey(), CreatedAt: now}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var usedBy string
	err = tx.QueryRowContext(ctx, `SELECT used_by_user_id FROM app_invites WHERE code = ?`, inviteCode).Scan(&usedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("邀请码不存在")
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(usedBy) != "" {
		return nil, fmt.Errorf("邀请码已被使用")
	}
	storedEmail := username + "@local.user"
	_, err = tx.ExecContext(ctx, `INSERT INTO app_users (id,username,email,name,password_hash,role,image_api_key,created_at) VALUES (?,?,?,?,?,?,?,?)`, user.ID, user.Username, storedEmail, user.Name, string(hash), user.Role, user.ImageAPIKey, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, fmt.Errorf("用户名已存在")
		}
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE app_invites SET used_by_user_id = ?, used_at = ? WHERE code = ? AND COALESCE(used_by_user_id, '') = ''`, user.ID, now, inviteCode)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return user, nil
}

func (s *Store) Authenticate(ctx context.Context, username, password string) (*User, error) {
	username = normalizeUsername(username)
	var user User
	var hash string
	var disabledAt string
	err := s.db.QueryRowContext(ctx, `SELECT id,COALESCE(username, ''),COALESCE(email, ''),COALESCE(name, ''),password_hash,role,image_api_key,created_at,COALESCE(disabled_at, '') FROM app_users WHERE username = ? OR email = ?`, username, username).Scan(&user.ID, &user.Username, &user.Email, &user.Name, &hash, &user.Role, &user.ImageAPIKey, &user.CreatedAt, &disabledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("用户名或密码不正确")
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(disabledAt) != "" {
		return nil, fmt.Errorf("用户已被禁用")
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, fmt.Errorf("用户名或密码不正确")
	}
	user.Disabled = false
	if strings.HasSuffix(user.Email, "@local.user") {
		user.Email = ""
	}
	return &user, nil
}

func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `INSERT INTO app_user_sessions (token_hash,user_id,created_at,expires_at) VALUES (?,?,?,?)`, hashToken(token), userID, now.Format(time.RFC3339Nano), now.Add(ttl).Format(time.RFC3339Nano))
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) UserBySession(ctx context.Context, token string) (*User, error) {
	var user User
	var expiresAt string
	var disabledAt string
	err := s.db.QueryRowContext(ctx, `SELECT u.id,COALESCE(u.username, ''),COALESCE(u.email, ''),COALESCE(u.name, ''),u.role,u.image_api_key,u.created_at,s.expires_at,COALESCE(u.disabled_at, '') FROM app_user_sessions s JOIN app_users u ON u.id = s.user_id WHERE s.token_hash = ?`, hashToken(token)).Scan(&user.ID, &user.Username, &user.Email, &user.Name, &user.Role, &user.ImageAPIKey, &user.CreatedAt, &expiresAt, &disabledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("session not found")
	}
	if err != nil {
		return nil, err
	}
	expires, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err == nil && time.Now().UTC().After(expires) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM app_user_sessions WHERE token_hash = ?`, hashToken(token))
		return nil, fmt.Errorf("session expired")
	}
	if strings.TrimSpace(disabledAt) != "" {
		return nil, fmt.Errorf("用户已被禁用")
	}
	if strings.HasSuffix(user.Email, "@local.user") {
		user.Email = ""
	}
	return &user, nil
}

func (s *Store) UserByImageAPIKey(ctx context.Context, apiKey string) (*User, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("image api key is required")
	}
	var user User
	var disabledAt string
	err := s.db.QueryRowContext(ctx, `SELECT id,COALESCE(username, ''),COALESCE(email, ''),COALESCE(name, ''),role,image_api_key,created_at,COALESCE(disabled_at, '') FROM app_users WHERE image_api_key = ?`, apiKey).Scan(&user.ID, &user.Username, &user.Email, &user.Name, &user.Role, &user.ImageAPIKey, &user.CreatedAt, &disabledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("image api key not found")
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(disabledAt) != "" {
		return nil, fmt.Errorf("用户已被禁用")
	}
	if strings.HasSuffix(user.Email, "@local.user") {
		user.Email = ""
	}
	return &user, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE(username, ''),COALESCE(email, ''),COALESCE(name, ''),role,image_api_key,created_at,COALESCE(disabled_at, '') FROM app_users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]User, 0)
	for rows.Next() {
		var item User
		var disabledAt string
		if err := rows.Scan(&item.ID, &item.Username, &item.Email, &item.Name, &item.Role, &item.ImageAPIKey, &item.CreatedAt, &disabledAt); err != nil {
			return nil, err
		}
		item.Disabled = strings.TrimSpace(disabledAt) != ""
		if strings.HasSuffix(item.Email, "@local.user") {
			item.Email = ""
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SetUserDisabled(ctx context.Context, userID string, disabled bool) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("user id is required")
	}
	disabledAt := ""
	if disabled {
		disabledAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE app_users SET disabled_at = ? WHERE id = ?`, disabledAt, userID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return fmt.Errorf("user not found")
	}
	if disabled {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM app_user_sessions WHERE user_id = ?`, userID)
	}
	return nil
}

func (s *Store) DeleteUser(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("user id is required")
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM app_user_sessions WHERE user_id = ?`, userID)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM app_user_daily_image_quotas WHERE user_id = ?`, userID)
	_, _ = s.db.ExecContext(ctx, `UPDATE app_invites SET used_by_user_id = '', used_at = '' WHERE used_by_user_id = ?`, userID)
	result, err := s.db.ExecContext(ctx, `DELETE FROM app_users WHERE id = ?`, userID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

func (s *Store) CreateInvite(ctx context.Context, createdBy string) (*Invite, error) {
	createdBy = strings.TrimSpace(createdBy)
	invite := &Invite{
		Code:      newInviteCode(),
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO app_invites (code,created_by,created_at,used_by_user_id,used_at) VALUES (?,?,?,?,?)`, invite.Code, invite.CreatedBy, invite.CreatedAt, "", "")
	if err != nil {
		return nil, err
	}
	return invite, nil
}

func (s *Store) ListInvites(ctx context.Context) ([]Invite, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.code,COALESCE(i.created_by, ''),COALESCE(i.created_at, ''),COALESCE(i.used_by_user_id, ''),COALESCE(i.used_at, ''),COALESCE(u.username, ''),COALESCE(u.name, '') FROM app_invites i LEFT JOIN app_users u ON u.id = i.used_by_user_id ORDER BY i.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Invite, 0)
	for rows.Next() {
		var item Invite
		if err := rows.Scan(&item.Code, &item.CreatedBy, &item.CreatedAt, &item.UsedByUserID, &item.UsedAt, &item.UsedByUsername, &item.UsedByDisplayName); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ensureImageAPIKeys(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM app_users WHERE COALESCE(image_api_key, '') = ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `UPDATE app_users SET image_api_key = ? WHERE id = ?`, newImageAPIKey(), id); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) ensureUsernames(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,email FROM app_users WHERE COALESCE(username, '') = ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type userSeed struct {
		id    string
		email string
	}
	items := make([]userSeed, 0)
	for rows.Next() {
		var item userSeed
		if err := rows.Scan(&item.id, &item.email); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		base := normalizeUsername(strings.TrimSuffix(item.email, filepath.Ext(item.email)))
		if base == "" {
			base = "user"
		}
		candidate := base
		for index := 1; ; index++ {
			var exists int
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM app_users WHERE username = ? AND id <> ?`, candidate, item.id).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				if _, err := s.db.ExecContext(ctx, `UPDATE app_users SET username = ? WHERE id = ? AND COALESCE(username, '') = ''`, candidate, item.id); err != nil {
					return err
				}
				break
			}
			candidate = fmt.Sprintf("%s-%d", base, index)
		}
	}
	return nil
}

func (s *Store) GetDailyImageQuota(ctx context.Context, userID string) (DailyImageQuota, error) {
	status := DailyImageQuota{
		DateKey:       currentQuotaDateKey(),
		FreeLimit:     DailyFreeImageLimit,
		PaidLimit:     DailyPaidImageLimit,
		FreeRemaining: DailyFreeImageLimit,
		PaidRemaining: DailyPaidImageLimit,
	}
	userID = strings.TrimSpace(userID)
	if userID == "" || userID == "admin" {
		return status, nil
	}
	var freeUsed, paidUsed int
	err := s.db.QueryRowContext(ctx, `SELECT free_used, paid_used FROM app_user_daily_image_quotas WHERE user_id = ? AND quota_date = ?`, userID, status.DateKey).Scan(&freeUsed, &paidUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	status.FreeUsed = freeUsed
	status.PaidUsed = paidUsed
	status.FreeRemaining = maxInt(0, status.FreeLimit-freeUsed)
	status.PaidRemaining = maxInt(0, status.PaidLimit-paidUsed)
	return status, nil
}

func (s *Store) ConsumeDailyImageQuota(ctx context.Context, userID, quotaKind string) (DailyImageQuota, error) {
	status := DailyImageQuota{
		DateKey:       currentQuotaDateKey(),
		FreeLimit:     DailyFreeImageLimit,
		PaidLimit:     DailyPaidImageLimit,
		FreeRemaining: DailyFreeImageLimit,
		PaidRemaining: DailyPaidImageLimit,
	}
	userID = strings.TrimSpace(userID)
	if userID == "" || userID == "admin" {
		return status, nil
	}
	quotaKind = strings.ToLower(strings.TrimSpace(quotaKind))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return status, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, `INSERT INTO app_user_daily_image_quotas (user_id, quota_date, free_used, paid_used, updated_at) VALUES (?, ?, 0, 0, ?) ON CONFLICT(user_id, quota_date) DO NOTHING`, userID, status.DateKey, now); err != nil {
		return status, err
	}
	var result sql.Result
	switch quotaKind {
	case "paid":
		result, err = tx.ExecContext(ctx, `UPDATE app_user_daily_image_quotas SET paid_used = paid_used + 1, updated_at = ? WHERE user_id = ? AND quota_date = ? AND paid_used < ?`, now, userID, status.DateKey, DailyPaidImageLimit)
	default:
		result, err = tx.ExecContext(ctx, `UPDATE app_user_daily_image_quotas SET free_used = free_used + 1, updated_at = ? WHERE user_id = ? AND quota_date = ? AND free_used < ?`, now, userID, status.DateKey, DailyFreeImageLimit)
	}
	if err != nil {
		return status, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return status, err
	}
	if rowsAffected == 0 {
		if quotaKind == "paid" {
			return status, fmt.Errorf("今日 paid 图片额度已用完")
		}
		return status, fmt.Errorf("今日 free 图片额度已用完")
	}
	if err := tx.Commit(); err != nil {
		return status, err
	}
	committed = true
	return s.GetDailyImageQuota(ctx, userID)
}

func currentQuotaDateKey() string {
	shanghai := time.FixedZone("CST", 8*3600)
	return time.Now().In(shanghai).Format("2006-01-02")
}

func normalizeUsername(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	builder := strings.Builder{}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '_' || r == '-':
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func isValidUsername(value string) bool {
	if len(value) < 3 || len(value) > 32 {
		return false
	}
	return value == normalizeUsername(value)
}

func normalizeInviteCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func newUserID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("user-%d", time.Now().UnixNano())
	}
	return "user_" + hex.EncodeToString(b)
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "usr_" + hex.EncodeToString(b), nil
}

func newImageAPIKey() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("img_%d", time.Now().UnixNano())
	}
	return "img_" + hex.EncodeToString(b)
}

func newInviteCode() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("INV-%d", time.Now().UnixNano())
	}
	hexPart := strings.ToUpper(hex.EncodeToString(b))
	return fmt.Sprintf("INV-%s-%s", hexPart[:4], hexPart[4:8])
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
