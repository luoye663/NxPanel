package plugin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxKVValueBytes    = 64 << 10
	maxKVPluginBytes   = 1 << 20
	maxHTTPResultBytes = 4 << 20
)

// SQLCapabilityBroker implements the small, versioned host surface. New host
// methods must be registered here with an explicit manifest permission.
type SQLCapabilityBroker struct {
	db       *sql.DB
	resolver *net.Resolver
}

func NewCapabilityBroker(db *sql.DB) *SQLCapabilityBroker {
	return &SQLCapabilityBroker{db: db, resolver: net.DefaultResolver}
}

var brokerPermissions = map[string]string{
	"kv.get": "plugin.kv", "kv.put": "plugin.kv", "kv.delete": "plugin.kv",
	"events.publish": "plugin.events", "http.fetch": "http.fetch",
}

func (b *SQLCapabilityBroker) Call(ctx context.Context, pluginID, method string, payload json.RawMessage) (json.RawMessage, error) {
	permission, registered := brokerPermissions[method]
	if !registered {
		return nil, errors.New("unknown host capability")
	}
	if pluginID == "" || b.db == nil {
		return nil, errors.New("host capability denied")
	}
	var allowed int
	err := b.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plugin_permissions p JOIN plugin_installations i ON i.plugin_id=p.plugin_id WHERE p.plugin_id=? AND p.permission=? AND i.enabled=1`, pluginID, permission).Scan(&allowed)
	if err != nil || allowed != 1 {
		return nil, errors.New("plugin permission denied")
	}
	switch method {
	case "kv.get":
		return b.kvGet(ctx, pluginID, payload)
	case "kv.put":
		return b.kvPut(ctx, pluginID, payload)
	case "kv.delete":
		return b.kvDelete(ctx, pluginID, payload)
	case "events.publish":
		return b.publishEvent(ctx, pluginID, payload)
	case "http.fetch":
		return b.fetch(ctx, pluginID, payload)
	default:
		return nil, errors.New("unknown host capability")
	}
}

type kvRequest struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value,omitempty"`
}

func validPluginKey(key string) bool {
	return key != "" && len(key) <= 128 && !strings.ContainsAny(key, "\x00\r\n")
}

func (b *SQLCapabilityBroker) kvGet(ctx context.Context, pluginID string, raw json.RawMessage) (json.RawMessage, error) {
	var req kvRequest
	if json.Unmarshal(raw, &req) != nil || !validPluginKey(req.Key) {
		return nil, errors.New("invalid KV key")
	}
	var value []byte
	err := b.db.QueryRowContext(ctx, `SELECT value FROM plugin_kv WHERE plugin_id=? AND key=?`, pluginID, req.Key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return json.RawMessage(`{"value":null}`), nil
	}
	if err != nil {
		return nil, err
	}
	result, _ := json.Marshal(map[string]json.RawMessage{"value": value})
	return result, nil
}

func (b *SQLCapabilityBroker) kvPut(ctx context.Context, pluginID string, raw json.RawMessage) (json.RawMessage, error) {
	var req kvRequest
	if json.Unmarshal(raw, &req) != nil || !validPluginKey(req.Key) || len(req.Value) == 0 || len(req.Value) > maxKVValueBytes || !json.Valid(req.Value) {
		return nil, errors.New("invalid KV value")
	}
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var used int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(length(value)),0) FROM plugin_kv WHERE plugin_id=? AND key<>?`, pluginID, req.Key).Scan(&used); err != nil {
		return nil, err
	}
	if used+int64(len(req.Value)) > maxKVPluginBytes {
		return nil, errors.New("plugin KV quota exceeded")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO plugin_kv(plugin_id,key,value,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(plugin_id,key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, pluginID, req.Key, []byte(req.Value), time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"stored":true}`), nil
}

func (b *SQLCapabilityBroker) kvDelete(ctx context.Context, pluginID string, raw json.RawMessage) (json.RawMessage, error) {
	var req kvRequest
	if json.Unmarshal(raw, &req) != nil || !validPluginKey(req.Key) {
		return nil, errors.New("invalid KV key")
	}
	_, err := b.db.ExecContext(ctx, `DELETE FROM plugin_kv WHERE plugin_id=? AND key=?`, pluginID, req.Key)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(`{"deleted":true}`), nil
}

