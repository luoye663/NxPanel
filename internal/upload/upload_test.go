package upload

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func multipartRequest(t *testing.T, path string, content []byte, mutate func(*multipart.Writer)) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("path", path); err != nil {
		t.Fatal(err)
	}
	part, err := w.CreateFormFile("file", "test.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(w)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestParseMultipartExactLimitAndPlusOne(t *testing.T) {
	for _, tc := range []struct {
		name     string
		size     int
		tooLarge bool
	}{{"exact", 8, false}, {"plus_one", 9, true}} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := Parse(multipartRequest(t, "/tmp/test", bytes.Repeat([]byte("x"), tc.size), nil), 8)
			if tc.tooLarge {
				if !errors.Is(err, ErrTooLarge) {
					t.Fatalf("expected ErrTooLarge, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if file.Size != 8 {
				t.Fatalf("size = %d", file.Size)
			}
		})
	}
}

func TestParseJSONExactDecodedLimitAndPlusOne(t *testing.T) {
	for _, size := range []int{8, 9} {
		encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("z"), size))
		body := fmt.Sprintf(`{"path":"/tmp/test","content_base64":%q}`, encoded)
		req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		file, err := Parse(req, 8)
		if size == 9 {
			if !errors.Is(err, ErrTooLarge) {
				t.Fatalf("expected ErrTooLarge, got %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
}

func TestParseMultipartRejectsDuplicateAndUnknownPartsWithoutTempLeak(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	for _, tc := range []struct {
		name   string
		mutate func(*multipart.Writer)
	}{
		{"duplicate_file", func(w *multipart.Writer) { _, _ = w.CreateFormFile("file", "again.bin") }},
		{"unknown", func(w *multipart.Writer) { _ = w.WriteField("extra", "no") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if file, err := Parse(multipartRequest(t, "/tmp/test", []byte("ok"), tc.mutate), 8); err == nil {
				file.Close()
				t.Fatal("expected malformed multipart error")
			}
		})
	}
	entries, err := os.ReadDir(os.Getenv("TMPDIR"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary uploads leaked: %v", entries)
	}
}
