package systemmetrics

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Collector struct {
	// prev* 保存上一轮采样值，用于计算“每秒速率”或“使用率”。
	// Linux /proc 中很多值是开机以来的累计值，必须用两次采样差值才有实时意义。
	prevCPU  map[string]cpuStat
	prevNet  map[string]netStat
	prevDisk map[string]diskStat
	prevProc map[int]procStat
	prevTime time.Time
	cpuInfo  CPUInfo
	pageSize uint64
}

type CollectOptions struct {
	// IncludeTop 为 false 时不扫描 /proc/[pid]，用于常驻仪表盘轻量采集。
	IncludeTop bool
}

type cpuStat struct{ vals [10]uint64 }
type netStat struct{ rx, tx uint64 }
type diskStat struct {
	readSectors, writeSectors uint64
	reads, writes             uint64
	ioTimeMs                  uint64
}
type procStat struct{ ticks uint64 }

func NewCollector() *Collector {
	return &Collector{
		prevCPU:  make(map[string]cpuStat),
		prevNet:  make(map[string]netStat),
		prevDisk: make(map[string]diskStat),
		prevProc: make(map[int]procStat),
		cpuInfo:  readCPUInfo(),
		pageSize: uint64(os.Getpagesize()),
	}
}

func (c *Collector) Collect(opts CollectOptions) (Snapshot, error) {
	now := time.Now()
	delta := now.Sub(c.prevTime).Seconds()
	if delta <= 0 {
		delta = 1
	}

	mem := readMemory()
	cpu, logicalCores := c.readCPU()
	if c.cpuInfo.LogicalCores == 0 && logicalCores > 0 {
		c.cpuInfo.LogicalCores = logicalCores
	}
	cpu.Info = c.cpuInfo

	load := readLoad()
	cores := cpu.Info.LogicalCores
	if cores <= 0 {
		cores = 1
	}
	// 负载百分比按 1 分钟负载 / 逻辑核心数估算，便于用圆形进度展示。
	load.Percent = clamp(load.Load1/float64(cores)*100, 0, 100)

	net := c.readNetwork(delta)
	diskIO := c.readDiskIO(delta)
	top := TopProcessMetrics{}
	if opts.IncludeTop {
		// 进程 top5 只在弹窗详情需要时计算，避免每次都遍历所有进程。
		top = c.readTopProcesses(delta, mem.TotalBytes)
	}

	c.prevTime = now
	return Snapshot{
		Timestamp: now.Unix(),
		System:    readSystemMetrics(),
		Load:      load,
		CPU:       cpu,
		Memory:    mem,
		Disks:     readDiskUsage(),
		Network:   net,
		DiskIO:    diskIO,
		Top:       top,
	}, nil
}

func readSystemMetrics() SystemMetrics {
	name, pretty := readOSRelease()
	uptime := 0.0
	data, err := os.ReadFile("/proc/uptime")
	if err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			uptime, _ = strconv.ParseFloat(fields[0], 64)
		}
	}
	return SystemMetrics{Name: name, PrettyName: pretty, UptimeSeconds: uptime}
}

func readOSRelease() (string, string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", ""
	}
	values := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := line[:idx]
		val := strings.Trim(line[idx+1:], `"`)
		values[key] = val
	}
	name := values["PRETTY_NAME"]
	if name == "" {
		name = strings.TrimSpace(values["NAME"] + " " + values["VERSION_ID"])
	}
	pretty := values["PRETTY_NAME"]
	return name, pretty
}

func readLoad() LoadMetrics {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return LoadMetrics{}
	}
	fields := strings.Fields(string(data))
	if len(fields) < 4 {
		return LoadMetrics{}
	}
	load1, _ := strconv.ParseFloat(fields[0], 64)
	load5, _ := strconv.ParseFloat(fields[1], 64)
	load15, _ := strconv.ParseFloat(fields[2], 64)
	running, total := int64(0), int64(0)
	parts := strings.Split(fields[3], "/")
	if len(parts) == 2 {
		running, _ = strconv.ParseInt(parts[0], 10, 64)
		total, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	return LoadMetrics{Load1: load1, Load5: load5, Load15: load15, RunningProcess: running, TotalProcess: total}
}

