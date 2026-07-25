package acme

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/sse"
)

func newAdmissionTestService(t *testing.T, root context.Context, limit int) *Service {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	sites := repo.NewSiteRepo(database)
	if err := sites.Create(&repo.Site{ID: "site-1", PrimaryDomain: "example.com", DomainsJSON: `["example.com"]`, Status: "enabled", RootPath: "/var/www/example", ConfigPath: "/etc/nginx/example.conf"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(root, limit, sites, repo.NewSSLRepo(database), repo.NewCertificateRepo(database), repo.NewACMERepo(database), repo.NewOperationRepo(database), nil, nil, sse.NewHub(), &app.Config{})
	t.Cleanup(svc.Close)
	return svc
}

func TestApplyAdmissionRejectsWithoutOrderAndRecoversSlot(t *testing.T) {
	svc := newAdmissionTestService(t, context.Background(), 1)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	svc.applyJob = func(ctx context.Context, _ string, _ string, _ []string, _ string, _ string, _ string, _ bool) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	req := &ApplyRequest{SiteID: "site-1", Domains: []string{"example.com"}, ChallengeType: "http-01", Email: "admin@example.com"}
	if _, err := svc.ApplyCertificate(context.Background(), req, "req-1"); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := svc.ApplyCertificate(context.Background(), req, "req-2"); !isBusy(err) {
		t.Fatalf("second admission error = %v, want BUSY", err)
	}
	orders, err := svc.acmeRepo.ListOrdersBySiteID("site-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Fatalf("rejected admission created state: orders=%d", len(orders))
	}
	close(release)
	svc.jobsWG.Wait()
	if _, err := svc.ApplyCertificate(context.Background(), req, "req-3"); err != nil {
		t.Fatalf("slot was not recovered: %v", err)
	}
}

func TestApplyCreatesStreamBeforeWorkerStarts(t *testing.T) {
	svc := newAdmissionTestService(t, context.Background(), 1)
	release := make(chan struct{})
	svc.applyJob = func(ctx context.Context, _ string, _ string, _ []string, _ string, _ string, _ string, _ bool) {
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	orderID, err := svc.ApplyCertificate(context.Background(), &ApplyRequest{SiteID: "site-1", Domains: []string{"example.com"}, Email: "admin@example.com"}, "req-stream")
	if err != nil {
		t.Fatal(err)
	}
	if stream := svc.StreamLogs(orderID); stream == nil {
		t.Fatal("stream was not available immediately after durable order creation")
	}
	close(release)
}

func TestApplyPanicReleasesSlotAndRootCancellationReachesJob(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	svc := newAdmissionTestService(t, root, 1)
	panicked := make(chan struct{})
	svc.applyJob = func(context.Context, string, string, []string, string, string, string, bool) {
		close(panicked)
		panic("test panic")
	}
	req := &ApplyRequest{SiteID: "site-1", Domains: []string{"example.com"}, Email: "admin@example.com"}
	if _, err := svc.ApplyCertificate(context.Background(), req, "req-1"); err != nil {
		t.Fatal(err)
	}
	<-panicked
	svc.jobsWG.Wait()
	cancelled := make(chan struct{})
	svc.applyJob = func(ctx context.Context, _ string, _ string, _ []string, _ string, _ string, _ string, _ bool) {
		<-ctx.Done()
		close(cancelled)
	}
	if _, err := svc.ApplyCertificate(context.Background(), req, "req-2"); err != nil {
		t.Fatalf("panic did not release slot: %v", err)
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("root cancellation did not reach ACME job")
	}
}

func isBusy(err error) bool {
	var appErr *app.AppError
	return errors.As(err, &appErr) && appErr.Code == app.ErrBusy
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBoundHTTPClientCancelsTransportWithJobContext(t *testing.T) {
	jobCtx, cancelJob := context.WithCancel(context.Background())
	started := make(chan struct{})
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	client := bindHTTPClientContext(jobCtx, &http.Client{Transport: base, Timeout: time.Minute})
	if client.Timeout <= 0 {
		t.Fatal("bound lego HTTP client must retain a finite timeout")
	}
	errCh := make(chan error, 1)
	go func() {
		req, err := http.NewRequest(http.MethodGet, "https://acme.invalid/directory", nil)
		if err != nil {
			errCh <- err
			return
		}
		_, err = client.Do(req)
		errCh <- err
	}()
	<-started
	cancelJob()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("HTTP request error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("lego HTTP transport did not stop after job cancellation")
	}
}
