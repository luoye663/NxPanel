package systemmetrics

import "strings"

type Scope struct {
	raw          string
	LoadDetail   bool
	CPUDetail    bool
	MemoryDetail bool
	DiskDetail   bool
}

// ParseScope 将 URL 查询参数 scope 解析成布尔标记。
// 例如：summary,network,disk_io,cpu_detail 会开启 CPU 详情下发。
func ParseScope(raw string) Scope {
	scope := Scope{raw: raw}
	for _, part := range strings.Split(raw, ",") {
		switch strings.TrimSpace(part) {
		case "load_detail":
			scope.LoadDetail = true
		case "cpu_detail":
			scope.CPUDetail = true
		case "memory_detail":
			scope.MemoryDetail = true
		case "disk_detail":
			scope.DiskDetail = true
		}
	}
	return scope
}

// FilterSnapshot 根据订阅 scope 裁剪快照，避免常驻仪表盘接收弹窗专用的大字段。
// 注意：这里返回的是 Snapshot 值拷贝，清空切片/结构体不会影响 Service 缓存的原始 last。
func FilterSnapshot(snapshot Snapshot, rawScope string) Snapshot {
	scope := ParseScope(rawScope)
	if !scope.LoadDetail {
		snapshot.CPU.Times = CPUTimePercent{}
	}
	if !scope.CPUDetail {
		snapshot.CPU.Cores = nil
	}
	if !scope.LoadDetail && !scope.CPUDetail && !scope.MemoryDetail {
		snapshot.Top = TopProcessMetrics{}
	} else {
		if !scope.MemoryDetail {
			snapshot.Top.Memory = nil
		}
		if !scope.LoadDetail && !scope.CPUDetail {
			snapshot.Top.CPU = nil
		}
	}
	return snapshot
}