type eventRequest struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (b *SQLCapabilityBroker) publishEvent(ctx context.Context, pluginID string, raw json.RawMessage) (json.RawMessage, error) {
	var req eventRequest
	if json.Unmarshal(raw, &req) != nil || req.Type == "" || len(req.Type) > 80 || len(req.Data) > maxKVValueBytes || !json.Valid(req.Data) {
		return nil, errors.New("invalid plugin event")
	}
	// A time-derived ID is sufficient for this append-only, plugin-scoped queue;
	// the plugin id and nanosecond suffix avoid cross-plugin collisions.
	id := fmt.Sprintf("%s-%d", strings.ReplaceAll(pluginID, ".", "-"), time.Now().UnixNano())
	_, err := b.db.ExecContext(ctx, `INSERT INTO plugin_events(id,plugin_id,event_type,payload_json,created_at) VALUES(?,?,?,?,?)`, id, pluginID, req.Type, string(req.Data), time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return json.RawMessage(`{"published":true}`), nil
}

type fetchRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

type fetchResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers"`
	Body    string              `json:"body"`
}

func (b *SQLCapabilityBroker) fetch(ctx context.Context, pluginID string, raw json.RawMessage) (json.RawMessage, error) {
	var req fetchRequest
	if json.Unmarshal(raw, &req) != nil || len(req.Body) > MaxRPCBytes {
		return nil, errors.New("invalid HTTP request")
	}
	u, err := url.Parse(req.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("only public HTTPS URLs are allowed")
	}
	if err := b.allowManifestDomain(ctx, pluginID, u.Hostname()); err != nil {
		return nil, err
	}
	method := strings.ToUpper(req.Method)
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodPost {
		return nil, errors.New("HTTP method is not allowed")
	}
	request, err := http.NewRequestWithContext(ctx, method, u.String(), strings.NewReader(req.Body))
	if err != nil {
		return nil, err
	}
	for key, value := range req.Headers {
		if !strings.EqualFold(key, "Host") && !strings.EqualFold(key, "Cookie") && len(key) <= 80 && len(value) <= 4096 {
			request.Header.Set(key, value)
		}
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{Proxy: nil, DialContext: func(callCtx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := b.resolver.LookupIPAddr(callCtx, host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("HTTP host resolution failed")
		}
		for _, ip := range ips {
			if !publicIP(ip.IP) {
				return nil, errors.New("private HTTP destinations are forbidden")
			}
		}
		return dialer.DialContext(callCtx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	redirects := 0
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		redirects++
		if redirects > 3 || next.URL.Scheme != "https" {
			return errors.New("HTTP redirect is not allowed")
		}
		return b.allowManifestDomain(next.Context(), pluginID, next.URL.Hostname())
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxHTTPResultBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxHTTPResultBytes {
		return nil, errors.New("HTTP response exceeds 4 MiB")
	}
	encoded, _ := json.Marshal(fetchResponse{Status: response.StatusCode, Headers: response.Header, Body: string(bytes.ToValidUTF8(body, []byte("�")))})
	return encoded, nil
}

func (b *SQLCapabilityBroker) allowManifestDomain(ctx context.Context, pluginID, host string) error {
	var manifestJSON string
	if err := b.db.QueryRowContext(ctx, `SELECT v.manifest_json FROM plugin_installations i JOIN plugin_versions v
		ON v.plugin_id=i.plugin_id AND v.version=i.active_version WHERE i.plugin_id=?`, pluginID).Scan(&manifestJSON); err != nil {
		return errors.New("plugin manifest is unavailable")
	}
	var manifest Manifest
	if json.Unmarshal([]byte(manifestJSON), &manifest) != nil {
		return errors.New("plugin manifest is invalid")
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, allowed := range manifest.NetworkDomains {
		allowed = strings.ToLower(strings.TrimSuffix(allowed, "."))
		if host == allowed {
			return nil
		}
	}
	return errors.New("HTTP domain is not declared by the plugin")
}

func publicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}
