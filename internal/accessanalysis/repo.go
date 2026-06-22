package accessanalysis

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Repo struct{ db *sql.DB }

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

func defaultSettings(siteID string) *Settings {
	return &Settings{SiteID: siteID, ScanTime: "03:00", RetentionDays: 30, LogFormat: "combined", EntriesRetentionDays: 3, MaxEntries: 50000, PathTopN: 1000, IPTopN: 1000}
}

func (r *Repo) GetSettings(siteID string) (*Settings, error) {
	settings := defaultSettings(siteID)
	var enabled, includeRotated, normalizeQuery, saveEntries int
	err := r.db.QueryRow(`SELECT site_id, enabled, scan_time, retention_days, include_rotated, log_format, COALESCE(custom_pattern, ''), normalize_query, save_entries, entries_retention_days, max_entries, path_top_n, ip_top_n, created_at, updated_at FROM access_analysis_settings WHERE site_id = ?`, siteID).Scan(&settings.SiteID, &enabled, &settings.ScanTime, &settings.RetentionDays, &includeRotated, &settings.LogFormat, &settings.CustomPattern, &normalizeQuery, &saveEntries, &settings.EntriesRetentionDays, &settings.MaxEntries, &settings.PathTopN, &settings.IPTopN, &settings.CreatedAt, &settings.UpdatedAt)
	if err == sql.ErrNoRows {
		return settings, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询访问分析设置失败: %w", err)
	}
	settings.Enabled, settings.IncludeRotated, settings.NormalizeQuery, settings.SaveEntries = enabled == 1, includeRotated == 1, normalizeQuery == 1, saveEntries == 1
	return settings, nil
}

func (r *Repo) SaveSettings(settings *Settings) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(`INSERT INTO access_analysis_settings (site_id, enabled, scan_time, retention_days, include_rotated, log_format, custom_pattern, normalize_query, save_entries, entries_retention_days, max_entries, path_top_n, ip_top_n, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(site_id) DO UPDATE SET enabled = excluded.enabled, scan_time = excluded.scan_time, retention_days = excluded.retention_days, include_rotated = excluded.include_rotated, log_format = excluded.log_format, custom_pattern = excluded.custom_pattern, normalize_query = excluded.normalize_query, save_entries = excluded.save_entries, entries_retention_days = excluded.entries_retention_days, max_entries = excluded.max_entries, path_top_n = excluded.path_top_n, ip_top_n = excluded.ip_top_n, updated_at = excluded.updated_at`, settings.SiteID, boolToInt(settings.Enabled), settings.ScanTime, settings.RetentionDays, boolToInt(settings.IncludeRotated), settings.LogFormat, nilIfEmpty(settings.CustomPattern), boolToInt(settings.NormalizeQuery), boolToInt(settings.SaveEntries), settings.EntriesRetentionDays, settings.MaxEntries, settings.PathTopN, settings.IPTopN, now, now)
	return err
}

func (r *Repo) GetCursor(siteID, logPath string) (Cursor, error) {
	var cursor Cursor
	err := r.db.QueryRow(`SELECT inode, offset, file_size FROM access_analysis_cursors WHERE site_id = ? AND log_path = ?`, siteID, logPath).Scan(&cursor.Inode, &cursor.Offset, &cursor.FileSize)
	if err == sql.ErrNoRows {
		return cursor, nil
	}
	return cursor, err
}

