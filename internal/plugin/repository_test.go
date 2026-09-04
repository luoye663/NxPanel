package plugin

import (
	"context"
	"path/filepath"
	"testing"

	paneldb "github.com/luoye663/nxpanel/internal/db"
)

func TestRepositoryLifecycle(t *testing.T) {
	database, err := paneldb.Open(paneldb.DSNFromPath(filepath.Join(t.TempDir(), "test.db"), 1000))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := paneldb.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(database)
	manifest := &Manifest{SchemaVersion: 1, ID: "org.nxpanel.test", Name: "Test", Version: "1.0.0", Publisher: "nxPanel", PluginAPIVersion: "v1", Backend: "plugin.wasm", Permissions: []string{"plugin.kv"}, Files: []FileDigest{{Path: "plugin.wasm", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 8}}}
	ctx := context.Background()
	if err := repository.Activate(ctx, manifest, "package-hash", "/plugins/test", manifest.Permissions); err != nil {
		t.Fatal(err)
	}
	item, err := repository.Get(ctx, manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Enabled || len(item.Permissions) != 1 || item.Manifest.Version != manifest.Version {
		t.Fatalf("unexpected installation: %+v", item)
	}
	if err := repository.SetEnabled(ctx, manifest.ID, true); err != nil {
		t.Fatal(err)
	}
	items, err := repository.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Enabled {
		t.Fatalf("unexpected list: %+v", items)
	}
	if err := repository.Delete(ctx, manifest.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(ctx, manifest.ID); err != ErrPluginNotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}
