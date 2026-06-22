package repo

import (
	"database/sql"
	"fmt"
	"time"
)

type SiteBackupScheduleRepo struct {
	db *sql.DB
}

func NewSiteBackupScheduleRepo(db *sql.DB) *SiteBackupScheduleRepo {
	return &SiteBackupScheduleRepo{db: db}
}

func (r *SiteBackupScheduleRepo) GetBySiteID(siteID string) (*SiteBackupSchedule, error) {
	item := &SiteBackupSchedule{}
	var enabled int
	err := r.db.QueryRow(
		`SELECT site_id, enabled, backup_type, backup_dir, retention_count, schedule_type, schedule_time, weekday, month_day, last_run_at, created_at, updated_at
		FROM site_backup_schedules WHERE site_id = ?`, siteID,
	).Scan(&item.SiteID, &enabled, &item.BackupType, &item.BackupDir, &item.RetentionCount, &item.ScheduleType, &item.ScheduleTime, &item.Weekday, &item.MonthDay, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询站点定时备份配置失败 site_id=%s: %w", siteID, err)
	}
	item.Enabled = enabled == 1
	return item, nil
}

func (r *SiteBackupScheduleRepo) Upsert(item *SiteBackupSchedule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`INSERT INTO site_backup_schedules (site_id, enabled, backup_type, backup_dir, retention_count, schedule_type, schedule_time, weekday, month_day, last_run_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(site_id) DO UPDATE SET
			enabled = excluded.enabled,
			backup_type = excluded.backup_type,
			backup_dir = excluded.backup_dir,
			retention_count = excluded.retention_count,
			schedule_type = excluded.schedule_type,
			schedule_time = excluded.schedule_time,
			weekday = excluded.weekday,
			month_day = excluded.month_day,
			updated_at = excluded.updated_at`,
		item.SiteID, boolToInt(item.Enabled), item.BackupType, item.BackupDir, item.RetentionCount, item.ScheduleType, item.ScheduleTime, item.Weekday, item.MonthDay, item.LastRunAt, now, now,
	)
	if err != nil {
		return fmt.Errorf("保存站点定时备份配置失败 site_id=%s: %w", item.SiteID, err)
	}
	return nil
}

func (r *SiteBackupScheduleRepo) Enabled() ([]*SiteBackupSchedule, error) {
	rows, err := r.db.Query(
		`SELECT site_id, enabled, backup_type, backup_dir, retention_count, schedule_type, schedule_time, weekday, month_day, last_run_at, created_at, updated_at
		FROM site_backup_schedules WHERE enabled = 1`,
	)
	if err != nil {
		return nil, fmt.Errorf("查询启用的定时备份配置失败: %w", err)
	}
	defer rows.Close()
	var result []*SiteBackupSchedule
	for rows.Next() {
		item := &SiteBackupSchedule{}
		var enabled int
		if err := rows.Scan(&item.SiteID, &enabled, &item.BackupType, &item.BackupDir, &item.RetentionCount, &item.ScheduleType, &item.ScheduleTime, &item.Weekday, &item.MonthDay, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *SiteBackupScheduleRepo) All() ([]*SiteBackupSchedule, error) {
	rows, err := r.db.Query(
		`SELECT site_id, enabled, backup_type, backup_dir, retention_count, schedule_type, schedule_time, weekday, month_day, last_run_at, created_at, updated_at
		FROM site_backup_schedules`,
	)
	if err != nil {
		return nil, fmt.Errorf("查询全部定时备份配置失败: %w", err)
	}
	defer rows.Close()
	var result []*SiteBackupSchedule
	for rows.Next() {
		item := &SiteBackupSchedule{}
		var enabled int
		if err := rows.Scan(&item.SiteID, &enabled, &item.BackupType, &item.BackupDir, &item.RetentionCount, &item.ScheduleType, &item.ScheduleTime, &item.Weekday, &item.MonthDay, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *SiteBackupScheduleRepo) MarkRun(siteID string, runAt time.Time) error {
	_, err := r.db.Exec(`UPDATE site_backup_schedules SET last_run_at = ?, updated_at = ? WHERE site_id = ?`, runAt.UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), siteID)
	if err != nil {
		return fmt.Errorf("更新定时备份运行时间失败 site_id=%s: %w", siteID, err)
	}
	return nil
}
