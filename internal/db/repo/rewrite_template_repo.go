// repo 包 — rewrite_templates 表的读写
//
// Location 模板由用户在面板内动态管理（新增/编辑/删除/启停），
// 0022 迁移种子化内置模板（spa / docker-proxy）以保证旧 ID 兼容。
package repo

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// RewriteTemplateRepo 提供 rewrite_templates 表的数据访问
type RewriteTemplateRepo struct {
	db *sql.DB
}

// NewRewriteTemplateRepo 创建 rewrite template repository
func NewRewriteTemplateRepo(db *sql.DB) *RewriteTemplateRepo {
	return &RewriteTemplateRepo{db: db}
}

const rewriteTemplateColumns = "id, name, category, description, params_json, template, enabled, sort_order, created_at, updated_at"

// scanner 兼容 *sql.Row 和 *sql.Rows 的 Scan 接口
type scanner interface {
	Scan(dest ...any) error
}

func scanRewriteTemplate(row scanner, t *RewriteTemplate) error {
	var paramsJSON string
	var enabled int
	if err := row.Scan(
		&t.ID, &t.Name, &t.Category, &t.Description, &paramsJSON, &t.Template,
		&enabled, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return err
	}
	t.Enabled = enabled != 0
	t.Params = nil
	if paramsJSON != "" {
		if err := json.Unmarshal([]byte(paramsJSON), &t.Params); err != nil {
			return fmt.Errorf("解析 params_json 失败 id=%s: %w", t.ID, err)
		}
	}
	if t.Params == nil {
		t.Params = []RewriteTemplateParam{}
	}
	return nil
}

// List 返回全部模板，按 sort_order、id 排序（管理页使用，包含禁用项）
func (r *RewriteTemplateRepo) List() ([]RewriteTemplate, error) {
	rows, err := r.db.Query(
		"SELECT " + rewriteTemplateColumns + " FROM rewrite_templates ORDER BY sort_order ASC, id ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("查询 Location 模板列表失败: %w", err)
	}
	defer rows.Close()

	var items []RewriteTemplate
	for rows.Next() {
		var t RewriteTemplate
		if err := scanRewriteTemplate(rows, &t); err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

// ListEnabled 仅返回启用模板，按 sort_order、id 排序（应用弹窗使用）
func (r *RewriteTemplateRepo) ListEnabled() ([]RewriteTemplate, error) {
	rows, err := r.db.Query(
		"SELECT " + rewriteTemplateColumns + " FROM rewrite_templates WHERE enabled = 1 ORDER BY sort_order ASC, id ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("查询启用 Location 模板失败: %w", err)
	}
	defer rows.Close()

	var items []RewriteTemplate
	for rows.Next() {
		var t RewriteTemplate
		if err := scanRewriteTemplate(rows, &t); err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

// GetByID 根据 ID 获取单个模板
func (r *RewriteTemplateRepo) GetByID(id string) (*RewriteTemplate, error) {
	row := r.db.QueryRow(
		"SELECT "+rewriteTemplateColumns+" FROM rewrite_templates WHERE id = ?", id,
	)
	var t RewriteTemplate
	if err := scanRewriteTemplate(row, &t); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("查询 Location 模板失败 id=%s: %w", id, err)
	}
	return &t, nil
}

// Create 创建模板
func (r *RewriteTemplateRepo) Create(t *RewriteTemplate) error {
	now := time.Now().UTC().Format(time.RFC3339)
	paramsJSON, err := encodeParams(t.Params)
	if err != nil {
		return err
	}
	enabled := 0
	if t.Enabled {
		enabled = 1
	}
	if _, err := r.db.Exec(
		`INSERT INTO rewrite_templates (id, name, category, description, params_json, template, enabled, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Category, t.Description, paramsJSON, t.Template, enabled, t.SortOrder, now, now,
	); err != nil {
		return fmt.Errorf("创建 Location 模板失败 id=%s: %w", t.ID, err)
	}
	t.CreatedAt = now
	t.UpdatedAt = now
	return nil
}

// Update 更新模板（全字段）
func (r *RewriteTemplateRepo) Update(t *RewriteTemplate) error {
	now := time.Now().UTC().Format(time.RFC3339)
	paramsJSON, err := encodeParams(t.Params)
	if err != nil {
		return err
	}
	enabled := 0
	if t.Enabled {
		enabled = 1
	}
	res, err := r.db.Exec(
		`UPDATE rewrite_templates SET
			name = ?, category = ?, description = ?, params_json = ?, template = ?,
			enabled = ?, sort_order = ?, updated_at = ?
		WHERE id = ?`,
		t.Name, t.Category, t.Description, paramsJSON, t.Template, enabled, t.SortOrder, now, t.ID,
	)
	if err != nil {
		return fmt.Errorf("更新 Location 模板失败 id=%s: %w", t.ID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("获取更新影响行数失败: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	t.UpdatedAt = now
	return nil
}

// Delete 删除模板
func (r *RewriteTemplateRepo) Delete(id string) error {
	res, err := r.db.Exec("DELETE FROM rewrite_templates WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("删除 Location 模板失败 id=%s: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("获取删除影响行数失败: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func encodeParams(params []RewriteTemplateParam) (string, error) {
	if params == nil {
		params = []RewriteTemplateParam{}
	}
	data, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("序列化 params 失败: %w", err)
	}
	return string(data), nil
}
