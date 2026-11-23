package agent

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
	"github.com/luoye663/nxpanel/internal/trust"
)

type moduleTestTransport func(*http.Request) (*http.Response, error)

func (transport moduleTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestRootPolicyAuthorizesMirroredReleaseAndOfflineReceipt(t *testing.T) {
	keyPair := func(id string) (string, ed25519.PrivateKey) {
		t.Helper()
		public, private, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		return id + ":" + base64.StdEncoding.EncodeToString(public), private
	}
	root, rootPrivate := keyPair("root")
	publisher, publisherPrivate := keyPair("publisher")
	oldRoot, oldClient := trust.RootPublicKey, moduleCatalogClient
	trust.RootPublicKey = root
	t.Cleanup(func() { trust.RootPublicKey, moduleCatalogClient = oldRoot, oldClient })
	entry := trust.Module{ID: "geoip2", Name: "GeoIP2", Description: "GeoIP2 module", Repository: "official/geoip2", Symbol: nginxmodule.Symbol, Keys: publisher}
	now := time.Now()
	policy := trust.Policy{Version: 1, Sequence: 1, IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Modules: []trust.Module{entry}}
	snapshot := func() moduleIndexSnapshot {
		t.Helper()
		body, err := json.Marshal(policy)
		if err != nil {
			t.Fatal(err)
		}
		signed, err := trust.Sign(body, rootPrivate)
		if err != nil {
			t.Fatal(err)
		}
		return moduleIndexSnapshot{Index: signed.Policy, Signature: signed.Signature}
	}
	const host = "modules.example.test"
	source := moduleSource{base: "https://" + host + "/v1/"}
	original := snapshot()
	feed, err := source.verifyIndex(original, nginxmodule.Floor{})
	if err != nil {
		t.Fatal(err)
	}
	registered, ok := feed.entry(entry.ID)
	if !ok {
		t.Fatal("root-authorized module missing")
	}
	module := registered.DisplayModule()
	module.Release.Tag = "v3.4.0"
	script := []byte("fixture uninstall")
	digest := sha256Text(script)
	module.Install = nginxmodule.Script{Name: nginxmodule.ScriptName(module.ID, "install", digest), Size: int64(len(script)), SHA256: digest}
	module.Uninstall = nginxmodule.Script{Name: nginxmodule.ScriptName(module.ID, "uninstall", digest), Size: int64(len(script)), SHA256: digest}
	manifest := nginxmodule.Manifest{Protocol: nginxmodule.BuildProtocol, Module: module, Build: &nginxmodule.BuildRecipe{Source: nginxmodule.Script{Name: nginxmodule.SourceName(module.ID, digest), Size: 10, SHA256: digest}}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := json.Marshal(nginxmodule.Sign(raw, map[string]ed25519.PrivateKey{"publisher": publisherPrivate}, nginxmodule.ReleasePurpose, false))
	if err != nil {
		t.Fatal(err)
	}
	releasePath := "/v1/repos/official/geoip2/releases/"
	assets := releasePath + "download/v3.4.0/"
	files := map[string][]byte{
		releasePath + "tags/v3.4.0.json":  []byte(`{"tag_name":"v3.4.0","draft":false,"prerelease":false,"published_at":"2026-01-01T00:00:00Z"}`),
		assets + nginxmodule.ManifestName: raw,
		assets + nginxmodule.ManifestSig:  signature,
	}
	moduleCatalogClient = &http.Client{Transport: moduleTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme != "https" || request.URL.Host != host {
			t.Fatalf("module Agent left the store origin: %s", request.URL)
		}
		status, body := http.StatusOK, files[request.URL.Path]
		if body == nil {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	_, receipt, err := source.releaseManifest(t.Context(), registered, module.Release.Tag)
	if err != nil {
		t.Fatal(err)
	}
	policy.Sequence++
	policy.IssuedAt, policy.ExpiresAt = now.Add(-2*time.Hour), now.Add(-time.Hour)
	expired, err := source.verifyIndex(snapshot(), feed.floor)
	if err != nil || expired.policy.RequireCurrent(now) == nil {
		t.Fatalf("expired policy must remain usable only as an offline proof: %v", err)
	}
	registered, _ = expired.entry(entry.ID)
	offline, err := nginxmodule.VerifyManifest(receipt.Manifest, receipt.Signature, registered, receipt.Tag, false)
	if err != nil || !moduleScriptMatches(script, offline.Module.Uninstall) {
		t.Fatalf("offline receipt cannot be verified against cached authorization: %v", err)
	}
	policy.Sequence++
	policy.ExpiresAt = now.Add(time.Hour)
	policy.Modules = []trust.Module{}
	revoked, err := source.verifyIndex(snapshot(), expired.floor)
	if _, ok := revoked.entry(entry.ID); err != nil || ok {
		t.Fatal("revoked module still authorized")
	}
	if _, err := source.verifyIndex(original, revoked.floor); err == nil {
		t.Fatal("replayed root policy restored revoked module")
	}
}

func TestModuleReleaseDiscoveryAndIndependentHistory(t *testing.T) {
	const origin = "http://127.0.0.1/"
	source := moduleSource{test: true, base: origin}
	files := make(map[string][]byte)
	var requests []string
	oldClient := moduleCatalogClient
	t.Cleanup(func() { moduleCatalogClient = oldClient })
	moduleCatalogClient = &http.Client{Transport: moduleTestTransport(func(request *http.Request) (*http.Response, error) {
		path := request.URL.Path
		requests = append(requests, path)
		data, found := files[path]
		status := http.StatusOK
		if !found {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	put := func(path string, value any) []byte {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = body
		return body
	}
	newModule := func(id string) (nginxmodule.IndexEntry, ed25519.PrivateKey) {
		t.Helper()
		public, private, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		entry := nginxmodule.IndexEntry{ID: id, Name: id, Description: "Example module", Repository: "fixture/" + id,
			Symbol: "ngx_http_" + id + "_module", Keys: "test:" + base64.StdEncoding.EncodeToString(public)}
		if id == nginxmodule.GeoIP2 {
			entry.Symbol = nginxmodule.Symbol
		}
		return entry, private
	}
	first, firstKey := newModule(nginxmodule.GeoIP2)
	second, secondKey := newModule("other")
	base := func(entry nginxmodule.IndexEntry) string { return "/repos/" + entry.Repository + "/releases" }
	metadata := func(tag string, preview bool) map[string]any {
		return map[string]any{"tag_name": tag, "draft": false, "prerelease": preview, "published_at": "2025-01-01T00:00:00Z"}
	}
	publish := func(entry nginxmodule.IndexEntry, key ed25519.PrivateKey, tag string) {
		module := entry.DisplayModule()
		module.Release.Tag = tag
		digest := strings.Repeat("a", 64)
		module.Install = nginxmodule.Script{Name: nginxmodule.ScriptName(entry.ID, "install", digest), Size: 10, SHA256: digest}
		module.Uninstall = nginxmodule.Script{Name: nginxmodule.ScriptName(entry.ID, "uninstall", digest), Size: 10, SHA256: digest}
		manifest := nginxmodule.Manifest{Protocol: nginxmodule.Protocol, Module: module, Assets: []nginxmodule.Asset{{
			Module: entry.ID, Version: tag, Target: nginxmodule.Target{OS: "debian:12", Arch: "amd64", Runtime: "nginx", RuntimeVersion: "1.27.5", BinarySHA256: digest},
			Name: nginxmodule.AssetName(entry.ID, digest), SHA256: digest, Size: 10,
		}}}
		put(base(entry)+"/tags/"+tag+".json", metadata(tag, false))
		dir := base(entry) + "/download/" + tag + "/"
		raw := put(dir+nginxmodule.ManifestName, manifest)
		put(dir+nginxmodule.ManifestSig, nginxmodule.Sign(raw, map[string]ed25519.PrivateKey{"test": key}, nginxmodule.ReleasePurpose, true))
	}
	publish(first, firstKey, "v1.0.0")
	publish(second, secondKey, "v1.0.0")
	publish(first, firstKey, "v3.0.0")
	put(base(first)+"/latest.json", metadata("v3.0.0", false))
	put(base(first)+"/tags/v4.0.0.json", metadata("v4.0.0", true))
	page := []any{metadata("v4.0.0", true)}
	for number := nginxmodule.ReleasePageSize - 1; number > 0; number-- {
		page = append(page, metadata(fmt.Sprintf("v2.%d.0", number), false))
	}
	put(base(first)+"/pages/1.json", page)
	put(base(first)+"/pages/2.json", []any{metadata("v1.0.0", false)})
	if result, err := source.releasePage(t.Context(), first, 1); err != nil || result.Latest != "v3.0.0" || result.NextPage != 2 || len(result.Tags) != len(page) || result.Tags[0] != result.Latest {
		t.Fatalf("latest was not independently discovered or prerelease was included: %+v %v", result, err)
	}
	if result, err := source.releasePage(t.Context(), first, 2); err != nil || result.NextPage != 0 || len(result.Tags) != 1 || result.Tags[0] != "v1.0.0" {
		t.Fatalf("historical page missing: %+v %v", result, err)
	}
	for _, choice := range []struct {
		entry nginxmodule.IndexEntry
		tag   string
	}{{first, "v3.0.0"}, {first, "v1.0.0"}, {second, "v1.0.0"}} {
		manifest, receipt, err := source.releaseManifest(t.Context(), choice.entry, choice.tag)
		if err != nil || manifest.Module.ID != choice.entry.ID || receipt.Tag != choice.tag {
			t.Fatalf("could not verify independently selected release: %+v %v", manifest, err)
		}
	}
	if _, _, err := source.releaseManifest(t.Context(), first, "v4.0.0"); err == nil {
		t.Fatal("prerelease was installable")
	}
	for _, path := range requests {
		if strings.HasSuffix(path, ".sh") || strings.HasSuffix(path, ".so") {
			t.Fatalf("preparing a release fetched an executable asset: %s", path)
		}
	}
}
