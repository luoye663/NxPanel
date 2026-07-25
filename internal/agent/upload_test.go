package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
)

func agentUploadServer(t *testing.T, allowedDir string) *Server {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.API.MaxUploadSize = "8"
	return &Server{cfg: cfg, policy: NewPathPolicy([]string{allowedDir})}
}

func agentMultipartUpload(t *testing.T, path string, size int, duplicate bool) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("path", path)
	part, _ := w.CreateFormFile("file", "test.bin")
	_, _ = part.Write(bytes.Repeat([]byte("a"), size))
	if duplicate {
		_, _ = w.CreateFormFile("file", "again.bin")
	}
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/files/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestAgentMultipartUploadExactLimitAndPlusOne(t *testing.T) {
	dir := t.TempDir()
	server := agentUploadServer(t, dir)
	for _, size := range []int{8, 9} {
		target := filepath.Join(dir, fmt.Sprintf("file-%d", size))
		rec := httptest.NewRecorder()
		server.handleFilesUpload(rec, agentMultipartUpload(t, target, size, false))
		if size == 9 {
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("+1 status = %d body=%s", rec.Code, rec.Body.String())
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatal("oversized upload created target")
			}
			continue
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("exact status = %d body=%s", rec.Code, rec.Body.String())
		}
	}
}

func TestAgentJSONUploadExactLimitAndPlusOne(t *testing.T) {
	dir := t.TempDir()
	server := agentUploadServer(t, dir)
	for _, size := range []int{8, 9} {
		target := filepath.Join(dir, fmt.Sprintf("json-%d", size))
		encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("b"), size))
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/files/upload", strings.NewReader(fmt.Sprintf(`{"path":%q,"content_base64":%q}`, target, encoded)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		server.handleFilesUpload(rec, req)
		want := http.StatusOK
		if size == 9 {
			want = http.StatusRequestEntityTooLarge
		}
		if rec.Code != want {
			t.Fatalf("size %d status = %d body=%s", size, rec.Code, rec.Body.String())
		}
	}
}

func TestAgentRejectsDuplicateMultipartWithoutTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "duplicate")
	rec := httptest.NewRecorder()
	agentUploadServer(t, dir).handleFilesUpload(rec, agentMultipartUpload(t, target, 2, true))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("malformed upload created target")
	}
}

func TestAtomicUploadCancellationCleansPartialFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cancelled")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := writeFileAtomicFromReader(ctx, target, strings.NewReader("content"), 0644); err == nil {
		t.Fatal("cancelled write should fail")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("partial upload leaked: %v", entries)
	}
}