func (c *Collector) readCPU() (CPUMetrics, int) {
	stats := readCPUStats()
	logical := 0
	var result CPUMetrics
	for name, stat := range stats {
		prev, ok := c.prevCPU[name]
		if name != "cpu" {
			logical++
		}
		if !ok {
			continue
		}
		totalDelta := uint64(0)
		for i := range stat.vals {
			if stat.vals[i] >= prev.vals[i] {
				totalDelta += stat.vals[i] - prev.vals[i]
			}
		}
		if totalDelta == 0 {
			continue
		}
		// CPU 使用率 = 非空闲 tick 差值 / 总 tick 差值。
		// stat.vals[3] 是 idle，stat.vals[4] 是 iowait，二者都视为空闲时间。
		idleDelta := deltaAt(stat.vals[3], prev.vals[3]) + deltaAt(stat.vals[4], prev.vals[4])
		usage := clamp(float64(totalDelta-idleDelta)/float64(totalDelta)*100, 0, 100)
		if name == "cpu" {
			result.Percent = usage
			result.Times = CPUTimePercent{
				User:      pct(deltaAt(stat.vals[0], prev.vals[0]), totalDelta),
				Nice:      pct(deltaAt(stat.vals[1], prev.vals[1]), totalDelta),
				System:    pct(deltaAt(stat.vals[2], prev.vals[2]), totalDelta),
				Idle:      pct(deltaAt(stat.vals[3], prev.vals[3]), totalDelta),
				IOWait:    pct(deltaAt(stat.vals[4], prev.vals[4]), totalDelta),
				IRQ:       pct(deltaAt(stat.vals[5], prev.vals[5]), totalDelta),
				SoftIRQ:   pct(deltaAt(stat.vals[6], prev.vals[6]), totalDelta),
				Steal:     pct(deltaAt(stat.vals[7], prev.vals[7]), totalDelta),
				Guest:     pct(deltaAt(stat.vals[8], prev.vals[8]), totalDelta),
				GuestNice: pct(deltaAt(stat.vals[9], prev.vals[9]), totalDelta),
			}
		} else {
			result.Cores = append(result.Cores, CPUCoreUsage{Name: strings.TrimPrefix(name, "cpu"), Percent: usage})
		}
	}
	c.prevCPU = stats
	sort.Slice(result.Cores, func(i, j int) bool { return naturalLess(result.Cores[i].Name, result.Cores[j].Name) })
	return result, logical
}

func readCPUStats() map[string]cpuStat {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil
	}
	stats := make(map[string]cpuStat)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		var stat cpuStat
		for i := 1; i < len(fields) && i <= 10; i++ {
			stat.vals[i-1], _ = strconv.ParseUint(fields[i], 10, 64)
		}
		stats[fields[0]] = stat
	}
	return stats
}

func readMemory() MemoryMetrics {
	values := readMeminfo()
	total := values["MemTotal"]
	free := values["MemFree"]
	available := values["MemAvailable"]
	buffers := values["Buffers"]
	cached := values["Cached"] + values["SReclaimable"]
	shared := values["Shmem"]
	used := uint64(0)
	if total > available {
		used = total - available
	}
	percent := 0.0
	if total > 0 {
		percent = float64(used) / float64(total) * 100
	}
	return MemoryMetrics{TotalBytes: total, UsedBytes: used, FreeBytes: free, AvailableBytes: available, SharedBytes: shared, BuffersBytes: buffers, CachedBytes: cached, Percent: percent}
}

func readMeminfo() map[string]uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return map[string]uint64{}
	}
	values := make(map[string]uint64)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		val, _ := strconv.ParseUint(fields[1], 10, 64)
		values[key] = val * 1024
	}
	return values
}

