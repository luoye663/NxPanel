package repo

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type AuthAccountRepo struct {
	db *sql.DB
}

func NewAuthAccountRepo(db *sql.DB) *AuthAccountRepo {
	return &AuthAccountRepo{db: db}
}

func (r *AuthAccountRepo) ListForSite(siteID string) ([]*AuthAccount, error) {
	rows, err := r.db.Query(
		`SELECT id, scope, COALESCE(site_id, ''), username, password_hash, enabled, created_at, updated_at
		FROM auth_accounts
		WHERE scope = 'global' OR site_id = ?
		ORDER BY scope, username`,
		siteID,
	)
	if err != nil {
		return nil, fmt.Errorf("查询访问账户失败 site_id=%s: %w", siteID, err)
	}
	defer rows.Close()
	return scanAuthAccounts(rows)
}

func (r *AuthAccountRepo) ListByIDs(ids []string) ([]*AuthAccount, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := r.db.Query(
		`SELECT id, scope, COALESCE(site_id, ''), username, password_hash, enabled, created_at, updated_at
		FROM auth_accounts WHERE id IN (`+strings.Join(placeholders, ",")+") ORDER BY username",
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("按 ID 查询访问账户失败: %w", err)
	}
	defer rows.Close()
	return scanAuthAccounts(rows)
}

func (r *AuthAccountRepo) GetByID(id string) (*AuthAccount, error) {
	row := r.db.QueryRow(
		`SELECT id, scope, COALESCE(site_id, ''), username, password_hash, enabled, created_at, updated_at
		FROM auth_accounts WHERE id = ?`, id,
	)
	account := &AuthAccount{}
	var enabledInt int
	if err := row.Scan(&account.ID, &account.Scope, &account.SiteID, &account.Username, &account.PasswordHash, &enabledInt, &account.CreatedAt, &account.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("查询访问账户失败 id=%s: %w", id, err)
	}
	account.Enabled = enabledInt == 1
	return account, nil
}

func (r *AuthAccountRepo) Create(account *AuthAccount) error {
	now := time.Now().UTC().Format(time.RFC3339)
	account.CreatedAt = now
	account.UpdatedAt = now
	_, err := r.db.Exec(
		`INSERT INTO auth_accounts (id, scope, site_id, username, password_hash, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		account.ID, account.Scope, nullIfEmpty(account.SiteID), account.Username, account.PasswordHash, boolToInt(account.Enabled), account.CreatedAt, account.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("创建访问账户失败: %w", err)
	}
	return nil
}

func (r *AuthAccountRepo) Update(account *AuthAccount) error {
	now := time.Now().UTC().Format(time.RFC3339)
	account.UpdatedAt = now
	_, err := r.db.Exec(
		`UPDATE auth_accounts SET scope = ?, site_id = ?, username = ?, password_hash = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		account.Scope, nullIfEmpty(account.SiteID), account.Username, account.PasswordHash, boolToInt(account.Enabled), account.UpdatedAt, account.ID,
	)
	if err != nil {
		return fmt.Errorf("更新访问账户失败 id=%s: %w", account.ID, err)
	}
	return nil
}

func (r *AuthAccountRepo) Delete(id string) error {
	if _, err := r.db.Exec(`DELETE FROM auth_accounts WHERE id = ?`, id); err != nil {
		return fmt.Errorf("删除访问账户失败 id=%s: %w", id, err)
	}
	return nil
}

func (r *AuthAccountRepo) UsernameExists(username, excludeID string) (bool, error) {
	query := `SELECT COUNT(*) FROM auth_accounts WHERE username = ?`
	args := []any{username}
	if excludeID != "" {
		query += ` AND id != ?`
		args = append(args, excludeID)
	}
	var count int
	if err := r.db.QueryRow(query, args...).Scan(&count); err != nil {
		return false, fmt.Errorf("检查账户名失败: %w", err)
	}
	return count > 0, nil
}

func (r *AuthAccountRepo) CountReferences(id string) (int, error) {
	var count int
	if err := r.db.QueryRow(
		`SELECT
			(SELECT COUNT(*) FROM site_auth_rule_accounts WHERE account_id = ?) +
			(SELECT COUNT(*) FROM site_proxy_auth_accounts WHERE account_id = ?)`,
		id, id,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("统计账户引用失败: %w", err)
	}
	return count, nil
}

func scanAuthAccounts(rows *sql.Rows) ([]*AuthAccount, error) {
	var accounts []*AuthAccount
	for rows.Next() {
		account := &AuthAccount{}
		var enabledInt int
		if err := rows.Scan(&account.ID, &account.Scope, &account.SiteID, &account.Username, &account.PasswordHash, &enabledInt, &account.CreatedAt, &account.UpdatedAt); err != nil {
			return nil, err
		}
		account.Enabled = enabledInt == 1
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
