// repo 包 — backups 表的读写
//
// backups 记录每次写操作前的文件备份信息。
package repo

import (
	"database/sql"
	"fmt"
	"time"
)

// BackupRepo 提供 backups 表的数据访问
type BackupRepo struct {
	db *sql.DB
}

// NewBackupRepo 创建 backup repository
func NewBackupRepo(db *sql.DB) *BackupRepo {
	return &BackupRepo{db: db}
}

// Create 创建备份记录
func (r *BackupRepo) Create(b *Backup) error {
	_, err := r.db.Exec(
		`INSERT INTO backups (
			id, operation_id, file_path, backup_path,
			original_sha256, backup_sha256, file_existed, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.OperationID, b.FilePath, b.BackupPath,
		b.OriginalSHA256, b.BackupSHA256, boolToInt(b.FileExisted),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("创建备份记录失败: %w", err)
	}
	return nil
}

// ListByOperationID 查询某个操作的所有备份记录
func (r *BackupRepo) ListByOperationID(operationID string) ([]*Backup, error) {
	rows, err := r.db.Query(
		`SELECT id, operation_id, file_path, backup_path,
			original_sha256, backup_sha256, file_existed, created_at
		FROM backups WHERE operation_id = ?`,
		operationID,
	)
	if err != nil {
		return nil, fmt.Errorf("查询备份记录失败 operation_id=%s: %w", operationID, err)
	}
	defer rows.Close()

	var backups []*Backup
	for rows.Next() {
		b := &Backup{}
		var fileExistedInt int
		if err := rows.Scan(
			&b.ID, &b.OperationID, &b.FilePath, &b.BackupPath,
			&b.OriginalSHA256, &b.BackupSHA256, &fileExistedInt, &b.CreatedAt,
		); err != nil {
			return nil, err
		}
		b.FileExisted = fileExistedInt == 1
		backups = append(backups, b)
	}

	return backups, rows.Err()
}
