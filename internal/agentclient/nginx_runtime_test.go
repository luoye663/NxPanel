package agentclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNginxRuntimeClient(t *testing.T) {
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
	for _, action := range []string{"start", "stop", "restart"} {
		if err := c.ControlNginx(context.Background(), action); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.ControlNginx(context.Background(), "../reload"); err == nil {
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
