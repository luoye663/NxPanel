package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/luoye663/nxpanel/internal/plugin"
)

func TestPluginAuthorizationErrorsUsePanelSafeStatusCodes(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
		reason string
	}{
		{"authorization challenge", &plugin.ServiceError{Status: http.StatusUnauthorized, Code: "authorization_required", Message: "login required", Details: map[string]any{"plugin_id": "org.example.paid"}}, http.StatusConflict, "PLUGIN_AUTHORIZATION_REQUIRED", ""},
		{"entitlement denied", &plugin.ServiceError{Status: http.StatusForbidden, Code: "instance_limit_reached", Message: "limit reached"}, http.StatusForbidden, "PLUGIN_ENTITLEMENT_DENIED", "instance_limit_reached"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/plugins/x/install", nil)
			(&PluginHandler{}).respond(recorder, request, nil, tt.err)
			if recorder.Code != tt.status {
				t.Fatalf("status=%d", recorder.Code)
			}
			var response Response
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error == nil || response.Error.Code != tt.code {
				t.Fatalf("response=%s", recorder.Body.String())
			}
			if tt.reason != "" && response.Error.Details["reason"] != tt.reason {
				t.Fatalf("details=%v", response.Error.Details)
			}
		})
	}
}

func TestPluginAuthorizationWireFieldNames(t *testing.T) {
	raw, err := json.Marshal(struct {
		Authorization plugin.AuthorizationSummary `json:"authorization"`
		Challenge     plugin.DeviceAttempt        `json:"challenge"`
	}{
		Authorization: plugin.AuthorizationSummary{ID: "auth-1", AccountID: "acct-1", DisplayName: "Example", EmailMasked: "e***@example.com"},
		Challenge:     plugin.DeviceAttempt{AttemptID: "attempt-1", Interval: 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["authorization"]["account_label"] != "Example" || wire["authorization"]["account_email"] != "e***@example.com" {
		t.Fatalf("authorization wire=%s", raw)
	}
	if wire["challenge"]["interval_seconds"] != float64(7) {
		t.Fatalf("challenge wire=%s", raw)
	}
	for _, legacy := range []string{"display_name", "email_masked", "interval"} {
		if _, ok := wire["authorization"][legacy]; ok {
			t.Fatalf("legacy field %q in authorization wire=%s", legacy, raw)
		}
		if _, ok := wire["challenge"][legacy]; ok {
			t.Fatalf("legacy field %q in challenge wire=%s", legacy, raw)
		}
	}
}
