package repo

import (
	"database/sql"
	"fmt"
	"time"
)

type SiteRepo struct {
	db *sql.DB
}

func NewSiteRepo(db *sql.DB) *SiteRepo {
	return &SiteRepo{db: db}
}

func (r *SiteRepo) Create(s *Site) error {
	now := time.Now().UTC().Format(time.RFC3339)
	accessLogInt := boolToInt(s.AccessLogEnabled)
	autoindexInt := boolToInt(s.AutoindexEnabled)
	autoindexExactSizeInt := boolToInt(s.AutoindexExactSize)
	autoindexLocaltimeInt := boolToInt(s.AutoindexLocaltime)
	autoindexFormat := defaultAutoindexFormat(s.AutoindexFormat)
	_, err := r.db.Exec(
		`INSERT INTO sites (
			id, primary_domain, domains_json, bindings_json, status,
			http_port, https_port, root_path, index_files,
			access_log_enabled, access_log_path, error_log_path,
			config_path, enabled_path, rewrite_path, access_limit_path,
			hotlink_path, autoindex_enabled, autoindex_exact_size, autoindex_localtime, autoindex_format, error_page_404, error_page_403,
			marker_version, last_sync_warning,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.PrimaryDomain, s.DomainsJSON, nilIfEmpty(s.BindingsJSON),
		s.Status,
		s.HTTPPort, s.HTTPSPort, s.RootPath, s.IndexFiles,
		accessLogInt, s.AccessLogPath, s.ErrorLogPath,
		s.ConfigPath, s.EnabledPath, s.RewritePath, nilIfEmpty(s.AccessLimitPath),
		s.HotlinkPath, autoindexInt, autoindexExactSizeInt, autoindexLocaltimeInt, autoindexFormat, s.ErrorPage404, s.ErrorPage403,
		s.MarkerVersion, s.LastSyncWarning,
		now, now,
	)
	if err != nil {
		return fmt.Errorf("创建站点失败 primary_domain=%s: %w", s.PrimaryDomain, err)
	}
	return nil
}

func (r *SiteRepo) GetByID(id string) (*Site, error) {
	s := &Site{}
	var accessLogInt, autoindexInt, autoindexExactSizeInt, autoindexLocaltimeInt int
	var bindingsJSON, accessLimitPath, hotlinkPath sql.NullString

	err := r.db.QueryRow(
		`SELECT id, primary_domain, domains_json, bindings_json, status,
			http_port, https_port, root_path, index_files,
			access_log_enabled, access_log_path, error_log_path,
			config_path, enabled_path, rewrite_path, access_limit_path,
			hotlink_path, autoindex_enabled, autoindex_exact_size, autoindex_localtime, autoindex_format, error_page_404, error_page_403,
			marker_version, last_sync_warning,
			created_at, updated_at
		FROM sites WHERE id = ?`,
		id,
	).Scan(
		&s.ID, &s.PrimaryDomain, &s.DomainsJSON, &bindingsJSON, &s.Status,
		&s.HTTPPort, &s.HTTPSPort, &s.RootPath, &s.IndexFiles,
		&accessLogInt, &s.AccessLogPath, &s.ErrorLogPath,
		&s.ConfigPath, &s.EnabledPath, &s.RewritePath, &accessLimitPath,
		&hotlinkPath, &autoindexInt, &autoindexExactSizeInt, &autoindexLocaltimeInt, &s.AutoindexFormat, &s.ErrorPage404, &s.ErrorPage403,
		&s.MarkerVersion, &s.LastSyncWarning,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询站点失败 id=%s: %w", id, err)
	}

	s.AccessLogEnabled = accessLogInt == 1
	s.AutoindexEnabled = autoindexInt == 1
	s.AutoindexExactSize = autoindexExactSizeInt == 1
	s.AutoindexLocaltime = autoindexLocaltimeInt == 1
	s.AutoindexFormat = defaultAutoindexFormat(s.AutoindexFormat)
	s.BindingsJSON = bindingsJSON.String
	s.AccessLimitPath = accessLimitPath.String
	s.HotlinkPath = hotlinkPath.String
	return s, nil
}

func (r *SiteRepo) GetByPrimaryDomain(domain string) (*Site, error) {
	s := &Site{}
	var accessLogInt, autoindexInt, autoindexExactSizeInt, autoindexLocaltimeInt int
	var bindingsJSON, accessLimitPath, hotlinkPath sql.NullString

	err := r.db.QueryRow(
		`SELECT id, primary_domain, domains_json, bindings_json, status,
			http_port, https_port, root_path, index_files,
			access_log_enabled, access_log_path, error_log_path,
			config_path, enabled_path, rewrite_path, access_limit_path,
			hotlink_path, autoindex_enabled, autoindex_exact_size, autoindex_localtime, autoindex_format, error_page_404, error_page_403,
			marker_version, last_sync_warning,
			created_at, updated_at
		FROM sites WHERE primary_domain = ?`,
		domain,
	).Scan(
		&s.ID, &s.PrimaryDomain, &s.DomainsJSON, &bindingsJSON, &s.Status,
		&s.HTTPPort, &s.HTTPSPort, &s.RootPath, &s.IndexFiles,
		&accessLogInt, &s.AccessLogPath, &s.ErrorLogPath,
		&s.ConfigPath, &s.EnabledPath, &s.RewritePath, &accessLimitPath,
		&hotlinkPath, &autoindexInt, &autoindexExactSizeInt, &autoindexLocaltimeInt, &s.AutoindexFormat, &s.ErrorPage404, &s.ErrorPage403,
		&s.MarkerVersion, &s.LastSyncWarning,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询站点失败 domain=%s: %w", domain, err)
	}

	s.AccessLogEnabled = accessLogInt == 1
	s.AutoindexEnabled = autoindexInt == 1
	s.AutoindexExactSize = autoindexExactSizeInt == 1
	s.AutoindexLocaltime = autoindexLocaltimeInt == 1
	s.AutoindexFormat = defaultAutoindexFormat(s.AutoindexFormat)
	s.BindingsJSON = bindingsJSON.String
	s.AccessLimitPath = accessLimitPath.String
	s.HotlinkPath = hotlinkPath.String
	return s, nil
}

func (r *SiteRepo) List(page, pageSize int, keyword, status string) ([]*Site, int, error) {
	where := "WHERE 1=1"
	args := []any{}

	if keyword != "" {
		where += " AND primary_domain LIKE ?"
		args = append(args, "%"+keyword+"%")
	}
	if status != "" {
		where += " AND status = ?"
		args = append(args, status)
	}

	var total int
	countSQL := "SELECT COUNT(*) FROM sites " + where
	if err := r.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("查询站点总数失败: %w", err)
	}

	offset := (page - 1) * pageSize
	querySQL := `SELECT id, primary_domain, domains_json, bindings_json, status,
		http_port, https_port, root_path, index_files,
		access_log_enabled, access_log_path, error_log_path,
		config_path, enabled_path, rewrite_path, access_limit_path,
		hotlink_path, autoindex_enabled, autoindex_exact_size, autoindex_localtime, autoindex_format, error_page_404, error_page_403,
		marker_version, last_sync_warning,
		created_at, updated_at
	FROM sites ` + where + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, pageSize, offset)

	rows, err := r.db.Query(querySQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询站点列表失败: %w", err)
	}
	defer rows.Close()

	var sites []*Site
	for rows.Next() {
		s := &Site{}
		var accessLogInt, autoindexInt, autoindexExactSizeInt, autoindexLocaltimeInt int
		var bindingsJSON, accessLimitPath, hotlinkPath sql.NullString

		if err := rows.Scan(
			&s.ID, &s.PrimaryDomain, &s.DomainsJSON, &bindingsJSON, &s.Status,
			&s.HTTPPort, &s.HTTPSPort, &s.RootPath, &s.IndexFiles,
			&accessLogInt, &s.AccessLogPath, &s.ErrorLogPath,
			&s.ConfigPath, &s.EnabledPath, &s.RewritePath, &accessLimitPath,
			&hotlinkPath, &autoindexInt, &autoindexExactSizeInt, &autoindexLocaltimeInt, &s.AutoindexFormat, &s.ErrorPage404, &s.ErrorPage403,
			&s.MarkerVersion, &s.LastSyncWarning,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}

		s.AccessLogEnabled = accessLogInt == 1
		s.AutoindexEnabled = autoindexInt == 1
		s.AutoindexExactSize = autoindexExactSizeInt == 1
		s.AutoindexLocaltime = autoindexLocaltimeInt == 1
		s.AutoindexFormat = defaultAutoindexFormat(s.AutoindexFormat)
		s.BindingsJSON = bindingsJSON.String
		s.AccessLimitPath = accessLimitPath.String
		s.HotlinkPath = hotlinkPath.String
		sites = append(sites, s)
	}

	return sites, total, rows.Err()
}

func (r *SiteRepo) Update(s *Site) error {
	now := time.Now().UTC().Format(time.RFC3339)
	accessLogInt := boolToInt(s.AccessLogEnabled)
	autoindexInt := boolToInt(s.AutoindexEnabled)
	autoindexExactSizeInt := boolToInt(s.AutoindexExactSize)
	autoindexLocaltimeInt := boolToInt(s.AutoindexLocaltime)
	autoindexFormat := defaultAutoindexFormat(s.AutoindexFormat)
	_, err := r.db.Exec(
		`UPDATE sites SET
			primary_domain = ?, domains_json = ?, bindings_json = ?,
			status = ?,
			http_port = ?, https_port = ?, root_path = ?, index_files = ?,
			access_log_enabled = ?, access_log_path = ?, error_log_path = ?,
			access_limit_path = ?, hotlink_path = ?, autoindex_enabled = ?, autoindex_exact_size = ?, autoindex_localtime = ?, autoindex_format = ?,
			error_page_404 = ?, error_page_403 = ?,
			marker_version = ?, last_sync_warning = ?,
			updated_at = ?
		WHERE id = ?`,
		s.PrimaryDomain, s.DomainsJSON, nilIfEmpty(s.BindingsJSON),
		s.Status,
		s.HTTPPort, s.HTTPSPort, s.RootPath, s.IndexFiles,
		accessLogInt, s.AccessLogPath, s.ErrorLogPath,
		nilIfEmpty(s.AccessLimitPath), s.HotlinkPath, autoindexInt, autoindexExactSizeInt, autoindexLocaltimeInt, autoindexFormat,
		s.ErrorPage404, s.ErrorPage403,
		s.MarkerVersion, s.LastSyncWarning,
		now, s.ID,
	)
	if err != nil {
		return fmt.Errorf("更新站点失败 id=%s: %w", s.ID, err)
	}
	return nil
}

func defaultAutoindexFormat(value string) string {
	if value == "" {
		return "html"
	}
	return value
}

func (r *SiteRepo) UpdateStatus(id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		"UPDATE sites SET status = ?, updated_at = ? WHERE id = ?",
		status, now, id,
	)
	if err != nil {
		return fmt.Errorf("更新站点状态失败 id=%s: %w", id, err)
	}
	return nil
}

func (r *SiteRepo) Delete(id string) error {
	_, err := r.db.Exec("DELETE FROM sites WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("删除站点失败 id=%s: %w", id, err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
