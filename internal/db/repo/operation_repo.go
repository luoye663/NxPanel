// repo 包 — operations 表的读写
//
// operations 是操作审计表，所有写操作（创建站点、修改配置等）都记录一行。
package repo

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// OperationRepo 提供 operations 表的数据访问
type OperationRepo struct {
	db *sql.DB
}

// NewOperationRepo 创建 operation repository
func NewOperationRepo(db *sql.DB) *OperationRepo {
	return &OperationRepo{db: db}
}

// Create 创建操作记录（状态默认 pending）
func (r *OperationRepo) Create(o *Operation) error {
	return r.CreateContext(context.Background(), o)
}

func (r *OperationRepo) CreateContext(ctx context.Context, o *Operation) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO operations (
			id, action, target_type, target_id, status,
			request_id, actor, ip, user_agent,
			message, error_code, error_message, stderr,
			created_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.ID, o.Action, o.TargetType, o.TargetID, o.Status,
		o.RequestID, o.Actor, o.IP, o.UserAgent,
		o.Message, o.ErrorCode, o.ErrorMessage, o.Stderr,
		o.CreatedAt, nil,
	)
	if err != nil {
		return fmt.Errorf("创建操作记录失败: %w", err)
	}
	return nil
}

// UpdateStatus 更新操作状态
func (r *OperationRepo) UpdateStatus(id, status string) error {
	return r.UpdateStatusContext(context.Background(), id, status)
}

func (r *OperationRepo) UpdateStatusContext(ctx context.Context, id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.db.ExecContext(ctx,
		"UPDATE operations SET status = ?, finished_at = ? WHERE id = ?",
		status, now, id,
	)
	if err != nil {
		return fmt.Errorf("更新操作状态失败 id=%s: %w", id, err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return fmt.Errorf("更新操作状态失败 id=%s: operation 不存在", id)
	}
	return nil
}

// UpdateError 更新操作错误信息
func (r *OperationRepo) UpdateError(id, status, errorCode, errorMessage, stderr string) error {
	return r.UpdateErrorContext(context.Background(), id, status, errorCode, errorMessage, stderr)
}

func (r *OperationRepo) UpdateErrorContext(ctx context.Context, id, status, errorCode, errorMessage, stderr string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.db.ExecContext(ctx,
		`UPDATE operations SET status = ?, error_code = ?, error_message = ?, stderr = ?, finished_at = ?
		WHERE id = ?`,
		status, errorCode, errorMessage, stderr, now, id,
	)
	if err != nil {
		return fmt.Errorf("更新操作错误信息失败 id=%s: %w", id, err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return fmt.Errorf("更新操作错误信息失败 id=%s: operation 不存在", id)
	}
	return nil
}

// GetByID 根据 ID 获取操作记录
func (r *OperationRepo) GetByID(id string) (*Operation, error) {
	o := &Operation{}
	err := r.db.QueryRow(
		`SELECT id, action, target_type, target_id, status,
			request_id, actor, ip, user_agent,
			message, error_code, error_message, stderr,
			created_at, finished_at
		FROM operations WHERE id = ?`,
		id,
	).Scan(
		&o.ID, &o.Action, &o.TargetType, &o.TargetID, &o.Status,
		&o.RequestID, &o.Actor, &o.IP, &o.UserAgent,
		&o.Message, &o.ErrorCode, &o.ErrorMessage, &o.Stderr,
		&o.CreatedAt, &o.FinishedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询操作记录失败 id=%s: %w", id, err)
	}
	return o, nil
}

// List 分页查询操作记录
func (r *OperationRepo) List(page, pageSize int, targetType, targetID string) ([]*Operation, int, error) {
	where := "WHERE 1=1"
	args := []any{}

	if targetType != "" {
		where += " AND target_type = ?"
		args = append(args, targetType)
	}
	if targetID != "" {
		where += " AND target_id = ?"
		args = append(args, targetID)
	}

	var total int
	if err := r.db.QueryRow(
		"SELECT COUNT(*) FROM operations "+where, args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("查询操作记录总数失败: %w", err)
	}

	offset := (page - 1) * pageSize
	querySQL := `SELECT id, action, target_type, target_id, status,
		request_id, actor, ip, user_agent,
		message, error_code, error_message, stderr,
		created_at, finished_at
	FROM operations ` + where + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, pageSize, offset)

	rows, err := r.db.Query(querySQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询操作记录列表失败: %w", err)
	}
	defer rows.Close()

	var ops []*Operation
	for rows.Next() {
		o := &Operation{}
		if err := rows.Scan(
			&o.ID, &o.Action, &o.TargetType, &o.TargetID, &o.Status,
			&o.RequestID, &o.Actor, &o.IP, &o.UserAgent,
			&o.Message, &o.ErrorCode, &o.ErrorMessage, &o.Stderr,
			&o.CreatedAt, &o.FinishedAt,
		); err != nil {
			return nil, 0, err
		}
		ops = append(ops, o)
	}

	return ops, total, rows.Err()
}

func (r *OperationRepo) DeleteAll() error {
	_, err := r.db.Exec("DELETE FROM operations WHERE status <> 'pending'")
	if err != nil {
		return fmt.Errorf("清空操作记录失败: %w", err)
	}
	return nil
}
