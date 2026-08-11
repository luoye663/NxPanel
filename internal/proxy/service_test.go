package proxy

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
)

type proxyTestAgent struct {
	mu           sync.Mutex
	config       []byte
	readErr      error
	applyErr     error
	nilResponse  bool
	applyDelay   time.Duration
	readPaths    []string
	removedPaths []string
	transactions []*agentclient.TransactionRequest
	activeApply  int
	maxApply     int
}

func (a *proxyTestAgent) ReadFile(_ context.Context, path string) ([]byte, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.readPaths = append(a.readPaths, path)
	if a.readErr != nil {
		return nil, "", a.readErr
	}
	if strings.HasSuffix(path, ".conf") {
		return a.config, "", nil
	}
	return []byte("test CA"), "", nil
}

func (a *proxyTestAgent) ApplyTransaction(_ context.Context, req *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error) {
	a.mu.Lock()
	a.transactions = append(a.transactions, req)
	a.activeApply++
	if a.activeApply > a.maxApply {
		a.maxApply = a.activeApply
	}
	delay, applyErr, nilResponse := a.applyDelay, a.applyErr, a.nilResponse
	a.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	a.mu.Lock()
	a.activeApply--
	a.mu.Unlock()
	if applyErr != nil {
		return nil, applyErr
	}
	if nilResponse {
		return nil, nil
	}
	return &agentclient.TransactionResponse{Backups: []agentclient.BackupRecord{{
		FilePath: req.Changes[0].Path, BackupPath: "/backups/site.conf", Existed: true,
	}}}, nil
}

func (a *proxyTestAgent) FilesRemove(_ context.Context, paths []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.removedPaths = append(a.removedPaths, paths...)
	return nil
}
func (*proxyTestAgent) FilesChown(context.Context, string, string, string, bool) error { return nil }
func (*proxyTestAgent) FilesChmod(context.Context, string, string, bool) error         { return nil }

func setupProxyService(t *testing.T, agent *proxyTestAgent) (*Service, *repo.UpstreamRepo, *repo.ProxyRepo, *repo.OperationRepo, *repo.BackupRepo) {
	t.Helper()
	if err := nginx.InitTemplates("../../configs/templates"); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	upstreams := repo.NewUpstreamRepo(database)
	proxies := repo.NewProxyRepo(database)
	operations := repo.NewOperationRepo(database)
	backups := repo.NewBackupRepo(database)
	service := NewService(repo.NewSiteRepo(database), proxies, upstreams, repo.NewAuthAccountRepo(database),
		operations, backups, agent, &app.Config{Nginx: app.NginxConfig{PanelDir: "/panel", WebUser: "nginx"}})
	return service, upstreams, proxies, operations, backups
}

