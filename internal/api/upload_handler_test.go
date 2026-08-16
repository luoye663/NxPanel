package api

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
)

func apiMultipartUpload(t *testing.T, size int, duplicate bool) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("path", "/tmp/")
	part, _ := w.CreateFormFile("file", "test.bin")
	_, _ = part.Write(bytes.Repeat([]byte("x"), size))
	if duplicate {
		_, _ = w.CreateFormFile("file", "duplicate.bin")
	}
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestAPIReceiveMultipartUploadExactLimitAndPlusOne(t *testing.T) {
	server := &Server{cfg: &app.Config{API: app.APIConfig{MaxUploadSize: "8"}}}
	for _, size := range []int{8, 9} {
		rec := httptest.NewRecorder()
		file, target, ok := server.receiveUpload(rec, apiMultipartUpload(t, size, false))
		if size == 9 {
			if ok || rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("+1 upload = ok %v status %d", ok, rec.Code)
			}
			continue
		}
		if !ok || target != "/tmp/test.bin" || file.Size != 8 {
			t.Fatalf("exact upload = ok %v target %q", ok, target)
		}
		file.Close()
	}
}

func TestAPIReceiveJSONUploadExactLimitAndPlusOne(t *testing.T) {
	server := &Server{cfg: &app.Config{API: app.APIConfig{MaxUploadSize: "8"}}}
	for _, size := range []int{8, 9} {
		encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), size))
		req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(fmt.Sprintf(`{"path":"/tmp/test","content_base64":%q}`, encoded)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		file, _, ok := server.receiveUpload(rec, req)
		if size == 9 {
			if ok || rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("+1 JSON upload = ok %v status %d", ok, rec.Code)
			}
			continue
		}
		if !ok || file.Size != 8 {
			t.Fatal("exact JSON upload should pass")
		}
		file.Close()
	}
}

func TestAPIReceiveUploadRejectsDuplicatePart(t *testing.T) {
	server := &Server{cfg: &app.Config{API: app.APIConfig{MaxUploadSize: "8"}}}
	rec := httptest.NewRecorder()
	if file, _, ok := server.receiveUpload(rec, apiMultipartUpload(t, 2, true)); ok {
		file.Close()
		t.Fatal("duplicate file part should be rejected")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAPIRejectedUploadNeverCallsAgent(t *testing.T) {
	server := &Server{cfg: &app.Config{API: app.APIConfig{MaxUploadSize: "8"}}}
	rec := httptest.NewRecorder()
	server.handleGlobalFilesUpload(rec, apiMultipartUpload(t, 9, false))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", rec.Code)
	}
}
