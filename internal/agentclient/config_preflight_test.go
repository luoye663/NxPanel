package agentclient

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccessDiagnosticOldOrUnknownAgentResponse(t *testing.T) {
	for _, data := range []string{`{}`, `null`, `{"complete":true}`, `{"warnings":null,"complete":true,"truncated":false}`} {
		client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"data":` + data + `}`)), Header: make(http.Header)}, nil
		})}}
		report, err := client.DiagnoseAccessConfig(context.Background(), &ConfigPreflightRequest{})
		if err != nil || report.Complete || len(report.Warnings) != 1 {
			t.Fatalf("unknown response treated as safe: %s %+v %v", data, report, err)
		}
	}
}

func TestAccessPreflightLargeGeoKeepsFileReadLimit(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "nginx.conf")
	data := filepath.Join(dir, "countries.conf")
	if err := os.WriteFile(global, []byte("events {} http { include countries.conf; }"), 0600); err != nil {
		t.Fatal(err)
	}
	// More than 16 MiB and far more than 65,536 data entries. The preflight
	// must skip data semantically, but still consume and bound all its bytes.
	body := "geo $country {\n" + strings.Repeat("    192.0.2.0/24 CN;\n", 900000) + "}\n"
	if len(body) <= 16<<20 {
		t.Fatal("fixture too small")
	}
	if err := os.WriteFile(data, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	socket, token := setupTestAgent(t, dir)
	client := NewWithDefaults(socket, token)
	defer client.Close()
	req := &ConfigPreflightRequest{SitePath: filepath.Join(dir, "site.conf"), GlobalPath: global, Main: "server { location / { proxy_pass http://backend; } }"}
	if report, err := client.DiagnoseAccessConfig(context.Background(), req); err != nil || !report.Complete || len(report.Warnings) != 0 {
		t.Fatal(err)
	}
	if _, _, err := client.ReadFile(context.Background(), data); err == nil || !strings.Contains(err.Error(), "16 MB") {
		t.Fatalf("ordinary read limit changed: %v", err)
	}
	if err := os.WriteFile(data, []byte(body+"auth_basic Private;"), 0600); err != nil {
		t.Fatal(err)
	}
	if report, err := client.DiagnoseAccessConfig(context.Background(), req); err != nil || !report.Complete || len(report.Warnings) != 1 || report.Warnings[0].Directive != "auth_basic" {
		t.Fatalf("conflict after large geo missed: %+v %v", report, err)
	}
}

func TestAccessPreflightRejectsUnsafeIncludes(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	forbidden := filepath.Join(outside, "secret.conf")
	if err := os.WriteFile(forbidden, []byte("set $x 1;"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(forbidden, filepath.Join(dir, "escape.conf")); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(global, []byte("events {} http {}"), 0600); err != nil {
		t.Fatal(err)
	}
	socket, token := setupTestAgent(t, dir)
	client := NewWithDefaults(socket, token)
	defer client.Close()
	for _, include := range []string{forbidden, "escape.conf", "missing.conf"} {
		req := &ConfigPreflightRequest{SitePath: filepath.Join(dir, "site.conf"), GlobalPath: global, Main: "server { include " + include + "; }"}
		if report, err := client.DiagnoseAccessConfig(context.Background(), req); err == nil && report.Complete {
			t.Fatalf("unsafe include accepted: %s", include)
		}
	}
}
