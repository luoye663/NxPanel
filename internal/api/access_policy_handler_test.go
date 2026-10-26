package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/luoye663/nxpanel/internal/accesspolicy"
	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/db"
)

type accessPolicyHandlerAgent struct{ applies int }

func (*accessPolicyHandlerAgent) ReadFile(context.Context, string) ([]byte, string, error) {
	return []byte("server {\n#NXPANEL-ACCESS-LIMIT-START\n#NXPANEL-ACCESS-LIMIT-END\n#NXPANEL-HOTLINK-START\n#NXPANEL-HOTLINK-END\n}\n"), "", nil
}
func (a *accessPolicyHandlerAgent) ApplyTransaction(context.Context, *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error) {
	a.applies++
	return &agentclient.TransactionResponse{}, nil
}

func accessPolicyHandlerFixture(t *testing.T) (*Server, *accesspolicy.Repo, *accessPolicyHandlerAgent, *sql.DB) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO sites (id,primary_domain,domains_json,status,root_path,access_log_path,error_log_path,config_path,enabled_path,rewrite_path) VALUES ('site_policy','policy.example.com','[]','enabled','/tmp','/tmp/a','/tmp/e','/tmp/c','/tmp/n','/tmp/r')`); err != nil {
		t.Fatal(err)
	}
	agent := &accessPolicyHandlerAgent{}
	service := accesspolicy.NewService(database, agent, nil, nil, "/panel")
	return &Server{accessPolicySvc: service}, accesspolicy.NewRepo(database), agent, database
}

func accessPolicyRequest(t *testing.T, method, path, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	router := chi.NewRouter()
	router.MethodFunc(method, "/sites/{site_id}/"+path, handler)
	request := httptest.NewRequest(method, "/sites/site_policy/"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestAccessPolicyGetDoesNotPersistDraft(t *testing.T) {
	server, store, agent, _ := accessPolicyHandlerFixture(t)
	response := accessPolicyRequest(t, http.MethodGet, "access-policy", "", server.handleAccessPolicyGet)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"mode":"legacy"`) {
		t.Fatalf("get: %d %s", response.Code, response.Body)
	}
	if p, err := store.Get("site_policy"); err != nil || p != nil || agent.applies != 0 {
		t.Fatalf("GET changed state: %#v %v", p, err)
	}
}

func TestAccessPolicySaveRejectsStaleVersion(t *testing.T) {
	server, store, agent, _ := accessPolicyHandlerFixture(t)
	p := accesspolicy.Policy{DefaultAction: accesspolicy.Action{Type: "allow"}, Rules: []accesspolicy.Rule{}}
	if _, err := store.SaveDesired("site_policy", p, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkApplied("site_policy", 1); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	response := accessPolicyRequest(t, http.MethodPut, "access-policy", string(raw), server.handleAccessPolicySave)
	if response.Code != http.StatusConflict || agent.applies != 0 {
		t.Fatalf("stale save: %d %s applies=%d", response.Code, response.Body, agent.applies)
	}
}

func TestAccessPolicySaveStrictJSON(t *testing.T) {
	server, _, agent, _ := accessPolicyHandlerFixture(t)
	for _, body := range []string{`{"default_action":{"type":"allow"},"unknown":true}`, `{"default_action":{"type":"allow"}} {"version":0}`} {
		response := accessPolicyRequest(t, http.MethodPut, "access-policy", body, server.handleAccessPolicySave)
		if response.Code != http.StatusBadRequest || agent.applies != 0 {
			t.Fatalf("invalid body accepted: %d %s", response.Code, response.Body)
		}
	}
}

func TestLegacyAccessGuardRejectsPendingAndAppliedPolicies(t *testing.T) {
	server, store, _, _ := accessPolicyHandlerFixture(t)
	called := false
	handler := server.guardLegacyAccess(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) })
	response := accessPolicyRequest(t, http.MethodPost, "ip-limit-rules", "{}", handler)
	if response.Code != http.StatusNoContent || !called {
		t.Fatal("legacy site write blocked")
	}
	if _, err := store.SaveDesired("site_policy", accesspolicy.Policy{DefaultAction: accesspolicy.Action{Type: "allow"}}, 0); err != nil {
		t.Fatal(err)
	}
	for _, applied := range []bool{false, true} {
		if applied {
			if err := store.MarkApplied("site_policy", 1); err != nil {
				t.Fatal(err)
			}
		}
		called = false
		response = accessPolicyRequest(t, http.MethodPost, "ip-limit-rules", "{}", handler)
		if response.Code != http.StatusConflict || called {
			t.Fatalf("guard allowed old writer applied=%v: %d %s", applied, response.Code, response.Body)
		}
	}
}