func readDiskUsage() []DiskUsage {
	mounts := readMounts()
	disks := make([]DiskUsage, 0, len(mounts))
	seen := make(map[string]bool)
	for _, m := range mounts {
		// 过滤 proc/tmpfs/overlay 等虚拟文件系统，只保留真实磁盘挂载点用于容量统计。
		if !isRealFS(m.fsType, m.device) || seen[m.mountpoint] {
			continue
		}
		seen[m.mountpoint] = true
		var st syscall.Statfs_t
		if err := syscall.Statfs(m.mountpoint, &st); err != nil || st.Blocks == 0 {
			continue
		}
		block := uint64(st.Bsize)
		total := st.Blocks * block
		free := st.Bfree * block
		avail := st.Bavail * block
		used := uint64(0)
		if total > free {
			used = total - free
		}
		inodeTotal := st.Files
		inodeFree := st.Ffree
		inodeUsed := uint64(0)
		if inodeTotal > inodeFree {
			inodeUsed = inodeTotal - inodeFree
		}
		disks = append(disks, DiskUsage{
			Device: m.device, Mountpoint: m.mountpoint, FSType: m.fsType, UsageKey: m.usageKey(),
			TotalBytes: total, UsedBytes: used, FreeBytes: free, AvailBytes: avail,
			Percent: pct(used, total),
			Inodes:  InodeUsage{Total: inodeTotal, Used: inodeUsed, Free: inodeFree, Percent: pct(inodeUsed, inodeTotal)},
		})
	}
	markCountedDisks(disks)
	sort.Slice(disks, func(i, j int) bool { return disks[i].Mountpoint < disks[j].Mountpoint })
	return disks
}

type mountInfo struct{ device, mountpoint, fsType, majorMinor string }

func (m mountInfo) usageKey() string {
	if m.majorMinor != "" {
		return m.majorMinor
	}
	return m.fsType + ":" + m.device
}

func readMounts() []mountInfo {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	mounts := []mountInfo{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		// mountinfo 中 " - " 之后是 fs_type 和 source，比 /proc/mounts 更适合识别挂载来源。
		sep := strings.Index(line, " - ")
		if sep < 0 {
			continue
		}
		left := strings.Fields(line[:sep])
		right := strings.Fields(line[sep+3:])
		if len(left) < 5 || len(right) < 3 {
			continue
		}
		mounts = append(mounts, mountInfo{majorMinor: left[2], mountpoint: unescapeMount(left[4]), fsType: right[0], device: right[1]})
	}
	return mounts
}

func markCountedDisks(disks []DiskUsage) {
	bestByKey := make(map[string]int)
	for i := range disks {
		key := disks[i].UsageKey
		if key == "" {
			key = disks[i].Mountpoint
			disks[i].UsageKey = key
		}
		best, exists := bestByKey[key]
		if !exists || preferMountpoint(disks[i].Mountpoint, disks[best].Mountpoint) {
			bestByKey[key] = i
		}
	}

	for _, best := range bestByKey {
		representative := disks[best].Mountpoint
		for i := range disks {
			if disks[i].UsageKey != disks[best].UsageKey {
				continue
			}
			disks[i].Counted = i == best
			if i != best {
				disks[i].DuplicateOf = representative
			}
		}
	}
}

func preferMountpoint(candidate, current string) bool {
	if candidate == "/" || current == "" {
		return true
	}
	if current == "/" {
		return false
	}
	candidateDepth := strings.Count(strings.Trim(candidate, "/"), "/")
	currentDepth := strings.Count(strings.Trim(current, "/"), "/")
	if candidateDepth != currentDepth {
		return candidateDepth < currentDepth
	}
	if len(candidate) != len(current) {
		return len(candidate) < len(current)
	}
	return candidate < current
}

func (c *Collector) readNetwork(delta float64) NetworkMetrics {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return NetworkMetrics{}
	}
	current := make(map[string]netStat)
	metrics := NetworkMetrics{Interfaces: []NetworkInterface{}}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		if name == "lo" {
			continue
		}
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 16 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		cur := netStat{rx: rx, tx: tx}
		current[name] = cur
		prev, hasPrev := c.prevNet[name]
		item := NetworkInterface{Name: name, RxBytes: rx, TxBytes: tx}
		if hasPrev {
			// /proc/net/dev 是累计字节数；两次差值除以采样间隔得到实时速率。
			item.RxBytesPerSec = float64(deltaAt(rx, prev.rx)) / delta
			item.TxBytesPerSec = float64(deltaAt(tx, prev.tx)) / delta
		}
		metrics.Interfaces = append(metrics.Interfaces, item)
		metrics.Total.RxBytes += rx
		metrics.Total.TxBytes += tx
		metrics.Total.RxBytesPerSec += item.RxBytesPerSec
		metrics.Total.TxBytesPerSec += item.TxBytesPerSec
	}
	metrics.Total.Name = "全部"
	c.prevNet = current
	return metrics
}

