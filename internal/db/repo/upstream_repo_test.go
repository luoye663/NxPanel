package repo

import (
	"context"
	"strings"
	"testing"
	"time"
)

func testUpstream(id, name string) *NginxUpstream {
	now := time.Now().UTC().Format(time.RFC3339)
	return &NginxUpstream{
		ID: id, Name: name, Algorithm: "round_robin", CreatedAt: now, UpdatedAt: now,
		Servers: []*NginxUpstreamServer{{
			ID: id + "_server", UpstreamID: id, Address: "127.0.0.1:8080",
			Weight: 1, MaxFails: 1, FailTimeoutSeconds: 10, CreatedAt: now, UpdatedAt: now,
		}},
	}
}

func TestUpstreamRepoCRUDAndCaseInsensitiveUnique(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()
	store := NewUpstreamRepo(database)
	u := testUpstream("up_1", "API_Backend")
	if err := store.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if duplicate := store.Create(ctx, testUpstream("up_2", "api_backend")); duplicate == nil {
		t.Fatal("case-insensitive duplicate name should fail")
	}
	got, err := store.GetByName(ctx, "Api_Backend")
	if err != nil || got == nil || len(got.Servers) != 1 {
		t.Fatalf("get aggregate: got=%#v err=%v", got, err)
	}
	got.Name = "attempted_rename"
	got.Algorithm = "least_conn"
	got.Servers = []*NginxUpstreamServer{{
		ID: "replacement", UpstreamID: got.ID, Address: "backend.example.com:8443",
		Weight: 2, MaxFails: 0, FailTimeoutSeconds: 30, SortOrder: 2,
		CreatedAt: got.CreatedAt, UpdatedAt: got.UpdatedAt,
	}}
	if err := store.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	updated, _ := store.GetByID(ctx, got.ID)
	if updated.Name != "API_Backend" || updated.Algorithm != "least_conn" || len(updated.Servers) != 1 || updated.Servers[0].ID != "replacement" {
		t.Fatalf("unexpected updated aggregate: %#v", updated)
	}
	if refs, err := store.CountReferences(ctx, got.ID); err != nil || refs != 0 {
		t.Fatalf("phase-1 reference count=%d err=%v", refs, err)
	}
	if err := store.Delete(ctx, got.ID); err != nil {
		t.Fatal(err)
	}
	if deleted, _ := store.GetByID(ctx, got.ID); deleted != nil {
		t.Fatal("upstream should be deleted")
	}
}

func TestUpstreamRepoUpdateMembersIsTransactional(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()
	store := NewUpstreamRepo(database)
	u := testUpstream("up_tx", "transactional")
	if err := store.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	u.Algorithm = "least_conn"
	u.Servers = []*NginxUpstreamServer{
		{ID: "duplicate", UpstreamID: u.ID, Address: "127.0.0.1:9001", Weight: 1, MaxFails: 1, FailTimeoutSeconds: 10},
		{ID: "duplicate", UpstreamID: u.ID, Address: "127.0.0.1:9002", Weight: 1, MaxFails: 1, FailTimeoutSeconds: 10},
	}
	if err := store.Update(ctx, u); err == nil {
		t.Fatal("invalid member replacement should fail")
	}
	got, err := store.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Algorithm != "round_robin" || len(got.Servers) != 1 || got.Servers[0].Address != "127.0.0.1:8080" {
		t.Fatalf("failed update was not rolled back: %#v", got)
	}
}

