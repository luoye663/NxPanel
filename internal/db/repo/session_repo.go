// repo 包 — sessions 表的读写
package repo

import (
	"database/sql"
	"fmt"
	"time"
)

// SessionRepo 提供 sessions 表的数据访问
type SessionRepo struct {
	db *sql.DB
}

// NewSessionRepo 创建 session repository
func NewSessionRepo(db *sql.DB) *SessionRepo {
	return &SessionRepo{db: db}
}

// Create 创建新会话
func (r *SessionRepo) Create(s *Session) error {
	_, err := r.db.Exec(
		"INSERT INTO sessions (id, csrf_token_hash, user_agent, ip, expires_at, created_at, last_seen_at) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?)",
		s.ID, s.CSRFTokenHash, s.UserAgent, s.IP,
		s.ExpiresAt, s.CreatedAt, s.LastSeenAt,
	)
	if err != nil {
		return fmt.Errorf("创建 session 失败: %w", err)
	}
	return nil
}

// GetByID 根据 session ID 查找会话
func (r *SessionRepo) GetByID(id string) (*Session, error) {
	s := &Session{}
	err := r.db.QueryRow(
		"SELECT id, csrf_token_hash, user_agent, ip, expires_at, created_at, last_seen_at "+
			"FROM sessions WHERE id = ?",
		id,
	).Scan(&s.ID, &s.CSRFTokenHash, &s.UserAgent, &s.IP,
		&s.ExpiresAt, &s.CreatedAt, &s.LastSeenAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询 session 失败 id=%s: %w", id, err)
	}
	return s, nil
}

// TouchLastSeen 更新会话最后访问时间
func (r *SessionRepo) TouchLastSeen(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		"UPDATE sessions SET last_seen_at = ? WHERE id = ?",
		now, id,
	)
	if err != nil {
		return fmt.Errorf("更新 session last_seen_at 失败: %w", err)
	}
	return nil
}

// Delete 删除会话（登出）
func (r *SessionRepo) Delete(id string) error {
	_, err := r.db.Exec("DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("删除 session 失败: %w", err)
	}
	return nil
}

// DeleteAll 删除所有会话（用于修改密码后强制所有设备登出）
func (r *SessionRepo) DeleteAll() error {
	_, err := r.db.Exec("DELETE FROM sessions")
	if err != nil {
		return fmt.Errorf("删除所有 session 失败: %w", err)
	}
	return nil
}

// DeleteExpired 删除所有过期的会话
// 返回删除的数量
func (r *SessionRepo) DeleteExpired() (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.db.Exec(
		"DELETE FROM sessions WHERE expires_at < ?",
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("清理过期 session 失败: %w", err)
	}
	return result.RowsAffected()
}
