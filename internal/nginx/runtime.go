package nginx

import "encoding/hex"

// RuntimeStatus describes the configured Nginx executable's process tree.
// RSS sums resident pages, so shared pages may be counted more than once.
type RuntimeStatus struct {
	Running             bool              `json:"running"`
	MasterPID           int               `json:"master_pid"`
	Workers             int               `json:"workers"`
	Processes           int               `json:"processes"`
	MemoryRSS           uint64            `json:"memory_rss"`
	MemoryPSS           *uint64           `json:"memory_pss"` // nil when any process lacks smaps_rollup data
	UptimeSeconds       uint64            `json:"uptime_seconds"`
	Instances           []RuntimeInstance `json:"instances"`
	BlockedReason       string            `json:"blocked_reason,omitempty"`
	OtherActionsBlocked bool              `json:"other_actions_blocked,omitempty"`
	RestartGeneration   string            `json:"restart_generation,omitempty"`
}

type RuntimeInstance struct {
	PID      int    `json:"pid"`
	ConfPath string `json:"conf_path,omitempty"`
	Match    string `json:"match"`
}

type RuntimeControlRequest struct {
	RestartGeneration string `json:"restart_generation,omitempty"`
}

func (req RuntimeControlRequest) ValidFor(action string) bool {
	if req.RestartGeneration == "" {
		return true
	}
	if action != "restart" || len(req.RestartGeneration) != 64 {
		return false
	}
	_, err := hex.DecodeString(req.RestartGeneration)
	return err == nil
}
