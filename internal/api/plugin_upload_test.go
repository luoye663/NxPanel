package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/luoye663/nxpanel/internal/plugin"
)

type uploadTestCatalog struct{}

func (uploadTestCatalog) List(context.Context) ([]plugin.CatalogEntry, error) {
	return []plugin.CatalogEntry{}, nil
}
func (uploadTestCatalog) Resolve(context.Context, string, string) (plugin.CatalogEntry, error) {
	return plugin.CatalogEntry{}, plugin.ErrPluginNotFound
}
func (uploadTestCatalog) Fetch(context.Context, plugin.CatalogEntry, string) error {
	return errors.New("unused")
}

func TestDeveloperPackageUploadPassesGlobalBodyLimit(t *testing.T) {
	s := newTestServer(t)
	// Keep all uploaded package state in the test directory.
	svc := plugin.NewService(s.db, t.TempDir(), uploadTestCatalog{}, nil)
	defer svc.Close(context.Background())
	s.pluginHandler.service = svc
	if err := svc.SetDeveloperMode(context.Background(), true, true); err != nil {
		t.Fatal(err)
	}
	setupTestAdmin(t, s)
	csrf, cookie := parseLoginResponse(t, doLogin(s, "admin", "Test-password-123"))
	wasm := []byte{0, 97, 115, 109, 1, 0, 0, 0, 1, 5, 1, 96, 0, 1, 127, 3, 2, 1, 0, 7, 14, 1, 10, 'n', 'x', 'p', '_', 'h', 'e', 'a', 'l', 't', 'h', 0, 0, 10, 6, 1, 4, 0, 65, 0, 11}
	padding := make([]byte, 3<<20)
	if _, err := rand.Read(padding); err != nil {
		t.Fatal(err)
	}
	m := plugin.Manifest{SchemaVersion: 1, ID: "com.example.upload", Name: "Upload", Version: "1.0.0", Publisher: "Test", PluginAPIVersion: "v1", Backend: "plugin.wasm"}
	files := map[string][]byte{"plugin.wasm": wasm, "padding.bin": padding}
	for path, data := range files {
		sum := sha256.Sum256(data)
		m.Files = append(m.Files, plugin.FileDigest{Path: path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))})
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	files["manifest.json"] = raw
	var pkg bytes.Buffer
	gz := gzip.NewWriter(&pkg)
	tw := tar.NewWriter(gz)
	for path, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: path, Mode: 0600, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "test.nxp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pkg.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	if body.Len() <= 2<<20 {
		t.Fatal("fixture must exceed old limit")
	}
	request := func(auth, token bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, apiTestPath(s, "/plugins/developer/packages/inspect"), bytes.NewReader(body.Bytes()))
		req.Header.Set("Content-Type", mw.FormDataContentType())
		if auth {
			req.AddCookie(&http.Cookie{Name: "openrest_session", Value: cookie})
		}
		if token {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := request(false, false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("auth status %d", rec.Code)
	}
	if rec := request(true, false); rec.Code != http.StatusForbidden {
		t.Fatalf("CSRF status %d", rec.Code)
	}
	rec := request(true, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload %d: %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Data struct {
			UploadToken string `json:"upload_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	svc.DiscardDeveloperPackage(result.Data.UploadToken)
}