func createManagedUpstream(t *testing.T, store *repo.UpstreamRepo, id, name string) {
	t.Helper()
	now := "2026-08-11T00:00:00Z"
	err := store.Create(context.Background(), &repo.NginxUpstream{
		ID: id, Name: name, Algorithm: "round_robin", CreatedAt: now, UpdatedAt: now,
		Servers: []*repo.NginxUpstreamServer{{
			ID: id + "_server", UpstreamID: id, Address: "127.0.0.1:8080", Weight: 1,
			MaxFails: 1, FailTimeoutSeconds: 10, CreatedAt: now, UpdatedAt: now,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func boolPointer(value bool) *bool { return &value }

func createProxyServiceSite(t *testing.T, service *Service, id string) {
	t.Helper()
	if err := service.siteRepo.Create(&repo.Site{
		ID: id, PrimaryDomain: id + ".example.com", DomainsJSON: `["` + id + `.example.com"]`, Status: "enabled",
		HTTPPort: 80, HTTPSPort: 443, RootPath: "/www/" + id, IndexFiles: "index.html",
		AccessLogPath: "/logs/" + id + ".access.log", ErrorLogPath: "/logs/" + id + ".error.log",
		ConfigPath: "/panel/sites/" + id + ".conf", EnabledPath: "/panel/enabled/" + id + ".conf", RewritePath: "/panel/rewrite/" + id + ".conf",
	}); err != nil {
		t.Fatal(err)
	}
}

func directCreateRequest(name, location string) *CreateProxyRequest {
	return &CreateProxyRequest{
		Name: name, Enabled: true, LocationPath: location, UpstreamURL: "http://service_api:8080",
		HostHeader: "$host", ConnectTimeout: 60, SendTimeout: 60, ReadTimeout: 60,
	}
}

func TestResolveTargetDirectAndManagedHTTP(t *testing.T) {
	agent := &proxyTestAgent{}
	service, upstreams, _, _, _ := setupProxyService(t, agent)
	createManagedUpstream(t, upstreams, "up_http", "immutable_backend")
	direct, err := service.resolveTarget(context.Background(), nil, "https", "https://backend.example.com:8443/api", "", nil, "", 0)
	if err != nil || direct.url != "https://backend.example.com:8443/api" || direct.id != nil || direct.verify {
		t.Fatalf("direct=%#v err=%v", direct, err)
	}
	id := "up_http"
	managed, err := service.resolveTarget(context.Background(), &id, "http", "", "bad;$name", boolPointer(true), "/ca.pem", 9)
	if err != nil || managed.url != "http://immutable_backend" || managed.id == nil || managed.serverName != "" || managed.verify || managed.trustedCertificate != "" || managed.verifyDepth != 0 {
		t.Fatalf("managed=%#v err=%v", managed, err)
	}
	if _, err := service.resolveTarget(context.Background(), &id, "http", "http://forged", "", nil, "", 0); appErrorCode(err) != app.ErrValidationFailed {
		t.Fatalf("forged URL error=%v", err)
	}
	for _, tc := range []struct {
		name       string
		serverName string
		verify     *bool
		ca         string
		depth      int
	}{
		{name: "server name", serverName: "origin.example.com"},
		{name: "verify", verify: boolPointer(false)},
		{name: "CA", ca: "/etc/ssl/ca.pem"},
		{name: "depth", depth: 3},
	} {
		t.Run("direct rejects "+tc.name, func(t *testing.T) {
			if _, err := service.resolveTarget(context.Background(), nil, "http", "http://service_api:8080", tc.serverName, tc.verify, tc.ca, tc.depth); appErrorCode(err) != app.ErrValidationFailed {
				t.Fatalf("direct TLS field error=%v", err)
			}
		})
	}
	missing := "missing"
	if _, err := service.resolveTarget(context.Background(), &missing, "http", "", "", nil, "", 0); appErrorCode(err) != app.ErrValidationFailed {
		t.Fatalf("missing upstream error=%v", err)
	}
}

func TestResolveTargetManagedHTTPSValidationAndCAAccess(t *testing.T) {
	agent := &proxyTestAgent{}
	service, upstreams, _, _, _ := setupProxyService(t, agent)
	createManagedUpstream(t, upstreams, "up_https", "secure_backend")
	id := "up_https"
	target, err := service.resolveTarget(context.Background(), &id, "https", "", "origin.example.com", nil, "/etc/ssl/certs/origin-ca.pem", 0)
	if err != nil || target.url != "https://secure_backend" || !target.verify || target.verifyDepth != 3 || len(agent.readPaths) != 1 {
		t.Fatalf("target=%#v reads=%v err=%v", target, agent.readPaths, err)
	}
	off, err := service.resolveTarget(context.Background(), &id, "https", "", "192.0.2.10", boolPointer(false), "/ignored.pem", 20)
	if err != nil || off.verify || off.trustedCertificate != "" || off.verifyDepth != 0 || len(agent.readPaths) != 1 {
		t.Fatalf("verify off=%#v reads=%v err=%v", off, agent.readPaths, err)
	}
	invalidNames := []string{"$proxy_host", "origin.example.com:443", "origin;bad", "origin example"}
	for _, name := range invalidNames {
		if _, err := service.resolveTarget(context.Background(), &id, "https", "", name, nil, "/ca.pem", 3); appErrorCode(err) != app.ErrValidationFailed {
			t.Fatalf("SNI %q error=%v", name, err)
		}
	}
	if _, err := service.resolveTarget(context.Background(), &id, "https", "", "origin.example.com", nil, "relative-ca.pem", 3); appErrorCode(err) != app.ErrValidationFailed {
		t.Fatalf("relative CA error=%v", err)
	}
	agent.readErr = errors.New("读取文件失败: 路径不在白名单内")
	if _, err := service.resolveTarget(context.Background(), &id, "https", "", "origin.example.com", nil, "/outside/ca.pem", 3); appErrorCode(err) != app.ErrAgentDenied {
		t.Fatalf("denied CA error=%v", err)
	}
}

func TestResolveTargetRejectsCAUnderWebsiteRootsByDirectoryBoundary(t *testing.T) {
	agent := &proxyTestAgent{}
	service, upstreams, _, _, _ := setupProxyService(t, agent)
	service.dangerousCARoots = append(service.dangerousCARoots, "/srv/websites")
	createManagedUpstream(t, upstreams, "up_ca", "ca_backend")
	id := "up_ca"
	for _, path := range []string{
		"/www/wwwroot/site/ca.pem",
		"/var/www/site/ca.pem",
		"/srv/websites/site/ca.pem",
	} {
		if _, err := service.resolveTarget(context.Background(), &id, "https", "", "origin.example.com", nil, path, 3); appErrorCode(err) != app.ErrValidationFailed {
			t.Fatalf("website CA %q error=%v", path, err)
		}
	}
	for _, path := range []string{"/www/wwwroot-other/ca.pem", "/srv/websites-other/ca.pem", "/etc/ssl/certs/ca.pem"} {
		if _, err := service.resolveTarget(context.Background(), &id, "https", "", "origin.example.com", nil, path, 3); err != nil {
			t.Fatalf("controlled CA %q rejected: %v", path, err)
		}
	}
}

func TestCreateManagedProxyGeneratesURLAndRecordsOperationBackup(t *testing.T) {
	agent := &proxyTestAgent{}
	service, upstreams, proxies, operations, backups := setupProxyService(t, agent)
	createManagedUpstream(t, upstreams, "up_create", "create_backend")
	agent.config = []byte("server {\n    #NXPANEL-DOCUMENT-START\n    #NXPANEL-DOCUMENT-END\n}\n# user-custom-content\n")
	createProxyServiceSite(t, service, "site_create")
	id := "up_create"
	response, operationID, err := service.Create(context.Background(), "site_create", &CreateProxyRequest{
		Name: "managed", Enabled: true, LocationPath: "/", UpstreamID: &id, UpstreamScheme: "http",
		HostHeader: "$host", ConnectTimeout: 60, SendTimeout: 60, ReadTimeout: 60,
	}, "req-proxy-create")
	if err != nil {
		t.Fatal(err)
	}
	if response.UpstreamURL != "http://create_backend" || response.UpstreamID == nil || operationID == "" {
		t.Fatalf("response=%#v operation=%q", response, operationID)
	}
	persisted, err := proxies.GetByID(response.ID)
	if err != nil || persisted.UpstreamURL != "http://create_backend" {
		t.Fatalf("persisted=%#v err=%v", persisted, err)
	}
	operation, err := operations.GetByID(operationID)
	if err != nil || operation.RequestID != "req-proxy-create" || operation.Status != "success" {
		t.Fatalf("operation=%#v err=%v", operation, err)
	}
	recorded, err := backups.ListByOperationID(operationID)
	if err != nil || len(recorded) != 1 || recorded[0].BackupPath != "/backups/site.conf" {
		t.Fatalf("backups=%#v err=%v", recorded, err)
	}
	if len(agent.transactions) != 1 {
		t.Fatalf("transactions=%d", len(agent.transactions))
	}
	patched, err := base64.StdEncoding.DecodeString(agent.transactions[0].Changes[0].ContentBase64)
	if err != nil || !strings.Contains(string(patched), "# user-custom-content") || !strings.Contains(string(patched), "proxy_pass http://create_backend;") {
		t.Fatalf("patched config=%q err=%v", patched, err)
	}
}

func TestCreateApplyFailuresKeepDesiredAndMarkOperationFailed(t *testing.T) {
	tests := []struct {
		name        string
		agentError  error
		nilResponse bool
		wantOutcome string
		wantApplied bool
	}{
		{name: "agent error", agentError: errors.New("agent unavailable"), wantOutcome: "unknown"},
		{name: "nil response", nilResponse: true, wantApplied: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent := &proxyTestAgent{config: []byte("server {\n    #NXPANEL-DOCUMENT-START\n    #NXPANEL-DOCUMENT-END\n}\n"), applyErr: tc.agentError, nilResponse: tc.nilResponse}
			service, _, proxies, operations, _ := setupProxyService(t, agent)
			createProxyServiceSite(t, service, "site_failure")
			_, _, err := service.Create(context.Background(), "site_failure", directCreateRequest("failure", "/"), "req-failure")
			appErr, ok := err.(*app.AppError)
			if !ok || appErr.Details["desired_saved"] != true || appErr.Details["sync_required"] != true {
				t.Fatalf("error=%#v", err)
			}
			if tc.wantApplied {
				if appErr.Details["config_applied"] != true {
					t.Fatalf("applied details=%#v", appErr.Details)
				}
			} else if appErr.Details["config_apply_outcome"] != tc.wantOutcome {
				t.Fatalf("outcome details=%#v", appErr.Details)
			}
			items, listErr := proxies.ListBySiteID("site_failure")
			if listErr != nil || len(items) != 1 {
				t.Fatalf("desired proxies=%#v err=%v", items, listErr)
			}
			opID, _ := appErr.Details["operation_id"].(string)
			operation, operationErr := operations.GetByID(opID)
			if operationErr != nil || operation == nil || operation.Status != "failed" {
				t.Fatalf("operation=%#v err=%v", operation, operationErr)
			}
		})
	}
}

type failingBackupStore struct {
	err       error
	ctxUsable bool
}

func (s *failingBackupStore) CreateMany(ctx context.Context, _ []*repo.Backup) error {
	s.ctxUsable = ctx.Err() == nil
	return s.err
}

func TestCreateBackupFailureReportsAppliedAndUsesDetachedContext(t *testing.T) {
	agent := &proxyTestAgent{config: []byte("server {\n    #NXPANEL-DOCUMENT-START\n    #NXPANEL-DOCUMENT-END\n}\n")}
	service, _, proxies, operations, _ := setupProxyService(t, agent)
	createProxyServiceSite(t, service, "site_backup_failure")
	backupStore := &failingBackupStore{err: errors.New("backup persistence failed")}
	service.backupRepo = backupStore
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := service.Create(ctx, "site_backup_failure", directCreateRequest("backup-failure", "/"), "req-backup-failure")
	appErr, ok := err.(*app.AppError)
	if !ok || appErr.Details["config_applied"] != true || appErr.Details["desired_saved"] != true || appErr.Details["sync_required"] != true || !backupStore.ctxUsable {
		t.Fatalf("error=%#v detached=%v", err, backupStore.ctxUsable)
	}
	items, listErr := proxies.ListBySiteID("site_backup_failure")
	if listErr != nil || len(items) != 1 {
		t.Fatalf("desired proxies=%#v err=%v", items, listErr)
	}
	opID, _ := appErr.Details["operation_id"].(string)
	operation, operationErr := operations.GetByID(opID)
	if operationErr != nil || operation == nil || operation.Status != "failed" {
		t.Fatalf("operation=%#v err=%v", operation, operationErr)
	}
}

func TestUpdateApplyFailureKeepsUpdatedDesired(t *testing.T) {
	agent := &proxyTestAgent{config: []byte("server {\n    #NXPANEL-DOCUMENT-START\n    #NXPANEL-DOCUMENT-END\n}\n")}
	service, _, proxies, operations, _ := setupProxyService(t, agent)
	createProxyServiceSite(t, service, "site_update_failure")
	created, _, err := service.Create(context.Background(), "site_update_failure", directCreateRequest("before", "/"), "req-create")
	if err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	agent.applyErr = errors.New("update apply failed")
	agent.mu.Unlock()
	_, _, err = service.Update(context.Background(), "site_update_failure", created.ID, &UpdateProxyRequest{
		Name: "after", Enabled: true, LocationPath: "/updated", UpstreamURL: "http://service_api:9090",
		HostHeader: "$host", ConnectTimeout: 60, SendTimeout: 60, ReadTimeout: 60,
	}, "req-update")
	appErr, ok := err.(*app.AppError)
	if !ok || appErr.Details["desired_saved"] != true || appErr.Details["sync_required"] != true || appErr.Details["config_apply_outcome"] != "unknown" {
		t.Fatalf("update error=%#v", err)
	}
	persisted, getErr := proxies.GetByID(created.ID)
	if getErr != nil || persisted == nil || persisted.Name != "after" || persisted.LocationPath != "/updated" || persisted.UpstreamURL != "http://service_api:9090" {
		t.Fatalf("updated desired=%#v err=%v", persisted, getErr)
	}
	opID, _ := appErr.Details["operation_id"].(string)
	operation, operationErr := operations.GetByID(opID)
	if operationErr != nil || operation == nil || operation.Status != "failed" {
		t.Fatalf("operation=%#v err=%v", operation, operationErr)
	}
}

func TestDeletePersistsDesiredBeforeApplyAndDefersCleanup(t *testing.T) {
	agent := &proxyTestAgent{config: []byte("server {\n    #NXPANEL-DOCUMENT-START\n    #NXPANEL-DOCUMENT-END\n}\n")}
	service, _, proxies, operations, _ := setupProxyService(t, agent)
	createProxyServiceSite(t, service, "site_delete")
	created, _, err := service.Create(context.Background(), "site_delete", directCreateRequest("delete-me", "/"), "req-create")
	if err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	agent.applyErr = errors.New("apply delete failed")
	agent.removedPaths = nil
	agent.mu.Unlock()
	_, err = service.Delete(context.Background(), "site_delete", created.ID, "req-delete")
	appErr, ok := err.(*app.AppError)
	if !ok || appErr.Details["desired_saved"] != true || appErr.Details["sync_required"] != true || appErr.Details["config_apply_outcome"] != "unknown" {
		t.Fatalf("delete error=%#v", err)
	}
	if deleted, getErr := proxies.GetByID(created.ID); getErr != nil || deleted != nil {
		t.Fatalf("deleted desired=%#v err=%v", deleted, getErr)
	}
	agent.mu.Lock()
	removed := append([]string(nil), agent.removedPaths...)
	agent.mu.Unlock()
	if len(removed) != 0 {
		t.Fatalf("cleanup ran before applied config: %v", removed)
	}
	opID, _ := appErr.Details["operation_id"].(string)
	operation, operationErr := operations.GetByID(opID)
	if operationErr != nil || operation == nil || operation.Status != "failed" {
		t.Fatalf("operation=%#v err=%v", operation, operationErr)
	}
}

func TestSyncRepairsDeleteUnknownFromRemainingDesiredState(t *testing.T) {
	agent := &proxyTestAgent{config: []byte("server {\n    #NXPANEL-DOCUMENT-START\n    #NXPANEL-DOCUMENT-END\n}\n")}
	service, _, proxies, operations, _ := setupProxyService(t, agent)
	createProxyServiceSite(t, service, "site_sync_delete")
	firstRequest := directCreateRequest("first", "/")
	firstRequest.UpstreamURL = "http://service_api:8081"
	first, _, err := service.Create(context.Background(), "site_sync_delete", firstRequest, "req-first")
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := directCreateRequest("second", "/api")
	secondRequest.UpstreamURL = "http://service_api:8082"
	second, _, err := service.Create(context.Background(), "site_sync_delete", secondRequest, "req-second")
	if err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	agent.applyErr = errors.New("delete outcome unknown")
	agent.mu.Unlock()
	if _, err := service.Delete(context.Background(), "site_sync_delete", first.ID, "req-delete"); err == nil {
		t.Fatal("delete apply should fail")
	}
	if deleted, err := proxies.GetByID(first.ID); err != nil || deleted != nil {
		t.Fatalf("deleted desired=%#v err=%v", deleted, err)
	}
	if remaining, err := proxies.GetByID(second.ID); err != nil || remaining == nil {
		t.Fatalf("remaining desired=%#v err=%v", remaining, err)
	}
	agent.mu.Lock()
	agent.applyErr = nil
	agent.mu.Unlock()
	opID, err := service.Sync(context.Background(), "site_sync_delete", "req-sync")
	if err != nil || opID == "" {
		t.Fatalf("sync operation=%q err=%v", opID, err)
	}
	agent.mu.Lock()
	lastRequest := agent.transactions[len(agent.transactions)-1]
	agent.mu.Unlock()
	content, err := base64.StdEncoding.DecodeString(lastRequest.Changes[0].ContentBase64)
	if err != nil || !strings.Contains(string(content), "proxy_pass http://service_api:8082;") || strings.Contains(string(content), "proxy_pass http://service_api:8081;") {
		t.Fatalf("synced config=%q err=%v", content, err)
	}
	operation, err := operations.GetByID(opID)
	if err != nil || operation == nil || operation.Action != "proxy.sync" || operation.RequestID != "req-sync" || operation.Status != "success" {
		t.Fatalf("operation=%#v err=%v", operation, err)
	}
}

func TestSyncRebuildsAuthHtpasswdFiles(t *testing.T) {
	agent := &proxyTestAgent{config: []byte("server {\n    #NXPANEL-DOCUMENT-START\n    #NXPANEL-DOCUMENT-END\n}\n")}
	service, _, _, _, _ := setupProxyService(t, agent)
	createProxyServiceSite(t, service, "site_sync_auth")
	account := &repo.AuthAccount{
		ID: "account_sync", Scope: "site", SiteID: "site_sync_auth", Username: "sync-user",
		PasswordHash: "sync-user:{SHA}testhash", Enabled: true,
	}
	if err := service.accountRepo.Create(account); err != nil {
		t.Fatal(err)
	}
	request := directCreateRequest("auth", "/private")
	request.AuthEnabled = true
	request.AuthAccountIDs = []string{account.ID}
	created, _, err := service.Create(context.Background(), "site_sync_auth", request, "req-create-auth")
	if err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	agent.transactions = nil
	agent.mu.Unlock()
	if _, err := service.Sync(context.Background(), "site_sync_auth", "req-sync-auth"); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	lastRequest := agent.transactions[len(agent.transactions)-1]
	agent.mu.Unlock()
	expectedPath := proxyHtpasswdPath(service.panelDir, created.ID)
	found := false
	for _, change := range lastRequest.Changes {
		if change.Type != "write" || change.Path != expectedPath {
			continue
		}
		content, decodeErr := base64.StdEncoding.DecodeString(change.ContentBase64)
		if decodeErr != nil || string(content) != account.PasswordHash+"\n" {
			t.Fatalf("htpasswd content=%q err=%v", content, decodeErr)
		}
		found = true
	}
	if !found {
		t.Fatalf("sync transaction omitted htpasswd path %q: %#v", expectedPath, lastRequest.Changes)
	}
}

func TestValidateProxyTimeoutDefaultsAndRange(t *testing.T) {
	create := &CreateProxyRequest{Name: "defaults", LocationPath: "/", UpstreamURL: "http://service_api:8080", HostHeader: "$host"}
	if err := validateCreateRequest(create); err != nil {
		t.Fatal(err)
	}
	if create.ConnectTimeout != 60 || create.SendTimeout != 60 || create.ReadTimeout != 60 {
		t.Fatalf("create defaults=%d/%d/%d", create.ConnectTimeout, create.SendTimeout, create.ReadTimeout)
	}
	update := &UpdateProxyRequest{Name: "defaults", LocationPath: "/", UpstreamURL: "http://service_api:8080", HostHeader: "$host"}
	if err := validateUpdateRequest(update); err != nil {
		t.Fatal(err)
	}
	if update.ConnectTimeout != 60 || update.SendTimeout != 60 || update.ReadTimeout != 60 {
		t.Fatalf("update defaults=%d/%d/%d", update.ConnectTimeout, update.SendTimeout, update.ReadTimeout)
	}
	invalid := []*CreateProxyRequest{
		{Name: "negative", LocationPath: "/", HostHeader: "$host", ConnectTimeout: -1, SendTimeout: 60, ReadTimeout: 60},
		{Name: "too-large", LocationPath: "/", HostHeader: "$host", ConnectTimeout: 60, SendTimeout: 3601, ReadTimeout: 60},
	}
	for _, request := range invalid {
		if err := validateCreateRequest(request); err == nil || err.Code != app.ErrValidationFailed {
			t.Fatalf("invalid timeouts accepted: %#v err=%v", request, err)
		}
	}
}

func TestProxyWritesAreSerialized(t *testing.T) {
	agent := &proxyTestAgent{
		config:     []byte("server {\n    #NXPANEL-DOCUMENT-START\n    #NXPANEL-DOCUMENT-END\n}\n"),
		applyDelay: 40 * time.Millisecond,
	}
	service, _, _, _, _ := setupProxyService(t, agent)
	createProxyServiceSite(t, service, "site_serial")
	requests := []*CreateProxyRequest{directCreateRequest("one", "/one"), directCreateRequest("two", "/two")}
	errCh := make(chan error, len(requests))
	var wg sync.WaitGroup
	for _, request := range requests {
		request := request
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := service.Create(context.Background(), "site_serial", request, "req-serial")
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	agent.mu.Lock()
	maxApply := agent.maxApply
	agent.mu.Unlock()
	if maxApply != 1 {
		t.Fatalf("concurrent ApplyTransaction calls=%d", maxApply)
	}
}

type terminalFailureOperationStore struct {
	statusErr error
	errorErr  error
}

func (*terminalFailureOperationStore) Create(*repo.Operation) error { return nil }
func (s *terminalFailureOperationStore) UpdateErrorContext(context.Context, string, string, string, string, string) error {
	return s.errorErr
}
func (s *terminalFailureOperationStore) UpdateStatusContext(context.Context, string, string) error {
	return s.statusErr
}

type successfulBackupStore struct{}

func (*successfulBackupStore) CreateMany(context.Context, []*repo.Backup) error { return nil }

func TestSuccessOperationTerminalFailureIsReturned(t *testing.T) {
	agent := &proxyTestAgent{config: []byte("server {\n    #NXPANEL-DOCUMENT-START\n    #NXPANEL-DOCUMENT-END\n}\n")}
	service, _, proxies, _, _ := setupProxyService(t, agent)
	createProxyServiceSite(t, service, "site_terminal")
	service.opRepo = &terminalFailureOperationStore{statusErr: errors.New("terminal update failed")}
	service.backupRepo = &successfulBackupStore{}
	_, _, err := service.Create(context.Background(), "site_terminal", directCreateRequest("terminal", "/"), "req-terminal")
	appErr, ok := err.(*app.AppError)
	if !ok || appErr.Details["operation_update_failed"] != true || appErr.Details["config_applied"] != true || appErr.Details["desired_saved"] != true {
		t.Fatalf("terminal error=%#v", err)
	}
	items, listErr := proxies.ListBySiteID("site_terminal")
	if listErr != nil || len(items) != 1 {
		t.Fatalf("desired proxies=%#v err=%v", items, listErr)
	}
}

func TestFailureOperationTerminalFailureIsReturned(t *testing.T) {
	agent := &proxyTestAgent{
		config:   []byte("server {\n    #NXPANEL-DOCUMENT-START\n    #NXPANEL-DOCUMENT-END\n}\n"),
		applyErr: errors.New("apply failed"),
	}
	service, _, proxies, _, _ := setupProxyService(t, agent)
	createProxyServiceSite(t, service, "site_failure_terminal")
	service.opRepo = &terminalFailureOperationStore{errorErr: errors.New("failed terminal update")}
	_, _, err := service.Create(context.Background(), "site_failure_terminal", directCreateRequest("terminal", "/"), "req-terminal")
	appErr, ok := err.(*app.AppError)
	if !ok || appErr.Details["operation_update_failed"] != true || appErr.Details["config_apply_outcome"] != "unknown" || appErr.Details["desired_saved"] != true || appErr.Details["sync_required"] != true {
		t.Fatalf("terminal error=%#v", err)
	}
	items, listErr := proxies.ListBySiteID("site_failure_terminal")
	if listErr != nil || len(items) != 1 {
		t.Fatalf("desired proxies=%#v err=%v", items, listErr)
	}
}

func appErrorCode(err error) string {
	if appErr, ok := err.(*app.AppError); ok {
		return appErr.Code
	}
	return ""
}
