package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrPluginNotFound = errors.New("plugin not found")

type Installation struct {
	ID                 string    `json:"id"`
	ActiveVersion      string    `json:"active_version"`
	Enabled            bool      `json:"enabled"`
	HealthStatus       string    `json:"health_status"`
	LastError          string    `json:"last_error,omitempty"`
	InstalledAt        string    `json:"installed_at"`
	UpdatedAt          string    `json:"updated_at"`
	Manifest           *Manifest `json:"manifest,omitempty"`
	Permissions        []string  `json:"approved_permissions"`
	Source             string    `json:"source"`
	VerificationStatus string    `json:"verification_status"`
	SourceConflict     bool      `json:"source_conflict,omitempty"`
}

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(ctx context.Context) ([]Installation, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT plugin_id, active_version, enabled, health_status,
		last_error, installed_at, updated_at, source, verification_status, source_conflict
		FROM plugin_installations ORDER BY plugin_id`)
	if err != nil {
		return nil, err
	}
	items := make([]Installation, 0)
	for rows.Next() {
		var item Installation
		var enabled int
		var conflict int
		if err := rows.Scan(&item.ID, &item.ActiveVersion, &enabled, &item.HealthStatus, &item.LastError, &item.InstalledAt, &item.UpdatedAt, &item.Source, &item.VerificationStatus, &conflict); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		item.SourceConflict = conflict != 0
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range items {
		if err := r.enrich(ctx, &items[i]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (r *Repository) Get(ctx context.Context, id string) (*Installation, error) {
	item := &Installation{}
	var enabled int
	var conflict int
	err := r.db.QueryRowContext(ctx, `SELECT plugin_id, active_version, enabled, health_status,
		last_error, installed_at, updated_at, source, verification_status, source_conflict
		FROM plugin_installations WHERE plugin_id=?`, id).
		Scan(&item.ID, &item.ActiveVersion, &enabled, &item.HealthStatus, &item.LastError, &item.InstalledAt, &item.UpdatedAt, &item.Source, &item.VerificationStatus, &conflict)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPluginNotFound
	}
	if err != nil {
		return nil, err
	}
	item.Enabled = enabled != 0
	item.SourceConflict = conflict != 0
	if err := r.enrich(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

func (r *Repository) enrich(ctx context.Context, item *Installation) error {
	var raw string
	if err := r.db.QueryRowContext(ctx, `SELECT manifest_json FROM plugin_versions WHERE plugin_id=? AND version=?`, item.ID, item.ActiveVersion).Scan(&raw); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(raw), &item.Manifest); err != nil {
		return err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT permission FROM plugin_permissions WHERE plugin_id=? ORDER BY permission`, item.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	item.Permissions = make([]string, 0)
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return err
		}
		item.Permissions = append(item.Permissions, permission)
	}
	return rows.Err()
}

func (r *Repository) Activate(ctx context.Context, manifest *Manifest, packageHash, installPath string, approved []string) error {
	return r.ActivateWithSource(ctx, manifest, packageHash, installPath, approved, "official", "tuf_verified")
}

func (r *Repository) ActivateWithSource(ctx context.Context, manifest *Manifest, packageHash, installPath string, approved []string, source, verification string) error {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO plugin_installations
		(plugin_id, active_version, enabled, health_status, last_error, installed_at, updated_at, source, verification_status, source_conflict)
		VALUES (?, ?, 0, 'ready', '', ?, ?, ?, ?, 0)
		ON CONFLICT(plugin_id) DO UPDATE SET active_version=excluded.active_version,
		enabled=0, health_status='ready', last_error='', updated_at=excluded.updated_at,
		source=excluded.source, verification_status=excluded.verification_status, source_conflict=0`, manifest.ID, manifest.Version, now, now, source, verification)
	if err != nil {
		return fmt.Errorf("upsert plugin installation: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO plugin_versions
		(plugin_id, version, package_sha256, manifest_json, install_path, installed_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(plugin_id, version) DO UPDATE SET package_sha256=excluded.package_sha256,
		manifest_json=excluded.manifest_json, install_path=excluded.install_path, installed_at=excluded.installed_at`,
		manifest.ID, manifest.Version, packageHash, string(raw), installPath, now)
	if err != nil {
		return fmt.Errorf("upsert plugin version: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM plugin_permissions WHERE plugin_id=?`, manifest.ID); err != nil {
		return err
	}
	for _, permission := range approved {
		if _, err := tx.ExecContext(ctx, `INSERT INTO plugin_permissions(plugin_id, permission, approved_at) VALUES (?, ?, ?)`, manifest.ID, permission, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) SetHealth(ctx context.Context, id, status, lastError string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE plugin_installations SET health_status=?, last_error=?, updated_at=? WHERE plugin_id=?`, status, lastError, time.Now().UTC(), id)
	return err
}

func (r *Repository) SyncDeveloperConflicts(ctx context.Context, officialIDs map[string]bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE plugin_installations SET source_conflict=0 WHERE source='developer'`); err != nil {
		return err
	}
	for id := range officialIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE plugin_installations SET source_conflict=1 WHERE source='developer' AND plugin_id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) SetEnabled(ctx context.Context, id string, enabled bool) error {
	result, err := r.db.ExecContext(ctx, `UPDATE plugin_installations SET enabled=?, updated_at=? WHERE plugin_id=?`, enabled, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrPluginNotFound
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM plugin_installations WHERE plugin_id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrPluginNotFound
	}
	return nil
}
