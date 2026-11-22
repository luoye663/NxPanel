package agent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/luoye663/nxpanel/internal/configpreflight"
)

func TestPreflightRejectsNonRegularFiles(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo.conf")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	source := preflightSource{NewPathPolicy([]string{dir})}
	for _, path := range []string{fifo, dir} {
		f, err := source.Open(path)
		if err == nil {
			f.Close()
			t.Fatalf("accepted nonregular file %s", path)
		}
	}
}

func TestPreflightGlobAndRequestValidation(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "nginx.conf")
	for name, content := range map[string]string{
		"nginx.conf":    "events {} http { include data/*.conf; }",
		"data/geo.conf": "geo $country { default ZZ; }",
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{policy: NewPathPolicy([]string{dir})}
	req := configpreflight.Request{SitePath: filepath.Join(dir, "site.conf"), GlobalPath: global, Main: "server {}"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		body   string
		status int
	}{
		{string(data), http.StatusOK},
		{string(data) + " {}", http.StatusBadRequest},
		{`{"unknown":true}`, http.StatusBadRequest},
	} {
		w := httptest.NewRecorder()
		s.handleConfigPreflight(w, httptest.NewRequest("POST", "/", strings.NewReader(tt.body)))
		if w.Code != tt.status {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "data", "conflict.conf"), []byte("deny all;"), 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleConfigPreflight(w, httptest.NewRequest("POST", "/", bytes.NewReader(data)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"directive":"deny"`) || !strings.Contains(w.Body.String(), `"complete":true`) {
		t.Fatalf("glob conflict missed: %d %s", w.Code, w.Body)
	}
}
