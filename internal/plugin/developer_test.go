package plugin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	paneldb "github.com/luoye663/nxpanel/internal/db"
)

type developerTestCatalog struct{ ids map[string]bool }

func (c developerTestCatalog) List(context.Context) ([]CatalogEntry, error) {
	items := make([]CatalogEntry, 0, len(c.ids))
	for id := range c.ids {
		items = append(items, CatalogEntry{ID: id})
	}
	return items, nil
}
func (c developerTestCatalog) Resolve(context.Context, string, string) (CatalogEntry, error) {
	return CatalogEntry{}, errors.New("unused")
}
func (c developerTestCatalog) Fetch(context.Context, CatalogEntry, string) error {
	return errors.New("unused")
}

type developerTestRuntime struct{}

func (developerTestRuntime) Validate(context.Context, *Manifest, string) error { return nil }
func (developerTestRuntime) Enable(context.Context, *Manifest, string) error   { return nil }
func (developerTestRuntime) Disable(context.Context, string) error             { return nil }
func (developerTestRuntime) Close(context.Context) error                       { return nil }

func newPluginTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := paneldb.Open(paneldb.DSNFromPath(filepath.Join(t.TempDir(), "plugin.db"), 1000))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := paneldb.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	return database
}

func developerPackage(t *testing.T, id string) string {
	t.Helper()
	sum := sha256.Sum256(healthyWASM)
	manifest := Manifest{SchemaVersion: 1, ID: id, Name: "Developer Test", Version: "1.0.0", Publisher: "Local developer", PluginAPIVersion: "v1", Backend: "plugin.wasm", Permissions: []string{"plugin.kv"}, Files: []FileDigest{{Path: "plugin.wasm", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(healthyWASM))}}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "developer.nxp")
	writeTestArchive(t, path, map[string][]byte{"manifest.json": raw, "plugin.wasm": healthyWASM})
	return path
}

func TestDeveloperInstallIsTwoStageSingleUseAndModeGated(t *testing.T) {
	database, dataDir := newPluginTestDB(t), t.TempDir()
	service := NewService(database, dataDir, developerTestCatalog{}, developerTestRuntime{})
	ctx := context.Background()
	packagePath := developerPackage(t, "com.example.developer")
	f, _ := os.Open(packagePath)
	if _, err := service.InspectDeveloperPackage(ctx, filepath.Base(packagePath), f); !errors.Is(err, ErrDeveloperModeDisabled) {
		t.Fatalf("inspect while disabled: %v", err)
	}
	_ = f.Close()
	if err := service.SetDeveloperMode(ctx, true, false); !errors.Is(err, ErrRiskAcknowledgementRequired) {
		t.Fatalf("enable without acknowledgement: %v", err)
	}
	if err := service.SetDeveloperMode(ctx, true, true); err != nil {
		t.Fatal(err)
	}
	f, _ = os.Open(packagePath)
	inspection, err := service.InspectDeveloperPackage(ctx, filepath.Base(packagePath), f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	installed, err := service.InstallDeveloperPackage(ctx, inspection.UploadToken, []string{"plugin.kv"})
	if err != nil {
		t.Fatal(err)
	}
	if installed.Source != "developer" || installed.VerificationStatus != "developer_unverified" {
		t.Fatalf("unexpected trust fields: %+v", installed)
	}
	if _, err := service.InstallDeveloperPackage(ctx, inspection.UploadToken, []string{"plugin.kv"}); !errors.Is(err, ErrDeveloperTokenInvalid) {
		t.Fatalf("token reuse: %v", err)
	}
	if _, err := service.Enable(ctx, installed.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.SetDeveloperMode(ctx, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Enable(ctx, installed.ID); err != nil {
		t.Fatalf("already-enabled plugin must continue: %v", err)
	}
	if _, err := service.Disable(ctx, installed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Enable(ctx, installed.ID); !errors.Is(err, ErrDeveloperModeDisabled) {
		t.Fatalf("re-enable while mode off: %v", err)
	}
	if err := service.Delete(ctx, installed.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDeveloperInstallRejectsReservedAndOfficialIDs(t *testing.T) {
	database, dataDir := newPluginTestDB(t), t.TempDir()
	service := NewService(database, dataDir, developerTestCatalog{ids: map[string]bool{"com.example.official": true}}, developerTestRuntime{})
	if err := service.SetDeveloperMode(context.Background(), true, true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"org.nxpanel.local", "com.example.official"} {
		f, _ := os.Open(developerPackage(t, id))
		_, err := service.InspectDeveloperPackage(context.Background(), "plugin.nxp", f)
		_ = f.Close()
		if !errors.Is(err, ErrOfficialIDReserved) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
}
