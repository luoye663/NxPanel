package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/upstream"
)

type upstreamAPIAgent struct{}

func (upstreamAPIAgent) ApplyTransaction(_ context.Context, req *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error) {
	return &agentclient.TransactionResponse{Backups: []agentclient.BackupRecord{{
		FilePath: req.Changes[len(req.Changes)-1].Path, BackupPath: "/backup/test", Existed: true,
	}}}, nil
}

func installTestUpstreamService(server *Server) {
	server.upstreamSvc = upstream.NewService(
		repo.NewUpstreamRepo(server.db), server.opRepo, upstreamAPIAgent{}, "/panel",
	)
}

func authenticatedUpstreamRequest(t *testing.T, server *Server, method, path, body string, csrf bool) *http.Request {
	t.Helper()
	login := doLogin(server, "admin", "Test-password-123")
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	csrfToken, sessionCookie := parseLoginResponse(t, login)
	req := httptest.NewRequest(method, apiTestPath(server, path), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "openrest_session", Value: sessionCookie})
	if csrf {
		req.Header.Set("X-CSRF-Token", csrfToken)
	}
	return req
}

func TestUpstreamRoutesRequireAuthAndCSRF(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)
	installTestUpstreamService(server)
	body := `{"name":"api_backend","algorithm":"round_robin","hash_key":"","consistent":false,"keepalive":0,"keepalive_requests":0,"keepalive_timeout_seconds":0,"advanced_directives":"","servers":[{"address":"127.0.0.1:8080","weight":1,"max_fails":1,"fail_timeout_seconds":10,"backup":false,"down":false,"sort_order":0}]}`

	unauthenticated := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, apiTestPath(server, "/nginx/upstreams"), strings.NewReader(body)))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", unauthenticated.Code)
	}

	withoutCSRF := httptest.NewRecorder()
	server.Handler().ServeHTTP(withoutCSRF, authenticatedUpstreamRequest(t, server, http.MethodPost, "/nginx/upstreams", body, false))
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d body=%s", withoutCSRF.Code, withoutCSRF.Body.String())
	}
}

func TestUpstreamCreateAndListResponses(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)
	installTestUpstreamService(server)
	body := `{"name":"api_backend","algorithm":"least_conn","hash_key":"","consistent":false,"keepalive":16,"keepalive_requests":100,"keepalive_timeout_seconds":30,"advanced_directives":"zone api_backend 64k;","servers":[{"address":"127.0.0.1:8080","weight":1,"max_fails":1,"fail_timeout_seconds":10,"backup":false,"down":false,"sort_order":0}]}`
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, authenticatedUpstreamRequest(t, server, http.MethodPost, "/nginx/upstreams", body, true))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response Response
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	data, ok := response.Data.(map[string]any)
	if !ok || data["operation_id"] == "" || data["upstream"] == nil {
		t.Fatalf("create response=%#v", response.Data)
	}

	listReq := authenticatedUpstreamRequest(t, server, http.MethodGet, "/nginx/upstreams", "", false)
	listRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK || !strings.Contains(listRecorder.Body.String(), "api_backend") || !strings.Contains(listRecorder.Body.String(), `"reference_count":0`) {
		t.Fatalf("list status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
}

func TestUpstreamUnknownFieldRejected(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)
	installTestUpstreamService(server)
	body := `{"name":"api_backend","algorithm":"round_robin","servers":[],"unexpected":true}`
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, authenticatedUpstreamRequest(t, server, http.MethodPost, "/nginx/upstreams/validate", body, true))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestUpstreamStatusRequiresAuthAndReturnsHashes(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)
	installTestUpstreamService(server)

	unauthenticated := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, apiTestPath(server, "/nginx/upstreams/status"), nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("status without auth=%d", unauthenticated.Code)
	}

	authenticated := httptest.NewRecorder()
	server.Handler().ServeHTTP(authenticated, authenticatedUpstreamRequest(t, server, http.MethodGet, "/nginx/upstreams/status", "", false))
	if authenticated.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", authenticated.Code, authenticated.Body.String())
	}
	var response Response
	if err := json.NewDecoder(authenticated.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	data, ok := response.Data.(map[string]any)
	if !ok || data["desired_hash"] == "" || data["applied_hash"] != "" || data["synced"] != false || data["path"] != "/panel/conf.d/nxpanel-upstreams.conf" {
		t.Fatalf("unexpected status response=%#v", response.Data)
	}
}
