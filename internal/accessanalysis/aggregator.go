package accessanalysis

import (
	"sort"
	"strings"
	"time"
)

const maxAggregateItems = 1000

type Aggregator struct {
	from      time.Time
	to        time.Time
	hourly    map[string]*hourBucket
	paths     map[string]*pathBucket
	ips       map[string]*ipBucket
	anomalies map[string]*Anomaly
	entries   []Entry
}

type hourBucket struct {
	HourlyPoint
	ips map[string]struct{}
}

type pathBucket struct {
	PathStat
	ips map[string]struct{}
}

type ipBucket struct {
	IPStat
	paths map[string]struct{}
}

func NewAggregator(from, to time.Time) *Aggregator {
	return &Aggregator{from: from, to: to, hourly: map[string]*hourBucket{}, paths: map[string]*pathBucket{}, ips: map[string]*ipBucket{}, anomalies: map[string]*Anomaly{}, entries: make([]Entry, 0, 500)}
}

func (a *Aggregator) Add(entry Entry) bool {
	ts, err := time.Parse(time.RFC3339, entry.TS)
	if err != nil || ts.Before(a.from) || !ts.Before(a.to) {
		return false
	}
	date := ts.Format("2006-01-02")
	hour := ts.Format("2006-01-02T15:00:00Z")

	hb := a.hourly[hour]
	if hb == nil {
		hb = &hourBucket{HourlyPoint: HourlyPoint{Hour: hour}, ips: map[string]struct{}{}}
		a.hourly[hour] = hb
	}
	hb.Requests++
	hb.Bytes += entry.Bytes
	hb.ips[entry.IP] = struct{}{}
	if entry.Status >= 400 && entry.Status < 500 {
		hb.Status4xx++
	}
	if entry.Status >= 500 {
		hb.Status5xx++
	}

	// 聚合 map 做 Top N 保护，避免异常日志把内存撑爆。
	if pb := a.pathBucket(date, entry.Path, entry.TS); pb != nil {
		pb.Requests++
		pb.Bytes += entry.Bytes
		pb.ips[entry.IP] = struct{}{}
		pb.LastSeenAt = maxTimeString(pb.LastSeenAt, entry.TS)
		addStatus(&pb.PathStat, entry.Status)
	}
	if ib := a.ipBucket(date, entry.IP, entry.TS); ib != nil {
		ib.Requests++
		ib.Bytes += entry.Bytes
		ib.paths[entry.Path] = struct{}{}
		if entry.Status >= 400 {
			ib.ErrorRequests++
		}
		if ib.SampleUserAgent == "" {
			ib.SampleUserAgent = entry.UserAgent
		}
		ib.FirstSeenAt = minTimeString(ib.FirstSeenAt, entry.TS)
		ib.LastSeenAt = maxTimeString(ib.LastSeenAt, entry.TS)
	}
	if len(a.entries) < 5000 {
		a.entries = append(a.entries, entry)
	}
	a.collectAnomaly(date, entry)
	return true
}

func (a *Aggregator) Result() AgentScanResponse {
	hourly := make([]HourlyPoint, 0, len(a.hourly))
	for _, bucket := range a.hourly {
		bucket.UniqueIPs = int64(len(bucket.ips))
		hourly = append(hourly, bucket.HourlyPoint)
	}
	sort.Slice(hourly, func(i, j int) bool { return hourly[i].Hour < hourly[j].Hour })

	paths := make([]PathStat, 0, len(a.paths))
	for _, bucket := range a.paths {
		bucket.UniqueIPs = int64(len(bucket.ips))
		paths = append(paths, bucket.PathStat)
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i].Requests > paths[j].Requests })

	ips := make([]IPStat, 0, len(a.ips))
	for _, bucket := range a.ips {
		bucket.UniquePaths = int64(len(bucket.paths))
		ips = append(ips, bucket.IPStat)
	}
	sort.Slice(ips, func(i, j int) bool { return ips[i].Requests > ips[j].Requests })

	anomalies := make([]Anomaly, 0, len(a.anomalies))
	for _, item := range a.anomalies {
		anomalies = append(anomalies, *item)
	}
	sort.Slice(anomalies, func(i, j int) bool { return anomalies[i].Requests > anomalies[j].Requests })

	return AgentScanResponse{Hourly: hourly, Paths: paths, IPs: ips, EntriesSample: a.entries, Anomalies: anomalies}
}

func (a *Aggregator) pathBucket(date, path, ts string) *pathBucket {
	key := date + "\x00" + path
	if bucket := a.paths[key]; bucket != nil {
		return bucket
	}
	if len(a.paths) >= maxAggregateItems {
		return nil
	}
	bucket := &pathBucket{PathStat: PathStat{Date: date, Path: path, LastSeenAt: ts}, ips: map[string]struct{}{}}
	a.paths[key] = bucket
	return bucket
}

func (a *Aggregator) ipBucket(date, ip, ts string) *ipBucket {
	key := date + "\x00" + ip
	if bucket := a.ips[key]; bucket != nil {
		return bucket
	}
	if len(a.ips) >= maxAggregateItems {
		return nil
	}
	bucket := &ipBucket{IPStat: IPStat{Date: date, IP: ip, FirstSeenAt: ts, LastSeenAt: ts}, paths: map[string]struct{}{}}
	a.ips[key] = bucket
	return bucket
}

func (a *Aggregator) collectAnomaly(date string, entry Entry) {
	kind, target, reason, severity := "", "", "", "medium"
	if entry.IsAnomaly {
		kind, target, reason = "entry", entry.Path, entry.AnomalyReason
		if strings.Contains(reason, "5xx") {
			severity = "high"
		}
	} else if entry.Status == 404 {
		kind, target, reason = "high_404_path", entry.Path, "404 高频路径"
	} else if entry.Status >= 500 {
		kind, target, reason, severity = "5xx_path", entry.Path, "5xx 路径", "high"
	}
	if kind == "" {
		return
	}
	key := date + "\x00" + kind + "\x00" + target
	item := a.anomalies[key]
	if item == nil {
		item = &Anomaly{Date: date, Kind: kind, Target: target, Severity: severity, Reason: reason, FirstSeenAt: entry.TS, LastSeenAt: entry.TS}
		a.anomalies[key] = item
	}
	item.Requests++
	item.FirstSeenAt = minTimeString(item.FirstSeenAt, entry.TS)
	item.LastSeenAt = maxTimeString(item.LastSeenAt, entry.TS)
}

func addStatus(stat *PathStat, status int) {
	switch {
	case status >= 200 && status < 300:
		stat.Status2xx++
	case status >= 300 && status < 400:
		stat.Status3xx++
	case status >= 400 && status < 500:
		stat.Status4xx++
	case status >= 500:
		stat.Status5xx++
	}
}

func minTimeString(a, b string) string {
	if a == "" || b < a {
		return b
	}
	return a
}

func maxTimeString(a, b string) string {
	if b > a {
		return b
	}
	return a
}
