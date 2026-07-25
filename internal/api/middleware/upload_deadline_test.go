package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUploadReadDeadlineAddsRouteScopedContextTimeout(t *testing.T) {
	var remaining time.Duration
	handler := UploadReadDeadline(2 * time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Fatal("upload context has no deadline")
		}
		remaining = time.Until(deadline)
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/upload", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if remaining <= 0 || remaining > 2*time.Second {
		t.Fatalf("remaining timeout = %s", remaining)
	}
}
