package repo

import (
	"context"
	"database/sql"
	"fmt"
)

type UpstreamRepo struct {
	db *sql.DB
}

func NewUpstreamRepo(db *sql.DB) *UpstreamRepo { return &UpstreamRepo{db: db} }

func (r *UpstreamRepo) List(ctx context.Context) ([]*NginxUpstream, error) {
	return r.queryAggregates(ctx, "", nil)
}

func (r *UpstreamRepo) GetByID(ctx context.Context, id string) (*NginxUpstream, error) {
	items, err := r.queryAggregates(ctx, "WHERE u.id = ?", []any{id})
	return firstUpstream(items, err)
}

func (r *UpstreamRepo) GetByName(ctx context.Context, name string) (*NginxUpstream, error) {
	items, err := r.queryAggregates(ctx, "WHERE u.name = ? COLLATE NOCASE", []any{name})
	return firstUpstream(items, err)
}

// queryAggregates loads each parent and all of its members from one SQLite statement snapshot.
func (r *UpstreamRepo) queryAggregates(ctx context.Context, where string, args []any) ([]*NginxUpstream, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT
		u.id, u.name, u.algorithm, u.hash_key, u.consistent, u.keepalive,
		u.keepalive_requests, u.keepalive_timeout_seconds, u.advanced_directives,
		u.created_at, u.updated_at,
		s.id, s.upstream_id, s.address, s.weight, s.max_fails,
		s.fail_timeout_seconds, s.backup, s.down, s.sort_order, s.created_at, s.updated_at
		FROM nginx_upstreams u
		LEFT JOIN nginx_upstream_servers s ON s.upstream_id = u.id
		`+where+`
		ORDER BY u.name COLLATE NOCASE, u.id, s.sort_order, s.address COLLATE NOCASE, s.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("query upstream aggregates: %w", err)
	}
	defer rows.Close()

	items := make([]*NginxUpstream, 0)
	byID := make(map[string]*NginxUpstream)
	for rows.Next() {
		u := &NginxUpstream{}
		var consistent int
		var serverID, upstreamID, address, serverCreatedAt, serverUpdatedAt sql.NullString
		var weight, maxFails, failTimeout, backup, down, sortOrder sql.NullInt64
		if err := rows.Scan(
			&u.ID, &u.Name, &u.Algorithm, &u.HashKey, &consistent, &u.Keepalive,
			&u.KeepaliveRequests, &u.KeepaliveTimeoutSeconds, &u.AdvancedDirectives,
			&u.CreatedAt, &u.UpdatedAt,
			&serverID, &upstreamID, &address, &weight, &maxFails, &failTimeout,
			&backup, &down, &sortOrder, &serverCreatedAt, &serverUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan upstream aggregate: %w", err)
		}
		u.Consistent = consistent != 0
		existing := byID[u.ID]
		if existing == nil {
			u.Servers = make([]*NginxUpstreamServer, 0)
			items = append(items, u)
			byID[u.ID] = u
			existing = u
		}
		if serverID.Valid {
			existing.Servers = append(existing.Servers, &NginxUpstreamServer{
				ID: serverID.String, UpstreamID: upstreamID.String, Address: address.String,
				Weight: int(weight.Int64), MaxFails: int(maxFails.Int64),
				FailTimeoutSeconds: int(failTimeout.Int64), Backup: backup.Int64 != 0,
				Down: down.Int64 != 0, SortOrder: int(sortOrder.Int64),
				CreatedAt: serverCreatedAt.String, UpdatedAt: serverUpdatedAt.String,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func firstUpstream(items []*NginxUpstream, err error) (*NginxUpstream, error) {
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func (r *UpstreamRepo) Create(ctx context.Context, u *NginxUpstream) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO nginx_upstreams
		(id, name, algorithm, hash_key, consistent, keepalive, keepalive_requests,
		 keepalive_timeout_seconds, advanced_directives, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, u.ID, u.Name, u.Algorithm, u.HashKey,
		boolToInt(u.Consistent), u.Keepalive, u.KeepaliveRequests, u.KeepaliveTimeoutSeconds,
		u.AdvancedDirectives, u.CreatedAt, u.UpdatedAt); err != nil {
		return fmt.Errorf("create upstream: %w", err)
	}
	if err = insertUpstreamServers(ctx, tx, u); err != nil {
		return err
	}
	return tx.Commit()
}

// Update replaces all members in the same transaction and deliberately never updates name.
func (r *UpstreamRepo) Update(ctx context.Context, u *NginxUpstream) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE nginx_upstreams SET algorithm=?, hash_key=?, consistent=?,
		keepalive=?, keepalive_requests=?, keepalive_timeout_seconds=?, advanced_directives=?, updated_at=?
		WHERE id=?`, u.Algorithm, u.HashKey, boolToInt(u.Consistent), u.Keepalive,
		u.KeepaliveRequests, u.KeepaliveTimeoutSeconds, u.AdvancedDirectives, u.UpdatedAt, u.ID)
	if err != nil {
		return fmt.Errorf("update upstream: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM nginx_upstream_servers WHERE upstream_id = ?", u.ID); err != nil {
		return fmt.Errorf("replace upstream servers: %w", err)
	}
	if err = insertUpstreamServers(ctx, tx, u); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *UpstreamRepo) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM nginx_upstreams WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete upstream: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// CountReferences is the phase-2 extension point. Phase 1 has no referencing schema.
func (r *UpstreamRepo) CountReferences(context.Context, string) (int, error) { return 0, nil }

func (r *UpstreamRepo) GetAppliedState(ctx context.Context) (*NginxUpstreamAppliedState, error) {
	state := &NginxUpstreamAppliedState{}
	if err := r.db.QueryRowContext(ctx, `SELECT applied_hash, applied_path, applied_at
		FROM nginx_upstream_applied_state WHERE id = 1`).Scan(&state.AppliedHash, &state.AppliedPath, &state.AppliedAt); err != nil {
		return nil, fmt.Errorf("query upstream applied state: %w", err)
	}
	return state, nil
}

// RecordApplied atomically persists every Agent backup and the hash known to be applied.
func (r *UpstreamRepo) RecordApplied(ctx context.Context, operationID, appliedHash, appliedPath, appliedAt string, backups []*Backup) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, b := range backups {
		if _, err := tx.ExecContext(ctx, `INSERT INTO backups (
			id, operation_id, file_path, backup_path, original_sha256, backup_sha256,
			file_existed, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			b.ID, operationID, b.FilePath, b.BackupPath, b.OriginalSHA256,
			b.BackupSHA256, boolToInt(b.FileExisted), appliedAt); err != nil {
			return fmt.Errorf("record upstream backup: %w", err)
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE nginx_upstream_applied_state
		SET applied_hash = ?, applied_path = ?, applied_at = ? WHERE id = 1`, appliedHash, appliedPath, appliedAt)
	if err != nil {
		return fmt.Errorf("record upstream applied state: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("upstream applied state singleton is missing")
	}
	return tx.Commit()
}

func insertUpstreamServers(ctx context.Context, tx *sql.Tx, u *NginxUpstream) error {
	for _, s := range u.Servers {
		if _, err := tx.ExecContext(ctx, `INSERT INTO nginx_upstream_servers
			(id, upstream_id, address, weight, max_fails, fail_timeout_seconds, backup, down,
			 sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.ID, u.ID, s.Address, s.Weight, s.MaxFails, s.FailTimeoutSeconds,
			boolToInt(s.Backup), boolToInt(s.Down), s.SortOrder, s.CreatedAt, s.UpdatedAt); err != nil {
			return fmt.Errorf("create upstream server: %w", err)
		}
	}
	return nil
}
