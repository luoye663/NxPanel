package repo

import (
	"context"
	"time"
)

type CertificateSite struct {
	SiteID        string `json:"site_id"`
	PrimaryDomain string `json:"primary_domain"`
}

func (r *CertificateRepo) AssociatedSites(id string) ([]CertificateSite, error) {
	rows, err := r.db.Query(`SELECT DISTINCT s.id, s.primary_domain FROM sites s
		LEFT JOIN certificate_sites cs ON cs.site_id=s.id
		LEFT JOIN site_ssl ssl ON ssl.site_id=s.id
		JOIN certificates c ON c.id=?
		WHERE cs.certificate_id=c.id OR ssl.cert_store_id=c.id
		OR ssl.cert_path=c.cert_path OR ssl.key_path=c.key_path
		OR (c.cert_sha256<>'' AND ssl.cert_sha256=c.cert_sha256)
		ORDER BY s.primary_domain`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]CertificateSite, 0)
	for rows.Next() {
		var site CertificateSite
		if err := rows.Scan(&site.SiteID, &site.PrimaryDomain); err != nil {
			return nil, err
		}
		result = append(result, site)
	}
	return result, rows.Err()
}

// AdoptExternal atomically reuses the existing store and records the exact
// external references. Neither this method nor its caller writes any files.
func (r *CertificateRepo) AdoptExternal(ctx context.Context, c *Certificate, siteID, configPath, fileHash, serverHash string, operationID ...string) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = tx.ExecContext(ctx, `INSERT INTO certificates
		(id,name,domains_json,issuer,subject,not_before,not_after,cert_sha256,key_sha256,cert_path,key_path,created_at,updated_at,source)
		SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,'external' WHERE NOT EXISTS (SELECT 1 FROM certificates WHERE cert_sha256=?)`,
		c.ID, c.Name, c.DomainsJSON, c.Issuer, c.Subject, c.NotBefore, c.NotAfter, c.CertSHA256, c.KeySHA256, c.CertPath, c.KeyPath, now, now, c.CertSHA256)
	if err != nil {
		return "", err
	}
	var id string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM certificates WHERE cert_sha256=? ORDER BY created_at,id LIMIT 1`, c.CertSHA256).Scan(&id); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO certificate_sites
		(site_id,certificate_id,config_path,file_hash,server_hash,cert_path,key_path) VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(site_id) DO UPDATE SET certificate_id=excluded.certificate_id,config_path=excluded.config_path,
		file_hash=excluded.file_hash,server_hash=excluded.server_hash,cert_path=excluded.cert_path,key_path=excluded.key_path`,
		siteID, id, configPath, fileHash, serverHash, c.CertPath, c.KeyPath)
	if err != nil {
		return "", err
	}
	// Refresh existing metadata without creating SSL desired state or changing
	// enabled/listen/HTTPS policy. This is also the post-apply recovery path.
	if _, err = tx.ExecContext(ctx, `UPDATE site_ssl SET cert_path=?,key_path=?,cert_sha256=?,key_sha256=?,issuer=?,subject=?,not_before=?,not_after=?,dns_names_json=?,cert_store_id=?,updated_at=? WHERE site_id=?`, c.CertPath, c.KeyPath, c.CertSHA256, c.KeySHA256, c.Issuer, c.Subject, c.NotBefore, c.NotAfter, c.DomainsJSON, id, now, siteID); err != nil {
		return "", err
	}
	current := ""
	if len(operationID) > 0 {
		current = operationID[0]
	}
	if _, err = tx.ExecContext(ctx, `UPDATE operations SET status='failed',error_message='实际证书引用已由后续扫描/局部替换对账取代',finished_at=? WHERE target_type='site' AND target_id=? AND action='site.ssl.replace' AND status='pending' AND id<>?`, now, siteID, current); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

// ForgetExternal never removes an external certificate or private key file.
func (r *CertificateRepo) ForgetExternal(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM certificate_sites WHERE certificate_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE site_ssl SET cert_store_id=NULL WHERE cert_store_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM certificates WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
