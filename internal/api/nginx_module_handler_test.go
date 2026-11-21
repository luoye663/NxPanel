package api

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/nginxmodule"
)

func TestNginxModuleRoutesAndAsyncAudit(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)
	socket := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan nginxmodule.InstallRequest, 1)
	allowCompletion := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/nginx/modules", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"data":{"feed_state":"online","modules":[{"id":"geoip2","name":"GeoIP2","state":"available"},{"id":"test-module","name":"Online module","state":"available"}]}}`))
	})
	mux.HandleFunc("POST /internal/v1/nginx/modules/install", func(w http.ResponseWriter, r *http.Request) {
		var request nginxmodule.InstallRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		started <- request
		select {
		case <-allowCompletion:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"state":"success"}}`))
	})
	agent := &http.Server{Handler: mux}
	go func() { _ = agent.Serve(listener) }()
	t.Cleanup(func() { _ = agent.Close() })
	_ = server.agentClient.Close()
	server.agentClient = agentclient.NewWithDefaults(socket, "test")
	login := doLogin(server, "admin", "Test-password-123")
	csrf, session := parseLoginResponse(t, login)
	request := func(method, path, body string, authenticated, protected bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, apiTestPath(server, path), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if authenticated {
			req.AddCookie(&http.Cookie{Name: "openrest_session", Value: session})
		}
		if protected {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, req)
		return recorder
	}
	const listPath = "/nginx/modules"
	const installPath = listPath + "/install"
	confirmation := strings.Repeat("a", 64)
	body := `{"id":"geoip2","confirmation":"` + confirmation + `"}`
	if response := request(http.MethodGet, listPath, "", false, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list = %d", response.Code)
	}
	if response := request(http.MethodPost, installPath, body, true, false); response.Code != http.StatusForbidden {
		t.Fatalf("install without CSRF = %d", response.Code)
	}
	response := request(http.MethodPost, installPath, body, true, true)
	if response.Code != http.StatusAccepted {
		t.Fatalf("install = %d %s", response.Code, response.Body.String())
	}
	var accepted struct{ Data nginxModuleJob }
	if err := json.NewDecoder(response.Body).Decode(&accepted); err != nil || accepted.Data.ID == "" {
		t.Fatalf("invalid installation job: %+v %v", accepted, err)
	}
	select {
	case got := <-started:
		if got.ID != nginxmodule.GeoIP2 || got.Confirmation != confirmation {
			t.Fatalf("unexpected Agent request: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("background job did not contact Agent")
	}
	if response := request(http.MethodPost, installPath, body, true, true); response.Code != http.StatusConflict {
		t.Fatalf("duplicate installation = %d", response.Code)
	}
	if response := request(http.MethodGet, listPath, "", true, false); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), accepted.Data.ID) || !strings.Contains(response.Body.String(), `"name":"Online module"`) {
		t.Fatalf("installation not discoverable: %d %s", response.Code, response.Body.String())
	}
	allowCompletion <- struct{}{}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		op, err := server.opRepo.GetByID(accepted.Data.ID)
		if err != nil {
			t.Fatal(err)
		}
		if op != nil && op.Status == "success" {
			response := request(http.MethodGet, listPath+"/jobs/"+op.ID, "", true, false)
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"success"`) {
				t.Fatalf("job status = %d %s", response.Code, response.Body.String())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("installation completion was not audited")
}
