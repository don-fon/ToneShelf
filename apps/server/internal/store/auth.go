package store

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

const accountKind = "account-pbkdf2-sha256-v1"
const SessionLifetime = 30 * 24 * time.Hour

var ErrInvalidCredential = errors.New("账号无效或已停用")

func RandomCredential() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// SetAccount is a server administration operation; it never changes ownership.
func (s *Store) SetAccount(ctx context.Context, userID, name, account string, create bool) error {
	if strings.TrimSpace(userID) == "" || len(userID) > 128 || strings.TrimSpace(account) == "" || len(account) > 256 {
		return errors.New("用户标识及账号不能为空，账号最多 256 字节")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	verifier, err := pbkdf2.Key(sha256.New, account, salt, 600000, 32)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=?)`, userID).Scan(&exists); err != nil {
		return err
	}
	if !exists && !create {
		return errors.New("用户不存在")
	}
	now := nowText()
	if !exists {
		if name == "" {
			name = userID
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO users(id,display_name,created_at) VALUES(?,?,?)`, userID, name, now); err != nil {
			return err
		}
		playlistID, err := RandomCredential()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO playlists(id,owner_user_id,name,is_default,created_at,updated_at) VALUES(?,?,?,1,?,?)`, playlistID, userID, "默认歌单", now, now); err != nil {
			return err
		}
	}
	if err = ensureDefaultTagGroupTx(ctx, tx, userID); err != nil {
		return err
	}
	var configured bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_credentials WHERE user_id=? AND kind=?)`, userID, accountKind).Scan(&configured); err != nil {
		return err
	}
	if create && configured {
		return errors.New("账号已配置，请使用 reset 重置")
	}
	existing, err := tx.QueryContext(ctx, `SELECT salt,verifier FROM user_credentials WHERE user_id<>? AND kind=?`, userID, accountKind)
	if err != nil {
		return err
	}
	type storedCredential struct{ salt, verifier []byte }
	var others []storedCredential
	for existing.Next() {
		var item storedCredential
		if err = existing.Scan(&item.salt, &item.verifier); err != nil {
			existing.Close()
			return err
		}
		others = append(others, item)
	}
	err = existing.Err()
	existing.Close()
	if err != nil {
		return err
	}
	for _, item := range others {
		derived, err := pbkdf2.Key(sha256.New, account, item.salt, 600000, 32)
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare(derived, item.verifier) == 1 {
			return errors.New("账号已被其他用户使用")
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_credentials(user_id,kind,salt,verifier,created_at) VALUES(?,?,?,?,?) ON CONFLICT(user_id,kind) DO UPDATE SET salt=excluded.salt,verifier=excluded.verifier,created_at=excluded.created_at`, userID, accountKind, salt, verifier, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM login_sessions WHERE user_id=?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DisableAccount(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_credentials WHERE user_id=?`, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM login_sessions WHERE user_id=?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

type AccountInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func (s *Store) Accounts(ctx context.Context) ([]AccountInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id,u.display_name,EXISTS(SELECT 1 FROM user_credentials c WHERE c.user_id=u.id) FROM users u ORDER BY u.created_at,u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AccountInfo, 0)
	for rows.Next() {
		var item AccountInfo
		if err = rows.Scan(&item.ID, &item.Name, &item.Enabled); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) AuthenticateAccount(ctx context.Context, account string) (string, []byte, error) {
	if strings.TrimSpace(account) == "" || len(account) > 256 {
		return "", nil, ErrInvalidCredential
	}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id,salt,verifier FROM user_credentials WHERE kind=?`, accountKind)
	if err != nil {
		return "", nil, err
	}
	type credential struct {
		id             string
		salt, verifier []byte
	}
	var items []credential
	for rows.Next() {
		var item credential
		if err = rows.Scan(&item.id, &item.salt, &item.verifier); err != nil {
			rows.Close()
			return "", nil, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", nil, err
	}
	match := ""
	var proof []byte
	for _, item := range items {
		if err = ctx.Err(); err != nil {
			return "", nil, err
		}
		derived, err := pbkdf2.Key(sha256.New, account, item.salt, 600000, 32)
		if err != nil {
			return "", nil, err
		}
		if subtle.ConstantTimeCompare(derived, item.verifier) == 1 {
			if match != "" {
				return "", nil, ErrInvalidCredential
			}
			match = item.id
			proof = item.verifier
		}
	}
	if match == "" {
		return "", nil, ErrInvalidCredential
	}
	return match, proof, nil
}

func (s *Store) CreateSession(ctx context.Context, userID string, proof []byte, now time.Time) (string, error) {
	token, err := RandomCredential()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(token))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM login_sessions WHERE expires_at<=?`, now.Unix()); err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO login_sessions(token_hash,user_id,expires_at,created_at) SELECT ?,?,?,? WHERE EXISTS(SELECT 1 FROM user_credentials WHERE user_id=? AND kind=? AND verifier=?)`, hash[:], userID, now.Add(SessionLifetime).Unix(), now.Unix(), userID, accountKind, proof)
	if err != nil {
		return "", err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", ErrInvalidCredential
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) SessionUser(ctx context.Context, token string, now time.Time) (string, error) {
	if len(token) != 43 {
		return "", ErrInvalidCredential
	}
	hash := sha256.Sum256([]byte(token))
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT s.user_id FROM login_sessions s WHERE s.token_hash=? AND s.expires_at>? AND EXISTS(SELECT 1 FROM user_credentials c WHERE c.user_id=s.user_id AND c.kind=?)`, hash[:], now.Unix(), accountKind).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidCredential
	}
	return id, err
}
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	hash := sha256.Sum256([]byte(token))
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_sessions WHERE token_hash=?`, hash[:])
	return err
}
