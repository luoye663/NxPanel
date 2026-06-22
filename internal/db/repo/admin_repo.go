// repo 包 — admin_account 表的读写
//
// admin_account 表只有一行（CHECK(id=1)），实现单管理员。
package repo

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// AdminRepo 提供 admin_account 表的数据访问
type AdminRepo struct {
	db *sql.DB
}

// NewAdminRepo 创建 admin repository
func NewAdminRepo(db *sql.DB) *AdminRepo {
	return &AdminRepo{db: db}
}

// Exists 检查管理员是否已初始化
func (r *AdminRepo) Exists() (bool, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM admin_account").Scan(&count)
	if err != nil {
		return false, fmt.Errorf("查询管理员是否存在失败: %w", err)
	}
	return count > 0, nil
}

// Create 初始化管理员账户（只能执行一次）
func (r *AdminRepo) Create(username, passwordHash, passwordAlgo string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		"INSERT INTO admin_account (id, username, password_hash, password_algo, created_at, updated_at) "+
			"VALUES (1, ?, ?, ?, ?, ?)",
		username, passwordHash, passwordAlgo, now, now,
	)
	if err != nil {
		return fmt.Errorf("创建管理员失败: %w", err)
	}
	return nil
}

// GetByUsername 根据用户名查找管理员
func (r *AdminRepo) GetByUsername(username string) (*Admin, error) {
	a := &Admin{}
	var totpEnabled int
	err := r.db.QueryRow(
		"SELECT id, username, password_hash, password_algo, COALESCE(totp_secret,''), COALESCE(totp_enabled,0), COALESCE(recovery_codes,'[]'), COALESCE(last_totp_code,''), COALESCE(last_totp_time,''), created_at, updated_at "+
			"FROM admin_account WHERE username = ?",
		username,
	).Scan(&a.ID, &a.Username, &a.PasswordHash, &a.PasswordAlgo, &a.TOTPSecret, &totpEnabled, &a.RecoveryCodes, &a.LastTOTPCode, &a.LastTOTPTime, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	a.TOTPEnabled = totpEnabled == 1
	if err != nil {
		return nil, fmt.Errorf("查询管理员失败 username=%s: %w", username, err)
	}
	return a, nil
}

// Get 获取唯一的管理员账户
func (r *AdminRepo) Get() (*Admin, error) {
	a := &Admin{}
	var totpEnabled int
	err := r.db.QueryRow(
		"SELECT id, username, password_hash, password_algo, COALESCE(totp_secret,''), COALESCE(totp_enabled,0), COALESCE(recovery_codes,'[]'), COALESCE(last_totp_code,''), COALESCE(last_totp_time,''), created_at, updated_at "+
			"FROM admin_account WHERE id = 1",
	).Scan(&a.ID, &a.Username, &a.PasswordHash, &a.PasswordAlgo, &a.TOTPSecret, &totpEnabled, &a.RecoveryCodes, &a.LastTOTPCode, &a.LastTOTPTime, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	a.TOTPEnabled = totpEnabled == 1
	if err != nil {
		return nil, fmt.Errorf("查询管理员失败: %w", err)
	}
	return a, nil
}

// UpdatePassword 更新管理员密码
func (r *AdminRepo) UpdatePassword(passwordHash, passwordAlgo string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		"UPDATE admin_account SET password_hash = ?, password_algo = ?, updated_at = ? WHERE id = 1",
		passwordHash, passwordAlgo, now,
	)
	if err != nil {
		return fmt.Errorf("更新管理员密码失败: %w", err)
	}
	return nil
}

// RecordTOTPUsage 是旧的无条件记录方法，仅保留给潜在历史调用兼容。
// 新的验证码验证流程必须使用 ConsumeTOTPCode，才能获得数据库 CAS 防重放保护。
func (r *AdminRepo) RecordTOTPUsage(code, timeStr string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		"UPDATE admin_account SET last_totp_code = ?, last_totp_time = ?, updated_at = ? WHERE id = 1",
		code, timeStr, now,
	)
	if err != nil {
		return fmt.Errorf("记录 TOTP 使用失败: %w", err)
	}
	return nil
}

