package sitebackup

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
	"github.com/luoye663/nxpanel/internal/sse"
)

type blockingBackupAgent struct {
	started chan struct{}
	release chan struct{}
	panicOn atomic.Bool
}

func (a *blockingBackupAgent) SiteBackupCreate(ctx context.Context, _ *agentclient.SiteBackupCreateRequest) (*agentclient.SiteBackupCreateResponse, error) {
	a.started <- struct{}{}
	if a.panicOn.Swap(false) {
		panic("test panic")
	}
	select {
	case <-a.release:
		return &agentclient.SiteBackupCreateResponse{Size: 10}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (*blockingBackupAgent) SiteBackupDownload(context.Context, string) (*http.Response, error) {
	return nil, errors.New("unused")
}
func (*blockingBackupAgent) SiteBackupRestore(context.Context, *agentclient.SiteBackupRestoreRequest) error {
	return errors.New("unused")
}
func (*blockingBackupAgent) SiteBackupRemove(context.Context, string) error { return nil }
func (*blockingBackupAgent) SSLInspectFiles(context.Context, *agentclient.SSLInspectFilesRequest) (*agentclient.SSLInspectResponse, error) {
	return nil, errors.New("unused")
}

type admissionSSLRepo struct{}

func (admissionSSLRepo) GetBySiteID(string) (*repo.SiteSSL, error) { return nil, nil }
func (admissionSSLRepo) Upsert(*repo.SiteSSL) error                { return nil }

type admissionOpRepo struct{}

func (admissionOpRepo) Create(*repo.Operation) error                             { return nil }
func (admissionOpRepo) UpdateStatus(string, string) error                        { return nil }
func (admissionOpRepo) UpdateError(string, string, string, string, string) error { return nil }

func newBackupAdmissionService(t *testing.T, root context.Context, agent agentClient) (*Service, *repo.SiteBackupRepo) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	site := &repo.Site{ID: "site-1", PrimaryDomain: "example.com", RootPath: "/var/www/example", ConfigPath: "/etc/nginx/example.conf", Status: "enabled"}
	if err := repo.NewSiteRepo(database).Create(site); err != nil {
		t.Fatal(err)
	}
	backups := repo.NewSiteBackupRepo(database)
	svc := NewService(root, 1, fakeSiteRepo{sites: map[string]*repo.Site{site.ID: site}}, backups, repo.NewSiteBackupScheduleRepo(database), admissionSSLRepo{}, admissionOpRepo{}, agent, "/panel", sse.NewHub())
	t.Cleanup(svc.Close)
	return svc, backups
}

func TestBackupAdmissionRejectsWithoutTaskOrBackupAndRecovers(t *testing.T) {
	agent := &blockingBackupAgent{started: make(chan struct{}, 2), release: make(chan struct{})}
	svc, backups := newBackupAdmissionService(t, context.Background(), agent)
	req := &CreateRequest{BackupType: "full"}
	if _, err := svc.StartCreate("site-1", req, "req-1"); err != nil {
		t.Fatal(err)
	}
	<-agent.started
	if _, err := svc.StartCreate("site-1", req, "req-2"); !backupBusy(err) {
		t.Fatalf("second admission error = %v, want BUSY", err)
	}
	owned := &repo.SiteBackup{ID: "existing", SiteID: "site-1", BackupType: "full", Name: "existing", BackupPath: "/panel/existing.tar.gz", Status: "success"}
	if err := backups.Create(owned); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartRestore("site-1", owned.ID, &RestoreRequest{}, "req-restore"); !backupBusy(err) {
		t.Fatalf("restore did not share backup admission: %v", err)
	}
	if err := svc.RunScheduled(context.Background(), "site-1", "full", "", 1, scheduledtask.RunContext{RunID: "run-1"}); !backupBusy(err) {
		t.Fatalf("scheduled backup did not share admission: %v", err)
	}
	items, err := backups.ListBySiteID("site-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || len(svc.tasks) != 1 {
		t.Fatalf("rejected admission created state: backups=%d tasks=%d", len(items), len(svc.tasks))
	}
	close(agent.release)
	svc.jobsWG.Wait()
	if _, err := svc.StartCreate("site-1", req, "req-3"); err != nil {
		t.Fatalf("slot was not recovered: %v", err)
	}
}

func TestBackupPanicReleasesSlotAndRootCancellationReachesAgent(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	agent := &blockingBackupAgent{started: make(chan struct{}, 2), release: make(chan struct{})}
	agent.panicOn.Store(true)
	svc, _ := newBackupAdmissionService(t, root, agent)
	req := &CreateRequest{BackupType: "full"}
	if _, err := svc.StartCreate("site-1", req, "req-1"); err != nil {
		t.Fatal(err)
	}
	<-agent.started
	svc.jobsWG.Wait()
	if _, err := svc.StartCreate("site-1", req, "req-2"); err != nil {
		t.Fatalf("panic did not release slot: %v", err)
	}
	<-agent.started
	cancel()
	done := make(chan struct{})
	go func() {
		svc.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("root cancellation did not stop backup job")
	}
}

func TestPruneTasksBeforeOnlyRemovesCompletedTasks(t *testing.T) {
	svc := &Service{tasks: make(map[string]*TaskResponse), hub: sse.NewHub()}
	completed := svc.newTask("backup.create")
	running := svc.newTask("backup.restore")
	svc.finishTask(completed.TaskID, "success", "done", "", nil)
	if got := svc.PruneTasksBefore(time.Now().UTC().Add(time.Second)); got != 1 {
		t.Fatalf("PruneTasksBefore() = %d, want 1", got)
	}
	if _, ok := svc.tasks[running.TaskID]; !ok {
		t.Fatal("running task was pruned")
	}
}

func backupBusy(err error) bool {
	var appErr *app.AppError
	return errors.As(err, &appErr) && appErr.Code == app.ErrBusy
}
