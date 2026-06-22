package repo

import (
	"database/sql"
	"fmt"
	"time"
)

type CertificateRepo struct {
	db *sql.DB
}

func NewCertificateRepo(db *sql.DB) *CertificateRepo {
	return &CertificateRepo{db: db}
}

func (r *CertificateRepo) List() ([]*Certificate, error) {
	rows, err := r.db.Query(
		`SELECT id, name, domains_json, issuer, subject,
			not_before, not_after, cert_sha256, key_sha256,
			cert_path, key_path,
			created_at, updated_at
		FROM certificates ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("查询证书列表失败: %w", err)
	}
	defer rows.Close()

	var certs []*Certificate
	for rows.Next() {
		c := &Certificate{}
		if err := rows.Scan(
			&c.ID, &c.Name, &c.DomainsJSON, &c.Issuer, &c.Subject,
			&c.NotBefore, &c.NotAfter, &c.CertSHA256, &c.KeySHA256,
			&c.CertPath, &c.KeyPath,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("扫描证书行失败: %w", err)
		}
		certs = append(certs, c)
	}
	return certs, rows.Err()
}

func (r *CertificateRepo) GetByID(id string) (*Certificate, error) {
	c := &Certificate{}
	err := r.db.QueryRow(
		`SELECT id, name, domains_json, issuer, subject,
			not_before, not_after, cert_sha256, key_sha256,
			cert_path, key_path,
			created_at, updated_at
		FROM certificates WHERE id = ?`,
		id,
	).Scan(
		&c.ID, &c.Name, &c.DomainsJSON, &c.Issuer, &c.Subject,
		&c.NotBefore, &c.NotAfter, &c.CertSHA256, &c.KeySHA256,
		&c.CertPath, &c.KeyPath,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询证书失败 id=%s: %w", id, err)
	}
	return c, nil
}

func (r *CertificateRepo) GetBySHA256(sha256 string) (*Certificate, error) {
	c := &Certificate{}
	err := r.db.QueryRow(
		`SELECT id, name, domains_json, issuer, subject,
			not_before, not_after, cert_sha256, key_sha256,
			cert_path, key_path,
			created_at, updated_at
		FROM certificates WHERE cert_sha256 = ?`,
		sha256,
	).Scan(
		&c.ID, &c.Name, &c.DomainsJSON, &c.Issuer, &c.Subject,
		&c.NotBefore, &c.NotAfter, &c.CertSHA256, &c.KeySHA256,
		&c.CertPath, &c.KeyPath,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("按 SHA256 查询证书失败: %w", err)
	}
	return c, nil
}

func (r *CertificateRepo) Create(c *Certificate) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`INSERT INTO certificates (
			id, name, domains_json, issuer, subject,
			not_before, not_after, cert_sha256, key_sha256,
			cert_path, key_path,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.DomainsJSON, c.Issuer, c.Subject,
		c.NotBefore, c.NotAfter, c.CertSHA256, c.KeySHA256,
		c.CertPath, c.KeyPath,
		now, now,
	)
	if err != nil {
		return fmt.Errorf("创建证书失败: %w", err)
	}
	return nil
}

func (r *CertificateRepo) Delete(id string) error {
	_, err := r.db.Exec("DELETE FROM certificates WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("删除证书失败 id=%s: %w", id, err)
	}
	return nil
}
