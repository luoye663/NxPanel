package repo

import (
	"database/sql"
	"fmt"
	"time"
)

type GeoAccessRepo struct {
	db *sql.DB
}

func NewGeoAccessRepo(db *sql.DB) *GeoAccessRepo { return &GeoAccessRepo{db: db} }

func (r *GeoAccessRepo) GetSiteSettings(siteID string) (*SiteGeoSettings, error) {
	item := &SiteGeoSettings{SiteID: siteID, DefaultAction: "allow", ApplyStatus: "disabled"}
	var enabled int
	err := r.db.QueryRow(`SELECT site_id, enabled, default_action, desired_hash, applied_hash, apply_status,
		last_error, created_at, updated_at FROM site_geo_settings WHERE site_id = ?`, siteID).
		Scan(&item.SiteID, &enabled, &item.DefaultAction, &item.DesiredHash, &item.AppliedHash,
			&item.ApplyStatus, &item.LastError, &item.CreatedAt, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return item, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询站点地域设置失败: %w", err)
	}
	item.Enabled = enabled == 1
	return item, nil
}

func (r *GeoAccessRepo) SaveSiteSettings(item *SiteGeoSettings) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(`INSERT INTO site_geo_settings
		(site_id, enabled, default_action, desired_hash, applied_hash, apply_status, last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(site_id) DO UPDATE SET enabled=excluded.enabled, default_action=excluded.default_action,
		desired_hash=excluded.desired_hash, applied_hash=excluded.applied_hash, apply_status=excluded.apply_status,
		last_error=excluded.last_error, updated_at=excluded.updated_at`, item.SiteID, boolToInt(item.Enabled),
		item.DefaultAction, item.DesiredHash, item.AppliedHash, item.ApplyStatus, item.LastError, now, now)
	if err != nil {
		return fmt.Errorf("保存站点地域设置失败: %w", err)
	}
	return nil
}

