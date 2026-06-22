// repo 包 — site_proxy 表的读写
//
// 支持每站点多个反向代理配置。
package repo

import (
	"database/sql"
	"fmt"
	"time"
)

// ProxyRepo 提供 site_proxy 表的数据访问
type ProxyRepo struct {
	db *sql.DB
}

// NewProxyRepo 创建 proxy repository
func NewProxyRepo(db *sql.DB) *ProxyRepo {
	return &ProxyRepo{db: db}
}

// Create 创建反向代理配置
func (r *ProxyRepo) Create(p *SiteProxy) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if p.CreatedAt == "" {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	enabledInt := boolToInt(p.Enabled)
	wsInt := boolToInt(p.WebSocketEnabled)
	cacheEnabledInt := boolToInt(p.CacheEnabled)

	_, err := r.db.Exec(
		`INSERT INTO site_proxy (
			id, site_id, name, enabled, location_path, upstream_url, host_header,
			websocket_enabled, connect_timeout, send_timeout, read_timeout,
			cache_enabled, cache_type, cache_time,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.SiteID, p.Name, enabledInt, p.LocationPath, p.UpstreamURL, p.HostHeader,
		wsInt, p.ConnectTimeout, p.SendTimeout, p.ReadTimeout,
		cacheEnabledInt, p.CacheType, p.CacheTime,
		p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("创建反代配置失败: %w", err)
	}
	return nil
}

// GetByID 根据 id 获取反向代理配置
func (r *ProxyRepo) GetByID(id string) (*SiteProxy, error) {
	p := &SiteProxy{}
	var enabledInt, wsInt, cacheEnabledInt int

	err := r.db.QueryRow(
		`SELECT id, site_id, name, enabled, location_path, upstream_url, host_header,
			websocket_enabled, connect_timeout, send_timeout, read_timeout,
			cache_enabled, cache_type, cache_time,
			created_at, updated_at
		FROM site_proxy WHERE id = ?`,
		id,
	).Scan(
		&p.ID, &p.SiteID, &p.Name, &enabledInt, &p.LocationPath, &p.UpstreamURL, &p.HostHeader,
		&wsInt, &p.ConnectTimeout, &p.SendTimeout, &p.ReadTimeout,
		&cacheEnabledInt, &p.CacheType, &p.CacheTime,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询反代配置失败 id=%s: %w", id, err)
	}

	p.Enabled = enabledInt == 1
	p.WebSocketEnabled = wsInt == 1
	p.CacheEnabled = cacheEnabledInt == 1
	return p, nil
}

// ListBySiteID 列出站点的所有反向代理配置
func (r *ProxyRepo) ListBySiteID(siteID string) ([]*SiteProxy, error) {
	rows, err := r.db.Query(
		`SELECT id, site_id, name, enabled, location_path, upstream_url, host_header,
			websocket_enabled, connect_timeout, send_timeout, read_timeout,
			cache_enabled, cache_type, cache_time,
			created_at, updated_at
		FROM site_proxy WHERE site_id = ? ORDER BY location_path`,
		siteID,
	)
	if err != nil {
		return nil, fmt.Errorf("查询反代配置列表失败 site_id=%s: %w", siteID, err)
	}
	defer rows.Close()

	var proxies []*SiteProxy
	for rows.Next() {
		p := &SiteProxy{}
		var enabledInt, wsInt, cacheEnabledInt int
		if err := rows.Scan(
			&p.ID, &p.SiteID, &p.Name, &enabledInt, &p.LocationPath, &p.UpstreamURL, &p.HostHeader,
			&wsInt, &p.ConnectTimeout, &p.SendTimeout, &p.ReadTimeout,
			&cacheEnabledInt, &p.CacheType, &p.CacheTime,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("扫描反代配置行失败: %w", err)
		}
		p.Enabled = enabledInt == 1
		p.WebSocketEnabled = wsInt == 1
		p.CacheEnabled = cacheEnabledInt == 1
		proxies = append(proxies, p)
	}
	return proxies, nil
}

// Update 更新反向代理配置
func (r *ProxyRepo) Update(p *SiteProxy) error {
	now := time.Now().UTC().Format(time.RFC3339)
	p.UpdatedAt = now

	enabledInt := boolToInt(p.Enabled)
	wsInt := boolToInt(p.WebSocketEnabled)
	cacheEnabledInt := boolToInt(p.CacheEnabled)

	_, err := r.db.Exec(
		`UPDATE site_proxy SET
			name = ?, enabled = ?, location_path = ?, upstream_url = ?, host_header = ?,
			websocket_enabled = ?, connect_timeout = ?, send_timeout = ?, read_timeout = ?,
			cache_enabled = ?, cache_type = ?, cache_time = ?,
			updated_at = ?
		WHERE id = ?`,
		p.Name, enabledInt, p.LocationPath, p.UpstreamURL, p.HostHeader,
		wsInt, p.ConnectTimeout, p.SendTimeout, p.ReadTimeout,
		cacheEnabledInt, p.CacheType, p.CacheTime,
		p.UpdatedAt,
		p.ID,
	)
	if err != nil {
		return fmt.Errorf("更新反代配置失败 id=%s: %w", p.ID, err)
	}
	return nil
}

// Delete 删除反向代理配置
func (r *ProxyRepo) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM site_proxy WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除反代配置失败 id=%s: %w", id, err)
	}
	return nil
}

// DeleteBySiteID 删除站点的所有反向代理配置
func (r *ProxyRepo) DeleteBySiteID(siteID string) error {
	_, err := r.db.Exec(`DELETE FROM site_proxy WHERE site_id = ?`, siteID)
	if err != nil {
		return fmt.Errorf("删除站点反代配置失败 site_id=%s: %w", siteID, err)
	}
	return nil
}

// CheckPathConflict 检查路径是否与其他代理冲突
// excludeID 排除的代理 ID（编辑时排除自己）
func (r *ProxyRepo) CheckPathConflict(siteID, locationPath, excludeID string) (bool, error) {
	var count int
	query := `SELECT COUNT(*) FROM site_proxy WHERE site_id = ? AND location_path = ?`
	args := []interface{}{siteID, locationPath}

	if excludeID != "" {
		query += ` AND id != ?`
		args = append(args, excludeID)
	}

	err := r.db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("检测路径冲突失败: %w", err)
	}
	return count > 0, nil
}

// GetBySiteID 根据 site_id 获取反向代理配置（兼容旧代码，返回第一个）
func (r *ProxyRepo) EnabledBySiteIDs(siteIDs []string) (map[string]bool, error) {
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
	query := "SELECT DISTINCT site_id FROM site_proxy WHERE enabled = 1 AND site_id IN (" + placeholders + ")"
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("批量查询反代启用状态失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var siteID string
		if err := rows.Scan(&siteID); err != nil {
			return nil, fmt.Errorf("扫描反代启用状态失败: %w", err)
		}
		result[siteID] = true
	}
	return result, nil
}

func (r *ProxyRepo) GetBySiteID(siteID string) (*SiteProxy, error) {
	proxies, err := r.ListBySiteID(siteID)
	if err != nil {
		return nil, err
	}
	if len(proxies) == 0 {
		return nil, nil
	}
	return proxies[0], nil
}
