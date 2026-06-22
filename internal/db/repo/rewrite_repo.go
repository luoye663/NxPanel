// repo 包 — site_rewrite 表的读写
//
// 只存储自定义 Location 文件的元信息（hash、大小），实际内容由文件系统管理。
package repo

import (
	"database/sql"
	"fmt"
	"time"
)

// RewriteRepo 提供 site_rewrite 表的数据访问
type RewriteRepo struct {
	db *sql.DB
}

// NewRewriteRepo 创建 rewrite repository
func NewRewriteRepo(db *sql.DB) *RewriteRepo {
	return &RewriteRepo{db: db}
}

// GetBySiteID 根据 site_id 获取自定义 Location 元信息
func (r *RewriteRepo) GetBySiteID(siteID string) (*SiteRewrite, error) {
	sr := &SiteRewrite{}
	err := r.db.QueryRow(
		"SELECT site_id, content_hash, size_bytes, updated_at FROM site_rewrite WHERE site_id = ?",
		siteID,
	).Scan(&sr.SiteID, &sr.ContentHash, &sr.SizeBytes, &sr.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询自定义 Location 元信息失败 site_id=%s: %w", siteID, err)
	}
	return sr, nil
}

// Upsert 创建或更新自定义 Location 元信息
func (r *RewriteRepo) Upsert(sr *SiteRewrite) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`INSERT INTO site_rewrite (site_id, content_hash, size_bytes, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(site_id) DO UPDATE SET
			content_hash = excluded.content_hash,
			size_bytes = excluded.size_bytes,
			updated_at = excluded.updated_at`,
		sr.SiteID, sr.ContentHash, sr.SizeBytes, now,
	)
	if err != nil {
		return fmt.Errorf("保存自定义 Location 元信息失败 site_id=%s: %w", sr.SiteID, err)
	}
	return nil
}
