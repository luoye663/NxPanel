package systemmetrics

type Snapshot struct {
	Timestamp int64             `json:"timestamp"`
	System    SystemMetrics     `json:"system"`
	Load      LoadMetrics       `json:"load"`
	CPU       CPUMetrics        `json:"cpu"`
	Memory    MemoryMetrics     `json:"memory"`
	Disks     []DiskUsage       `json:"disks"`
	Network   NetworkMetrics    `json:"network"`
	DiskIO    DiskIOMetrics     `json:"disk_io"`
	Top       TopProcessMetrics `json:"top"`
}

type SystemMetrics struct {
	Name          string  `json:"name"`
	PrettyName    string  `json:"pretty_name"`
	UptimeSeconds float64 `json:"uptime_seconds"`
}

type LoadMetrics struct {
	Load1          float64 `json:"load1"`
	Load5          float64 `json:"load5"`
	Load15         float64 `json:"load15"`
	RunningProcess int64   `json:"running_processes"`
	TotalProcess   int64   `json:"total_processes"`
	Percent        float64 `json:"percent"`
}

type CPUTimePercent struct {
	User      float64 `json:"user"`
	Nice      float64 `json:"nice"`
	System    float64 `json:"system"`
	Idle      float64 `json:"idle"`
	IOWait    float64 `json:"iowait"`
	IRQ       float64 `json:"irq"`
	SoftIRQ   float64 `json:"softirq"`
	Steal     float64 `json:"steal"`
	Guest     float64 `json:"guest"`
	GuestNice float64 `json:"guest_nice"`
}

type CPUCoreUsage struct {
	Name    string  `json:"name"`
	Percent float64 `json:"percent"`
}

type CPUInfo struct {
	Model         string `json:"model"`
	PhysicalCPUs  int    `json:"physical_cpus"`
	PhysicalCores int    `json:"physical_cores"`
	LogicalCores  int    `json:"logical_cores"`
}

type CPUMetrics struct {
	Percent float64        `json:"percent"`
	Times   CPUTimePercent `json:"times"`
	Cores   []CPUCoreUsage `json:"cores"`
	Info    CPUInfo        `json:"info"`
}

type MemoryMetrics struct {
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	FreeBytes      uint64  `json:"free_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	SharedBytes    uint64  `json:"shared_bytes"`
	BuffersBytes   uint64  `json:"buffers_bytes"`
	CachedBytes    uint64  `json:"cached_bytes"`
	Percent        float64 `json:"percent"`
}

type InodeUsage struct {
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Free    uint64  `json:"free"`
	Percent float64 `json:"percent"`
}

type DiskUsage struct {
	Device      string     `json:"device"`
	Mountpoint  string     `json:"mountpoint"`
	FSType      string     `json:"fs_type"`
	UsageKey    string     `json:"usage_key"`
	Counted     bool       `json:"counted"`
	DuplicateOf string     `json:"duplicate_of,omitempty"`
	TotalBytes  uint64     `json:"total_bytes"`
	UsedBytes   uint64     `json:"used_bytes"`
	FreeBytes   uint64     `json:"free_bytes"`
	AvailBytes  uint64     `json:"avail_bytes"`
	Percent     float64    `json:"percent"`
	Inodes      InodeUsage `json:"inodes"`
}

type NetworkInterface struct {
	Name          string  `json:"name"`
	RxBytes       uint64  `json:"rx_bytes"`
	TxBytes       uint64  `json:"tx_bytes"`
	RxBytesPerSec float64 `json:"rx_bytes_per_sec"`
	TxBytesPerSec float64 `json:"tx_bytes_per_sec"`
}

type NetworkMetrics struct {
	Interfaces []NetworkInterface `json:"interfaces"`
	Total      NetworkInterface   `json:"total"`
}

type DiskIODevice struct {
	Name             string  `json:"name"`
	ReadBytes        uint64  `json:"read_bytes"`
	WriteBytes       uint64  `json:"write_bytes"`
	ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
	WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
	IOPS             float64 `json:"iops"`
	LatencyMs        float64 `json:"latency_ms"`
}

type DiskIOMetrics struct {
	Devices []DiskIODevice `json:"devices"`
	Total   DiskIODevice   `json:"total"`
}

type ProcessInfo struct {
	PID        int     `json:"pid"`
	Name       string  `json:"name"`
	Command    string  `json:"command"`
	CPUPercent float64 `json:"cpu_percent"`
	MemPercent float64 `json:"mem_percent"`
	RSSBytes   uint64  `json:"rss_bytes"`
}

type TopProcessMetrics struct {
	CPU    []ProcessInfo `json:"cpu"`
	Memory []ProcessInfo `json:"memory"`
}
