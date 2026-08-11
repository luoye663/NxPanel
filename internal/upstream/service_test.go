package upstream

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

type fakeAgent struct {
	err      error
	requests []*agentclient.TransactionRequest
}

func (a *fakeAgent) ApplyTransaction(_ context.Context, req *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error) {
	a.requests = append(a.requests, req)
	if a.err != nil {
		return nil, a.err
	}
	return &agentclient.TransactionResponse{Backups: []agentclient.BackupRecord{{
		FilePath: req.Changes[len(req.Changes)-1].Path, BackupPath: "/backups/upstreams.conf", Existed: true,
	}}}, nil
}

func setupService(t *testing.T, agent *fakeAgent) (*Service, *repo.UpstreamRepo, *sql.DB) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	store := repo.NewUpstreamRepo(database)
	return NewService(store, repo.NewOperationRepo(database), agent, "/panel"), store, database
}

func TestServiceCreateAppliesAuditsAndRecordsState(t *testing.T) {
	agent := &fakeAgent{}
	service, store, database := setupService(t, agent)
	result, err := service.Create(context.Background(), requestForService("primary"), "req-create")
	if err != nil {
		t.Fatal(err)
	}
	if result.OperationID == "" || result.Upstream == nil || len(agent.requests) != 1 {
		t.Fatalf("unexpected result=%#v requests=%d", result, len(agent.requests))
	}
	req := agent.requests[0]
	if !req.TestNginx || !req.ReloadNginx || req.Changes[1].Path != "/panel/conf.d/nxpanel-upstreams.conf" {
		t.Fatalf("unexpected agent request: %#v", req)
	}
	content, err := base64.StdEncoding.DecodeString(req.Changes[1].ContentBase64)
	if err != nil || !strings.Contains(string(content), "upstream primary") {
		t.Fatalf("rendered content=%q err=%v", content, err)
	}
	if persisted, _ := store.GetByID(context.Background(), result.Upstream.ID); persisted == nil {
		t.Fatal("desired aggregate was not persisted")
	}
	op, err := repo.NewOperationRepo(database).GetByID(result.OperationID)
	if err != nil || op.Status != "success" || op.RequestID != "req-create" {
		t.Fatalf("operation=%#v err=%v", op, err)
	}
	backups, err := repo.NewBackupRepo(database).ListByOperationID(result.OperationID)
	if err != nil || len(backups) != 1 || backups[0].BackupPath != "/backups/upstreams.conf" {
		t.Fatalf("backups=%#v err=%v", backups, err)
	}
	state, err := store.GetAppliedState(context.Background())
	if err != nil || state.AppliedHash == "" || state.AppliedAt == nil {
		t.Fatalf("applied state=%#v err=%v", state, err)
	}
	if state.AppliedHash != hashContent(string(content)) {
		t.Fatalf("applied hash does not match Agent content: state=%s", state.AppliedHash)
	}
	status, err := service.Status(context.Background())
	if err != nil || !status.Synced || status.DesiredHash != state.AppliedHash {
		t.Fatalf("status=%#v err=%v", status, err)
	}
}

