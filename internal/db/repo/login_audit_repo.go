package repo

import (
	"database/sql"
	"fmt"
	"time"
)

type LoginAuditRepo struct {
	db *sql.DB
}

func NewLoginAuditRepo(db *sql.DB) *LoginAuditRepo {
	return &LoginAuditRepo{db: db}
}

func (r *LoginAuditRepo) Record(a *LoginAudit) error {
	success := 0
	if a.Success {
		success = 1
	}
	captchaVerified := 0
	if a.CaptchaVerified {
		captchaVerified = 1
	}
	totpUsed := 0
	if a.TOTPUsed {
		totpUsed = 1
	}
	_, err := r.db.Exec(
		"INSERT INTO login_audit (username, ip, user_agent, success, failure_reason, captcha_verified, totp_used, created_at) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		a.Username, a.IP, a.UserAgent, success, a.FailureReason, captchaVerified, totpUsed,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("记录登录审计失败: %w", err)
	}
	return nil
}

func (r *LoginAuditRepo) CountFailuresByIP(ip string, since time.Time) (int, error) {
	var count int
	err := r.db.QueryRow(
		"SELECT COUNT(*) FROM login_audit WHERE ip = ? AND success = 0 AND created_at > ?",
		ip, since.Format(time.RFC3339),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计登录失败次数失败: %w", err)
	}
	return count, nil
}

func (r *LoginAuditRepo) List(page, pageSize int) ([]*LoginAudit, int, error) {
	var total int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM login_audit").Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("查询登录审计总数失败: %w", err)
	}

	offset := (page - 1) * pageSize
	rows, err := r.db.Query(
		"SELECT id, username, ip, user_agent, success, failure_reason, captcha_verified, totp_used, created_at FROM login_audit ORDER BY created_at DESC LIMIT ? OFFSET ?",
		pageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("查询登录审计列表失败: %w", err)
	}
	defer rows.Close()

	var items []*LoginAudit
	for rows.Next() {
		a := &LoginAudit{}
		var success, captchaVerified, totpUsed int
		if err := rows.Scan(&a.ID, &a.Username, &a.IP, &a.UserAgent, &success, &a.FailureReason, &captchaVerified, &totpUsed, &a.CreatedAt); err != nil {
			return nil, 0, err
		}
		a.Success = success == 1
		a.CaptchaVerified = captchaVerified == 1
		a.TOTPUsed = totpUsed == 1
		items = append(items, a)
	}
	return items, total, rows.Err()
}

func (r *LoginAuditRepo) DeleteAll() error {
	_, err := r.db.Exec("DELETE FROM login_audit")
	if err != nil {
		return fmt.Errorf("清空登录审计失败: %w", err)
	}
	return nil
}
