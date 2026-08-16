package repo

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	LoginAuditUsernameMaxBytes = 256
	LoginAuditIPMaxBytes       = 64
	LoginAuditUAMaxBytes       = 512
	LoginAuditReasonMaxBytes   = 256
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
		truncateUTF8Bytes(a.Username, LoginAuditUsernameMaxBytes),
		truncateUTF8Bytes(a.IP, LoginAuditIPMaxBytes),
		truncateUTF8Bytes(a.UserAgent, LoginAuditUAMaxBytes),
		success, truncateUTF8Bytes(a.FailureReason, LoginAuditReasonMaxBytes), captchaVerified, totpUsed,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("记录登录审计失败: %w", err)
	}
	return nil
}

func (r *LoginAuditRepo) Prune(ctx context.Context, cutoff time.Time, maxCount int) (int64, error) {
	if maxCount < 1 {
		return 0, fmt.Errorf("登录审计最大保留数必须大于 0")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开始登录审计清理事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `DELETE FROM login_audit WHERE created_at < ?`, cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("按时间清理登录审计失败: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	result, err = tx.ExecContext(ctx, `DELETE FROM login_audit WHERE id IN (
		SELECT id FROM login_audit ORDER BY created_at DESC, id DESC LIMIT -1 OFFSET ?
	)`, maxCount)
	if err != nil {
		return 0, fmt.Errorf("按数量清理登录审计失败: %w", err)
	}
	countDeleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交登录审计清理事务失败: %w", err)
	}
	return deleted + countDeleted, nil
}

func truncateUTF8Bytes(value string, maxBytes int) string {
	value = strings.ToValidUTF8(value, "")
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
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
		"SELECT id, username, ip, user_agent, success, failure_reason, captcha_verified, totp_used, created_at FROM login_audit ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?",
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
