package api

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
)

func TestNginxControlRequestValidation(t *testing.T) {
	server := newNoAgentTestServer(t)
	defer server.Close()
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{"command":"rm"}`, http.StatusBadRequest},
		{`{"pid":42}`, http.StatusBadRequest},
		{`{"restart_generation":"` + strings.Repeat("a", 64) + `"}`, http.StatusBadRequest},
		{`{} {}`, http.StatusBadRequest},
		{`{`, http.StatusBadRequest},
		{`{}`, http.StatusServiceUnavailable},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/nginx/start", strings.NewReader(tc.body))
		server.handleNginxControl(rec, req)
		if rec.Code != tc.code {
			t.Fatalf("body %s: status %d: %s", tc.body, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	server.handleNginxStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nginx/status", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestNginxRestartConflictAudit(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	generation := strings.Repeat("a", 64)
	message := "重启目标已变化，请刷新状态后重试"
	agent := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request nginx.RuntimeControlRequest
		if r.URL.Path != "/internal/v1/nginx/restart" || json.NewDecoder(r.Body).Decode(&request) != nil || request.RestartGeneration != generation {
			t.Error("unexpected agent request")
		}
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": message})
	})}
	go func() { _ = agent.Serve(listener) }()
	t.Cleanup(func() { _ = agent.Close() })
	client := agentclient.NewWithDefaults(socket, "agent-token")
	t.Cleanup(func() { _ = client.Close() })
	server := &Server{agentClient: client, opRepo: repo.NewOperationRepo(newTestDB(t))}
	rec := httptest.NewRecorder()
	server.handleNginxControl(rec, httptest.NewRequest(http.MethodPost, "/api/v1/nginx/restart", strings.NewReader(`{"restart_generation":"`+generation+`"}`)))
	var response Response
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || rec.Code != http.StatusConflict || response.Error == nil || response.Error.Code != app.ErrConflict || response.Error.Message != message {
		t.Fatalf("unexpected response: %d %s", rec.Code, rec.Body)
	}
	opID, _ := response.Error.Details["operation_id"].(string)
	operation, err := server.opRepo.GetByID(opID)
	if err != nil || operation.Status != "failed" || operation.Action != "nginx.restart" || operation.ErrorCode != app.ErrConflict || operation.ErrorMessage != message {
		t.Fatalf("unexpected audit entry: %+v %v", operation, err)
	}
}
