package repo

import (
	"database/sql"
	"fmt"
	"time"
)

type ACMEAccount struct {
	ID, Email, PrivateKeyPEM, DirectoryURL string
	CreatedAt, UpdatedAt                   string
}

type ACMEOrder struct {
	ID, SiteID, DomainsJSON, ChallengeType, Email string
	Status, CertificateID                         string
	ErrorType, ErrorDetail                        string
	VerificationURL, VerificationContent          string
	LogText                                       string
	AutoRenew                                     bool
	LastRenewedAt                                 *string
	CreatedAt, UpdatedAt                          string
	ExpiresAt                                     *string
}

type ACMERepo struct {
	db *sql.DB
}

func NewACMERepo(db *sql.DB) *ACMERepo {
	return &ACMERepo{db: db}
}

func (r *ACMERepo) GetOrderByID(id string) (*ACMEOrder, error) {
	o := &ACMEOrder{}
	err := r.db.QueryRow(
		`SELECT id, site_id, domains_json, challenge_type, email,
			status, certificate_id,
			error_type, error_detail,
			verification_url, verification_content,
			log_text, auto_renew, last_renewed_at,
			created_at, updated_at, expires_at
		FROM acme_orders WHERE id = ?`, id,
	).Scan(
		&o.ID, &o.SiteID, &o.DomainsJSON, &o.ChallengeType, &o.Email,
		&o.Status, &o.CertificateID,
		&o.ErrorType, &o.ErrorDetail,
		&o.VerificationURL, &o.VerificationContent,
		&o.LogText, &o.AutoRenew, &o.LastRenewedAt,
		&o.CreatedAt, &o.UpdatedAt, &o.ExpiresAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询 ACME 订单失败 id=%s: %w", id, err)
	}
	return o, nil
}

