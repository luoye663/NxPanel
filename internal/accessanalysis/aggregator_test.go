package accessanalysis

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestAggregatorCardinalityBudgetsAreGlobalAndDeterministic(t *testing.T) {
	from := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	agg := NewAggregatorWithLimits(from, from.Add(24*time.Hour), AggregationLimits{
		Paths: 2, IPs: 2, Hourly: 1, Anomalies: 1, Entries: 2, TotalDistinct: 12,
	})
	for i := 0; i < 8; i++ {
		agg.Add(Entry{
			TS: from.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
			IP: fmt.Sprintf("192.0.2.%d", i), Path: fmt.Sprintf("/path/%d", i),
			Method: "GET", Status: 500, IsAnomaly: true, AnomalyReason: "5xx response",
		})
	}
	result := agg.Result()
	if len(result.Paths) > 2 || len(result.IPs) > 2 || len(result.Hourly) > 1 || len(result.Anomalies) > 1 || len(result.EntriesSample) > 2 {
		t.Fatalf("budgets exceeded: paths=%d ips=%d hourly=%d anomalies=%d entries=%d", len(result.Paths), len(result.IPs), len(result.Hourly), len(result.Anomalies), len(result.EntriesSample))
	}
	if !result.Truncation.Truncated || result.Truncation.PathsDropped == 0 || result.Truncation.IPsDropped == 0 || result.Truncation.HourlyDropped == 0 || result.Truncation.AnomaliesDropped == 0 || result.Truncation.EntriesDropped == 0 {
		t.Fatalf("missing truncation counters: %+v", result.Truncation)
	}
	if result.Paths[0].Path != "/path/0" || result.Paths[1].Path != "/path/1" {
		t.Fatalf("cardinality admission must keep first keys deterministically: %+v", result.Paths)
	}
}

func TestLogDerivedStringsAreUTF8SafeAndBounded(t *testing.T) {
	entry := NormalizeEntryStrings(Entry{
		IP: strings.Repeat("界", 100), Method: strings.Repeat("方", 20),
		Path: strings.Repeat("路", 1000), RawPath: strings.Repeat("径", 1000),
		Referer: strings.Repeat("参", 1000), UserAgent: strings.Repeat("客", 1000),
		AnomalyReason: string([]byte{'x', 0xff, 'y'}) + strings.Repeat("因", 300),
	})
	checks := []struct {
		value string
		max   int
	}{
		{entry.IP, MaxIPBytes}, {entry.Method, MaxMethodBytes}, {entry.Path, MaxPathBytes},
		{entry.RawPath, MaxPathBytes}, {entry.Referer, MaxRefererBytes},
		{entry.UserAgent, MaxUserAgentBytes}, {entry.AnomalyReason, MaxAnomalyReasonBytes},
	}
	for _, check := range checks {
		if len(check.value) > check.max || !utf8.ValidString(check.value) {
			t.Fatalf("invalid bounded string len=%d max=%d valid=%v", len(check.value), check.max, utf8.ValidString(check.value))
		}
	}
}

func TestAggregatorEntryCollectionCanBeDisabled(t *testing.T) {
	from := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	agg := NewAggregatorWithLimits(from, from.Add(time.Hour), AggregationLimits{
		Paths: 10, IPs: 10, Hourly: 10, Anomalies: 10, Entries: 0, TotalDistinct: 100,
	})
	for i := 0; i < 5; i++ {
		agg.Add(Entry{TS: from.Add(time.Duration(i) * time.Minute).Format(time.RFC3339), IP: "192.0.2.1", Method: "GET", Path: "/"})
	}
	result := agg.Result()
	if result.EntriesSample == nil || len(result.EntriesSample) != 0 {
		t.Fatalf("disabled entry collection returned entries: %#v", result.EntriesSample)
	}
	if result.Truncation.EntriesDropped != 0 {
		t.Fatalf("disabled collection must not count dropped entries: %+v", result.Truncation)
	}
	legacy := NewAggregator(from, from.Add(time.Hour))
	legacy.Add(Entry{TS: from.Format(time.RFC3339), IP: "192.0.2.1", Method: "GET", Path: "/"})
	if len(legacy.Result().EntriesSample) != 1 {
		t.Fatal("legacy constructor must retain default entry collection")
	}
}
