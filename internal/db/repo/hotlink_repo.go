package repo

import (
	"database/sql"
	"fmt"
	"time"
)

type HotlinkRuleRepo struct {
	db *sql.DB
}

func NewHotlinkRuleRepo(db *sql.DB) *HotlinkRuleRepo {
	return &HotlinkRuleRepo{db: db}
}

func (r *HotlinkRuleRepo) ListBySiteID(siteID string) ([]*SiteHotlinkRule, error) {
	rows, err := r.db.Query(
		`SELECT id, site_id, name, enabled, extensions, referers, allow_empty_referer, block_status, sort_order, created_at, updated_at
		FROM site_hotlink_rules WHERE site_id = ? ORDER BY sort_order, created_at`,
		siteID,
	)
	if err != nil {
		return nil, fmt.Errorf("查询防盗链规则失败 site_id=%s: %w", siteID, err)
	}
	defer rows.Close()

	var rules []*SiteHotlinkRule
	for rows.Next() {
		rule := &SiteHotlinkRule{}
		var enabledInt, allowEmptyInt int
		if err := rows.Scan(&rule.ID, &rule.SiteID, &rule.Name, &enabledInt, &rule.Extensions,
			&rule.Referers, &allowEmptyInt, &rule.BlockStatus, &rule.SortOrder,
			&rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		rule.Enabled = enabledInt == 1
		rule.AllowEmptyReferer = allowEmptyInt == 1
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (r *HotlinkRuleRepo) GetByID(id string) (*SiteHotlinkRule, error) {
	rule := &SiteHotlinkRule{}
	var enabledInt, allowEmptyInt int
	err := r.db.QueryRow(
		`SELECT id, site_id, name, enabled, extensions, referers, allow_empty_referer, block_status, sort_order, created_at, updated_at
		FROM site_hotlink_rules WHERE id = ?`, id,
	).Scan(&rule.ID, &rule.SiteID, &rule.Name, &enabledInt, &rule.Extensions,
		&rule.Referers, &allowEmptyInt, &rule.BlockStatus, &rule.SortOrder,
		&rule.CreatedAt, &rule.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询防盗链规则失败 id=%s: %w", id, err)
	}
	rule.Enabled = enabledInt == 1
	rule.AllowEmptyReferer = allowEmptyInt == 1
	return rule, nil
}

func (r *HotlinkRuleRepo) Create(rule *SiteHotlinkRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`INSERT INTO site_hotlink_rules (id, site_id, name, enabled, extensions, referers, allow_empty_referer, block_status, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, rule.SiteID, rule.Name, boolToInt(rule.Enabled), rule.Extensions, rule.Referers,
		boolToInt(rule.AllowEmptyReferer), rule.BlockStatus, rule.SortOrder, now, now,
	)
	if err != nil {
		return fmt.Errorf("创建防盗链规则失败: %w", err)
	}
	return nil
}

func (r *HotlinkRuleRepo) Update(rule *SiteHotlinkRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(
		`UPDATE site_hotlink_rules SET name = ?, enabled = ?, extensions = ?, referers = ?, allow_empty_referer = ?, block_status = ?, sort_order = ?, updated_at = ?
		WHERE id = ?`,
		rule.Name, boolToInt(rule.Enabled), rule.Extensions, rule.Referers,
		boolToInt(rule.AllowEmptyReferer), rule.BlockStatus, rule.SortOrder, now, rule.ID,
	)
	if err != nil {
		return fmt.Errorf("更新防盗链规则失败 id=%s: %w", rule.ID, err)
	}
	return nil
}

func (r *HotlinkRuleRepo) Delete(id string) error {
	_, err := r.db.Exec("DELETE FROM site_hotlink_rules WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("删除防盗链规则失败 id=%s: %w", id, err)
	}
	return nil
}
