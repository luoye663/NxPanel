package plugin

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"

	paneldb "github.com/luoye663/nxpanel/internal/db"
)

func TestCapabilityBrokerEnforcesPermissionAndKVQuota(t *testing.T) {
	database, err := paneldb.Open(paneldb.DSNFromPath(filepath.Join(t.TempDir(), "broker.db"), 1000))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := paneldb.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	manifest := &Manifest{SchemaVersion: 1, ID: "com.example.broker", Name: "Broker", Version: "1.0.0", Publisher: "Example", PluginAPIVersion: "v1", Backend: "plugin.wasm", Permissions: []string{"plugin.kv"}, Files: []FileDigest{{Path: "plugin.wasm", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 8}}}
	if err := NewRepository(database).Activate(context.Background(), manifest, "hash", "/plugins/broker", manifest.Permissions); err != nil {
		t.Fatal(err)
	}
	b := NewCapabilityBroker(database)
	if _, err := b.Call(context.Background(), manifest.ID, "kv.get", json.RawMessage(`{"key":"config"}`)); err == nil {
		t.Fatal("disabled plugin used a retained permission")
	}
	if err := NewRepository(database).SetEnabled(context.Background(), manifest.ID, true); err != nil {
		t.Fatal(err)
	}
	stored, err := b.Call(context.Background(), manifest.ID, "kv.put", json.RawMessage(`{"key":"config","value":{"enabled":true}}`))
	if err != nil || string(stored) != `{"stored":true}` {
		t.Fatalf("put = %s, %v", stored, err)
	}
	got, err := b.Call(context.Background(), manifest.ID, "kv.get", json.RawMessage(`{"key":"config"}`))
	if err != nil || string(got) != `{"value":{"enabled":true}}` {
		t.Fatalf("get = %s, %v", got, err)
	}
	if _, err := b.Call(context.Background(), manifest.ID, "http.fetch", json.RawMessage(`{"url":"https://example.com"}`)); err == nil {
		t.Fatal("http.fetch succeeded without permission")
	}
	large, _ := json.Marshal(map[string]any{"key": "large", "value": string(make([]byte, maxKVValueBytes+1))})
	if _, err := b.Call(context.Background(), manifest.ID, "kv.put", large); err == nil {
		t.Fatal("oversized KV value accepted")
	}
}

func TestPublicIPRejectsNonPublicDestinations(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "::1", "fc00::1"} {
		if publicIP(net.ParseIP(value)) {
			t.Fatalf("accepted non-public IP %s", value)
		}
	}
}
