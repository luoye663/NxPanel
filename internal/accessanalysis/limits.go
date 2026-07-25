package accessanalysis

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxIPBytes            = 64
	MaxMethodBytes        = 16
	MaxPathBytes          = 2048
	MaxRefererBytes       = 2048
	MaxUserAgentBytes     = 1024
	MaxAnomalyReasonBytes = 512
	MaxAnomalyKindBytes   = 64
	MaxSeverityBytes      = 16
	MaxDateBytes          = 10
	MaxTimestampBytes     = 20
	MaxScanHourlyResults  = 1000
	MaxScanPathResults    = 50000
	MaxScanIPResults      = 50000
	MaxScanAnomalies      = 10000
	MaxScanEntries        = 100000
	MaxScanParseErrors    = 20
)

type AggregationLimits struct {
	Paths         int
	IPs           int
	Hourly        int
	Anomalies     int
	Entries       int
	TotalDistinct int
}

func DefaultAggregationLimits() AggregationLimits {
	return AggregationLimits{Paths: 10000, IPs: 10000, Hourly: 1000, Anomalies: 1000, Entries: 100000, TotalDistinct: 50000}
}

func NormalizeEntryStrings(entry Entry) Entry {
	entry.IP = truncateUTF8(entry.IP, MaxIPBytes)
	entry.Method = truncateUTF8(entry.Method, MaxMethodBytes)
	entry.Path = truncateUTF8(entry.Path, MaxPathBytes)
	entry.RawPath = truncateUTF8(entry.RawPath, MaxPathBytes)
	entry.Referer = truncateUTF8(entry.Referer, MaxRefererBytes)
	entry.UserAgent = truncateUTF8(entry.UserAgent, MaxUserAgentBytes)
	entry.AnomalyReason = truncateUTF8(entry.AnomalyReason, MaxAnomalyReasonBytes)
	return entry
}

func TruncateUTF8(value string, maxBytes int) string {
	return truncateUTF8(value, maxBytes)
}

func normalizePathStat(item PathStat) PathStat {
	item.Path = truncateUTF8(item.Path, MaxPathBytes)
	return item
}

func normalizeIPStat(item IPStat) IPStat {
	item.IP = truncateUTF8(item.IP, MaxIPBytes)
	item.SampleUserAgent = truncateUTF8(item.SampleUserAgent, MaxUserAgentBytes)
	return item
}

func normalizeAnomaly(item Anomaly) Anomaly {
	item.Kind = truncateUTF8(item.Kind, MaxAnomalyKindBytes)
	item.Target = truncateUTF8(item.Target, MaxPathBytes)
	item.Severity = truncateUTF8(item.Severity, MaxSeverityBytes)
	item.Reason = truncateUTF8(item.Reason, MaxAnomalyReasonBytes)
	return item
}

func validateDate(value string) error {
	if len(value) != MaxDateBytes {
		return fmt.Errorf("日期必须是 YYYY-MM-DD 格式")
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return fmt.Errorf("日期必须是有效的 YYYY-MM-DD")
	}
	return nil
}

