package plugin

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const authorizationAAD = "nxpanel/plugin-authorization/v1"

var (
	ErrAuthorizationNotFound = errors.New("plugin authorization not found")
	ErrDeviceAttemptNotFound = errors.New("device authorization attempt is invalid or expired")
)

type AuthorizationSummary struct {
	ID               string    `json:"authorization_id"`
	AccountID        string    `json:"account_id"`
	DisplayName      string    `json:"account_label"`
	EmailMasked      string    `json:"account_email,omitempty"`
	Status           string    `json:"status"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	CreatedAt        time.Time `json:"created_at"`
	LastUsedAt       time.Time `json:"last_used_at"`
}

type authorizationTokens struct {
	AuthorizationSummary
	AccessToken  string
	RefreshToken string
}

type DeviceAttempt struct {
	AttemptID               string    `json:"attempt_id"`
	PluginID                string    `json:"plugin_id"`
	Version                 string    `json:"version"`
	UserCode                string    `json:"user_code"`
	VerificationURI         string    `json:"verification_uri"`
	VerificationURIComplete string    `json:"verification_uri_complete,omitempty"`
	ExpiresAt               time.Time `json:"expires_at"`
	Interval                int       `json:"interval_seconds"`
	deviceCode              string
	nextPoll                time.Time
}

type AuthorizationManager struct {
	db      *sql.DB
	client  *OfficialServiceClient
	keyPath string
	mu      sync.Mutex
	attempt map[string]DeviceAttempt
}

func NewAuthorizationManager(db *sql.DB, dataDir string, client *OfficialServiceClient) *AuthorizationManager {
	return &AuthorizationManager{db: db, client: client, keyPath: filepath.Join(dataDir, "secrets", "plugin-authorization.key"), attempt: make(map[string]DeviceAttempt)}
}

func (m *AuthorizationManager) List(ctx context.Context) ([]AuthorizationSummary, error) {
	_, keyErr := os.Stat(m.keyPath)
	rows, err := m.db.QueryContext(ctx, `SELECT authorization_id,account_id,display_name,email_masked,status,access_expires_at,refresh_expires_at,created_at,last_used_at FROM plugin_authorizations ORDER BY last_used_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuthorizationSummary, 0)
	for rows.Next() {
		var a AuthorizationSummary
		var access, refresh, created, used string
		if err := rows.Scan(&a.ID, &a.AccountID, &a.DisplayName, &a.EmailMasked, &a.Status, &access, &refresh, &created, &used); err != nil {
			return nil, err
		}
		if a.AccessExpiresAt, err = time.Parse(time.RFC3339, access); err != nil {
			return nil, err
		}
		if a.RefreshExpiresAt, err = time.Parse(time.RFC3339, refresh); err != nil {
			return nil, err
		}
		if a.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, err
		}
		if a.LastUsedAt, err = time.Parse(time.RFC3339, used); err != nil {
			return nil, err
		}
		if errors.Is(keyErr, os.ErrNotExist) {
			a.Status = "reauthorization_required"
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (m *AuthorizationManager) InstanceID(ctx context.Context) (string, error) {
	var id string
	err := m.db.QueryRowContext(ctx, `SELECT instance_uuid FROM plugin_instance_identity WHERE singleton=1`).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	raw := randomHex(16)
	id = raw[0:8] + "-" + raw[8:12] + "-4" + raw[13:16] + "-a" + raw[17:20] + "-" + raw[20:32]
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = m.db.ExecContext(ctx, `INSERT INTO plugin_instance_identity(singleton,instance_uuid,created_at) VALUES(1,?,?) ON CONFLICT(singleton) DO NOTHING`, id, now)
	if err != nil {
		return "", err
	}
	if err := m.db.QueryRowContext(ctx, `SELECT instance_uuid FROM plugin_instance_identity WHERE singleton=1`).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

func (m *AuthorizationManager) StartDevice(ctx context.Context, pluginID, version, instanceID string) (*DeviceAttempt, error) {
	if m.client == nil {
		return nil, ErrRepositoryNotConfigured
	}
	challenge, err := m.client.StartDevice(ctx, DeviceCodeRequest{ClientID: "nxpanel", PluginID: pluginID, Version: version, InstanceUUID: instanceID})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	attempt := DeviceAttempt{AttemptID: randomHex(32), PluginID: pluginID, Version: version, UserCode: challenge.UserCode, VerificationURI: challenge.VerificationURI, VerificationURIComplete: challenge.VerificationURIComplete, ExpiresAt: now.Add(time.Duration(challenge.ExpiresIn) * time.Second), Interval: challenge.Interval, deviceCode: challenge.DeviceCode}
	if attempt.Interval < 1 {
		attempt.Interval = 5
	}
	attempt.nextPoll = now
	m.mu.Lock()
	for id, old := range m.attempt {
		if !old.ExpiresAt.After(now) {
			delete(m.attempt, id)
		}
	}
	if len(m.attempt) >= 128 {
		m.mu.Unlock()
		return nil, errors.New("too many active plugin authorization attempts")
	}
	m.attempt[attempt.AttemptID] = attempt
	m.mu.Unlock()
	copy := attempt
	return &copy, nil
}

type DevicePollResult struct {
	Status        string                `json:"status"`
	Authorization *AuthorizationSummary `json:"authorization,omitempty"`
}

func (m *AuthorizationManager) PollDevice(ctx context.Context, attemptID string) (DevicePollResult, error) {
	now := time.Now().UTC()
	m.mu.Lock()
	a, ok := m.attempt[attemptID]
	if !ok || !a.ExpiresAt.After(now) {
		delete(m.attempt, attemptID)
		m.mu.Unlock()
		return DevicePollResult{}, ErrDeviceAttemptNotFound
	}
	if now.Before(a.nextPoll) {
		m.mu.Unlock()
		return DevicePollResult{Status: "slow_down"}, nil
	}
	a.nextPoll = now.Add(time.Duration(a.Interval) * time.Second)
	m.attempt[attemptID] = a
	m.mu.Unlock()
	tokens, err := m.client.ExchangeDevice(ctx, a.deviceCode)
	if err != nil {
		var oauth *OAuthError
		if errors.As(err, &oauth) {
			switch oauth.Code {
			case "authorization_pending":
				return DevicePollResult{Status: "pending"}, nil
			case "slow_down":
				m.mu.Lock()
				a.nextPoll = time.Now().UTC().Add(time.Duration(a.Interval+5) * time.Second)
				m.attempt[attemptID] = a
				m.mu.Unlock()
				return DevicePollResult{Status: "slow_down"}, nil
			case "access_denied":
				m.deleteAttempt(attemptID)
				return DevicePollResult{Status: "denied"}, nil
			case "expired_token":
				m.deleteAttempt(attemptID)
				return DevicePollResult{Status: "expired"}, nil
			}
		}
		return DevicePollResult{}, err
	}
	summary, err := m.save(ctx, tokens)
	if err != nil {
		return DevicePollResult{}, err
	}
	m.deleteAttempt(attemptID)
	return DevicePollResult{Status: "authorized", Authorization: summary}, nil
}

func (m *AuthorizationManager) deleteAttempt(id string) {
	m.mu.Lock()
	delete(m.attempt, id)
	m.mu.Unlock()
}

func (m *AuthorizationManager) Bind(ctx context.Context, pluginID, authorizationID string) error {
	var exists int
	if err := m.db.QueryRowContext(ctx, `SELECT 1 FROM plugin_authorizations WHERE authorization_id=?`, authorizationID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return ErrAuthorizationNotFound
	} else if err != nil {
		return err
	}
	_, err := m.db.ExecContext(ctx, `INSERT INTO plugin_authorization_bindings(plugin_id,authorization_id,updated_at) VALUES(?,?,?) ON CONFLICT(plugin_id) DO UPDATE SET authorization_id=excluded.authorization_id,updated_at=excluded.updated_at`, pluginID, authorizationID, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (m *AuthorizationManager) BoundID(ctx context.Context, pluginID string) (string, error) {
	var id string
	err := m.db.QueryRowContext(ctx, `SELECT authorization_id FROM plugin_authorization_bindings WHERE plugin_id=?`, pluginID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (m *AuthorizationManager) Tokens(ctx context.Context, id string) (*authorizationTokens, error) {
	var t authorizationTokens
	var accessCipher, accessNonce, refreshCipher, refreshNonce []byte
	var accessAt, refreshAt, created, used string
	err := m.db.QueryRowContext(ctx, `SELECT authorization_id,account_id,display_name,email_masked,status,access_expires_at,refresh_expires_at,created_at,last_used_at,access_ciphertext,access_nonce,refresh_ciphertext,refresh_nonce FROM plugin_authorizations WHERE authorization_id=?`, id).Scan(&t.ID, &t.AccountID, &t.DisplayName, &t.EmailMasked, &t.Status, &accessAt, &refreshAt, &created, &used, &accessCipher, &accessNonce, &refreshCipher, &refreshNonce)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAuthorizationNotFound
	}
	if err != nil {
		return nil, err
	}
	if t.AccessExpiresAt, err = time.Parse(time.RFC3339, accessAt); err != nil {
		return nil, err
	}
	if t.RefreshExpiresAt, err = time.Parse(time.RFC3339, refreshAt); err != nil {
		return nil, err
	}
	if t.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
		return nil, err
	}
	if t.LastUsedAt, err = time.Parse(time.RFC3339, used); err != nil {
		return nil, err
	}
	key, err := os.ReadFile(m.keyPath)
	if err != nil {
		return nil, fmt.Errorf("plugin authorization key unavailable; reauthorize account: %w", err)
	}
	access, err := decryptAuthorization(key, accessNonce, accessCipher, id+":access")
	if err != nil {
		return nil, err
	}
	refresh, err := decryptAuthorization(key, refreshNonce, refreshCipher, id+":refresh")
	if err != nil {
		return nil, err
	}
	t.AccessToken, t.RefreshToken = string(access), string(refresh)
	return &t, nil
}

func (m *AuthorizationManager) AccessToken(ctx context.Context, id string) (string, error) {
	t, err := m.Tokens(ctx, id)
	if err != nil {
		return "", err
	}
	if t.Status != "active" {
		return "", errors.New("plugin authorization requires login")
	}
	if time.Now().UTC().Add(30 * time.Second).Before(t.AccessExpiresAt) {
		return t.AccessToken, nil
	}
	if !time.Now().UTC().Before(t.RefreshExpiresAt) {
		_ = m.markInvalid(ctx, id)
		return "", errors.New("plugin authorization refresh token expired")
	}
	refreshed, err := m.client.Refresh(ctx, t.RefreshToken)
	if err != nil {
		_ = m.markInvalid(ctx, id)
		return "", err
	}
	refreshed.AuthorizationID = id
	if _, err = m.save(ctx, refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func (m *AuthorizationManager) Revoke(ctx context.Context, id string) error {
	t, err := m.Tokens(ctx, id)
	if err != nil {
		if errors.Is(err, ErrAuthorizationNotFound) {
			return err
		}
		_, deleteErr := m.db.ExecContext(ctx, `DELETE FROM plugin_authorizations WHERE authorization_id=?`, id)
		return deleteErr
	}
	if m.client != nil {
		if err := m.client.Revoke(ctx, t.RefreshToken); err != nil {
			return err
		}
	}
	_, err = m.db.ExecContext(ctx, `DELETE FROM plugin_authorizations WHERE authorization_id=?`, id)
	return err
}

func (m *AuthorizationManager) save(ctx context.Context, result TokenResponse) (*AuthorizationSummary, error) {
	id := result.AuthorizationID
	if id == "" {
		id = randomHex(24)
	}
	key, err := m.loadOrCreateKey()
	if err != nil {
		return nil, err
	}
	accessNonce, accessCipher, err := encryptAuthorization(key, []byte(result.AccessToken), id+":access")
	if err != nil {
		return nil, err
	}
	refreshNonce, refreshCipher, err := encryptAuthorization(key, []byte(result.RefreshToken), id+":refresh")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	accessAt := now.Add(time.Duration(result.ExpiresIn) * time.Second)
	refreshAt := now.Add(time.Duration(result.RefreshExpiresIn) * time.Second)
	if result.ExpiresIn <= 0 {
		accessAt = now.Add(15 * time.Minute)
	}
	if result.RefreshExpiresIn <= 0 {
		refreshAt = now.Add(90 * 24 * time.Hour)
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO plugin_authorizations(authorization_id,account_id,display_name,email_masked,access_ciphertext,access_nonce,refresh_ciphertext,refresh_nonce,access_expires_at,refresh_expires_at,status,created_at,last_used_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(authorization_id) DO UPDATE SET account_id=excluded.account_id,display_name=excluded.display_name,email_masked=excluded.email_masked,access_ciphertext=excluded.access_ciphertext,access_nonce=excluded.access_nonce,refresh_ciphertext=excluded.refresh_ciphertext,refresh_nonce=excluded.refresh_nonce,access_expires_at=excluded.access_expires_at,refresh_expires_at=excluded.refresh_expires_at,status='active',last_used_at=excluded.last_used_at`, id, result.Account.ID, result.Account.DisplayName, result.Account.EmailMasked, accessCipher, accessNonce, refreshCipher, refreshNonce, accessAt.Format(time.RFC3339), refreshAt.Format(time.RFC3339), "active", now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return &AuthorizationSummary{ID: id, AccountID: result.Account.ID, DisplayName: result.Account.DisplayName, EmailMasked: result.Account.EmailMasked, Status: "active", AccessExpiresAt: accessAt, RefreshExpiresAt: refreshAt, CreatedAt: now, LastUsedAt: now}, nil
}

func (m *AuthorizationManager) markInvalid(ctx context.Context, id string) error {
	_, err := m.db.ExecContext(ctx, `UPDATE plugin_authorizations SET status='reauthorization_required' WHERE authorization_id=?`, id)
	return err
}

func (m *AuthorizationManager) MarkInvalid(ctx context.Context, id string) error {
	return m.markInvalid(ctx, id)
}
func (m *AuthorizationManager) loadOrCreateKey() ([]byte, error) {
	key, err := os.ReadFile(m.keyPath)
	if err == nil {
		if len(key) != 32 {
			return nil, errors.New("invalid plugin authorization key")
		}
		_ = os.Chmod(m.keyPath, 0o600)
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(m.keyPath), 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(filepath.Dir(m.keyPath), 0o700)
	key = make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(m.keyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return m.loadOrCreateKey()
	}
	if err != nil {
		return nil, err
	}
	_, werr := f.Write(key)
	serr := f.Sync()
	cerr := f.Close()
	if err = errors.Join(werr, serr, cerr); err != nil {
		_ = os.Remove(m.keyPath)
		return nil, err
	}
	return key, nil
}
func encryptAuthorization(key, plain []byte, label string) ([]byte, []byte, error) {
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, nil, e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return nil, nil, e
	}
	n := make([]byte, g.NonceSize())
	if _, e = rand.Read(n); e != nil {
		return nil, nil, e
	}
	return n, g.Seal(nil, n, plain, []byte(authorizationAAD+":"+label)), nil
}
func decryptAuthorization(key, nonce, ciphertext []byte, label string) ([]byte, error) {
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	return g.Open(nil, nonce, ciphertext, []byte(authorizationAAD+":"+label))
}
func randomHex(bytes int) string {
	b := make([]byte, bytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
