package accessanalysis

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

func TestSaveScanResultCancellationRollsBackAndKeepsCursor(t *testing.T) {
	database, analysisRepo := newAtomicTestRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	analysisRepo.afterPersistStage = func(stage string) {
		if stage == "hourly" {
			cancel()
		}
	}
	err := analysisRepo.SaveScanResult(ctx, "site_1", "/tmp/access.log", "job_1", Cursor{Inode: 2, Offset: 200, FileSize: 300}, atomicTestResult(), atomicTestSettings(), 10)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	assertAtomicRollback(t, database)
}

func TestSaveScanResultInjectedFailureRollsBackAndKeepsCursor(t *testing.T) {
	database, analysisRepo := newAtomicTestRepo(t)
	if _, err := database.Exec(`CREATE TRIGGER fail_access_path BEFORE INSERT ON access_analysis_paths BEGIN SELECT RAISE(ABORT, 'injected path failure'); END`); err != nil {
		t.Fatal(err)
	}
	err := analysisRepo.SaveScanResult(context.Background(), "site_1", "/tmp/access.log", "job_1", Cursor{Inode: 2, Offset: 200, FileSize: 300}, atomicTestResult(), atomicTestSettings(), 10)
	if err == nil {
		t.Fatal("expected injected write failure")
	}
	assertAtomicRollback(t, database)
}

func TestSaveScanResultCommitsBatchJobAndCursorAtomically(t *testing.T) {
	database, analysisRepo := newAtomicTestRepo(t)
	result := atomicTestResult()
	result.EntriesSample = []Entry{{TS: "2026-07-26T01:02:03Z", IP: strings.Repeat("界", 100), Method: strings.Repeat("方", 20), Path: strings.Repeat("路", 1000), RawPath: strings.Repeat("径", 1000), Referer: strings.Repeat("参", 1000), UserAgent: strings.Repeat("客", 1000), AnomalyReason: strings.Repeat("因", 300)}}
	settings := atomicTestSettings()
	settings.SaveEntries = true
	if err := analysisRepo.SaveScanResult(context.Background(), "site_1", "/tmp/access.log", "job_1", Cursor{Inode: 2, Offset: 200, FileSize: 300}, result, settings, 10); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"access_analysis_hourly", "access_analysis_daily", "access_analysis_paths", "access_analysis_ips", "access_analysis_entries", "access_analysis_anomalies"} {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE site_id = 'site_1'`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	var status string
	if err := database.QueryRow(`SELECT status FROM access_analysis_jobs WHERE id = 'job_1'`).Scan(&status); err != nil || status != "success" {
		t.Fatalf("job status=%q err=%v", status, err)
	}
	cursor, err := analysisRepo.GetCursor(context.Background(), "site_1", "/tmp/access.log")
	if err != nil || cursor.Offset != 200 {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	var ip, method, path, referer, ua, reason string
	if err := database.QueryRow(`SELECT ip, method, path, referer, user_agent, anomaly_reason FROM access_analysis_entries WHERE site_id = 'site_1'`).Scan(&ip, &method, &path, &referer, &ua, &reason); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		value string
		max   int
	}{{ip, MaxIPBytes}, {method, MaxMethodBytes}, {path, MaxPathBytes}, {referer, MaxRefererBytes}, {ua, MaxUserAgentBytes}, {reason, MaxAnomalyReasonBytes}} {
		if len(check.value) > check.max {
			t.Fatalf("stored string len=%d max=%d", len(check.value), check.max)
		}
	}
}

func TestSaveScanResultRejectsMalformedAgentMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AgentScanResponse)
	}{
		{name: "short hour", mutate: func(result *AgentScanResponse) { result.Hourly[0].Hour = "x" }},
		{name: "unaligned hour", mutate: func(result *AgentScanResponse) { result.Hourly[0].Hour = "2026-07-26T01:30:00Z" }},
		{name: "invalid date", mutate: func(result *AgentScanResponse) { result.Paths[0].Date = "2026-99-99" }},
		{name: "invalid timestamp", mutate: func(result *AgentScanResponse) { result.IPs[0].LastSeenAt = strings.Repeat("x", MaxTimestampBytes+1) }},
		{name: "oversized kind", mutate: func(result *AgentScanResponse) { result.Anomalies[0].Kind = strings.Repeat("k", MaxAnomalyKindBytes+1) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			database, analysisRepo := newAtomicTestRepo(t)
			result := atomicTestResult()
			tc.mutate(result)
			if err := analysisRepo.SaveScanResult(context.Background(), "site_1", "/tmp/access.log", "job_1", Cursor{Inode: 2, Offset: 200, FileSize: 300}, result, atomicTestSettings(), 10); err == nil {
				t.Fatal("expected malformed metadata rejection")
			}
			assertAtomicRollback(t, database)
		})
	}
}

func TestSaveScanResultRejectsOversizedAgentCardinalityBeforeWrites(t *testing.T) {
	database, analysisRepo := newAtomicTestRepo(t)
	result := atomicTestResult()
	result.Paths = make([]PathStat, MaxScanPathResults+1)
	if err := analysisRepo.SaveScanResult(context.Background(), "site_1", "/tmp/access.log", "job_1", Cursor{Inode: 2, Offset: 200, FileSize: 300}, result, atomicTestSettings(), 10); err == nil {
		t.Fatal("expected oversized cardinality rejection")
	}
	assertAtomicRollback(t, database)
}

func TestValidateScanResultBoundsRejectsDiagnosticsAndCounters(t *testing.T) {
	settings := atomicTestSettings()
	tests := []struct {
		name   string
		mutate func(*AgentScanResponse)
	}{
		{name: "too many parse errors", mutate: func(result *AgentScanResponse) { result.ParseErrors = make([]string, MaxScanParseErrors+1) }},
		{name: "negative truncation counter", mutate: func(result *AgentScanResponse) { result.Truncation.PathsDropped = -1 }},
		{name: "inconsistent truncation flag", mutate: func(result *AgentScanResponse) { result.Truncation.PathsDropped = 1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := atomicTestResult()
			tc.mutate(result)
			if err := validateScanResultBounds(result, settings); err == nil {
				t.Fatal("expected bounds validation error")
			}
		})
	}
}

func TestSummaryPropagatesContextCancellation(t *testing.T) {
	_, analysisRepo := newAtomicTestRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := analysisRepo.Summary(ctx, "site_1", "2026-07-26"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Summary error=%v", err)
	}
}

func newAtomicTestRepo(t *testing.T) (*sql.DB, *Repo) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/panel.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	site := &repo.Site{ID: "site_1", PrimaryDomain: "example.com", DomainsJSON: `[]`, Status: "enabled", HTTPPort: 80, HTTPSPort: 443, RootPath: "/www/example.com", AccessLogPath: "/tmp/access.log", ErrorLogPath: "/tmp/error.log", ConfigPath: "/tmp/site.conf", EnabledPath: "/tmp/site.conf", RewritePath: "/tmp/rewrite.conf"}
	if err := repo.NewSiteRepo(database).Create(site); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO access_analysis_jobs (id, site_id, trigger, range_start, range_end, status, created_at) VALUES ('job_1', 'site_1', 'manual', '2026-07-26T00:00:00Z', '2026-07-27T00:00:00Z', 'running', '2026-07-26T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO access_analysis_cursors (site_id, log_path, inode, offset, file_size) VALUES ('site_1', '/tmp/access.log', 1, 100, 150)`); err != nil {
		t.Fatal(err)
	}
	return database, NewRepo(database)
}

