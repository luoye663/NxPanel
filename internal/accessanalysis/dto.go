package accessanalysis

type LogFormat string

const (
	FormatCommon      LogFormat = "common"
	FormatCombined    LogFormat = "combined"
	FormatNxpanelJSON LogFormat = "nxpanel_json"
	FormatCustom      LogFormat = "custom"
)

type TimeRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type ScanRequest struct {
	Range          string `json:"range"`
	From           string `json:"from"`
	To             string `json:"to"`
	IncludeRotated *bool  `json:"include_rotated,omitempty"`
}

type ScanResponse struct {
	JobID        string `json:"job_id"`
	Status       string `json:"status"`
	ScannedLines int64  `json:"scanned_lines"`
	SkippedLines int64  `json:"skipped_lines"`
	Truncated    bool   `json:"truncated"`
	DurationMS   int64  `json:"duration_ms"`
}

type Settings struct {
	SiteID               string `json:"site_id"`
	Enabled              bool   `json:"enabled"`
	ScanTime             string `json:"scan_time"`
	RetentionDays        int    `json:"retention_days"`
	IncludeRotated       bool   `json:"include_rotated"`
	LogFormat            string `json:"log_format"`
	CustomPattern        string `json:"custom_pattern"`
	NormalizeQuery       bool   `json:"normalize_query"`
	SaveEntries          bool   `json:"save_entries"`
	EntriesRetentionDays int    `json:"entries_retention_days"`
	MaxEntries           int    `json:"max_entries"`
	PathTopN             int    `json:"path_top_n"`
	IPTopN               int    `json:"ip_top_n"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

type Summary struct {
	TodayRequests int64  `json:"today_requests"`
	UniqueIPs     int64  `json:"unique_ips"`
	Status4xx     int64  `json:"status_4xx"`
	Status5xx     int64  `json:"status_5xx"`
	Bytes         int64  `json:"bytes"`
	TopPath       string `json:"top_path"`
	LastScanAt    string `json:"last_scan_at"`
	LastJobStatus string `json:"last_job_status"`
	LastError     string `json:"last_error"`
}

type SummaryResponse struct {
	Summary Summary       `json:"summary"`
	Trend   []HourlyPoint `json:"trend"`
}

type HourlyPoint struct {
	Hour      string `json:"hour"`
	Requests  int64  `json:"requests"`
	UniqueIPs int64  `json:"unique_ips"`
	Status4xx int64  `json:"status_4xx"`
	Status5xx int64  `json:"status_5xx"`
	Bytes     int64  `json:"bytes"`
}

type PathStat struct {
	Date       string `json:"date"`
	Path       string `json:"path"`
	Requests   int64  `json:"requests"`
	UniqueIPs  int64  `json:"unique_ips"`
	Status2xx  int64  `json:"status_2xx"`
	Status3xx  int64  `json:"status_3xx"`
	Status4xx  int64  `json:"status_4xx"`
	Status5xx  int64  `json:"status_5xx"`
	Bytes      int64  `json:"bytes"`
	LastSeenAt string `json:"last_seen_at"`
}

type IPStat struct {
	Date            string `json:"date"`
	IP              string `json:"ip"`
	Requests        int64  `json:"requests"`
	UniquePaths     int64  `json:"unique_paths"`
	ErrorRequests   int64  `json:"error_requests"`
	Bytes           int64  `json:"bytes"`
	FirstSeenAt     string `json:"first_seen_at"`
	LastSeenAt      string `json:"last_seen_at"`
	SampleUserAgent string `json:"sample_user_agent"`
}

type Entry struct {
	ID            int64  `json:"id,omitempty"`
	TS            string `json:"ts"`
	IP            string `json:"ip"`
	Method        string `json:"method"`
	Path          string `json:"path"`
	RawPath       string `json:"raw_path"`
	Status        int    `json:"status"`
	Bytes         int64  `json:"bytes"`
	Referer       string `json:"referer"`
	UserAgent     string `json:"user_agent"`
	IsAnomaly     bool   `json:"is_anomaly"`
	AnomalyReason string `json:"anomaly_reason"`
}

type Anomaly struct {
	ID          int64  `json:"id,omitempty"`
	Date        string `json:"date"`
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	Requests    int64  `json:"requests"`
	Severity    string `json:"severity"`
	Reason      string `json:"reason"`
	FirstSeenAt string `json:"first_seen_at"`
	LastSeenAt  string `json:"last_seen_at"`
}

type Job struct {
	ID           string `json:"id"`
	SiteID       string `json:"site_id"`
	Trigger      string `json:"trigger"`
	RangeStart   string `json:"range_start"`
	RangeEnd     string `json:"range_end"`
	Status       string `json:"status"`
	ScannedLines int64  `json:"scanned_lines"`
	SkippedLines int64  `json:"skipped_lines"`
	DurationMS   int64  `json:"duration_ms"`
	ErrorMessage string `json:"error_message"`
	CreatedAt    string `json:"created_at"`
	FinishedAt   string `json:"finished_at"`
}

type Page[T any] struct {
	Items    []T   `json:"items"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
}

type Query struct {
	From     string
	To       string
	IP       string
	Path     string
	Method   string
	Status   int
	Sort     string
	Page     int
	PageSize int
}

type FormatDetectRequest struct {
	Sample string `json:"sample"`
}

type FormatDetectResponse struct {
	Format          string   `json:"format"`
	Parseable       bool     `json:"parseable"`
	FailureRate     float64  `json:"failure_rate"`
	Samples         []Entry  `json:"samples"`
	RecommendedConf string   `json:"recommended_conf"`
	Errors          []string `json:"errors"`
}

type FormatTestRequest struct {
	Pattern string `json:"pattern"`
	Sample  string `json:"sample"`
}

type OptimizeResponse struct {
	RecommendedConf string `json:"recommended_conf"`
	OperationID     string `json:"operation_id"`
}

type AgentFormatDetectRequest struct {
	Path     string `json:"path"`
	MaxLines int    `json:"max_lines"`
}

type Cursor struct {
	Inode    uint64 `json:"inode"`
	Offset   int64  `json:"offset"`
	FileSize int64  `json:"file_size"`
}

type AgentScanRequest struct {
	Path           string `json:"path"`
	FromTime       string `json:"from_time"`
	ToTime         string `json:"to_time"`
	MaxBytes       int64  `json:"max_bytes"`
	MaxLines       int64  `json:"max_lines"`
	Format         string `json:"format"`
	CustomPattern  string `json:"custom_pattern"`
	Cursor         Cursor `json:"cursor"`
	IncludeRotated bool   `json:"include_rotated"`
	NormalizeQuery bool   `json:"normalize_query"`
}

type AgentScanResponse struct {
	Cursor        Cursor        `json:"cursor"`
	ScannedLines  int64         `json:"scanned_lines"`
	SkippedLines  int64         `json:"skipped_lines"`
	Truncated     bool          `json:"truncated"`
	Hourly        []HourlyPoint `json:"hourly"`
	Paths         []PathStat    `json:"paths"`
	IPs           []IPStat      `json:"ips"`
	EntriesSample []Entry       `json:"entries_sample"`
	Anomalies     []Anomaly     `json:"anomalies"`
	ParseErrors   []string      `json:"parse_errors"`
}
