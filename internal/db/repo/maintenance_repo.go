package repo

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrMaintenanceConflict = errors.New("maintenance state changed; refresh before retrying")

type MaintenanceState struct {
	SiteID         string  `json:"site_id"`
	Enabled        bool    `json:"maintenance_enabled"`
	DesiredEnabled bool    `json:"desired_enabled"`
	HTMLOverride   *string `json:"html_override"`
	Version        int64   `json:"version"`
	ApplyStatus    string  `json:"apply_status"`
	OperationID    string  `json:"operation_id"`
}
type MaintenanceDefaults struct {
	HTML    *string `json:"html"`
	Version int64   `json:"version"`
}
type MaintenanceRepo struct{ db *sql.DB }

func NewMaintenanceRepo(db *sql.DB) *MaintenanceRepo { return &MaintenanceRepo{db: db} }

func (r *MaintenanceRepo) Get(ctx context.Context, id string) (*MaintenanceState, error) {
	s := &MaintenanceState{SiteID: id}
	err := r.db.QueryRowContext(ctx, `SELECT s.maintenance_enabled,COALESCE(m.desired_enabled,0),m.html_override,
	COALESCE(m.version,0),COALESCE(m.apply_status,'synced'),COALESCE(m.operation_id,'') FROM sites s
	LEFT JOIN site_maintenance m ON m.site_id=s.id WHERE s.id=?`, id).Scan(&s.Enabled, &s.DesiredEnabled, &s.HTMLOverride, &s.Version, &s.ApplyStatus, &s.OperationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}
func (r *MaintenanceRepo) Defaults(ctx context.Context) (*MaintenanceDefaults, error) {
	d := &MaintenanceDefaults{}
	err := r.db.QueryRowContext(ctx, `SELECT html,version FROM maintenance_defaults WHERE id=1`).Scan(&d.HTML, &d.Version)
	return d, err
}

func insertMaintenanceOperation(ctx context.Context, tx *sql.Tx, op *Operation) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO operations(id,action,target_type,target_id,status,request_id,actor,message,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, op.ID, op.Action, op.TargetType, op.TargetID, "pending", op.RequestID, "admin", op.Message, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (r *MaintenanceRepo) Begin(ctx context.Context, id string, enabled bool, html *string, version int64, op *Operation) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE operations SET status='failed',finished_at=CURRENT_TIMESTAMP,error_message='已由后续显式维护对账取代' WHERE status='pending' AND id=(SELECT operation_id FROM site_maintenance WHERE site_id=?)`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO site_maintenance(site_id) VALUES(?)`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE site_maintenance SET desired_enabled=?,html_override=?,version=version+1,apply_status='pending',operation_id=? WHERE site_id=? AND version=?`, enabled, html, op.ID, id, version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrMaintenanceConflict
	}
	if err = insertMaintenanceOperation(ctx, tx, op); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *MaintenanceRepo) Confirm(ctx context.Context, id string, version int64, enabled bool, opID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE site_maintenance SET apply_status='synced' WHERE site_id=? AND version=? AND operation_id=?`, id, version, opID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrMaintenanceConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sites SET maintenance_enabled=? WHERE id=?`, enabled, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE operations SET status='success',finished_at=CURRENT_TIMESTAMP WHERE id=?`, opID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *MaintenanceRepo) SetDefaults(ctx context.Context, html *string, version int64, op *Operation) ([]string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE maintenance_defaults SET html=?,version=version+1 WHERE id=1 AND version=?`, html, version)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, ErrMaintenanceConflict
	}
	if err = insertMaintenanceOperation(ctx, tx, op); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE site_maintenance SET apply_status='pending',version=version+1 WHERE html_override IS NULL AND desired_enabled=1`); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT site_id FROM site_maintenance WHERE html_override IS NULL AND desired_enabled=1`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return ids, tx.Commit()
}

func (r *MaintenanceRepo) Blocked(ctx context.Context, id string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sites s LEFT JOIN site_maintenance m ON m.site_id=s.id WHERE (s.maintenance_enabled=1 OR m.apply_status='pending') AND (?='' OR s.id=?)`, id, id).Scan(&n)
	return n > 0, err
}
