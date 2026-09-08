package accesspolicy

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrVersionConflict means the caller edited a policy that has since changed.
var ErrVersionConflict = errors.New("访问策略已更新，请刷新后重试")

type Repo struct{ db *sql.DB }

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

type StoredPolicy struct {
	SiteID string
	Policy Policy
}

type policyScanner interface{ Scan(...any) error }

func scanPolicy(row policyScanner) (*Policy, error) {
	var raw, mode, status, lastError string
	var version int64
	if err := row.Scan(&raw, &mode, &version, &status, &lastError); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var policy Policy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return nil, fmt.Errorf("读取访问策略失败: %w", err)
	}
	policy.Mode, policy.Version = mode, version
	policy.ApplyStatus, policy.LastError = status, lastError
	return &policy, nil
}

func (r *Repo) Get(siteID string) (*Policy, error) {
	return scanPolicy(r.db.QueryRow(`SELECT desired_json, mode, version, apply_status, last_error
		FROM site_access_policies WHERE site_id=?`, siteID))
}

// GetApplied returns the last confirmed applied policy, never a pending edit.
func (r *Repo) GetApplied(siteID string) (*Policy, error) {
	return scanPolicy(r.db.QueryRow(`SELECT applied_json, 'unified', applied_version, 'applied', ''
		FROM site_access_policies WHERE site_id=? AND applied_version>0`, siteID))
}

// SaveDesired atomically compares the editor's version and stores the next
// desired policy. The live mode changes only after a successful MarkApplied.
func (r *Repo) SaveDesired(siteID string, policy Policy, expectedVersion int64) (*Policy, error) {
	if expectedVersion < 0 {
		return nil, ErrVersionConflict
	}
	policy.Version = expectedVersion + 1
	policy.Mode, policy.ApplyStatus, policy.LastError = "unified", "pending", ""
	raw, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("编码访问策略失败: %w", err)
	}
	var row *sql.Row
	if expectedVersion == 0 {
		row = r.db.QueryRow(`INSERT INTO site_access_policies (site_id, mode, version, desired_json)
			VALUES (?, 'legacy', 1, ?) ON CONFLICT(site_id) DO NOTHING
			RETURNING desired_json, mode, version, apply_status, last_error`, siteID, string(raw))
	} else {
		row = r.db.QueryRow(`UPDATE site_access_policies
			SET desired_json=?, version=version+1, apply_status='pending', last_error='', updated_at=CURRENT_TIMESTAMP
			WHERE site_id=? AND version=?
			RETURNING desired_json, mode, version, apply_status, last_error`, string(raw), siteID, expectedVersion)
	}
	saved, err := scanPolicy(row)
	if err == nil && saved == nil {
		return nil, ErrVersionConflict
	}
	return saved, err
}

func (r *Repo) MarkApplied(siteID string, version int64) error {
	result, err := r.db.Exec(`UPDATE site_access_policies
		SET mode='unified', applied_json=desired_json, applied_version=version,
		apply_status='applied', last_error='', updated_at=CURRENT_TIMESTAMP
		WHERE site_id=? AND version=?`, siteID, version)
	return policyUpdateResult(result, err)
}

// MarkError keeps the desired policy available for explicit synchronization.
// In particular, a transport timeout must not discard a potentially applied edit.
func (r *Repo) MarkError(siteID string, version int64, message string) error {
	result, err := r.db.Exec(`UPDATE site_access_policies SET apply_status='error', last_error=?,
		updated_at=CURRENT_TIMESTAMP WHERE site_id=? AND version=?`, message, siteID, version)
	return policyUpdateResult(result, err)
}

func policyUpdateResult(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrVersionConflict
	}
	return err
}

func (r *Repo) Mode(siteID string) (string, error) {
	var mode string
	err := r.db.QueryRow(`SELECT mode FROM site_access_policies WHERE site_id=?`, siteID).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return "legacy", nil
	}
	return mode, err
}

func (r *Repo) List() ([]StoredPolicy, error) {
	rows, err := r.db.Query(`SELECT site_id, desired_json, mode, version, apply_status, last_error
		FROM site_access_policies ORDER BY site_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]StoredPolicy, 0)
	for rows.Next() {
		var item StoredPolicy
		var raw, mode, status, lastError string
		var version int64
		if err := rows.Scan(&item.SiteID, &raw, &mode, &version, &status, &lastError); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &item.Policy); err != nil {
			return nil, fmt.Errorf("读取站点 %s 访问策略失败: %w", item.SiteID, err)
		}
		item.Policy.Mode, item.Policy.Version = mode, version
		item.Policy.ApplyStatus, item.Policy.LastError = status, lastError
		result = append(result, item)
	}
	return result, rows.Err()
}