func TestServiceDeleteRejectsReferencedUpstream(t *testing.T) {
	agent := &fakeAgent{}
	service, store, database := setupService(t, agent)
	created, err := service.Create(context.Background(), requestForService("referenced"), "req-create")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO sites
		(id, primary_domain, domains_json, status, root_path, access_log_path, error_log_path, config_path, enabled_path, rewrite_path)
		VALUES ('site_ref', 'ref.example.com', '["ref.example.com"]', 'enabled', '/www/ref', '/logs/ref.access', '/logs/ref.error', '/nginx/ref.conf', '/nginx/enabled/ref.conf', '/nginx/rewrite/ref.conf')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO site_proxy
		(id, site_id, name, upstream_url, upstream_id, upstream_scheme)
		VALUES ('proxy_ref', 'site_ref', 'ref', ?, ?, 'http')`, "http://referenced", created.Upstream.ID); err != nil {
		t.Fatal(err)
	}
	beforeRequests := len(agent.requests)
	if _, err := service.Delete(context.Background(), created.Upstream.ID, "req-delete"); appErrorCodeForUpstreamTest(err) != app.ErrConflict {
		t.Fatalf("delete error=%v", err)
	}
	if len(agent.requests) != beforeRequests {
		t.Fatal("delete protection should not apply upstream configuration")
	}
	item, err := store.GetByID(context.Background(), created.Upstream.ID)
	if err != nil || item == nil || item.ReferenceCount != 1 {
		t.Fatalf("upstream=%#v err=%v", item, err)
	}
}

type upstreamFKRaceStore struct {
	*repo.UpstreamRepo
	countCalls int
}

func (s *upstreamFKRaceStore) CountReferences(ctx context.Context, id string) (int, error) {
	s.countCalls++
	if s.countCalls == 1 {
		return 0, nil
	}
	return 1, nil
}

func (*upstreamFKRaceStore) Delete(context.Context, string) error {
	return errors.New("constraint failed: FOREIGN KEY constraint failed (787)")
}

func TestServiceDeleteMapsForeignKeyRaceToConflict(t *testing.T) {
	agent := &fakeAgent{}
	service, store, database := setupService(t, agent)
	created, err := service.Create(context.Background(), requestForService("race_ref"), "req-create")
	if err != nil {
		t.Fatal(err)
	}
	service.store = &upstreamFKRaceStore{UpstreamRepo: store}
	beforeRequests := len(agent.requests)
	if _, err := service.Delete(context.Background(), created.Upstream.ID, "req-delete-race"); appErrorCodeForUpstreamTest(err) != app.ErrConflict {
		t.Fatalf("delete race error=%v", err)
	}
	if len(agent.requests) != beforeRequests {
		t.Fatal("FK race must not apply upstream configuration")
	}
	operations, _, err := repo.NewOperationRepo(database).List(1, 10, "nginx_upstream", created.Upstream.ID)
	failed := false
	for _, operation := range operations {
		failed = failed || operation.Status == "failed"
	}
	if err != nil || len(operations) < 2 || !failed {
		t.Fatalf("operations=%#v err=%v", operations, err)
	}
}

func appErrorCodeForUpstreamTest(err error) string {
	if appErr, ok := err.(*app.AppError); ok {
		return appErr.Code
	}
	return ""
}

func TestServiceAgentFailureKeepsDesiredCreateUpdateDelete(t *testing.T) {
	agent := &fakeAgent{err: errors.New("ambiguous agent failure")}
	service, store, database := setupService(t, agent)
	ctx := context.Background()
	createResult, err := service.Create(ctx, requestForService("create_fail"), "req-create-fail")
	if err == nil || createResult != nil {
		t.Fatal("create apply should fail")
	}
	assertDesiredApplyError(t, err)
	created, _ := store.GetByName(ctx, "create_fail")
	if created == nil {
		t.Fatal("failed apply must retain created desired state")
	}
	if status, err := service.Status(ctx); err != nil || status.Synced {
		t.Fatalf("failed create status=%#v err=%v", status, err)
	}

	update := requestForService("")
	update.Algorithm = "least_conn"
	update.Servers[0].Address = "127.0.0.1:9000"
	if _, err := service.Update(ctx, created.ID, update, "req-update-fail"); err == nil {
		t.Fatal("update apply should fail")
	} else {
		assertDesiredApplyError(t, err)
	}
	afterUpdate, _ := store.GetByID(ctx, created.ID)
	if afterUpdate.Algorithm != "least_conn" || afterUpdate.Servers[0].Address != "127.0.0.1:9000" {
		t.Fatalf("failed apply must retain updated desired state: %#v", afterUpdate)
	}
	if _, err := service.Delete(ctx, created.ID, "req-delete-fail"); err == nil {
		t.Fatal("delete apply should fail")
	} else {
		assertDesiredApplyError(t, err)
	}
	if afterDelete, _ := store.GetByID(ctx, created.ID); afterDelete != nil {
		t.Fatal("failed apply must retain deleted desired state")
	}

	rows, err := database.Query("SELECT status, request_id FROM operations ORDER BY created_at, id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var status, requestID string
		if err := rows.Scan(&status, &requestID); err != nil {
			t.Fatal(err)
		}
		if status != "failed" || requestID == "" {
			t.Fatalf("unexpected operation status=%s request_id=%s", status, requestID)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("expected 3 failed operations, got %d", count)
	}
}

func TestServiceSyncRepairsDriftAndPersistsAppliedHash(t *testing.T) {
	agent := &fakeAgent{err: errors.New("ambiguous agent failure")}
	service, store, database := setupService(t, agent)
	ctx := context.Background()
	if _, err := service.Create(ctx, requestForService("sync_me"), "req-create-fail"); err == nil {
		t.Fatal("create apply should fail")
	}
	before, err := service.Status(ctx)
	if err != nil || before.Synced || before.AppliedHash != "" {
		t.Fatalf("before sync=%#v err=%v", before, err)
	}
	agent.err = nil
	result, err := service.Sync(ctx, "req-sync")
	if err != nil || result.OperationID == "" {
		t.Fatalf("sync result=%#v err=%v", result, err)
	}
	after, err := service.Status(ctx)
	if err != nil || !after.Synced || after.DesiredHash == "" || after.DesiredHash != after.AppliedHash || after.AppliedAt == nil {
		t.Fatalf("after sync=%#v err=%v", after, err)
	}
	state, err := store.GetAppliedState(ctx)
	if err != nil || state.AppliedHash != after.DesiredHash || state.AppliedPath != "/panel/conf.d/nxpanel-upstreams.conf" {
		t.Fatalf("persisted state=%#v err=%v", state, err)
	}
	op, err := repo.NewOperationRepo(database).GetByID(result.OperationID)
	if err != nil || op.Status != "success" || op.RequestID != "req-sync" {
		t.Fatalf("sync operation=%#v err=%v", op, err)
	}
	restarted := NewService(store, repo.NewOperationRepo(database), agent, "/panel")
	restartedStatus, err := restarted.Status(ctx)
	if err != nil || !restartedStatus.Synced || restartedStatus.AppliedHash != after.AppliedHash {
		t.Fatalf("restart status=%#v err=%v", restartedStatus, err)
	}
	moved := NewService(store, repo.NewOperationRepo(database), agent, "/other-panel")
	movedStatus, err := moved.Status(ctx)
	if err != nil || movedStatus.Synced {
		t.Fatalf("changed target path must require sync: %#v err=%v", movedStatus, err)
	}
}

type failingRecordStore struct{ *repo.UpstreamRepo }

func (f *failingRecordStore) RecordApplied(context.Context, string, string, string, string, []*repo.Backup) error {
	return errors.New("injected applied state failure")
}

func TestServiceAppliedStateFailureReportsKnownAppliedConfig(t *testing.T) {
	agent := &fakeAgent{}
	service, store, database := setupService(t, agent)
	service.store = &failingRecordStore{UpstreamRepo: store}
	_, err := service.Create(context.Background(), requestForService("state_failure"), "req-state-failure")
	appErr, ok := err.(*app.AppError)
	if !ok || appErr.Code != app.ErrInternalError || appErr.Details["config_applied"] != true || appErr.Details["desired_saved"] != true || appErr.Details["sync_required"] != true {
		t.Fatalf("unexpected applied state error: %#v", err)
	}
	if desired, _ := store.GetByName(context.Background(), "state_failure"); desired == nil {
		t.Fatal("desired state missing")
	}
	state, stateErr := store.GetAppliedState(context.Background())
	if stateErr != nil || state.AppliedHash != "" {
		t.Fatalf("failed state write must remain unsynced: %#v err=%v", state, stateErr)
	}
	var status string
	if queryErr := database.QueryRow("SELECT status FROM operations WHERE request_id = 'req-state-failure'").Scan(&status); queryErr != nil || status != "failed" {
		t.Fatalf("operation status=%q err=%v", status, queryErr)
	}
}

type failingTerminalOperations struct {
	base        *repo.OperationRepo
	failSuccess bool
	failFailure bool
}

func (o *failingTerminalOperations) Create(operation *repo.Operation) error {
	return o.base.Create(operation)
}

func (o *failingTerminalOperations) UpdateStatus(id, status string) error {
	if o.failSuccess {
		return errors.New("injected success terminal failure")
	}
	return o.base.UpdateStatus(id, status)
}

func (o *failingTerminalOperations) UpdateError(id, status, code, message, stderr string) error {
	if o.failFailure {
		return errors.New("injected failure terminal failure")
	}
	return o.base.UpdateError(id, status, code, message, stderr)
}

func TestServiceSurfacesOperationTerminalFailures(t *testing.T) {
	t.Run("applied configuration", func(t *testing.T) {
		agent := &fakeAgent{}
		service, store, database := setupService(t, agent)
		service.operations = &failingTerminalOperations{base: repo.NewOperationRepo(database), failSuccess: true}
		_, err := service.Create(context.Background(), requestForService("terminal_success"), "req-terminal-success")
		assertOperationTerminalError(t, err, true)
		status, statusErr := service.Status(context.Background())
		if statusErr != nil || !status.Synced {
			t.Fatalf("applied state must remain synced: %#v err=%v", status, statusErr)
		}
		if desired, _ := store.GetByName(context.Background(), "terminal_success"); desired == nil {
			t.Fatal("desired state missing")
		}
	})

	t.Run("agent failure", func(t *testing.T) {
		agent := &fakeAgent{err: errors.New("agent failure")}
		service, store, database := setupService(t, agent)
		service.operations = &failingTerminalOperations{base: repo.NewOperationRepo(database), failFailure: true}
		_, err := service.Create(context.Background(), requestForService("terminal_failure"), "req-terminal-failure")
		assertOperationTerminalError(t, err, false)
		if desired, _ := store.GetByName(context.Background(), "terminal_failure"); desired == nil {
			t.Fatal("desired state missing")
		}
	})
}

func TestServiceUpdateRejectsNameChange(t *testing.T) {
	agent := &fakeAgent{}
	service, store, _ := setupService(t, agent)
	ctx := context.Background()
	req := requestForService("immutable_name")
	if err := NormalizeAndValidate(req); err != nil {
		t.Fatal(err)
	}
	item := requestToModel(req, "immutable-id", "2026-08-11T00:00:00Z")
	if err := store.Create(ctx, item); err != nil {
		t.Fatal(err)
	}
	changed := requestForService("different_name")
	if _, err := service.Update(ctx, item.ID, changed, "req-rename"); err == nil {
		t.Fatal("name change should be rejected")
	}
	if len(agent.requests) != 0 {
		t.Fatal("rejected rename must not reach Agent")
	}
}

func assertDesiredApplyError(t *testing.T, err error) {
	t.Helper()
	appErr, ok := err.(*app.AppError)
	if !ok || appErr.Details["operation_id"] == "" || appErr.Details["desired_saved"] != true || appErr.Details["sync_required"] != true || appErr.Details["config_apply_outcome"] != "unknown" {
		t.Fatalf("unexpected desired apply error: %#v", err)
	}
}

func assertOperationTerminalError(t *testing.T, err error, configApplied bool) {
	t.Helper()
	appErr, ok := err.(*app.AppError)
	if !ok || appErr.Code != app.ErrInternalError || appErr.Details["operation_update_failed"] != true {
		t.Fatalf("unexpected operation terminal error: %#v", err)
	}
	if configApplied && appErr.Details["config_applied"] != true {
		t.Fatalf("expected known applied outcome: %#v", err)
	}
	if !configApplied && appErr.Details["config_apply_outcome"] != "unknown" {
		t.Fatalf("expected unknown apply outcome: %#v", err)
	}
}

func requestForService(name string) *SaveRequest {
	return &SaveRequest{
		Name: name, Algorithm: "round_robin",
		Servers: []ServerRequest{{Address: "127.0.0.1:8080", Weight: 1, MaxFails: 1, FailTimeoutSeconds: 10}},
	}
}