// ConsumeTOTPCode 原子消费一次 TOTP 验证码。
//
// 这里把“是否在重放窗口内用过同一个 code”和“写入本次使用记录”放进同一条
// UPDATE 语句，依赖 SQLite 的行级写锁保证并发请求最多只有一个成功。
func (r *AdminRepo) ConsumeTOTPCode(adminID int, code string, usedAt time.Time, replayWindow time.Duration) (bool, error) {
	usedAtStr := usedAt.UTC().Format(time.RFC3339)
	cutoff := usedAt.UTC().Add(-replayWindow).Format(time.RFC3339)
	result, err := r.db.Exec(
		"UPDATE admin_account SET last_totp_code = ?, last_totp_time = ?, updated_at = ? "+
			"WHERE id = ? AND NOT (COALESCE(last_totp_code,'') = ? AND COALESCE(last_totp_time,'') >= ?)",
		code, usedAtStr, usedAtStr, adminID, code, cutoff,
	)
	if err != nil {
		return false, fmt.Errorf("消费 TOTP 验证码失败: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("检查 TOTP 消费结果失败: %w", err)
	}
	return n == 1, nil
}

func (r *AdminRepo) UpdateTOTP(secret string, enabled bool, recoveryCodes string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	e := 0
	if enabled {
		e = 1
	}
	_, err := r.db.Exec(
		"UPDATE admin_account SET totp_secret = ?, totp_enabled = ?, recovery_codes = ?, updated_at = ? WHERE id = 1",
		secret, e, recoveryCodes, now,
	)
	if err != nil {
		return fmt.Errorf("更新管理员 2FA 设置失败: %w", err)
	}
	return nil
}

func (r *AdminRepo) UpdateRecoveryCodes(recoveryCodes string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		"UPDATE admin_account SET recovery_codes = ?, updated_at = ? WHERE id = 1",
		recoveryCodes, now,
	)
	if err != nil {
		return fmt.Errorf("更新恢复码失败: %w", err)
	}
	return nil
}

func (r *AdminRepo) ConsumeRecoveryCode(codeHash string) (bool, error) {
	var current string
	err := r.db.QueryRow("SELECT recovery_codes FROM admin_account WHERE id = 1").Scan(&current)
	if err != nil {
		return false, fmt.Errorf("查询恢复码失败: %w", err)
	}

	var codes []string
	if err := json.Unmarshal([]byte(current), &codes); err != nil {
		return false, fmt.Errorf("解析恢复码失败: %w", err)
	}

	found := -1
	for i, h := range codes {
		if h == codeHash {
			found = i
			break
		}
	}
	if found < 0 {
		return false, nil
	}

	newCodes := append(codes[:found], codes[found+1:]...)
	newJSON, _ := json.Marshal(newCodes)
	now := time.Now().UTC().Format(time.RFC3339)

	result, err := r.db.Exec(
		"UPDATE admin_account SET recovery_codes = ?, updated_at = ? WHERE id = 1 AND recovery_codes = ?",
		string(newJSON), now, current,
	)
	if err != nil {
		return false, fmt.Errorf("消费恢复码失败: %w", err)
	}
	n, _ := result.RowsAffected()
	return n == 1, nil
}

func (r *AdminRepo) CountSessions() (int, error) {
	var count int
	now := time.Now().UTC().Format(time.RFC3339)
	err := r.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE expires_at > ?", now).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计会话数量失败: %w", err)
	}
	return count, nil
}

func (r *AdminRepo) DeleteOldestSession() error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		"DELETE FROM sessions WHERE id = (SELECT id FROM sessions WHERE expires_at > ? ORDER BY created_at ASC LIMIT 1)",
		now,
	)
	if err != nil {
		return fmt.Errorf("删除最旧会话失败: %w", err)
	}
	return nil
}
