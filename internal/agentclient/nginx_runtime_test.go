package agentclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/nginx"
)

func TestNginxRuntimeClient(t *testing.T) {
	generation := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-NxPanel-Agent-Token") != "token" {
			t.Error("missing agent authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/v1/nginx/status":
			if r.Method != http.MethodGet {
				t.Error("status not GET")
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"running":true,"master_pid":42,"workers":2,"processes":3,"memory_rss":4096,"uptime_seconds":60}}`))
		case "/internal/v1/nginx/start", "/internal/v1/nginx/stop", "/internal/v1/nginx/restart":
			if r.Method != http.MethodPost {
				t.Error("control not POST")
			}
			if strings.HasSuffix(r.URL.Path, "/restart") {
				var req nginx.RuntimeControlRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RestartGeneration != generation {
					t.Errorf("unexpected request: %+v %v", req, err)
				}
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"ok":false,"error":"target changed"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"ok":true}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	c := &Client{token: "token", httpClient: &http.Client{Transport: rewriteRuntimeTransport{target: server.URL}}}
	status, err := c.NginxStatus(context.Background())
	if err != nil || status.MasterPID != 42 || status.MemoryRSS != 4096 {
		t.Fatalf("status: %+v %v", status, err)
	}
	for _, action := range []string{"start", "stop"} {
		if err := c.ControlNginx(context.Background(), action, nginx.RuntimeControlRequest{}); err != nil {
			t.Fatal(err)
		}
	}
	var response *HTTPError
	if err := c.ControlNginx(context.Background(), "restart", nginx.RuntimeControlRequest{RestartGeneration: generation}); !errors.As(err, &response) || response.StatusCode != http.StatusConflict || response.Message != "target changed" {
		t.Fatalf("restart: %v", err)
	}
	if err := c.ControlNginx(context.Background(), "../reload", nginx.RuntimeControlRequest{}); err == nil {
		t.Fatal("invalid action accepted")
	}
}

type rewriteRuntimeTransport struct{ target string }

func (t rewriteRuntimeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, t.target+r.URL.Path, r.Body)
	if err != nil {
		return nil, err
	}
	req.Header = r.Header.Clone()
	return http.DefaultTransport.RoundTrip(req)
}
