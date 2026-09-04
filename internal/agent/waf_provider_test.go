package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplaceWAFLoadModuleBlock(t *testing.T) {
	original := []byte("user nginx;\nevents {}\nhttp {}\n")
	first, err := replaceWAFLoadModuleBlock(original, "/opt/nxpanel/nginx/providers/waf.modsecurity.v1/versions/1.0.0/ngx_http_modsecurity_module.so")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(first), "load_module") != 1 || !strings.HasSuffix(string(first), string(original)) {
		t.Fatalf("unexpected first activation:\n%s", first)
	}
	second, err := replaceWAFLoadModuleBlock(first, "/opt/nxpanel/nginx/providers/waf.modsecurity.v1/versions/1.1.0/ngx_http_modsecurity_module.so")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(second), "load_module") != 1 || strings.Contains(string(second), "/1.0.0/") {
		t.Fatalf("activation was not idempotent:\n%s", second)
	}
}

func TestReplaceWAFLoadModuleBlockRejectsMalformedMarker(t *testing.T) {
	_, err := replaceWAFLoadModuleBlock([]byte("# nxpanel-waf-provider begin\nevents {}\n"), "/safe/module.so")
	if err == nil {
		t.Fatal("expected malformed marker error")
	}
	_, err = replaceWAFLoadModuleBlock([]byte("events {}\n"), "/bad'path/module.so")
	if err == nil {
		t.Fatal("expected unsafe module path error")
	}
}

func TestWAFAuditOpaqueIDsReadAndCleanup(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "2026", "old-event")
	newPath := filepath.Join(root, "2026", "new-event")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte("secret request body"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte(strings.Repeat("x", 32)), 0640); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	files, err := scanWAFAuditFiles(context.Background(), root, "site_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || len(files[0].EventID) != 64 {
		t.Fatalf("unexpected scan result: %#v", files)
	}
	for _, file := range files {
		if strings.Contains(file.EventID, "old-event") || strings.Contains(file.EventID, string(filepath.Separator)) {
			t.Fatalf("event id leaks path: %s", file.EventID)
		}
	}
	preview, truncated, err := readAuditPreview(newPath, 8)
	if err != nil || !truncated || string(preview) != "xxxxxxxx" {
		t.Fatalf("unexpected preview: %q truncated=%v err=%v", preview, truncated, err)
	}
	removed, _, err := cleanupWAFAuditFiles(context.Background(), files, time.Now().Add(-24*time.Hour), 1<<30)
	if err != nil || removed != 1 {
		t.Fatalf("unexpected cleanup: removed=%d err=%v", removed, err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old event still exists: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new event removed: %v", err)
	}
}

func TestScanWAFAuditFilesSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "event")); err != nil {
		t.Fatal(err)
	}
	files, err := scanWAFAuditFiles(context.Background(), root, "site_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("symlink must not be indexed: %#v", files)
	}
}