func (c *Collector) readDiskIO(delta float64) DiskIOMetrics {
	data, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return DiskIOMetrics{}
	}
	current := make(map[string]diskStat)
	metrics := DiskIOMetrics{Devices: []DiskIODevice{}}
	totalOpsDelta := uint64(0)
	totalIOTimeDelta := uint64(0)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 14 || !isRealBlockDevice(fields[2]) {
			continue
		}
		name := fields[2]
		reads, _ := strconv.ParseUint(fields[3], 10, 64)
		readSectors, _ := strconv.ParseUint(fields[5], 10, 64)
		writes, _ := strconv.ParseUint(fields[7], 10, 64)
		writeSectors, _ := strconv.ParseUint(fields[9], 10, 64)
		ioTime, _ := strconv.ParseUint(fields[12], 10, 64)
		cur := diskStat{readSectors: readSectors, writeSectors: writeSectors, reads: reads, writes: writes, ioTimeMs: ioTime}
		current[name] = cur
		prev, hasPrev := c.prevDisk[name]
		readBytesDelta, writeBytesDelta, opsDelta, ioTimeDelta := uint64(0), uint64(0), uint64(0), uint64(0)
		if hasPrev {
			// diskstats 的 sector 默认按 512 字节换算，差值 / 时间得到读写速率。
			readBytesDelta = deltaAt(readSectors, prev.readSectors) * 512
			writeBytesDelta = deltaAt(writeSectors, prev.writeSectors) * 512
			opsDelta = deltaAt(reads, prev.reads) + deltaAt(writes, prev.writes)
			ioTimeDelta = deltaAt(ioTime, prev.ioTimeMs)
		}
		latency := 0.0
		if opsDelta > 0 {
			// 用 IO 时间差 / 操作数差粗略估算平均 IO 延迟，适合作为仪表盘趋势参考。
			latency = float64(ioTimeDelta) / float64(opsDelta)
		}
		totalOpsDelta += opsDelta
		totalIOTimeDelta += ioTimeDelta
		item := DiskIODevice{Name: name, ReadBytes: readSectors * 512, WriteBytes: writeSectors * 512, ReadBytesPerSec: float64(readBytesDelta) / delta, WriteBytesPerSec: float64(writeBytesDelta) / delta, IOPS: float64(opsDelta) / delta, LatencyMs: latency}
		metrics.Devices = append(metrics.Devices, item)
		metrics.Total.ReadBytes += item.ReadBytes
		metrics.Total.WriteBytes += item.WriteBytes
		metrics.Total.ReadBytesPerSec += item.ReadBytesPerSec
		metrics.Total.WriteBytesPerSec += item.WriteBytesPerSec
		metrics.Total.IOPS += item.IOPS
	}
	metrics.Total.Name = "全部"
	if totalOpsDelta > 0 {
		metrics.Total.LatencyMs = float64(totalIOTimeDelta) / float64(totalOpsDelta)
	}
	c.prevDisk = current
	return metrics
}

func (c *Collector) readTopProcesses(delta float64, totalMem uint64) TopProcessMetrics {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return TopProcessMetrics{}
	}
	current := make(map[int]procStat)
	items := make([]ProcessInfo, 0, 64)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		p, ok := c.readProcess(pid, totalMem)
		if !ok {
			continue
		}
		ticks := uint64(p.CPUPercent)
		current[pid] = procStat{ticks: ticks}
		prev, hasPrev := c.prevProc[pid]
		p.CPUPercent = 0
		if hasPrev {
			// 进程 CPU 使用率同样基于 utime+stime 的 tick 差值计算。
			p.CPUPercent = float64(deltaAt(ticks, prev.ticks)) / ticksPerSecond() / delta * 100
		}
		items = append(items, p)
	}
	c.prevProc = current
	cpuTop := append([]ProcessInfo(nil), items...)
	memTop := append([]ProcessInfo(nil), items...)
	sort.Slice(cpuTop, func(i, j int) bool { return cpuTop[i].CPUPercent > cpuTop[j].CPUPercent })
	sort.Slice(memTop, func(i, j int) bool { return memTop[i].RSSBytes > memTop[j].RSSBytes })
	return TopProcessMetrics{CPU: topN(cpuTop, 5), Memory: topN(memTop, 5)}
}

