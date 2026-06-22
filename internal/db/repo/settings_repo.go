// repo 包 — settings 表的读写
//
// settings 表是全局 KV 存储，用于保存 nginx_bin、panel_version 等配置。
package repo

import (
	"database/sql"
	"fmt"
	"time"
)

// SettingsRepo 提供 settings 表的数据访问
type SettingsRepo struct {
	db *sql.DB
}

// NewSettingsRepo 创建 settings repository
func NewSettingsRepo(db *sql.DB) *SettingsRepo {
	return &SettingsRepo{db: db}
}

// Get 根据 key 获取 setting 值
// 如果 key 不存在，返回空字符串和 nil error
func (r *SettingsRepo) Get(key string) (string, error) {
	var value string
	err := r.db.QueryRow(
		"SELECT value FROM settings WHERE key = ?",
		key,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("查询 setting 失败 key=%s: %w", key, err)
	}
	return value, nil
}

// Set 写入或更新一个 setting（UPSERT）
func (r *SettingsRepo) Set(key, value string) error {
	_, err := r.db.Exec(
		"INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?) "+
			"ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at",
		key, value, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("写入 setting 失败 key=%s: %w", key, err)
	}
	return nil
}

// GetAll 获取所有 settings
func (r *SettingsRepo) GetAll() (map[string]string, error) {
	rows, err := r.db.Query("SELECT key, value FROM settings")
	if err != nil {
		return nil, fmt.Errorf("查询所有 settings 失败: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, rows.Err()
}
