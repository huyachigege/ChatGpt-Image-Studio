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
)

type User struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Name        string `json:"name,omitempty"`
	Role        string `json:"role"`
	ImageAPIKey string `json:"imageApiKey,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
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
	store := &Store{db: db}
	if err := store.Init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
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
			email TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'user',
			image_api_key TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS app_user_sessions (
			token_hash TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			FOREIGN KEY(user_id) REFERENCES app_users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_app_user_sessions_user_id ON app_user_sessions(user_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE app_users ADD COLUMN image_api_key TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_app_users_image_api_key ON app_users(image_api_key)`); err != nil {
		return err
	}
	if err := s.ensureImageAPIKeys(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Store) Register(ctx context.Context, email, password, name string) (*User, error) {
	email = normalizeEmail(email)
	name = strings.TrimSpace(name)
	if email == "" || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("请输入有效邮箱")
	}
	if len([]rune(password)) < 6 {
		return nil, fmt.Errorf("密码至少 6 位")
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	user := &User{ID: newUserID(), Email: email, Name: name, Role: RoleUser, ImageAPIKey: newImageAPIKey(), CreatedAt: now}
	_, err = s.db.ExecContext(ctx, `INSERT INTO app_users (id,email,name,password_hash,role,image_api_key,created_at) VALUES (?,?,?,?,?,?,?)`, user.ID, user.Email, user.Name, string(hash), user.Role, user.ImageAPIKey, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, fmt.Errorf("邮箱已注册")
		}
		return nil, err
	}
	return user, nil
}

func (s *Store) Authenticate(ctx context.Context, email, password string) (*User, error) {
	email = normalizeEmail(email)
	var user User
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT id,email,name,password_hash,role,image_api_key,created_at FROM app_users WHERE email = ?`, email).Scan(&user.ID, &user.Email, &user.Name, &hash, &user.Role, &user.ImageAPIKey, &user.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("邮箱或密码不正确")
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, fmt.Errorf("邮箱或密码不正确")
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
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.email,u.name,u.role,u.image_api_key,u.created_at,s.expires_at FROM app_user_sessions s JOIN app_users u ON u.id = s.user_id WHERE s.token_hash = ?`, hashToken(token)).Scan(&user.ID, &user.Email, &user.Name, &user.Role, &user.ImageAPIKey, &user.CreatedAt, &expiresAt)
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
	return &user, nil
}

func (s *Store) UserByImageAPIKey(ctx context.Context, apiKey string) (*User, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("image api key is required")
	}
	var user User
	err := s.db.QueryRowContext(ctx, `SELECT id,email,name,role,image_api_key,created_at FROM app_users WHERE image_api_key = ?`, apiKey).Scan(&user.ID, &user.Email, &user.Name, &user.Role, &user.ImageAPIKey, &user.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("image api key not found")
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
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

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
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

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}
