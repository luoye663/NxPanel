package repo

import (
	"database/sql"
	"fmt"
	"time"
)

type SSLRepo struct {
	db *sql.DB
}

func NewSSLRepo(db *sql.DB) *SSLRepo {
	return &SSLRepo{db: db}
}

func (r *SSLRepo) GetBySiteID(siteID string) (*SiteSSL, error) {
	s := &SiteSSL{}
	var enabledInt, forceHTTPSInt, hstsInt int

	err := r.db.QueryRow(
		`SELECT site_id, enabled, mode, cert_path, key_path,
			cert_sha256, key_sha256, issuer, subject,
			not_before, not_after, dns_names_json,
			force_https, hsts_enabled, cert_store_id,
			created_at, updated_at
		FROM site_ssl WHERE site_id = ?`,
		siteID,
	).Scan(
		&s.SiteID, &enabledInt, &s.Mode, &s.CertPath, &s.KeyPath,
		&s.CertSHA256, &s.KeySHA256, &s.Issuer, &s.Subject,
		&s.NotBefore, &s.NotAfter, &s.DNSNamesJSON,
		&forceHTTPSInt, &hstsInt, &s.CertStoreID,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询 SSL 配置失败 site_id=%s: %w", siteID, err)
	}

	s.Enabled = enabledInt == 1
	s.ForceHTTPS = forceHTTPSInt == 1
	s.HSTSEnabled = hstsInt == 1
	return s, nil
}

func (r *SSLRepo) Upsert(s *SiteSSL) error {
	now := time.Now().UTC().Format(time.RFC3339)
	enabledInt := boolToInt(s.Enabled)
	forceHTTPSInt := boolToInt(s.ForceHTTPS)
	hstsInt := boolToInt(s.HSTSEnabled)
	_, err := r.db.Exec(
		`INSERT INTO site_ssl (
			site_id, enabled, mode, cert_path, key_path,
			cert_sha256, key_sha256, issuer, subject,
			not_before, not_after, dns_names_json,
			force_https, hsts_enabled, cert_store_id,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(site_id) DO UPDATE SET
			enabled = excluded.enabled,
			mode = excluded.mode,
			cert_path = excluded.cert_path,
			key_path = excluded.key_path,
			cert_sha256 = excluded.cert_sha256,
			key_sha256 = excluded.key_sha256,
			issuer = excluded.issuer,
			subject = excluded.subject,
			not_before = excluded.not_before,
			not_after = excluded.not_after,
			dns_names_json = excluded.dns_names_json,
			force_https = excluded.force_https,
			hsts_enabled = excluded.hsts_enabled,
			cert_store_id = excluded.cert_store_id,
			updated_at = excluded.updated_at`,
		s.SiteID, enabledInt, s.Mode, s.CertPath, s.KeyPath,
		s.CertSHA256, s.KeySHA256, s.Issuer, s.Subject,
		s.NotBefore, s.NotAfter, s.DNSNamesJSON,
		forceHTTPSInt, hstsInt, s.CertStoreID,
		now, now,
	)
	if err != nil {
		return fmt.Errorf("保存 SSL 配置失败 site_id=%s: %w", s.SiteID, err)
	}
	return nil
}

func (r *SSLRepo) Delete(siteID string) error {
	_, err := r.db.Exec("DELETE FROM site_ssl WHERE site_id = ?", siteID)
	if err != nil {
		return fmt.Errorf("删除 SSL 配置失败 site_id=%s: %w", siteID, err)
	}
	return nil
}

func (r *SSLRepo) EnabledBySiteIDs(siteIDs []string) (map[string]bool, error) {
	result := make(map[string]bool, len(siteIDs))
	if len(siteIDs) == 0 {
		return result, nil
	}
	placeholders := ""
	args := make([]interface{}, len(siteIDs))
	for i, id := range siteIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}
	query := "SELECT site_id FROM site_ssl WHERE enabled = 1 AND site_id IN (" + placeholders + ")"
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("批量查询 SSL 启用状态失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var siteID string
		if err := rows.Scan(&siteID); err != nil {
			return nil, fmt.Errorf("扫描 SSL 启用状态失败: %w", err)
		}
		result[siteID] = true
	}
	return result, nil
}

func (r *SSLRepo) CountByCertStoreID(certStoreID string) (int, error) {
	var count int
	err := r.db.QueryRow(
		"SELECT COUNT(*) FROM site_ssl WHERE cert_store_id = ? AND enabled = 1",
		certStoreID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("查询证书部署数量失败 cert_store_id=%s: %w", certStoreID, err)
	}
	return count, nil
}

func (r *SSLRepo) CountOtherSitesByCertPath(excludeSiteID, certPath string) (int, error) {
	var count int
	err := r.db.QueryRow(
		"SELECT COUNT(*) FROM site_ssl WHERE site_id != ? AND enabled = 1 AND cert_path = ?",
		excludeSiteID, certPath,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("查询证书共享站点数量失败: %w", err)
	}
	return count, nil
}
