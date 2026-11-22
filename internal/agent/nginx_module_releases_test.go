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

	"github.com/luoye663/nxpanel/internal/nginxmodule"
)

type moduleTestTransport func(*http.Request) (*http.Response, error)

func (transport moduleTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
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