func (r *GeoAccessRepo) ListEnabledSiteSettings() ([]*SiteGeoSettings, error) {
	rows, err := r.db.Query(`SELECT site_id, enabled, default_action, desired_hash, applied_hash, apply_status,
		last_error, created_at, updated_at FROM site_geo_settings WHERE enabled=1 ORDER BY site_id`)
	if err != nil {
		return nil, fmt.Errorf("查询启用地域设置失败: %w", err)
	}
	defer rows.Close()
	var result []*SiteGeoSettings
	for rows.Next() {
		item := &SiteGeoSettings{}
		var enabled int
		if err := rows.Scan(&item.SiteID, &enabled, &item.DefaultAction, &item.DesiredHash, &item.AppliedHash,
			&item.ApplyStatus, &item.LastError, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *GeoAccessRepo) ListPendingDisabledSiteIDs() ([]string, error) {
	rows, err := r.db.Query(`SELECT site_id FROM site_geo_settings
		WHERE enabled=0 AND apply_status='pending' ORDER BY site_id`)
	if err != nil {
		return nil, fmt.Errorf("查询待清理地域设置失败: %w", err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var siteID string
		if err := rows.Scan(&siteID); err != nil {
			return nil, err
		}
		result = append(result, siteID)
	}
	return result, rows.Err()
}

func (r *GeoAccessRepo) ListRules(siteID string) ([]*SiteGeoRule, error) {
	rows, err := r.db.Query(`SELECT id, site_id, name, countries_json, action, enabled, sort_order, created_at, updated_at
		FROM site_geo_rules WHERE site_id=? ORDER BY sort_order, created_at, id`, siteID)
	if err != nil {
		return nil, fmt.Errorf("查询地域规则失败: %w", err)
	}
	defer rows.Close()
	var result []*SiteGeoRule
	for rows.Next() {
		item := &SiteGeoRule{}
		var enabled int
		if err := rows.Scan(&item.ID, &item.SiteID, &item.Name, &item.CountriesJSON, &item.Action,
			&enabled, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *GeoAccessRepo) GetRule(id string) (*SiteGeoRule, error) {
	item := &SiteGeoRule{}
	var enabled int
	err := r.db.QueryRow(`SELECT id, site_id, name, countries_json, action, enabled, sort_order, created_at, updated_at
		FROM site_geo_rules WHERE id=?`, id).Scan(&item.ID, &item.SiteID, &item.Name, &item.CountriesJSON,
		&item.Action, &enabled, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询地域规则失败: %w", err)
	}
	item.Enabled = enabled == 1
	return item, nil
}

func (r *GeoAccessRepo) CreateRule(item *SiteGeoRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(`INSERT INTO site_geo_rules
		(id, site_id, name, countries_json, action, enabled, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, item.SiteID, item.Name, item.CountriesJSON,
		item.Action, boolToInt(item.Enabled), item.SortOrder, now, now)
	if err != nil {
		return fmt.Errorf("创建地域规则失败: %w", err)
	}
	return nil
}

func (r *GeoAccessRepo) UpdateRule(item *SiteGeoRule) error {
	result, err := r.db.Exec(`UPDATE site_geo_rules SET name=?, countries_json=?, action=?, enabled=?, sort_order=?, updated_at=? WHERE id=? AND site_id=?`,
		item.Name, item.CountriesJSON, item.Action, boolToInt(item.Enabled), item.SortOrder,
		time.Now().UTC().Format(time.RFC3339), item.ID, item.SiteID)
	if err != nil {
		return fmt.Errorf("更新地域规则失败: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *GeoAccessRepo) DeleteRule(siteID, id string) error {
	result, err := r.db.Exec(`DELETE FROM site_geo_rules WHERE id=? AND site_id=?`, id, siteID)
	if err != nil {
		return fmt.Errorf("删除地域规则失败: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *GeoAccessRepo) ReorderRules(siteID string, ids []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM site_geo_rules WHERE site_id=?`, siteID).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return fmt.Errorf("规则顺序必须包含站点的全部规则")
	}
	seen := make(map[string]struct{}, len(ids))
	for order, id := range ids {
		if _, exists := seen[id]; exists {
			return fmt.Errorf("规则顺序包含重复 ID")
		}
		seen[id] = struct{}{}
		result, err := tx.Exec(`UPDATE site_geo_rules SET sort_order=?, updated_at=? WHERE id=? AND site_id=?`, order,
			time.Now().UTC().Format(time.RFC3339), id, siteID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return fmt.Errorf("规则不属于当前站点: %s", id)
		}
	}
	return tx.Commit()
}

func (r *GeoAccessRepo) GetGeoIPSettings() (*GeoIPSettings, error) {
	item := &GeoIPSettings{AutoUpdate: true, TrustedProxiesJSON: "[]", CountriesJSON: "[]"}
	var autoUpdate int
	var attempt, success sql.NullString
	err := r.db.QueryRow(`SELECT account_id, license_key_encrypted, auto_update, trusted_proxies_json,
		active_db_path, active_cache_path, checksum, build_epoch, countries_json, last_attempt_at,
		last_success_at, last_error, updated_at FROM geoip_settings WHERE id=1`).Scan(&item.AccountID,
		&item.LicenseKeyEncrypted, &autoUpdate, &item.TrustedProxiesJSON, &item.ActiveDBPath,
		&item.ActiveCachePath, &item.Checksum, &item.BuildEpoch, &item.CountriesJSON, &attempt,
		&success, &item.LastError, &item.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("查询 GeoIP 设置失败: %w", err)
	}
	item.AutoUpdate = autoUpdate == 1
	item.LastAttemptAt, item.LastSuccessAt = attempt.String, success.String
	return item, nil
}

func (r *GeoAccessRepo) SaveGeoIPSettings(item *GeoIPSettings) error {
	_, err := r.db.Exec(`UPDATE geoip_settings SET account_id=?, license_key_encrypted=?, auto_update=?,
		trusted_proxies_json=?, active_db_path=?, active_cache_path=?, checksum=?, build_epoch=?, countries_json=?,
		last_attempt_at=?, last_success_at=?, last_error=?, updated_at=? WHERE id=1`, item.AccountID,
		item.LicenseKeyEncrypted, boolToInt(item.AutoUpdate), item.TrustedProxiesJSON, item.ActiveDBPath,
		item.ActiveCachePath, item.Checksum, item.BuildEpoch, item.CountriesJSON, nilIfEmpty(item.LastAttemptAt),
		nilIfEmpty(item.LastSuccessAt), item.LastError, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("保存 GeoIP 设置失败: %w", err)
	}
	return nil
}