func validateTimestamp(value string) error {
	if len(value) == 0 || len(value) > MaxTimestampBytes {
		return fmt.Errorf("时间戳长度无效")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || parsed.UTC().Format(time.RFC3339) != value {
		return fmt.Errorf("时间戳必须是 UTC RFC3339 格式")
	}
	return nil
}

func validateHour(value string) error {
	if err := validateTimestamp(value); err != nil {
		return err
	}
	parsed, _ := time.Parse(time.RFC3339, value)
	if parsed.Minute() != 0 || parsed.Second() != 0 || parsed.Nanosecond() != 0 {
		return fmt.Errorf("小时聚合时间必须对齐整点")
	}
	return nil
}

func validateScanResultMetadata(result *AgentScanResponse) error {
	if result == nil {
		return fmt.Errorf("Agent 扫描结果不能为空")
	}
	for _, item := range result.Hourly {
		if err := validateHour(item.Hour); err != nil {
			return fmt.Errorf("无效小时聚合 %q: %w", item.Hour, err)
		}
	}
	for _, item := range result.Paths {
		if err := validateDate(item.Date); err != nil {
			return fmt.Errorf("无效路径聚合日期 %q: %w", item.Date, err)
		}
		if err := validateTimestamp(item.LastSeenAt); err != nil {
			return fmt.Errorf("无效路径聚合时间: %w", err)
		}
	}
	for _, item := range result.IPs {
		if err := validateDate(item.Date); err != nil {
			return fmt.Errorf("无效 IP 聚合日期 %q: %w", item.Date, err)
		}
		if err := validateTimestamp(item.FirstSeenAt); err != nil {
			return fmt.Errorf("无效 IP 首次时间: %w", err)
		}
		if err := validateTimestamp(item.LastSeenAt); err != nil {
			return fmt.Errorf("无效 IP 最后时间: %w", err)
		}
	}
	for _, item := range result.EntriesSample {
		if err := validateTimestamp(item.TS); err != nil {
			return fmt.Errorf("无效明细时间: %w", err)
		}
	}
	for _, item := range result.Anomalies {
		if err := validateDate(item.Date); err != nil {
			return fmt.Errorf("无效异常日期 %q: %w", item.Date, err)
		}
		if item.Kind == "" || len(item.Kind) > MaxAnomalyKindBytes || !utf8.ValidString(item.Kind) {
			return fmt.Errorf("异常 kind 必须是 1-%d 字节的 UTF-8 字符串", MaxAnomalyKindBytes)
		}
		if err := validateTimestamp(item.FirstSeenAt); err != nil {
			return fmt.Errorf("无效异常首次时间: %w", err)
		}
		if err := validateTimestamp(item.LastSeenAt); err != nil {
			return fmt.Errorf("无效异常最后时间: %w", err)
		}
	}
	return nil
}

func validateScanResultBounds(result *AgentScanResponse, settings *Settings) error {
	if result == nil {
		return fmt.Errorf("Agent 扫描结果不能为空")
	}
	if settings == nil {
		return fmt.Errorf("访问分析设置不能为空")
	}
	if len(result.Hourly) > MaxScanHourlyResults {
		return fmt.Errorf("小时聚合数量超过上限 %d", MaxScanHourlyResults)
	}
	if len(result.Paths) > MaxScanPathResults {
		return fmt.Errorf("路径聚合数量超过上限 %d", MaxScanPathResults)
	}
	if len(result.IPs) > MaxScanIPResults {
		return fmt.Errorf("IP 聚合数量超过上限 %d", MaxScanIPResults)
	}
	if len(result.Anomalies) > MaxScanAnomalies {
		return fmt.Errorf("异常聚合数量超过上限 %d", MaxScanAnomalies)
	}
	entryLimit := min(max(settings.MaxEntries, 0), MaxScanEntries)
	if !settings.SaveEntries {
		entryLimit = 0
	}
	if len(result.EntriesSample) > entryLimit {
		return fmt.Errorf("访问明细数量超过请求上限 %d", entryLimit)
	}
	if len(result.ParseErrors) > MaxScanParseErrors {
		return fmt.Errorf("解析错误数量超过上限 %d", MaxScanParseErrors)
	}
	for _, parseError := range result.ParseErrors {
		if len(parseError) > MaxAnomalyReasonBytes || !utf8.ValidString(parseError) {
			return fmt.Errorf("解析错误必须是不超过 %d 字节的 UTF-8 字符串", MaxAnomalyReasonBytes)
		}
	}
	if result.ScannedLines < 0 || result.SkippedLines < 0 {
		return fmt.Errorf("扫描行数计数不能为负数")
	}
	counters := []int64{
		result.Truncation.PathsDropped,
		result.Truncation.IPsDropped,
		result.Truncation.HourlyDropped,
		result.Truncation.AnomaliesDropped,
		result.Truncation.EntriesDropped,
		result.Truncation.UniqueValuesDropped,
	}
	truncated := false
	for _, counter := range counters {
		if counter < 0 {
			return fmt.Errorf("截断计数不能为负数")
		}
		truncated = truncated || counter > 0
	}
	if result.Truncation.Truncated != truncated {
		return fmt.Errorf("截断标志与截断计数不一致")
	}
	return nil
}

func truncateUTF8(value string, maxBytes int) string {
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "\uFFFD")
	}
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}
