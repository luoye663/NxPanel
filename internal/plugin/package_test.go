package plugin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyPackage(t *testing.T) {
	sum := sha256.Sum256(healthyWASM)
	m := Manifest{SchemaVersion: 1, ID: "org.nxpanel.test", Name: "Test", Version: "1.0.0", Publisher: "nxPanel", PluginAPIVersion: "v1", Backend: "backend/plugin.wasm", Files: []FileDigest{{Path: "backend/plugin.wasm", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(healthyWASM))}}}
	manifest, _ := json.Marshal(m)
	pkg := filepath.Join(t.TempDir(), "test.nxp")
	writeTestArchive(t, pkg, map[string][]byte{"manifest.json": manifest, "backend/plugin.wasm": healthyWASM})
	got, err := VerifyPackage(context.Background(), pkg, "", filepath.Join(t.TempDir(), "out"), DefaultPackageLimits)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != m.ID {
		t.Fatalf("got id %q", got.ID)
	}
}

func TestVerifyPackageRejectsTraversal(t *testing.T) {
	pkg := filepath.Join(t.TempDir(), "bad.nxp")
	writeTestArchive(t, pkg, map[string][]byte{"../escape": []byte("bad")})
	if _, err := VerifyPackage(context.Background(), pkg, "", filepath.Join(t.TempDir(), "out"), DefaultPackageLimits); err == nil {
		t.Fatal("traversal entry accepted")
	}
}

func writeTestArchive(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o640, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