func TestUpstreamMigrationConstraints(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	invalidParents := []struct {
		name string
		sql  string
	}{
		{"trimmed name", `INSERT INTO nginx_upstreams (id, name, algorithm) VALUES ('bad_name', ' bad ', 'round_robin')`},
		{"boolean", `INSERT INTO nginx_upstreams (id, name, algorithm, consistent) VALUES ('bad_bool', 'bad_bool', 'round_robin', 2)`},
		{"hash combination", `INSERT INTO nginx_upstreams (id, name, algorithm) VALUES ('bad_hash', 'bad_hash', 'hash')`},
		{"keepalive range", `INSERT INTO nginx_upstreams (id, name, algorithm, keepalive) VALUES ('bad_keepalive', 'bad_keepalive', 'round_robin', 10001)`},
		{"keepalive dependency", `INSERT INTO nginx_upstreams (id, name, algorithm, keepalive_requests) VALUES ('bad_keepalive_dep', 'bad_keepalive_dep', 'round_robin', 1)`},
	}
	for _, tc := range invalidParents {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := database.Exec(tc.sql); err == nil {
				t.Fatal("expected parent CHECK failure")
			}
		})
	}
	if _, err := database.Exec(`INSERT INTO nginx_upstreams (id, name, algorithm) VALUES ('valid_parent', 'valid_parent', 'round_robin')`); err != nil {
		t.Fatal(err)
	}
	invalidServers := []struct {
		name string
		sql  string
	}{
		{"empty address", `INSERT INTO nginx_upstream_servers (id, upstream_id, address) VALUES ('bad_address', 'valid_parent', '')`},
		{"weight", `INSERT INTO nginx_upstream_servers (id, upstream_id, address, weight) VALUES ('bad_weight', 'valid_parent', '127.0.0.1:80', 0)`},
		{"max fails", `INSERT INTO nginx_upstream_servers (id, upstream_id, address, max_fails) VALUES ('bad_fails', 'valid_parent', '127.0.0.1:81', 101)`},
		{"timeout", `INSERT INTO nginx_upstream_servers (id, upstream_id, address, fail_timeout_seconds) VALUES ('bad_timeout', 'valid_parent', '127.0.0.1:82', 0)`},
		{"boolean", `INSERT INTO nginx_upstream_servers (id, upstream_id, address, backup) VALUES ('bad_server_bool', 'valid_parent', '127.0.0.1:83', 2)`},
		{"backup down", `INSERT INTO nginx_upstream_servers (id, upstream_id, address, backup, down) VALUES ('bad_combo', 'valid_parent', '127.0.0.1:84', 1, 1)`},
	}
	for _, tc := range invalidServers {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := database.Exec(tc.sql); err == nil {
				t.Fatal("expected server CHECK failure")
			}
		})
	}
	if _, err := database.Exec(`UPDATE nginx_upstream_applied_state SET applied_hash = 'short' WHERE id = 1`); err == nil {
		t.Fatal("expected applied hash CHECK failure")
	}
	if _, err := database.Exec(`INSERT INTO nginx_upstream_applied_state (id) VALUES (2)`); err == nil {
		t.Fatal("expected applied state singleton CHECK failure")
	}
}

func TestUpstreamRepoJoinLoadsCompleteStableAggregates(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()
	store := NewUpstreamRepo(database)
	second := testUpstream("up_z", "zeta")
	second.Servers = append(second.Servers, &NginxUpstreamServer{
		ID: "up_z_server_2", UpstreamID: second.ID, Address: "127.0.0.1:8081",
		Weight: 1, MaxFails: 1, FailTimeoutSeconds: 10, SortOrder: -1,
		CreatedAt: second.CreatedAt, UpdatedAt: second.UpdatedAt,
	})
	if err := store.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, testUpstream("up_a", "Alpha")); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "Alpha" || items[1].Name != "zeta" || len(items[1].Servers) != 2 || items[1].Servers[0].SortOrder != -1 {
		t.Fatalf("unexpected joined aggregates: %#v", items)
	}
}

func TestUpstreamRepoRecordAppliedIsAtomic(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()
	operations := NewOperationRepo(database)
	operation := &Operation{
		ID: "op_atomic", Action: "nginx.upstream.sync", TargetType: "nginx_upstream",
		TargetID: "all", Status: "pending", Actor: "admin", CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := operations.Create(operation); err != nil {
		t.Fatal(err)
	}
	store := NewUpstreamRepo(database)
	duplicateID := "backup_duplicate"
	backups := []*Backup{
		{ID: duplicateID, FilePath: "/a", BackupPath: "/backup/a", FileExisted: true},
		{ID: duplicateID, FilePath: "/b", BackupPath: "/backup/b", FileExisted: true},
	}
	if err := store.RecordApplied(ctx, operation.ID, strings.Repeat("a", 64), "/panel/conf.d/nxpanel-upstreams.conf", "2026-08-11T00:00:00Z", backups); err == nil {
		t.Fatal("duplicate backup should fail applied transaction")
	}
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM backups WHERE operation_id = ?", operation.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial backups persisted: count=%d err=%v", count, err)
	}
	state, err := store.GetAppliedState(ctx)
	if err != nil || state.AppliedHash != "" || state.AppliedPath != "" || state.AppliedAt != nil {
		t.Fatalf("applied state changed after rollback: %#v err=%v", state, err)
	}
}