func atomicTestResult() *AgentScanResponse {
	return &AgentScanResponse{
		ScannedLines: 1, Hourly: []HourlyPoint{{Hour: "2026-07-26T01:00:00Z", Requests: 1, UniqueIPs: 1, Bytes: 10}},
		Paths:     []PathStat{{Date: "2026-07-26", Path: "/", Requests: 1, UniqueIPs: 1, LastSeenAt: "2026-07-26T01:02:03Z"}},
		IPs:       []IPStat{{Date: "2026-07-26", IP: "192.0.2.1", Requests: 1, UniquePaths: 1, FirstSeenAt: "2026-07-26T01:02:03Z", LastSeenAt: "2026-07-26T01:02:03Z"}},
		Anomalies: []Anomaly{{Date: "2026-07-26", Kind: "entry", Target: "/", Requests: 1, Severity: "high", Reason: "test", FirstSeenAt: "2026-07-26T01:02:03Z", LastSeenAt: "2026-07-26T01:02:03Z"}},
	}
}

func atomicTestSettings() *Settings {
	return &Settings{PathTopN: 100, IPTopN: 100, MaxEntries: 1000}
}

func assertAtomicRollback(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, table := range []string{"access_analysis_hourly", "access_analysis_daily", "access_analysis_paths", "access_analysis_ips", "access_analysis_entries", "access_analysis_anomalies"} {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE site_id = 'site_1'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rollback failed for %s count=%d err=%v", table, count, err)
		}
	}
	var status string
	if err := database.QueryRow(`SELECT status FROM access_analysis_jobs WHERE id = 'job_1'`).Scan(&status); err != nil || status != "running" {
		t.Fatalf("job status=%q err=%v", status, err)
	}
	var offset int64
	if err := database.QueryRow(`SELECT offset FROM access_analysis_cursors WHERE site_id = 'site_1'`).Scan(&offset); err != nil || offset != 100 {
		t.Fatalf("cursor offset=%d err=%v", offset, err)
	}
}
