package accessanalysis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

type blockingAnalysisAgent struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

type captureAnalysisAgent struct {
	requests chan AgentScanRequest
}

func (a *captureAnalysisAgent) AccessAnalysisScan(_ context.Context, req *AgentScanRequest) (*AgentScanResponse, error) {
	a.requests <- *req
	return &AgentScanResponse{Cursor: Cursor{Offset: 10}}, nil
}

func (a *captureAnalysisAgent) AccessAnalysisFormatDetect(context.Context, *AgentFormatDetectRequest) (*FormatDetectResponse, error) {
	return nil, nil
}

func (a *blockingAnalysisAgent) AccessAnalysisScan(ctx context.Context, req *AgentScanRequest) (*AgentScanResponse, error) {
	if a.calls.Add(1) == 1 {
		close(a.started)
	}
	select {
	case <-a.release:
		return &AgentScanResponse{Cursor: Cursor{Offset: 10}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (a *blockingAnalysisAgent) AccessAnalysisFormatDetect(context.Context, *AgentFormatDetectRequest) (*FormatDetectResponse, error) {
	return nil, nil
}

func TestScanExcludesConcurrentScanForSameSite(t *testing.T) {
	root, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	agent := &blockingAnalysisAgent{started: make(chan struct{}), release: make(chan struct{})}
	database, service := newServiceResourceTest(t, root, agent)
	firstDone := make(chan error, 1)
	go func() {
		_, err := service.Scan(context.Background(), "site_1", ScanRequest{Range: "today"}, "req_1")
		firstDone <- err
	}()
	<-agent.started
	second, err := service.Scan(context.Background(), "site_1", ScanRequest{Range: "today"}, "req_2")
	if err != nil || second.Status != "running" {
		t.Fatalf("second scan=%+v err=%v", second, err)
	}
	if calls := agent.calls.Load(); calls != 1 {
		t.Fatalf("agent calls=%d want=1", calls)
	}
	close(agent.release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	var successes int
	if err := database.QueryRow(`SELECT COUNT(*) FROM access_analysis_jobs WHERE site_id = 'site_1' AND status = 'success'`).Scan(&successes); err != nil || successes != 1 {
		t.Fatalf("success jobs=%d err=%v", successes, err)
	}
}

func TestRootShutdownFinalizesRunningScanAsFailed(t *testing.T) {
	root, cancelRoot := context.WithCancel(context.Background())
	agent := &blockingAnalysisAgent{started: make(chan struct{}), release: make(chan struct{})}
	database, service := newServiceResourceTest(t, root, agent)
	done := make(chan error, 1)
	go func() {
		_, err := service.Scan(context.Background(), "site_1", ScanRequest{Range: "today"}, "req_1")
		done <- err
	}()
	<-agent.started
	cancelRoot()
	if err := <-done; err == nil {
		t.Fatal("shutdown scan should fail")
	}
	var status, finished string
	if err := database.QueryRow(`SELECT status, COALESCE(finished_at, '') FROM access_analysis_jobs WHERE site_id = 'site_1'`).Scan(&status, &finished); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || finished == "" {
		t.Fatalf("status=%q finished=%q", status, finished)
	}
}

func TestExportCSVCancellationStopsPagedIteration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	database, service := newServiceResourceTest(t, ctx, fakeAnalysisAgent{})
	for i := 0; i < 500; i++ {
		if _, err := database.Exec(`INSERT INTO access_analysis_paths (site_id, date, path, requests, unique_ips, last_seen_at) VALUES ('site_1', '2026-07-26', ?, 1, 1, '2026-07-26T00:00:00Z')`, fmt.Sprintf("/%04d/%s", i, strings.Repeat("x", 80))); err != nil {
			t.Fatal(err)
		}
	}
	w := &cancelAfterWriter{cancel: cancel, after: 1024}
	err := service.ExportCSV(ctx, w, "paths", "site_1", Query{From: "2026-07-26", To: "2026-07-27"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExportCSV error=%v", err)
	}
	if w.written > 64*1024 {
		t.Fatalf("export continued too far after cancellation: %d bytes", w.written)
	}
}

func TestScanRequestsEntriesOnlyWhenSettingsEnablePersistence(t *testing.T) {
	tests := []struct {
		name        string
		saveEntries bool
		maxEntries  int
	}{
		{name: "aggregate only", saveEntries: false, maxEntries: 50000},
		{name: "collect bounded entries", saveEntries: true, maxEntries: 1234},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			agent := &captureAnalysisAgent{requests: make(chan AgentScanRequest, 1)}
			_, service := newServiceResourceTest(t, ctx, agent)
			settings := defaultSettings("site_1")
			settings.SaveEntries = tc.saveEntries
			settings.MaxEntries = tc.maxEntries
			if err := service.repo.SaveSettings(ctx, settings); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Scan(ctx, "site_1", ScanRequest{Range: "today"}, "req_1"); err != nil {
				t.Fatal(err)
			}
			request := <-agent.requests
			if request.CollectEntries != tc.saveEntries || request.MaxEntries != tc.maxEntries {
				t.Fatalf("entry request=%+v", request)
			}
		})
	}
}

func TestRecoverStaleJobsMarksOldAndPreservesRecent(t *testing.T) {
	ctx := context.Background()
	database, service := newServiceResourceTest(t, ctx, fakeAnalysisAgent{})
	now := time.Now().UTC()
	insertAnalysisJob(t, database, "stale_job", now.Add(-11*time.Minute))
	insertAnalysisJob(t, database, "recent_job", now.Add(-9*time.Minute))
	if err := service.RecoverStaleJobs(ctx); err != nil {
		t.Fatal(err)
	}
	var staleStatus, staleFinished, staleError, recentStatus string
	if err := database.QueryRow(`SELECT status, COALESCE(finished_at, ''), COALESCE(error_message, '') FROM access_analysis_jobs WHERE id = 'stale_job'`).Scan(&staleStatus, &staleFinished, &staleError); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT status FROM access_analysis_jobs WHERE id = 'recent_job'`).Scan(&recentStatus); err != nil {
		t.Fatal(err)
	}
	if staleStatus != "failed" || staleFinished == "" || staleError != staleJobError || recentStatus != "running" {
		t.Fatalf("stale=%q finished=%q error=%q recent=%q", staleStatus, staleFinished, staleError, recentStatus)
	}
}

func TestRecoverStaleJobsDoesNotTouchActiveInProcessSite(t *testing.T) {
	ctx := context.Background()
	database, service := newServiceResourceTest(t, ctx, fakeAnalysisAgent{})
	insertAnalysisJob(t, database, "active_stale_job", time.Now().UTC().Add(-time.Hour))
	service.running.Store("site_1", struct{}{})
	defer service.running.Delete("site_1")
	if err := service.RecoverStaleJobs(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := database.QueryRow(`SELECT status FROM access_analysis_jobs WHERE id = 'active_stale_job'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "running" {
		t.Fatalf("active job status=%q", status)
	}
}

func TestScanRecoversStaleSiteJobBeforeAdmission(t *testing.T) {
	ctx := context.Background()
	agent := &captureAnalysisAgent{requests: make(chan AgentScanRequest, 1)}
	database, service := newServiceResourceTest(t, ctx, agent)
	insertAnalysisJob(t, database, "stale_job", time.Now().UTC().Add(-time.Hour))
	response, err := service.Scan(ctx, "site_1", ScanRequest{Range: "today"}, "req_1")
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "success" {
		t.Fatalf("scan response=%+v", response)
	}
	var staleStatus string
	if err := database.QueryRow(`SELECT status FROM access_analysis_jobs WHERE id = 'stale_job'`).Scan(&staleStatus); err != nil {
		t.Fatal(err)
	}
	if staleStatus != "failed" {
		t.Fatalf("stale status=%q", staleStatus)
	}
}

func TestScanPreservesRecentRunningJob(t *testing.T) {
	ctx := context.Background()
	agent := &captureAnalysisAgent{requests: make(chan AgentScanRequest, 1)}
	database, service := newServiceResourceTest(t, ctx, agent)
	insertAnalysisJob(t, database, "recent_job", time.Now().UTC().Add(-time.Minute))
	response, err := service.Scan(ctx, "site_1", ScanRequest{Range: "today"}, "req_1")
	if err != nil { t.Fatal(err) }
	if response.JobID != "recent_job" || response.Status != "running" {
		t.Fatalf("scan response=%+v", response)
	}
	if len(agent.requests) != 0 { t.Fatal("recent running job must prevent a second Agent scan") }
	var status string
	if err := database.QueryRow(`SELECT status FROM access_analysis_jobs WHERE id = 'recent_job'`).Scan(&status); err != nil { t.Fatal(err) }
	if status != "running" { t.Fatalf("recent status=%q", status) }
}

type cancelAfterWriter struct {
	cancel  context.CancelFunc
	after   int
	written int
}

func (w *cancelAfterWriter) Write(p []byte) (int, error) {
	w.written += len(p)
	if w.written >= w.after {
		w.cancel()
	}
	return len(p), nil
}

func newServiceResourceTest(t *testing.T, root context.Context, agent Agent) (*sql.DB, *Service) {
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
	return database, NewService(root, repo.NewSiteRepo(database), NewRepo(database), repo.NewOperationRepo(database), agent)
}

func insertAnalysisJob(t *testing.T, database *sql.DB, id string, createdAt time.Time) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO access_analysis_jobs (id, site_id, trigger, range_start, range_end, status, created_at) VALUES (?, 'site_1', 'manual', '2026-07-26T00:00:00Z', '2026-07-27T00:00:00Z', 'running', ?)`, id, createdAt.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
}

var _ io.Writer = (*cancelAfterWriter)(nil)