func (c *Collector) readProcess(pid int, totalMem uint64) (ProcessInfo, bool) {
	statData, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ProcessInfo{}, false
	}
	line := string(statData)
	end := strings.LastIndex(line, ")")
	start := strings.Index(line, "(")
	if start < 0 || end < start {
		return ProcessInfo{}, false
	}
	name := line[start+1 : end]
	fields := strings.Fields(line[end+2:])
	if len(fields) < 22 {
		return ProcessInfo{}, false
	}
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	rssPages, _ := strconv.ParseInt(fields[21], 10, 64)
	rss := uint64(0)
	if rssPages > 0 {
		rss = uint64(rssPages) * c.pageSize
	}
	cmd := readCmdline(pid)
	if cmd == "" {
		cmd = name
	}
	memPercent := 0.0
	if totalMem > 0 {
		memPercent = float64(rss) / float64(totalMem) * 100
	}
	return ProcessInfo{PID: pid, Name: name, Command: cmd, CPUPercent: float64(utime + stime), MemPercent: memPercent, RSSBytes: rss}, true
}

func readCmdline(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(data) == 0 {
		return ""
	}
	data = bytes.ReplaceAll(data, []byte{0}, []byte{' '})
	return strings.TrimSpace(string(data))
}

func readCPUInfo() CPUInfo {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return CPUInfo{}
	}
	info := CPUInfo{}
	physicalIDs := map[string]bool{}
	coreIDs := map[string]bool{}
	currentPhysical := "0"
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "processor":
			info.LogicalCores++
		case "model name":
			if info.Model == "" {
				info.Model = val
			}
		case "physical id":
			currentPhysical = val
			physicalIDs[val] = true
		case "core id":
			coreIDs[currentPhysical+":"+val] = true
		}
	}
	info.PhysicalCPUs = len(physicalIDs)
	info.PhysicalCores = len(coreIDs)
	if info.PhysicalCPUs == 0 && info.LogicalCores > 0 {
		info.PhysicalCPUs = 1
	}
	if info.PhysicalCores == 0 {
		info.PhysicalCores = info.LogicalCores
	}
	return info
}

func topN(items []ProcessInfo, n int) []ProcessInfo {
	if len(items) > n {
		items = items[:n]
	}
	return items
}

func deltaAt(cur, prev uint64) uint64 {
	if cur < prev {
		return 0
	}
	return cur - prev
}

func pct(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func ticksPerSecond() float64 { return 100 }

func isRealFS(fsType, device string) bool {
	if device == "" || strings.HasPrefix(device, "none") {
		return false
	}
	blocked := map[string]bool{
		"proc": true, "sysfs": true, "devtmpfs": true, "tmpfs": true, "devpts": true,
		"cgroup": true, "cgroup2": true, "pstore": true, "bpf": true, "securityfs": true,
		"debugfs": true, "tracefs": true, "configfs": true, "fusectl": true, "mqueue": true,
		"hugetlbfs": true, "rpc_pipefs": true, "overlay": true, "squashfs": true, "autofs": true,
	}
	return !blocked[fsType]
}

func isRealBlockDevice(name string) bool {
	if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "fd") {
		return false
	}
	if strings.HasPrefix(name, "dm-") {
		return true
	}
	if len(name) > 0 && name[len(name)-1] >= '0' && name[len(name)-1] <= '9' {
		if strings.HasPrefix(name, "nvme") || strings.HasPrefix(name, "mmcblk") {
			return !strings.Contains(name, "p")
		}
		return false
	}
	return true
}

func unescapeMount(s string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(s)
}

func naturalLess(a, b string) bool {
	ai, ae := strconv.Atoi(a)
	bi, be := strconv.Atoi(b)
	if ae == nil && be == nil {
		return ai < bi
	}
	return a < b
}
