package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestGateSecretValidator(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/v1/{gateSecret}", func(r chi.Router) {
		r.Use(GateSecretValidator(func() string { return "nx-secret" }))
		r.Get("/ok", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	})

	for path, want := range map[string]int{
		"/api/v1/nx-secret/ok": http.StatusNoContent,
		"/api/v1/wrong/ok":     http.StatusNotFound,
		"/api/v1//ok":          http.StatusNotFound,
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Fatalf("%s 状态码期望 %d，实际 %d", path, want, rec.Code)
		}
	}
}
