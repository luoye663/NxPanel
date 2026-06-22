package repo

import (
	"database/sql"
	"fmt"
	"time"
)

type SiteBackupRepo struct {
	db *sql.DB
}

func NewSiteBackupRepo(db *sql.DB) *SiteBackupRepo {
	return &SiteBackupRepo{db: db}
}

func (r *SiteBackupRepo) Create(backup *SiteBackup) error {
	now := time.Now().UTC().Format(time.RFC3339)
	backup.CreatedAt = now
	backup.UpdatedAt = now
	_, err := r.db.Exec(
		`INSERT INTO site_backups (id, site_id, backup_type, name, backup_path, size_bytes, status, message, created_at, updated_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		backup.ID, backup.SiteID, backup.BackupType, backup.Name, backup.BackupPath,
		backup.SizeBytes, backup.Status, backup.Message, backup.CreatedAt, backup.UpdatedAt, backup.FinishedAt,
	)
	if err != nil {
		return fmt.Errorf("创建站点备份记录失败: %w", err)
	}
	return nil
}

func (r *SiteBackupRepo) GetByID(id string) (*SiteBackup, error) {
	backup := &SiteBackup{}
	err := r.db.QueryRow(
		`SELECT id, site_id, backup_type, name, backup_path, size_bytes, status, message, created_at, updated_at, finished_at
		FROM site_backups WHERE id = ?`, id,
	).Scan(&backup.ID, &backup.SiteID, &backup.BackupType, &backup.Name, &backup.BackupPath,
		&backup.SizeBytes, &backup.Status, &backup.Message, &backup.CreatedAt, &backup.UpdatedAt, &backup.FinishedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询站点备份记录失败 id=%s: %w", id, err)
	}
	return backup, nil
}

func (r *SiteBackupRepo) ListBySiteID(siteID string) ([]*SiteBackup, error) {
	rows, err := r.db.Query(
		`SELECT id, site_id, backup_type, name, backup_path, size_bytes, status, message, created_at, updated_at, finished_at
		FROM site_backups WHERE site_id = ? AND status != 'deleted' ORDER BY created_at DESC`, siteID,
	)
	if err != nil {
		return nil, fmt.Errorf("查询站点备份列表失败 site_id=%s: %w", siteID, err)
	}
	defer rows.Close()

	var backups []*SiteBackup
	for rows.Next() {
		backup := &SiteBackup{}
		if err := rows.Scan(&backup.ID, &backup.SiteID, &backup.BackupType, &backup.Name, &backup.BackupPath,
			&backup.SizeBytes, &backup.Status, &backup.Message, &backup.CreatedAt, &backup.UpdatedAt, &backup.FinishedAt); err != nil {
			return nil, err
		}
		backups = append(backups, backup)
	}
	return backups, rows.Err()
}

func (r *SiteBackupRepo) MarkFinished(id, status, message string, sizeBytes int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`UPDATE site_backups SET status = ?, message = ?, size_bytes = ?, updated_at = ?, finished_at = ? WHERE id = ?`,
		status, message, sizeBytes, now, now, id,
	)
	if err != nil {
		return fmt.Errorf("更新站点备份状态失败 id=%s: %w", id, err)
	}
	return nil
}

func (r *SiteBackupRepo) MarkDeleted(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(`UPDATE site_backups SET status = 'deleted', updated_at = ? WHERE id = ?`, now, id)
	if err != nil {
		return fmt.Errorf("标记站点备份删除失败 id=%s: %w", id, err)
	}
	return nil
}

func (r *SiteBackupRepo) ListSuccessfulBySiteID(siteID string) ([]*SiteBackup, error) {
	rows, err := r.db.Query(
		`SELECT id, site_id, backup_type, name, backup_path, size_bytes, status, message, created_at, updated_at, finished_at
		FROM site_backups WHERE site_id = ? AND status = 'success' ORDER BY created_at DESC`, siteID,
	)
	if err != nil {
		return nil, fmt.Errorf("查询成功站点备份列表失败 site_id=%s: %w", siteID, err)
	}
	defer rows.Close()
	var backups []*SiteBackup
	for rows.Next() {
		backup := &SiteBackup{}
		if err := rows.Scan(&backup.ID, &backup.SiteID, &backup.BackupType, &backup.Name, &backup.BackupPath,
			&backup.SizeBytes, &backup.Status, &backup.Message, &backup.CreatedAt, &backup.UpdatedAt, &backup.FinishedAt); err != nil {
			return nil, err
		}
		backups = append(backups, backup)
	}
	return backups, rows.Err()
}