func (r *ACMERepo) ListOrdersBySiteID(siteID string) ([]*ACMEOrder, error) {
	rows, err := r.db.Query(
		`SELECT id, site_id, domains_json, challenge_type, email,
			status, certificate_id,
			error_type, error_detail,
			verification_url, verification_content,
			log_text, auto_renew, last_renewed_at,
			created_at, updated_at, expires_at
		FROM acme_orders WHERE site_id = ? ORDER BY created_at DESC`, siteID,
	)
	if err != nil {
		return nil, fmt.Errorf("查询站点 ACME 订单失败: %w", err)
	}
	defer rows.Close()

	var orders []*ACMEOrder
	for rows.Next() {
		o := &ACMEOrder{}
		if err := rows.Scan(
			&o.ID, &o.SiteID, &o.DomainsJSON, &o.ChallengeType, &o.Email,
			&o.Status, &o.CertificateID,
			&o.ErrorType, &o.ErrorDetail,
			&o.VerificationURL, &o.VerificationContent,
			&o.LogText, &o.AutoRenew, &o.LastRenewedAt,
			&o.CreatedAt, &o.UpdatedAt, &o.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("扫描 ACME 订单行失败: %w", err)
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

func (r *ACMERepo) ListExpiringOrders(beforeDate string) ([]*ACMEOrder, error) {
	rows, err := r.db.Query(
		`SELECT id, site_id, domains_json, challenge_type, email,
			status, certificate_id,
			error_type, error_detail,
			verification_url, verification_content,
			log_text, auto_renew, last_renewed_at,
			created_at, updated_at, expires_at
		FROM acme_orders
		WHERE status = 'success' AND auto_renew = 1 AND expires_at IS NOT NULL AND expires_at < ?`,
		beforeDate,
	)
	if err != nil {
		return nil, fmt.Errorf("查询即将过期 ACME 订单失败: %w", err)
	}
	defer rows.Close()

	var orders []*ACMEOrder
	for rows.Next() {
		o := &ACMEOrder{}
		if err := rows.Scan(
			&o.ID, &o.SiteID, &o.DomainsJSON, &o.ChallengeType, &o.Email,
			&o.Status, &o.CertificateID,
			&o.ErrorType, &o.ErrorDetail,
			&o.VerificationURL, &o.VerificationContent,
			&o.LogText, &o.AutoRenew, &o.LastRenewedAt,
			&o.CreatedAt, &o.UpdatedAt, &o.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("扫描 ACME 订单行失败: %w", err)
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

func (r *ACMERepo) CreateOrder(o *ACMEOrder) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`INSERT INTO acme_orders (
			id, site_id, domains_json, challenge_type, email,
			status, certificate_id,
			error_type, error_detail,
			verification_url, verification_content,
			log_text, auto_renew, last_renewed_at,
			created_at, updated_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.ID, o.SiteID, o.DomainsJSON, o.ChallengeType, o.Email,
		o.Status, o.CertificateID,
		o.ErrorType, o.ErrorDetail,
		o.VerificationURL, o.VerificationContent,
		o.LogText, o.AutoRenew, o.LastRenewedAt,
		now, now, o.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("创建 ACME 订单失败: %w", err)
	}
	return nil
}

func (r *ACMERepo) UpdateOrderStatus(id, status, errorType, errorDetail string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`UPDATE acme_orders SET status = ?, error_type = ?, error_detail = ?, updated_at = ? WHERE id = ?`,
		status, errorType, errorDetail, now, id,
	)
	if err != nil {
		return fmt.Errorf("更新 ACME 订单状态失败: %w", err)
	}
	return nil
}

func (r *ACMERepo) UpdateOrderVerification(id, verificationURL, verificationContent string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`UPDATE acme_orders SET verification_url = ?, verification_content = ?, updated_at = ? WHERE id = ?`,
		verificationURL, verificationContent, now, id,
	)
	if err != nil {
		return fmt.Errorf("更新 ACME 验证信息失败: %w", err)
	}
	return nil
}

func (r *ACMERepo) UpdateOrderLog(id, logText string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`UPDATE acme_orders SET log_text = ?, updated_at = ? WHERE id = ?`,
		logText, now, id,
	)
	if err != nil {
		return fmt.Errorf("更新 ACME 订单日志失败: %w", err)
	}
	return nil
}

func (r *ACMERepo) UpdateOrderSuccess(id, certID, expiresAt string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`UPDATE acme_orders SET status = 'success', certificate_id = ?, expires_at = ?, updated_at = ? WHERE id = ?`,
		certID, expiresAt, now, id,
	)
	if err != nil {
		return fmt.Errorf("更新 ACME 订单成功状态失败: %w", err)
	}
	return nil
}

func (r *ACMERepo) UpdateOrderAutoRenew(id string, autoRenew bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`UPDATE acme_orders SET auto_renew = ?, updated_at = ? WHERE id = ?`,
		autoRenew, now, id,
	)
	if err != nil {
		return fmt.Errorf("更新 ACME 自动续签失败: %w", err)
	}
	return nil
}

func (r *ACMERepo) UpdateOrderRenewed(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`UPDATE acme_orders SET last_renewed_at = ?, updated_at = ? WHERE id = ?`,
		now, now, id,
	)
	if err != nil {
		return fmt.Errorf("更新 ACME 续签时间失败: %w", err)
	}
	return nil
}

func (r *ACMERepo) DeleteOrder(id string) error {
	_, err := r.db.Exec("DELETE FROM acme_orders WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("删除 ACME 订单失败 id=%s: %w", id, err)
	}
	return nil
}

func (r *ACMERepo) GetAccountByEmail(email string) (*ACMEAccount, error) {
	a := &ACMEAccount{}
	err := r.db.QueryRow(
		`SELECT id, email, private_key_pem, directory_url, created_at, updated_at
		FROM acme_accounts WHERE email = ?`, email,
	).Scan(&a.ID, &a.Email, &a.PrivateKeyPEM, &a.DirectoryURL, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询 ACME 账号失败: %w", err)
	}
	return a, nil
}

func (r *ACMERepo) CreateAccount(a *ACMEAccount) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`INSERT INTO acme_accounts (id, email, private_key_pem, directory_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		a.ID, a.Email, a.PrivateKeyPEM, a.DirectoryURL, now, now,
	)
	if err != nil {
		return fmt.Errorf("创建 ACME 账号失败: %w", err)
	}
	return nil
}

func (r *ACMERepo) ListEmails() ([]string, error) {
	rows, err := r.db.Query("SELECT email FROM acme_emails ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("查询 ACME 邮箱列表失败: %w", err)
	}
	defer rows.Close()

	emails := make([]string, 0)
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, fmt.Errorf("扫描邮箱行失败: %w", err)
		}
		emails = append(emails, email)
	}
	return emails, rows.Err()
}

func (r *ACMERepo) SaveEmail(email string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`INSERT OR IGNORE INTO acme_emails (email, created_at) VALUES (?, ?)`,
		email, now,
	)
	if err != nil {
		return fmt.Errorf("保存 ACME 邮箱失败: %w", err)
	}
	return nil
}

func (r *ACMERepo) DeleteEmail(email string) error {
	_, err := r.db.Exec("DELETE FROM acme_emails WHERE email = ?", email)
	if err != nil {
		return fmt.Errorf("删除 ACME 邮箱失败: %w", err)
	}
	return nil
}
