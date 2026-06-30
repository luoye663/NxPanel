package repo

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type AuthRuleRepo struct {
	db *sql.DB
}

func NewAuthRuleRepo(db *sql.DB) *AuthRuleRepo {
	return &AuthRuleRepo{db: db}
}

func (r *AuthRuleRepo) ListBySiteID(siteID string) ([]*SiteAuthRule, error) {
	rows, err := r.db.Query(
		`SELECT id, site_id, name, path, username, password_hash, htpasswd_path, enabled, sort_order, created_at, updated_at
		FROM site_auth_rules WHERE site_id = ? ORDER BY sort_order, created_at`,
		siteID,
	)
	if err != nil {
		return nil, fmt.Errorf("查询加密访问规则失败 site_id=%s: %w", siteID, err)
	}
	defer rows.Close()

	var rules []*SiteAuthRule
	for rows.Next() {
		rule := &SiteAuthRule{}
		var enabledInt int
		if err := rows.Scan(&rule.ID, &rule.SiteID, &rule.Name, &rule.Path, &rule.Username,
			&rule.PasswordHash, &rule.HtpasswdPath, &enabledInt, &rule.SortOrder,
			&rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		rule.Enabled = enabledInt == 1
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (r *AuthRuleRepo) GetByID(id string) (*SiteAuthRule, error) {
	rule := &SiteAuthRule{}
	var enabledInt int
	err := r.db.QueryRow(
		`SELECT id, site_id, name, path, username, password_hash, htpasswd_path, enabled, sort_order, created_at, updated_at
		FROM site_auth_rules WHERE id = ?`, id,
	).Scan(&rule.ID, &rule.SiteID, &rule.Name, &rule.Path, &rule.Username,
		&rule.PasswordHash, &rule.HtpasswdPath, &enabledInt, &rule.SortOrder,
		&rule.CreatedAt, &rule.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询加密访问规则失败 id=%s: %w", id, err)
	}
	rule.Enabled = enabledInt == 1
	return rule, nil
}

func (r *AuthRuleRepo) Create(rule *SiteAuthRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	enabledInt := boolToInt(rule.Enabled)
	_, err := r.db.Exec(
		`INSERT INTO site_auth_rules (id, site_id, name, path, username, password_hash, htpasswd_path, enabled, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, rule.SiteID, rule.Name, rule.Path, rule.Username,
		rule.PasswordHash, rule.HtpasswdPath, enabledInt, rule.SortOrder, now, now,
	)
	if err != nil {
		return fmt.Errorf("创建加密访问规则失败: %w", err)
	}
	return nil
}

func (r *AuthRuleRepo) Update(rule *SiteAuthRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	enabledInt := boolToInt(rule.Enabled)
	_, err := r.db.Exec(
		`UPDATE site_auth_rules SET name = ?, path = ?, username = ?, password_hash = ?, htpasswd_path = ?, enabled = ?, sort_order = ?, updated_at = ?
		WHERE id = ?`,
		rule.Name, rule.Path, rule.Username, rule.PasswordHash, rule.HtpasswdPath, enabledInt, rule.SortOrder, now, rule.ID,
	)
	if err != nil {
		return fmt.Errorf("更新加密访问规则失败 id=%s: %w", rule.ID, err)
	}
	return nil
}

func (r *AuthRuleRepo) Delete(id string) error {
	_, err := r.db.Exec("DELETE FROM site_auth_rules WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("删除加密访问规则失败 id=%s: %w", id, err)
	}
	return nil
}

func (r *AuthRuleRepo) GetAccountIDs(ruleID string) ([]string, error) {
	rows, err := r.db.Query(`SELECT account_id FROM site_auth_rule_accounts WHERE rule_id = ? ORDER BY account_id`, ruleID)
	if err != nil {
		return nil, fmt.Errorf("查询加密访问规则账户失败 rule_id=%s: %w", ruleID, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *AuthRuleRepo) SetAccountIDs(ruleID string, accountIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM site_auth_rule_accounts WHERE rule_id = ?`, ruleID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("清理加密访问规则账户失败: %w", err)
	}
	for _, accountID := range accountIDs {
		if _, err := tx.Exec(`INSERT INTO site_auth_rule_accounts (rule_id, account_id) VALUES (?, ?)`, ruleID, accountID); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("保存加密访问规则账户失败: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交加密访问规则账户失败: %w", err)
	}
	return nil
}

func (r *AuthRuleRepo) ListRuleIDsByAccountID(accountID string) ([]string, error) {
	rows, err := r.db.Query(`SELECT rule_id FROM site_auth_rule_accounts WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, fmt.Errorf("查询账户关联规则失败 account_id=%s: %w", accountID, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

type DenyRuleRepo struct {
	db *sql.DB
}

func NewDenyRuleRepo(db *sql.DB) *DenyRuleRepo {
	return &DenyRuleRepo{db: db}
}

func (r *DenyRuleRepo) ListBySiteID(siteID string) ([]*SiteDenyRule, error) {
	rows, err := r.db.Query(
		`SELECT id, site_id, name, deny_type, pattern, extension_pattern, path_pattern, enabled, sort_order, created_at, updated_at
		FROM site_deny_rules WHERE site_id = ? ORDER BY sort_order, created_at`,
		siteID,
	)
	if err != nil {
		return nil, fmt.Errorf("查询禁止访问规则失败 site_id=%s: %w", siteID, err)
	}
	defer rows.Close()

	var rules []*SiteDenyRule
	for rows.Next() {
		rule := &SiteDenyRule{}
		var enabledInt int
		var extPattern, pathPattern sql.NullString
		if err := rows.Scan(&rule.ID, &rule.SiteID, &rule.Name, &rule.DenyType,
			&rule.Pattern, &extPattern, &pathPattern,
			&enabledInt, &rule.SortOrder,
			&rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		rule.ExtensionPattern = extPattern.String
		rule.PathPattern = pathPattern.String
		rule.Enabled = enabledInt == 1
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (r *DenyRuleRepo) GetByID(id string) (*SiteDenyRule, error) {
	rule := &SiteDenyRule{}
	var enabledInt int
	var extPattern, pathPattern sql.NullString
	err := r.db.QueryRow(
		`SELECT id, site_id, name, deny_type, pattern, extension_pattern, path_pattern, enabled, sort_order, created_at, updated_at
		FROM site_deny_rules WHERE id = ?`, id,
	).Scan(&rule.ID, &rule.SiteID, &rule.Name, &rule.DenyType,
		&rule.Pattern, &extPattern, &pathPattern,
		&enabledInt, &rule.SortOrder,
		&rule.CreatedAt, &rule.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询禁止访问规则失败 id=%s: %w", id, err)
	}
	rule.ExtensionPattern = extPattern.String
	rule.PathPattern = pathPattern.String
	rule.Enabled = enabledInt == 1
	return rule, nil
}

func (r *DenyRuleRepo) Create(rule *SiteDenyRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	enabledInt := boolToInt(rule.Enabled)
	_, err := r.db.Exec(
		`INSERT INTO site_deny_rules (id, site_id, name, deny_type, pattern, extension_pattern, path_pattern, enabled, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, rule.SiteID, rule.Name, rule.DenyType, rule.Pattern,
		nullableString(rule.ExtensionPattern), nullableString(rule.PathPattern),
		enabledInt, rule.SortOrder, now, now,
	)
	if err != nil {
		return fmt.Errorf("创建禁止访问规则失败: %w", err)
	}
	return nil
}

func (r *DenyRuleRepo) Update(rule *SiteDenyRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	enabledInt := boolToInt(rule.Enabled)
	_, err := r.db.Exec(
		`UPDATE site_deny_rules SET name = ?, deny_type = ?, pattern = ?, extension_pattern = ?, path_pattern = ?, enabled = ?, sort_order = ?, updated_at = ?
		WHERE id = ?`,
		rule.Name, rule.DenyType, rule.Pattern,
		nullableString(rule.ExtensionPattern), nullableString(rule.PathPattern),
		enabledInt, rule.SortOrder, now, rule.ID,
	)
	if err != nil {
		return fmt.Errorf("更新禁止访问规则失败 id=%s: %w", rule.ID, err)
	}
	return nil
}

func (r *DenyRuleRepo) Delete(id string) error {
	_, err := r.db.Exec("DELETE FROM site_deny_rules WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("删除禁止访问规则失败 id=%s: %w", id, err)
	}
	return nil
}

type IPWhitelistRuleRepo struct {
	db *sql.DB
}

func NewIPWhitelistRuleRepo(db *sql.DB) *IPWhitelistRuleRepo {
	return &IPWhitelistRuleRepo{db: db}
}

func (r *IPWhitelistRuleRepo) ListBySiteID(siteID string) ([]*SiteIPWhitelistRule, error) {
	rows, err := r.db.Query(
		`SELECT id, site_id, name, rule_type, ips_json, enabled, sort_order, created_at, updated_at
		FROM site_ip_whitelist_rules WHERE site_id = ? ORDER BY sort_order, created_at`,
		siteID,
	)
	if err != nil {
		return nil, fmt.Errorf("查询 IP 白名单规则失败 site_id=%s: %w", siteID, err)
	}
	defer rows.Close()

	var rules []*SiteIPWhitelistRule
	for rows.Next() {
		rule := &SiteIPWhitelistRule{}
		var enabledInt int
		if err := rows.Scan(&rule.ID, &rule.SiteID, &rule.Name, &rule.RuleType, &rule.IPsJSON, &enabledInt, &rule.SortOrder, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		rule.Enabled = enabledInt == 1
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (r *IPWhitelistRuleRepo) GetByID(id string) (*SiteIPWhitelistRule, error) {
	rule := &SiteIPWhitelistRule{}
	var enabledInt int
	err := r.db.QueryRow(
		`SELECT id, site_id, name, rule_type, ips_json, enabled, sort_order, created_at, updated_at
		FROM site_ip_whitelist_rules WHERE id = ?`, id,
	).Scan(&rule.ID, &rule.SiteID, &rule.Name, &rule.RuleType, &rule.IPsJSON, &enabledInt, &rule.SortOrder, &rule.CreatedAt, &rule.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询 IP 白名单规则失败 id=%s: %w", id, err)
	}
	rule.Enabled = enabledInt == 1
	return rule, nil
}

func (r *IPWhitelistRuleRepo) Create(rule *SiteIPWhitelistRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	enabledInt := boolToInt(rule.Enabled)
	ipsJSON, err := json.Marshal(ParseJSONStringSlice(rule.IPsJSON))
	if err != nil {
		return fmt.Errorf("创建 IP 白名单规则失败: %w", err)
	}
	_, err = r.db.Exec(
		`INSERT INTO site_ip_whitelist_rules (id, site_id, name, rule_type, ips_json, enabled, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, rule.SiteID, rule.Name, rule.RuleType, string(ipsJSON), enabledInt, rule.SortOrder, now, now,
	)
	if err != nil {
		return fmt.Errorf("创建 IP 白名单规则失败: %w", err)
	}
	return nil
}

func (r *IPWhitelistRuleRepo) Update(rule *SiteIPWhitelistRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	enabledInt := boolToInt(rule.Enabled)
	ipsJSON, err := json.Marshal(ParseJSONStringSlice(rule.IPsJSON))
	if err != nil {
		return fmt.Errorf("更新 IP 白名单规则失败: %w", err)
	}
	_, err = r.db.Exec(
		`UPDATE site_ip_whitelist_rules SET name = ?, rule_type = ?, ips_json = ?, enabled = ?, sort_order = ?, updated_at = ?
		WHERE id = ?`,
		rule.Name, rule.RuleType, string(ipsJSON), enabledInt, rule.SortOrder, now, rule.ID,
	)
	if err != nil {
		return fmt.Errorf("更新 IP 白名单规则失败 id=%s: %w", rule.ID, err)
	}
	return nil
}

func (r *IPWhitelistRuleRepo) Delete(id string) error {
	_, err := r.db.Exec("DELETE FROM site_ip_whitelist_rules WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("删除 IP 白名单规则失败 id=%s: %w", id, err)
	}
	return nil
}

func ParseJSONStringSlice(value string) []string {
	var items []string
	if err := json.Unmarshal([]byte(value), &items); err == nil {
		return items
	}
	return []string{}
}
