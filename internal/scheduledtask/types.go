package scheduledtask

import (
	"encoding/json"
	"time"
)

const (
	TaskStatusIdle     = "idle"
	TaskStatusRunning  = "running"
	TaskStatusDisabled = "disabled"
	TaskStatusError    = "error"

	RunStatusRunning   = "running"
	RunStatusSuccess   = "success"
	RunStatusFailed    = "failed"
	RunStatusTimeout   = "timeout"
	RunStatusCancelled = "cancelled"
	RunStatusSkipped   = "skipped"
	RunStatusAbandoned = "abandoned"

	TriggerSchedule  = "schedule"
	TriggerManual    = "manual"
	TriggerRetry     = "retry"
	TriggerMigration = "migration"
	TriggerSystem    = "system"

	ScheduleDaily    = "daily"
	ScheduleWeekly   = "weekly"
	ScheduleMonthly  = "monthly"
	ScheduleInterval = "interval"
	ScheduleCron     = "cron"

	ConcurrencySkip = "skip"
	MissedRunOnce   = "run_once"
	MissedSkip      = "skip"
)

// Task 是 scheduled_tasks 表的领域模型，时间字段统一用 UTC time.Time，避免业务层反复解析字符串。
type Task struct {
	ID                 string
	Type               string
	Name               string
	Enabled            bool
	System             bool
	SourceType         string
	SourceID           string
	Status             string
	ScheduleKind       string
	ScheduleExpr       string
	Timezone           string
	ParamsJSON         json.RawMessage
	ConcurrencyPolicy  string
	MissedPolicy       string
	TimeoutSeconds     int
	MaxRetries         int
	RetryDelaySeconds  int
	NextRunAt          *time.Time
	LastRunAt          *time.Time
	LastFinishedAt     *time.Time
	LastStatus         *string
	LastError          string
	LastDurationMillis int64
	LastRunID          string
	LockedBy           string
	LockedUntil        *time.Time
	Version            int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Run 是 scheduled_task_runs 表的执行记录，手动执行和定时执行共用同一套记录。
type Run struct {
	ID             string
	TaskID         string
	TaskType       string
	TaskName       string
	Trigger        string
	Status         string
	Attempt        int
	StartedAt      time.Time
	FinishedAt     *time.Time
	DurationMillis int64
	ErrorMessage   string
	LogFile        string
	OperationID    string
	RequestID      string
	CreatedAt      time.Time
}

// RunContext 传给具体任务 handler，后续写日志、审计和 request id 都从这里扩展。
type RunContext struct {
	RunID   string
	Trigger string
	Attempt int
}

type ScheduleDTO struct {
	Kind     string `json:"kind"`
	Expr     string `json:"expr"`
	Timezone string `json:"timezone"`
}

type TaskDefinition struct {
	Type              string         `json:"type"`
	Label             string         `json:"label"`
	Description       string         `json:"description"`
	System            bool           `json:"system"`
	SupportsManualRun bool           `json:"supports_manual_run"`
	DefaultSchedule   ScheduleDTO    `json:"default_schedule"`
	ParamSchema       map[string]any `json:"param_schema,omitempty"`
}