func (r *Repo) CreateJob(job *Job) error {
	_, err := r.db.Exec(`INSERT INTO access_analysis_jobs (id, site_id, trigger, range_start, range_end, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, job.ID, job.SiteID, job.Trigger, job.RangeStart, job.RangeEnd, job.Status, job.CreatedAt)
	return err
}

func (r *Repo) GetRunningJob(siteID string) (*Job, error) {
	return scanJob(r.db.QueryRow(`SELECT id, site_id, trigger, range_start, range_end, status, scanned_lines, skipped_lines, duration_ms, COALESCE(error_message, ''), created_at, COALESCE(finished_at, '') FROM access_analysis_jobs WHERE site_id = ? AND status = 'running' ORDER BY created_at DESC LIMIT 1`, siteID))
}

func (r *Repo) FinishJobFailed(jobID, message string, durationMS int64) error {
	_, err := r.db.Exec(`UPDATE access_analysis_jobs SET status = 'failed', error_message = ?, duration_ms = ?, finished_at = ? WHERE id = ?`, message, durationMS, time.Now().UTC().Format(time.RFC3339), jobID)
	return err
}

func (r *Repo) SaveScanResult(siteID, logPath, jobID string, cursor Cursor, result *AgentScanResponse, settings *Settings, durationMS int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE access_analysis_jobs SET status = 'success', scanned_lines = ?, skipped_lines = ?, duration_ms = ?, finished_at = ? WHERE id = ?`, result.ScannedLines, result.SkippedLines, durationMS, now, jobID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO access_analysis_cursors (site_id, log_path, inode, offset, file_size, last_scan_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(site_id, log_path) DO UPDATE SET inode = excluded.inode, offset = excluded.offset, file_size = excluded.file_size, last_scan_at = excluded.last_scan_at, updated_at = excluded.updated_at`, siteID, logPath, cursor.Inode, cursor.Offset, cursor.FileSize, now, now); err != nil {
		return err
	}
	if err := saveHourly(tx, siteID, result.Hourly); err != nil {
		return err
	}
	if err := saveDaily(tx, siteID, result.Hourly); err != nil {
		return err
	}
	if err := savePaths(tx, siteID, limitPaths(result.Paths, settings.PathTopN)); err != nil {
		return err
	}
	if err := saveIPs(tx, siteID, limitIPs(result.IPs, settings.IPTopN)); err != nil {
		return err
	}
	if settings.SaveEntries {
		if err := saveEntries(tx, siteID, result.EntriesSample); err != nil {
			return err
		}
	}
	if err := cleanupEntriesLimit(tx, siteID, settings.MaxEntries); err != nil {
		return err
	}
	if err := saveAnomalies(tx, siteID, result.Anomalies); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repo) Summary(siteID, today string) (*Summary, error) {
	s := &Summary{}
	_ = r.db.QueryRow(`SELECT COALESCE(requests, 0), COALESCE(unique_ips, 0), COALESCE(status_4xx, 0), COALESCE(status_5xx, 0), COALESCE(bytes, 0) FROM access_analysis_daily WHERE site_id = ? AND date = ?`, siteID, today).Scan(&s.TodayRequests, &s.UniqueIPs, &s.Status4xx, &s.Status5xx, &s.Bytes)
	_ = r.db.QueryRow(`SELECT COALESCE(path, '') FROM access_analysis_paths WHERE site_id = ? AND date = ? ORDER BY requests DESC LIMIT 1`, siteID, today).Scan(&s.TopPath)
	_ = r.db.QueryRow(`SELECT COALESCE(finished_at, ''), status, COALESCE(error_message, '') FROM access_analysis_jobs WHERE site_id = ? ORDER BY created_at DESC LIMIT 1`, siteID).Scan(&s.LastScanAt, &s.LastJobStatus, &s.LastError)
	return s, nil
}

func (r *Repo) Hourly(siteID, from, to string) ([]HourlyPoint, error) {
	rows, err := r.db.Query(`SELECT hour, requests, unique_ips, status_4xx, status_5xx, bytes FROM access_analysis_hourly WHERE site_id = ? AND hour >= ? AND hour < ? ORDER BY hour ASC`, siteID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []HourlyPoint
	for rows.Next() {
		var item HourlyPoint
		if err := rows.Scan(&item.Hour, &item.Requests, &item.UniqueIPs, &item.Status4xx, &item.Status5xx, &item.Bytes); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repo) Paths(siteID string, q Query) (*Page[PathStat], error) {
	where, args := dateWhere(siteID, q)
	total, err := r.count("access_analysis_paths", where, args)
	if err != nil {
		return nil, err
	}
	args = append(args, q.PageSize, offset(q))
	rows, err := r.db.Query(`SELECT date, path, requests, unique_ips, status_2xx, status_3xx, status_4xx, status_5xx, bytes, last_seen_at FROM access_analysis_paths `+where+` ORDER BY `+sortSQL(q.Sort, map[string]string{"requests": "requests", "bytes": "bytes", "last_seen_at": "last_seen_at"}, "requests")+` DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PathStat{}
	for rows.Next() {
		var item PathStat
		if err := rows.Scan(&item.Date, &item.Path, &item.Requests, &item.UniqueIPs, &item.Status2xx, &item.Status3xx, &item.Status4xx, &item.Status5xx, &item.Bytes, &item.LastSeenAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return &Page[PathStat]{Items: items, Page: q.Page, PageSize: q.PageSize, Total: total}, rows.Err()
}

func (r *Repo) IPs(siteID string, q Query) (*Page[IPStat], error) {
	where, args := dateWhere(siteID, q)
	total, err := r.count("access_analysis_ips", where, args)
	if err != nil {
		return nil, err
	}
	args = append(args, q.PageSize, offset(q))
	rows, err := r.db.Query(`SELECT date, ip, requests, unique_paths, error_requests, bytes, first_seen_at, last_seen_at, COALESCE(sample_user_agent, '') FROM access_analysis_ips `+where+` ORDER BY `+sortSQL(q.Sort, map[string]string{"requests": "requests", "bytes": "bytes", "error_requests": "error_requests", "last_seen_at": "last_seen_at"}, "requests")+` DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []IPStat{}
	for rows.Next() {
		var item IPStat
		if err := rows.Scan(&item.Date, &item.IP, &item.Requests, &item.UniquePaths, &item.ErrorRequests, &item.Bytes, &item.FirstSeenAt, &item.LastSeenAt, &item.SampleUserAgent); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return &Page[IPStat]{Items: items, Page: q.Page, PageSize: q.PageSize, Total: total}, rows.Err()
}

func (r *Repo) Entries(siteID string, q Query) (*Page[Entry], error) {
	where, args := entryWhere(siteID, q)
	total, err := r.count("access_analysis_entries", where, args)
	if err != nil {
		return nil, err
	}
	args = append(args, q.PageSize, offset(q))
	rows, err := r.db.Query(`SELECT id, ts, ip, COALESCE(method, ''), path, COALESCE(raw_path, ''), status, bytes, COALESCE(referer, ''), COALESCE(user_agent, ''), is_anomaly, COALESCE(anomaly_reason, '') FROM access_analysis_entries `+where+` ORDER BY `+sortSQL(q.Sort, map[string]string{"ts": "ts", "status": "status", "bytes": "bytes"}, "ts")+` DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Entry{}
	for rows.Next() {
		var item Entry
		var isAnomaly int
		if err := rows.Scan(&item.ID, &item.TS, &item.IP, &item.Method, &item.Path, &item.RawPath, &item.Status, &item.Bytes, &item.Referer, &item.UserAgent, &isAnomaly, &item.AnomalyReason); err != nil {
			return nil, err
		}
		item.IsAnomaly = isAnomaly == 1
		items = append(items, item)
	}
	return &Page[Entry]{Items: items, Page: q.Page, PageSize: q.PageSize, Total: total}, rows.Err()
}

func (r *Repo) Anomalies(siteID string, q Query) ([]Anomaly, error) {
	where, args := dateWhere(siteID, q)
	rows, err := r.db.Query(`SELECT id, date, kind, target, requests, severity, reason, first_seen_at, last_seen_at FROM access_analysis_anomalies `+where+` ORDER BY requests DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Anomaly{}
	for rows.Next() {
		var item Anomaly
		if err := rows.Scan(&item.ID, &item.Date, &item.Kind, &item.Target, &item.Requests, &item.Severity, &item.Reason, &item.FirstSeenAt, &item.LastSeenAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repo) Jobs(siteID string, page, pageSize int) (*Page[Job], error) {
	q := Query{Page: page, PageSize: pageSize}
	total, err := r.count("access_analysis_jobs", "WHERE site_id = ?", []any{siteID})
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(`SELECT id, site_id, trigger, range_start, range_end, status, scanned_lines, skipped_lines, duration_ms, COALESCE(error_message, ''), created_at, COALESCE(finished_at, '') FROM access_analysis_jobs WHERE site_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`, siteID, q.PageSize, offset(q))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Job{}
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *job)
	}
	return &Page[Job]{Items: items, Page: page, PageSize: pageSize, Total: total}, rows.Err()
}

func (r *Repo) EnabledSettings() ([]Settings, error) {
	rows, err := r.db.Query(`SELECT site_id, enabled, scan_time, retention_days, include_rotated, log_format, COALESCE(custom_pattern, ''), normalize_query, save_entries, entries_retention_days, max_entries, path_top_n, ip_top_n, created_at, updated_at FROM access_analysis_settings WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Settings{}
	for rows.Next() {
		item := *defaultSettings("")
		var enabled, includeRotated, normalizeQuery, saveEntries int
		if err := rows.Scan(&item.SiteID, &enabled, &item.ScanTime, &item.RetentionDays, &includeRotated, &item.LogFormat, &item.CustomPattern, &normalizeQuery, &saveEntries, &item.EntriesRetentionDays, &item.MaxEntries, &item.PathTopN, &item.IPTopN, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled, item.IncludeRotated, item.NormalizeQuery, item.SaveEntries = enabled == 1, includeRotated == 1, normalizeQuery == 1, saveEntries == 1
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repo) CleanupSite(siteID string, settings *Settings) error {
	retentionDays := settings.RetentionDays
	cutoffDate := time.Now().UTC().AddDate(0, 0, -retentionDays).Format("2006-01-02")
	for _, stmt := range []string{`DELETE FROM access_analysis_daily WHERE site_id = ? AND date < ?`, `DELETE FROM access_analysis_hourly WHERE site_id = ? AND substr(hour, 1, 10) < ?`, `DELETE FROM access_analysis_paths WHERE site_id = ? AND date < ?`, `DELETE FROM access_analysis_ips WHERE site_id = ? AND date < ?`, `DELETE FROM access_analysis_anomalies WHERE site_id = ? AND date < ?`} {
		if _, err := r.db.Exec(stmt, siteID, cutoffDate); err != nil {
			return err
		}
	}
	entriesCutoffTS := time.Now().UTC().AddDate(0, 0, -settings.EntriesRetentionDays).Format(time.RFC3339)
	if _, err := r.db.Exec(`DELETE FROM access_analysis_entries WHERE site_id = ? AND ts < ?`, siteID, entriesCutoffTS); err != nil {
		return err
	}
	return r.cleanupEntriesLimit(siteID, settings.MaxEntries)
}

func (r *Repo) cleanupEntriesLimit(siteID string, maxEntries int) error {
	if maxEntries <= 0 {
		_, err := r.db.Exec(`DELETE FROM access_analysis_entries WHERE site_id = ?`, siteID)
		return err
	}
	_, err := r.db.Exec(`DELETE FROM access_analysis_entries WHERE site_id = ? AND id NOT IN (SELECT id FROM access_analysis_entries WHERE site_id = ? ORDER BY ts DESC, id DESC LIMIT ?)`, siteID, siteID, maxEntries)
	return err
}

func cleanupEntriesLimit(tx *sql.Tx, siteID string, maxEntries int) error {
	if maxEntries <= 0 {
		_, err := tx.Exec(`DELETE FROM access_analysis_entries WHERE site_id = ?`, siteID)
		return err
	}
	_, err := tx.Exec(`DELETE FROM access_analysis_entries WHERE site_id = ? AND id NOT IN (SELECT id FROM access_analysis_entries WHERE site_id = ? ORDER BY ts DESC, id DESC LIMIT ?)`, siteID, siteID, maxEntries)
	return err
}

func limitPaths(items []PathStat, topN int) []PathStat {
	if topN <= 0 || len(items) == 0 {
		return nil
	}
	sorted := append([]PathStat(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Requests > sorted[j].Requests })
	counts := map[string]int{}
	limited := make([]PathStat, 0, min(len(sorted), topN))
	for _, item := range sorted {
		if counts[item.Date] >= topN {
			continue
		}
		counts[item.Date]++
		limited = append(limited, item)
	}
	return limited
}

func limitIPs(items []IPStat, topN int) []IPStat {
	if topN <= 0 || len(items) == 0 {
		return nil
	}
	sorted := append([]IPStat(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Requests > sorted[j].Requests })
	counts := map[string]int{}
	limited := make([]IPStat, 0, min(len(sorted), topN))
	for _, item := range sorted {
		if counts[item.Date] >= topN {
			continue
		}
		counts[item.Date]++
		limited = append(limited, item)
	}
	return limited
}

func saveHourly(tx *sql.Tx, siteID string, items []HourlyPoint) error {
	for _, item := range items {
		if _, err := tx.Exec(`INSERT INTO access_analysis_hourly (site_id, hour, requests, unique_ips, status_4xx, status_5xx, bytes) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(site_id, hour) DO UPDATE SET requests = requests + excluded.requests, unique_ips = MAX(unique_ips, excluded.unique_ips), status_4xx = status_4xx + excluded.status_4xx, status_5xx = status_5xx + excluded.status_5xx, bytes = bytes + excluded.bytes`, siteID, item.Hour, item.Requests, item.UniqueIPs, item.Status4xx, item.Status5xx, item.Bytes); err != nil {
			return err
		}
	}
	return nil
}
func saveDaily(tx *sql.Tx, siteID string, items []HourlyPoint) error {
	daily := map[string]Summary{}
	for _, item := range items {
		date := item.Hour[:10]
		s := daily[date]
		s.TodayRequests += item.Requests
		s.UniqueIPs += item.UniqueIPs
		s.Status4xx += item.Status4xx
		s.Status5xx += item.Status5xx
		s.Bytes += item.Bytes
		daily[date] = s
	}
	for date, item := range daily {
		if _, err := tx.Exec(`INSERT INTO access_analysis_daily (site_id, date, requests, unique_ips, status_4xx, status_5xx, bytes) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(site_id, date) DO UPDATE SET requests = requests + excluded.requests, unique_ips = MAX(unique_ips, excluded.unique_ips), status_4xx = status_4xx + excluded.status_4xx, status_5xx = status_5xx + excluded.status_5xx, bytes = bytes + excluded.bytes`, siteID, date, item.TodayRequests, item.UniqueIPs, item.Status4xx, item.Status5xx, item.Bytes); err != nil {
			return err
		}
	}
	return nil
}
func savePaths(tx *sql.Tx, siteID string, items []PathStat) error {
	for _, item := range items {
		if _, err := tx.Exec(`INSERT INTO access_analysis_paths (site_id, date, path, requests, unique_ips, status_2xx, status_3xx, status_4xx, status_5xx, bytes, last_seen_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(site_id, date, path) DO UPDATE SET requests = requests + excluded.requests, unique_ips = MAX(unique_ips, excluded.unique_ips), status_2xx = status_2xx + excluded.status_2xx, status_3xx = status_3xx + excluded.status_3xx, status_4xx = status_4xx + excluded.status_4xx, status_5xx = status_5xx + excluded.status_5xx, bytes = bytes + excluded.bytes, last_seen_at = MAX(last_seen_at, excluded.last_seen_at)`, siteID, item.Date, item.Path, item.Requests, item.UniqueIPs, item.Status2xx, item.Status3xx, item.Status4xx, item.Status5xx, item.Bytes, item.LastSeenAt); err != nil {
			return err
		}
	}
	return nil
}
func saveIPs(tx *sql.Tx, siteID string, items []IPStat) error {
	for _, item := range items {
		if _, err := tx.Exec(`INSERT INTO access_analysis_ips (site_id, date, ip, requests, unique_paths, error_requests, bytes, first_seen_at, last_seen_at, sample_user_agent) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(site_id, date, ip) DO UPDATE SET requests = requests + excluded.requests, unique_paths = MAX(unique_paths, excluded.unique_paths), error_requests = error_requests + excluded.error_requests, bytes = bytes + excluded.bytes, first_seen_at = MIN(first_seen_at, excluded.first_seen_at), last_seen_at = MAX(last_seen_at, excluded.last_seen_at), sample_user_agent = COALESCE(NULLIF(sample_user_agent, ''), excluded.sample_user_agent)`, siteID, item.Date, item.IP, item.Requests, item.UniquePaths, item.ErrorRequests, item.Bytes, item.FirstSeenAt, item.LastSeenAt, item.SampleUserAgent); err != nil {
			return err
		}
	}
	return nil
}
func saveEntries(tx *sql.Tx, siteID string, items []Entry) error {
	stmt, err := tx.Prepare(`INSERT INTO access_analysis_entries (site_id, ts, ip, method, path, raw_path, status, bytes, referer, user_agent, is_anomaly, anomaly_reason) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, item := range items {
		if _, err := stmt.Exec(siteID, item.TS, item.IP, item.Method, item.Path, item.RawPath, item.Status, item.Bytes, item.Referer, item.UserAgent, boolToInt(item.IsAnomaly), item.AnomalyReason); err != nil {
			return err
		}
	}
	return nil
}
func saveAnomalies(tx *sql.Tx, siteID string, items []Anomaly) error {
	for _, item := range items {
		if _, err := tx.Exec(`INSERT INTO access_analysis_anomalies (site_id, date, kind, target, requests, severity, reason, first_seen_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, siteID, item.Date, item.Kind, item.Target, item.Requests, item.Severity, item.Reason, item.FirstSeenAt, item.LastSeenAt); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) count(table, where string, args []any) (int64, error) {
	var total int64
	err := r.db.QueryRow("SELECT COUNT(*) FROM "+table+" "+where, args...).Scan(&total)
	return total, err
}
func scanJob(row *sql.Row) (*Job, error) {
	job := &Job{}
	err := row.Scan(&job.ID, &job.SiteID, &job.Trigger, &job.RangeStart, &job.RangeEnd, &job.Status, &job.ScannedLines, &job.SkippedLines, &job.DurationMS, &job.ErrorMessage, &job.CreatedAt, &job.FinishedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return job, err
}
func scanJobRows(rows *sql.Rows) (*Job, error) {
	job := &Job{}
	return job, rows.Scan(&job.ID, &job.SiteID, &job.Trigger, &job.RangeStart, &job.RangeEnd, &job.Status, &job.ScannedLines, &job.SkippedLines, &job.DurationMS, &job.ErrorMessage, &job.CreatedAt, &job.FinishedAt)
}
func dateWhere(siteID string, q Query) (string, []any) {
	where := "WHERE site_id = ?"
	args := []any{siteID}
	if q.From != "" {
		where += " AND date >= ?"
		args = append(args, q.From[:10])
	}
	if q.To != "" {
		where += " AND date <= ?"
		args = append(args, q.To[:10])
	}
	return where, args
}
func entryWhere(siteID string, q Query) (string, []any) {
	where := "WHERE site_id = ?"
	args := []any{siteID}
	if q.From != "" {
		where += " AND ts >= ?"
		args = append(args, q.From)
	}
	if q.To != "" {
		where += " AND ts < ?"
		args = append(args, q.To)
	}
	if q.IP != "" {
		where += " AND ip = ?"
		args = append(args, q.IP)
	}
	if q.Path != "" {
		where += " AND path LIKE ?"
		args = append(args, "%"+q.Path+"%")
	}
	if q.Method != "" {
		where += " AND method = ?"
		args = append(args, strings.ToUpper(q.Method))
	}
	if q.Status > 0 {
		where += " AND status = ?"
		args = append(args, q.Status)
	}
	return where, args
}
func sortSQL(input string, allowed map[string]string, fallback string) string {
	if v, ok := allowed[input]; ok {
		return v
	}
	return allowed[fallback]
}
func offset(q Query) int {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	return (q.Page - 1) * q.PageSize
}
func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
