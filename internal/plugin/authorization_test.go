package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func saveExpiringAuthorization(t *testing.T, m *AuthorizationManager) *AuthorizationSummary {
	t.Helper()
	authorization, err := m.save(context.Background(), TokenResponse{
		AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresIn: 1, RefreshExpiresIn: 3600,
		Account: AccountResponse{ID: "acct-1", DisplayName: "One", EmailMasked: "o***@example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return authorization
}

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

func TestAuthorizationRefreshTemporaryFailureRemainsUsable(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(TokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 900, RefreshExpiresIn: 3600, Account: AccountResponse{ID: "acct-1", DisplayName: "One"}})
	}))
	defer server.Close()
	client, err := NewOfficialServiceClient(server.URL, true, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	m := NewAuthorizationManager(newPluginTestDB(t), t.TempDir(), client)
	authorization := saveExpiringAuthorization(t, m)
	if _, err := m.AccessToken(context.Background(), authorization.ID); err == nil || errors.Is(err, ErrReauthorizationRequired) {
		t.Fatalf("temporary refresh error=%v", err)
	}
	items, err := m.List(context.Background())
	if err != nil || len(items) != 1 || items[0].Status != "active" {
		t.Fatalf("authorization was invalidated after temporary error: items=%+v err=%v", items, err)
	}
	if token, err := m.AccessToken(context.Background(), authorization.ID); err != nil || token != "new-access" {
		t.Fatalf("retry token=%q err=%v", token, err)
	}
}

func TestAuthorizationRefreshIsSerializedPerAccount(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(25 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(TokenResponse{AccessToken: "rotated-access", RefreshToken: "rotated-refresh", ExpiresIn: 900, RefreshExpiresIn: 3600, Account: AccountResponse{ID: "acct-1", DisplayName: "One"}})
	}))
	defer server.Close()
	client, err := NewOfficialServiceClient(server.URL, true, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	m := NewAuthorizationManager(newPluginTestDB(t), t.TempDir(), client)
	authorization := saveExpiringAuthorization(t, m)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			token, err := m.AccessToken(context.Background(), authorization.ID)
			if err == nil && token != "rotated-access" {
				err = fmt.Errorf("token=%q", token)
			}
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("refresh requests=%d, want 1", got)
	}
}

func TestAuthorizationCredentialsRequireReauthorization(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		dataDir := t.TempDir()
		m := NewAuthorizationManager(newPluginTestDB(t), dataDir, nil)
		authorization := saveExpiringAuthorization(t, m)
		if err := os.Remove(m.keyPath); err != nil {
			t.Fatal(err)
		}
		_, err := m.AccessToken(context.Background(), authorization.ID)
		if !errors.Is(err, ErrReauthorizationRequired) {
			t.Fatalf("error=%v", err)
		}
		items, listErr := m.List(context.Background())
		if listErr != nil || len(items) != 1 || items[0].Status != "reauthorization_required" {
			t.Fatalf("items=%+v err=%v", items, listErr)
		}
	})

	t.Run("expired refresh token", func(t *testing.T) {
		m := NewAuthorizationManager(newPluginTestDB(t), t.TempDir(), nil)
		authorization := saveExpiringAuthorization(t, m)
		_, err := m.db.Exec(`UPDATE plugin_authorizations SET refresh_expires_at=? WHERE authorization_id=?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), authorization.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = m.AccessToken(context.Background(), authorization.ID); !errors.Is(err, ErrReauthorizationRequired) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestAuthorizationInvalidGrantRequiresReauthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "refresh token was revoked"})
	}))
	defer server.Close()
	client, err := NewOfficialServiceClient(server.URL, true, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	m := NewAuthorizationManager(newPluginTestDB(t), t.TempDir(), client)
	authorization := saveExpiringAuthorization(t, m)
	if _, err := m.AccessToken(context.Background(), authorization.ID); !errors.Is(err, ErrReauthorizationRequired) {
		t.Fatalf("error=%v", err)
	}
	items, err := m.List(context.Background())
	if err != nil || len(items) != 1 || items[0].Status != "reauthorization_required" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestLicensedDownloadMapsMissingCredentialsToAuthorizationRequired(t *testing.T) {
	entry := CatalogEntry{ID: "org.example.paid", Version: "1.2.3", Package: "packages/paid.nxp", PackageSHA256: strings.Repeat("a", 64), Size: 42, Access: CatalogAccessLicensed}
	db, dataDir := newPluginTestDB(t), t.TempDir()
	svc := NewServiceWithOfficialClient(db, dataDir, resolvedTestCatalog{entry: entry}, developerTestRuntime{}, &OfficialServiceClient{})
	authorization, err := svc.authorizations.save(context.Background(), TokenResponse{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900, RefreshExpiresIn: 3600, Account: AccountResponse{ID: "acct-1", DisplayName: "One"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.authorizations.Bind(context.Background(), entry.ID, authorization.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(svc.authorizations.keyPath); err != nil {
		t.Fatal(err)
	}
	err = svc.fetchOfficialPackage(context.Background(), entry, filepath.Join(t.TempDir(), "plugin.nxp"))
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Code != "authorization_required" {
		t.Fatalf("error=%v", err)
	}
	if serviceErr.Details["plugin_id"] != entry.ID || serviceErr.Details["version"] != entry.Version {
		t.Fatalf("details=%v", serviceErr.Details)
	}
	accounts, ok := serviceErr.Details["authorizations"].([]AuthorizationSummary)
	if !ok || len(accounts) != 1 || accounts[0].Status != "reauthorization_required" {
		t.Fatalf("accounts=%#v", serviceErr.Details["authorizations"])
	}
	err = svc.BindAuthorization(context.Background(), entry.ID, entry.Version, authorization.ID)
	if !errors.As(err, &serviceErr) || serviceErr.Code != "authorization_required" {
		t.Fatalf("bind error=%v", err)
	}
	if serviceErr.Details["plugin_id"] != entry.ID || serviceErr.Details["version"] != entry.Version {
		t.Fatalf("bind details=%v", serviceErr.Details)
	}
}
