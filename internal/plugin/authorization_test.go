package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuthorizationStorageEncryptsAndSupportsMultipleAccounts(t *testing.T) {
	db, dataDir := newPluginTestDB(t), t.TempDir()
	m := NewAuthorizationManager(db, dataDir, nil)
	first, err := m.save(context.Background(), TokenResponse{AccessToken: "access-one", RefreshToken: "refresh-one", ExpiresIn: 900, RefreshExpiresIn: 7776000, Account: AccountResponse{ID: "acct-1", DisplayName: "One", EmailMasked: "o***@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.save(context.Background(), TokenResponse{AccessToken: "access-two", RefreshToken: "refresh-two", ExpiresIn: 900, RefreshExpiresIn: 7776000, Account: AccountResponse{ID: "acct-2", DisplayName: "Two", EmailMasked: "t***@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || first.ID == second.ID {
		t.Fatalf("unexpected authorizations: %+v", items)
	}
	var accessCipher, refreshCipher []byte
	if err := db.QueryRow(`SELECT access_ciphertext,refresh_ciphertext FROM plugin_authorizations WHERE authorization_id=?`, first.ID).Scan(&accessCipher, &refreshCipher); err != nil {
		t.Fatal(err)
	}
	if string(accessCipher) == "access-one" || string(refreshCipher) == "refresh-one" {
		t.Fatal("authorization token stored as plaintext")
	}
	info, err := os.Stat(filepath.Join(dataDir, "secrets", "plugin-authorization.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode=%o", info.Mode().Perm())
	}
	if err := m.Bind(context.Background(), "org.example.one", first.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Bind(context.Background(), "org.example.two", second.ID); err != nil {
		t.Fatal(err)
	}
	if id, _ := m.BoundID(context.Background(), "org.example.one"); id != first.ID {
		t.Fatalf("binding=%q", id)
	}
}

func TestDeviceAttemptDoesNotExposeDeviceCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/oauth/device/code" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(DeviceCodeResponse{DeviceCode: "server-secret", UserCode: "ABCD-EFGH", VerificationURI: "https://plugins.example/device", ExpiresIn: 600, Interval: 5})
	}))
	defer server.Close()
	client, err := NewOfficialServiceClient(server.URL, true, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	m := NewAuthorizationManager(newPluginTestDB(t), t.TempDir(), client)
	attempt, err := m.StartDevice(context.Background(), "org.example.paid", "1.0.0", "instance")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(attempt)
	if string(raw) == "" || bytes.Contains(raw, []byte("server-secret")) {
		t.Fatalf("device_code leaked: %s", raw)
	}
	if attempt.ExpiresAt.Before(time.Now().Add(9 * time.Minute)) {
		t.Fatalf("unexpected expiry: %v", attempt.ExpiresAt)
	}
}

func TestDeviceAuthorizationDoesNotChangeBindingUntilEntitlementCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/oauth/device/code":
			_ = json.NewEncoder(w).Encode(DeviceCodeResponse{DeviceCode: "device-secret", UserCode: "ABCD-EFGH", VerificationURI: "https://plugins.example/device", ExpiresIn: 600, Interval: 1})
		case "/api/v1/oauth/token":
			_ = json.NewEncoder(w).Encode(TokenResponse{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900, RefreshExpiresIn: 7776000, Account: AccountResponse{ID: "acct-2", DisplayName: "Second"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewOfficialServiceClient(server.URL, true, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	db, dataDir := newPluginTestDB(t), t.TempDir()
	m := NewAuthorizationManager(db, dataDir, client)
	first, err := m.save(context.Background(), TokenResponse{AccessToken: "first-access", RefreshToken: "first-refresh", ExpiresIn: 900, RefreshExpiresIn: 7776000, Account: AccountResponse{ID: "acct-1", DisplayName: "First"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Bind(context.Background(), "org.example.paid", first.ID); err != nil {
		t.Fatal(err)
	}
	attempt, err := m.StartDevice(context.Background(), "org.example.paid", "1.0.0", "instance")
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.PollDevice(context.Background(), attempt.AttemptID)
	if err != nil || result.Status != "authorized" || result.Authorization == nil {
		t.Fatalf("unexpected device result: %+v, err=%v", result, err)
	}
	boundID, err := m.BoundID(context.Background(), "org.example.paid")
	if err != nil {
		t.Fatal(err)
	}
	if boundID != first.ID {
		t.Fatalf("device login changed binding before entitlement validation: got %q want %q", boundID, first.ID)
	}
}

type resolvedTestCatalog struct{ entry CatalogEntry }

func (c resolvedTestCatalog) List(context.Context) ([]CatalogEntry, error) {
	return []CatalogEntry{c.entry}, nil
}
func (c resolvedTestCatalog) Resolve(context.Context, string, string) (CatalogEntry, error) {
	return c.entry, nil
}
func (c resolvedTestCatalog) Fetch(context.Context, CatalogEntry, string) error { return nil }
func (c resolvedTestCatalog) FetchResolved(context.Context, CatalogEntry, DownloadResolveResponse, string) error {
	return nil
}

func TestLicensedDownloadDoesNotAutomaticallyTryAnotherAccount(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/downloads/resolve" {
			http.NotFound(w, r)
			return
		}
		requests++
		if got := r.Header.Get("Authorization"); got != "Bearer denied-token" {
			t.Errorf("unexpected bearer %q", got)
		}
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "not_granted", "message": "no entitlement"}})
	}))
	defer server.Close()
	client, err := NewOfficialServiceClient(server.URL, true, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	db, dataDir := newPluginTestDB(t), t.TempDir()
	entry := CatalogEntry{ID: "org.example.paid", Version: "1.0.0", Package: "packages/paid.nxp", PackageSHA256: strings.Repeat("a", 64), Size: 42, Access: CatalogAccessLicensed}
	svc := NewServiceWithOfficialClient(db, dataDir, resolvedTestCatalog{entry: entry}, developerTestRuntime{}, client)
	first, err := svc.authorizations.save(context.Background(), TokenResponse{AccessToken: "denied-token", RefreshToken: "refresh-one", ExpiresIn: 900, RefreshExpiresIn: 7776000, Account: AccountResponse{ID: "acct-1"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.authorizations.save(context.Background(), TokenResponse{AccessToken: "would-succeed", RefreshToken: "refresh-two", ExpiresIn: 900, RefreshExpiresIn: 7776000, Account: AccountResponse{ID: "acct-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.authorizations.Bind(context.Background(), entry.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	err = svc.fetchOfficialPackage(context.Background(), entry, filepath.Join(t.TempDir(), "x.nxp"))
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Code != "not_granted" {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("resolve requests=%d, want 1", requests)
	}
}
