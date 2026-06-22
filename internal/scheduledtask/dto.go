package scheduledtask

import "encoding/json"

type CreateTaskRequest struct {
	Type              string          `json:"type"`
	Name              string          `json:"name"`
	Enabled           bool            `json:"enabled"`
	Schedule          ScheduleDTO     `json:"schedule"`
	Params            json.RawMessage `json:"params"`
	ConcurrencyPolicy string          `json:"concurrency_policy"`
	MissedPolicy      string          `json:"missed_policy"`
	TimeoutSeconds    int             `json:"timeout_seconds"`
	MaxRetries        int             `json:"max_retries"`
	RetryDelaySeconds int             `json:"retry_delay_seconds"`
}

type UpdateTaskRequest struct {
	Name              string          `json:"name"`
	Enabled           bool            `json:"enabled"`
	Schedule          ScheduleDTO     `json:"schedule"`
	Params            json.RawMessage `json:"params"`
	ConcurrencyPolicy string          `json:"concurrency_policy"`
	MissedPolicy      string          `json:"missed_policy"`
	TimeoutSeconds    int             `json:"timeout_seconds"`
	MaxRetries        int             `json:"max_retries"`
	RetryDelaySeconds int             `json:"retry_delay_seconds"`
	Version           int             `json:"version"`
}

type ToggleTaskRequest struct {
	Enabled bool `json:"enabled"`
}

type TaskListResponse struct {
	Items []TaskListItem `json:"items"`
}

type TaskDefinitionResponse struct {
	Items []TaskDefinition `json:"items"`
}

type RunListResponse struct {
	Items []RunListItem `json:"items"`
}

type TaskListItem struct {
	ID                string      `json:"id"`
	Type              string      `json:"type"`
	Name              string      `json:"name"`
	Enabled           bool        `json:"enabled"`
	System            bool        `json:"system"`
	Status            string      `json:"status"`
	Schedule          ScheduleDTO `json:"schedule"`
	Params            any         `json:"params"`
	NextRunAt         *string     `json:"next_run_at"`
	LastRunAt         *string     `json:"last_run_at"`
	LastStatus        *string     `json:"last_status"`
	LastError         string      `json:"last_error"`
	LastRunID         string      `json:"last_run_id"`
	ConcurrencyPolicy string      `json:"concurrency_policy"`
	MissedPolicy      string      `json:"missed_policy"`
	TimeoutSeconds    int         `json:"timeout_seconds"`
	MaxRetries        int         `json:"max_retries"`
	RetryDelaySeconds int         `json:"retry_delay_seconds"`
	Version           int         `json:"version"`
	Definition        *string     `json:"definition,omitempty"`
}

type RunListItem struct {
	ID             string  `json:"id"`
	TaskID         string  `json:"task_id"`
	TaskType       string  `json:"task_type"`
	TaskName       string  `json:"task_name"`
	Trigger        string  `json:"trigger"`
	Status         string  `json:"status"`
	Attempt        int     `json:"attempt"`
	StartedAt      string  `json:"started_at"`
	FinishedAt     *string `json:"finished_at"`
	DurationMillis int64   `json:"duration_ms"`
	ErrorMessage   string  `json:"error_message"`
	LogFile        string  `json:"log_file"`
	OperationID    string  `json:"operation_id"`
	RequestID      string  `json:"request_id"`
	CreatedAt      string  `json:"created_at"`
}
