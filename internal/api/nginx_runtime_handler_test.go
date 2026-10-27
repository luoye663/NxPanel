package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNginxControlRequestValidation(t *testing.T) {
	server := newNoAgentTestServer(t)
	defer server.Close()
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{"command":"rm"}`, http.StatusBadRequest},
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
