package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

type ServiceError struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (e *ServiceError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

type OAuthError struct{ Code, Description string }

func (e *OAuthError) Error() string {
	if e.Description != "" {
		return e.Description
	}
	return e.Code
}

type DownloadResolveRequest struct {
	PluginID     string `json:"plugin_id"`
	Version      string `json:"version"`
	Target       string `json:"target"`
	InstanceUUID string `json:"instance_uuid"`
	PanelVersion string `json:"panel_version"`
	Runtime      string `json:"runtime"`
}

type DownloadResolveResponse struct {
	DownloadURL string    `json:"download_url"`
	ExpiresAt   time.Time `json:"expires_at"`
	Target      string    `json:"target"`
	SHA256      string    `json:"sha256"`
	Length      int64     `json:"length"`
}

type DeviceCodeRequest struct{ ClientID, PluginID, Version, InstanceUUID string }
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}
type AccountResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	EmailMasked string `json:"email_masked"`
}
type TokenResponse struct {
	AuthorizationID  string          `json:"-"`
	TokenType        string          `json:"token_type"`
	AccessToken      string          `json:"access_token"`
	RefreshToken     string          `json:"refresh_token"`
	ExpiresIn        int             `json:"expires_in"`
	RefreshExpiresIn int             `json:"refresh_expires_in"`
	Account          AccountResponse `json:"account"`
}

type OfficialServiceClient struct {
	base string
	http *http.Client
}

func NewOfficialServiceClient(raw string, allowHTTP bool, client *http.Client) (*OfficialServiceClient, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || u.Host == "" {
		return nil, ErrRepositoryNotConfigured
	}
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return nil, errors.New("official plugin service URL must use HTTPS (HTTP is allowed only for loopback debugging)")
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	client = secureHTTPClient(client, allowHTTP)
	return &OfficialServiceClient{base: u.String(), http: client}, nil
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func secureHTTPClient(source *http.Client, allowLoopbackHTTP bool) *http.Client {
	copy := *source
	previous := source.CheckRedirect
	copy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("plugin service redirect limit exceeded")
		}
		if req.URL.Scheme != "https" && !(allowLoopbackHTTP && req.URL.Scheme == "http" && isLoopbackHost(req.URL.Hostname())) {
			return errors.New("plugin service redirect must use HTTPS")
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
	return &copy
}
func (c *OfficialServiceClient) ResolveDownload(ctx context.Context, req DownloadResolveRequest, bearer string) (DownloadResolveResponse, error) {
	var out DownloadResolveResponse
	err := c.request(ctx, "/api/v1/downloads/resolve", req, bearer, &out, false)
	return out, err
}
func (c *OfficialServiceClient) StartDevice(ctx context.Context, req DeviceCodeRequest) (DeviceCodeResponse, error) {
	var out DeviceCodeResponse
	err := c.request(ctx, "/api/v1/oauth/device/code", map[string]string{"client_id": req.ClientID, "plugin_id": req.PluginID, "version": req.Version, "instance_uuid": req.InstanceUUID}, "", &out, false)
	return out, err
}
func (c *OfficialServiceClient) ExchangeDevice(ctx context.Context, code string) (TokenResponse, error) {
	var out TokenResponse
	err := c.request(ctx, "/api/v1/oauth/token", map[string]string{"grant_type": deviceGrantType, "device_code": code, "client_id": "nxpanel"}, "", &out, true)
	return out, err
}
func (c *OfficialServiceClient) Refresh(ctx context.Context, token string) (TokenResponse, error) {
	var out TokenResponse
	err := c.request(ctx, "/api/v1/oauth/token", map[string]string{"grant_type": "refresh_token", "refresh_token": token, "client_id": "nxpanel"}, "", &out, true)
	return out, err
}
func (c *OfficialServiceClient) Revoke(ctx context.Context, token string) error {
	return c.request(ctx, "/api/v1/oauth/revoke", map[string]string{"token": token, "token_type_hint": "refresh_token"}, "", nil, false)
}

func (c *OfficialServiceClient) request(ctx context.Context, path string, input any, bearer string, output any, oauth bool) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if oauth {
			var oe struct {
				Error       string `json:"error"`
				Description string `json:"error_description"`
			}
			if json.Unmarshal(data, &oe) == nil && oe.Error != "" {
				return &OAuthError{Code: oe.Error, Description: oe.Description}
			}
		}
		var env struct {
			Error struct {
				Code    string         `json:"code"`
				Message string         `json:"message"`
				Details map[string]any `json:"details"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &env)
		if env.Error.Code == "" {
			return fmt.Errorf("plugin service returned HTTP %d", resp.StatusCode)
		}
		return &ServiceError{Status: resp.StatusCode, Code: env.Error.Code, Message: env.Error.Message, Details: env.Error.Details}
	}
	if output == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("decode plugin service response: %w", err)
	}
	return nil
}
