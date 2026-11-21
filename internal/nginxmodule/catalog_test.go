package nginxmodule

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/update"
)

func signedTestCatalog(t *testing.T, catalog Catalog, private ed25519.PrivateKey, purpose string) []byte {
	t.Helper()
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := json.Marshal(Envelope{Catalog: raw, Signatures: map[string]string{
		"test": base64.StdEncoding.EncodeToString(ed25519.Sign(private, update.SignatureMessage(purpose, raw))),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func withTestScripts(module Module) Module {
	digest := strings.Repeat("a", 64)
	module.Install = Script{Name: ScriptName(module.ID, "install", digest), SHA256: digest, Size: 12}
	module.Uninstall = Script{Name: ScriptName(module.ID, "uninstall", digest), SHA256: digest, Size: 12}
	return module
}

func TestCatalogTrustAndCompatibility(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, foreign, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]ed25519.PublicKey{"test": public}
	now := time.Now().UTC()
	target := Target{OS: "debian:12", Arch: "amd64", Runtime: "nginx", RuntimeVersion: "1.28.0", BinarySHA256: strings.Repeat("b", 64)}
	digest := strings.Repeat("a", 64)
	module := withTestScripts(GeoIP2Module())
	module.Release = Release{Repository: "official/modules", Tag: "nginx-modules-v3"}
	catalog := Catalog{Product: Product, Protocol: Protocol, Sequence: 10, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Modules: []Module{module},
		Assets: []Asset{{Module: GeoIP2, Version: "v3.4.0", Target: target, Name: AssetName(GeoIP2, digest), Size: 1024, SHA256: digest}}}
	raw := signedTestCatalog(t, catalog, private, Purpose)
	verified, floor, err := Verify(raw, keys, now, Floor{})
	if err != nil {
		t.Fatal(err)
	}
	if asset, ok := verified.Match(GeoIP2, target); !ok || asset.SHA256 != digest {
		t.Fatalf("matching target not found: %+v", asset)
	}
	for _, mismatch := range []func(*Target){
		func(target *Target) { target.Arch = "arm64" }, func(target *Target) { target.OS = "ubuntu:24.04" },
		func(target *Target) { target.BinarySHA256 = digest }, func(target *Target) { target.RuntimeVersion += ".1" },
		func(target *Target) { target.Runtime = "openresty" },
	} {
		probe := target
		mismatch(&probe)
		if _, ok := verified.Match(GeoIP2, probe); ok {
			t.Fatalf("incorrect match for %+v", probe)
		}
	}
	if _, _, err := Verify(raw, keys, now, floor); err != nil {
		t.Fatal(err)
	}
	later := now.Add(2 * time.Hour)
	if _, _, err := VerifyInstalled(raw, keys, later, false); err != nil {
		t.Fatalf("original receipt must work after the feed expires: %v", err)
	}
	if _, _, err := VerifyInstalled(raw, map[string]ed25519.PublicKey{"test": foreign.Public().(ed25519.PublicKey)}, later, false); err == nil {
		t.Fatal("receipt verified with revoked key")
	}
	if _, _, err := Verify(raw, keys, later, floor); err == nil {
		t.Fatal("expired receipt usable as a feed")
	}
	for _, test := range []struct {
		name    string
		mutate  func(*Catalog)
		key     ed25519.PrivateKey
		purpose string
	}{
		{"wrong-key", func(*Catalog) {}, foreign, Purpose},
		{"wrong-domain", func(*Catalog) {}, private, ""},
		{"old-domain", func(*Catalog) {}, private, "nxpanel-nginx-modules/v2"},
		{"expired", func(value *Catalog) { value.ExpiresAt = now }, private, Purpose},
		{"future", func(value *Catalog) { value.IssuedAt = now.Add(time.Hour) }, private, Purpose},
		{"rollback", func(value *Catalog) { value.Sequence-- }, private, Purpose},
		{"equivocation", func(value *Catalog) { value.ExpiresAt = now.Add(2 * time.Hour) }, private, Purpose},
		{"duplicate-target", func(value *Catalog) { value.Sequence++; value.Assets = append(value.Assets, value.Assets[0]) }, private, Purpose},
		{"unsafe-name", func(value *Catalog) { value.Assets[0].Name = "../module.so" }, private, Purpose},
		{"oversize", func(value *Catalog) { value.Assets[0].Size = MaxAsset + 1 }, private, Purpose},
		{"missing-install", func(value *Catalog) { value.Modules[0].Install = Script{} }, private, Purpose},
		{"oversize-uninstall", func(value *Catalog) { value.Modules[0].Uninstall.Size = MaxScript + 1 }, private, Purpose},
		{"wrong-script-purpose", func(value *Catalog) { value.Modules[0].Uninstall = value.Modules[0].Install }, private, Purpose},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := catalog
			probe.Modules = append([]Module(nil), catalog.Modules...)
			probe.Assets = append([]Asset(nil), catalog.Assets...)
			test.mutate(&probe)
			if _, _, err := Verify(signedTestCatalog(t, probe, test.key, test.purpose), keys, now, floor); err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
	encoded := base64.StdEncoding.EncodeToString(public)
	if _, err := Keys("mod:"+encoded, "update:"+encoded); err == nil {
		t.Fatal("accepted shared trust key under a different identifier")
	}
}

func TestCatalogTestDomainIsolation(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]ed25519.PublicKey{"test": public}
	now := time.Now().UTC()
	digest := strings.Repeat("a", 64)
	for _, target := range []Target{
		{OS: "ubuntu:22.04", Arch: "amd64", Runtime: "openresty", RuntimeVersion: "1.31.1.1", BinarySHA256: digest},
		{OS: "alpine:3.22", Arch: "arm64", Runtime: "nginx", RuntimeVersion: "1.27.1", BinarySHA256: digest},
	} {
		catalog := Catalog{Product: TestProduct, Protocol: Protocol, Sequence: 1, IssuedAt: now, ExpiresAt: now.Add(time.Hour), Modules: []Module{withTestScripts(GeoIP2Module())},
			Assets: []Asset{{Module: GeoIP2, Version: "v3.4.0", Target: target, Name: AssetName(GeoIP2, digest), Size: 1234, SHA256: digest}}}
		raw := signedTestCatalog(t, catalog, private, TestPurpose)
		verified, floor, err := VerifyTest(raw, keys, now, Floor{})
		if err != nil || floor.Sequence != 1 {
			t.Fatalf("test catalog should verify: %v", err)
		}
		mismatch := target
		mismatch.BinarySHA256 = strings.Repeat("b", 64)
		if _, ok := verified.Match(GeoIP2, mismatch); ok {
			t.Fatal("test catalog matched a different binary")
		}
		if _, _, err := Verify(raw, keys, now, Floor{}); err == nil {
			t.Fatal("production accepted a test signature")
		}
		if _, _, err := VerifyTest(signedTestCatalog(t, catalog, private, Purpose), keys, now, Floor{}); err == nil {
			t.Fatal("test verification accepted a production signature")
		}
		catalog.Product = Product
		if err := catalog.Validate(now); err == nil {
			t.Fatal("test target entered the production matrix")
		}
	}
}

func TestCatalogEntryAndReleaseChanges(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]ed25519.PublicKey{"test": public}
	now := time.Now()
	digest := strings.Repeat("a", 64)
	geo := withTestScripts(GeoIP2Module())
	geo.Release = Release{Repository: "org/module-assets", Tag: "v2"}
	fixture := withTestScripts(Module{ID: "fixture-http", Name: "Fixture", Description: "Fixture module", Kind: DynamicKind, Symbol: "ngx_http_nxpanel_fixture_module", Release: geo.Release})
	target := Target{OS: "alpine:3.22", Arch: "amd64", Runtime: "nginx", RuntimeVersion: "1.27.5", BinarySHA256: digest}
	geoAsset := Asset{Module: geo.ID, Version: "v3.4.0", Target: target, Name: AssetName(geo.ID, digest), Size: 100, SHA256: digest}
	fixtureAsset := Asset{Module: fixture.ID, Version: "v1.0.0", Target: target, Name: AssetName(fixture.ID, digest), Size: 100, SHA256: digest}
	first := Catalog{Product: Product, Protocol: Protocol, Sequence: 1, IssuedAt: now, ExpiresAt: now.Add(time.Hour), Modules: []Module{geo}, Assets: []Asset{geoAsset}}
	_, floor, err := Verify(signedTestCatalog(t, first, private, Purpose), keys, now, Floor{})
	if err != nil {
		t.Fatal(err)
	}
	fixture.Name, fixture.Release.Repository = "Fixture renamed", "other-org/release-assets"
	second := first
	second.Sequence++
	second.Modules, second.Assets = []Module{fixture}, []Asset{fixtureAsset}
	verified, next, err := Verify(signedTestCatalog(t, second, private, Purpose), keys, now, floor)
	if err != nil || next.Sequence != 2 || verified.Modules[0].Name != fixture.Name {
		t.Fatalf("updated signed metadata not accepted: %+v %v", verified, err)
	}
	if _, ok := verified.Match(geo.ID, target); ok {
		t.Fatal("removed module still matches the target")
	}
	if asset, ok := verified.Match(fixture.ID, target); !ok || ReleaseURL(verified.Modules[0].Release, asset.Name) != "https://github.com/other-org/release-assets/releases/download/v2/"+fixtureAsset.Name {
		t.Fatal("asset did not follow the repository from signed metadata")
	}
	second.Modules, second.Assets = []Module{geo, fixture}, []Asset{geoAsset, fixtureAsset}
	if err := second.Validate(now); err != nil {
		t.Fatalf("distinct modules must be able to share a runtime target: %v", err)
	}
	for _, invalid := range []Release{
		{Repository: "https://example.org/project", Tag: "v2"}, {Repository: "org/../path", Tag: "v2"}, {Repository: "org/repo", Tag: "v2?token="},
	} {
		second.Modules[1].Release = invalid
		if err := second.Validate(now); err == nil {
			t.Fatalf("invalid GitHub coordinates accepted: %+v", invalid)
		}
	}
}
