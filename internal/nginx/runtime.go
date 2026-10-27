package nginx

// RuntimeStatus describes the configured Nginx executable's process tree.
// RSS sums resident pages, so shared pages may be counted more than once.
type RuntimeStatus struct {
	Running       bool    `json:"running"`
	MasterPID     int     `json:"master_pid"`
	Workers       int     `json:"workers"`
	Processes     int     `json:"processes"`
	MemoryRSS     uint64  `json:"memory_rss"`
	MemoryPSS     *uint64 `json:"memory_pss"` // nil when any process lacks smaps_rollup data
	UptimeSeconds uint64  `json:"uptime_seconds"`
}
