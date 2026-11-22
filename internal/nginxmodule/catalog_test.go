package nginxmodule

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func keypair(t *testing.T) (map[string]ed25519.PublicKey, map[string]ed25519.PrivateKey, string) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]ed25519.PublicKey{"test": public}, map[string]ed25519.PrivateKey{"test": private}, "test:" + base64.StdEncoding.EncodeToString(public)
}

func jsonBytes(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestIndexTrustAndRollback(t *testing.T) {
	roots, signers, rootKey := keypair(t)
	_, _, moduleKey := keypair(t)
	updateKeys, _, updateKey := keypair(t)
	entry := IndexEntry{ID: GeoIP2, Name: "GeoIP2", Description: "Country lookup", Repository: "org/geoip2", Symbol: Symbol, Keys: moduleKey}
	verify := func(raw []byte, sequence uint64, floor Floor, test bool) ([]IndexEntry, Floor, error) {
		signed := IndexSignature{Sequence: sequence, Signatures: Sign(IndexPayload(raw, sequence), signers, IndexPurpose, test)}
		return VerifyIndex(raw, jsonBytes(t, signed), roots, updateKeys, floor, false)
	}
	raw := jsonBytes(t, []IndexEntry{entry})
	entries, floor, err := verify(raw, 11, Floor{}, false)
	if err != nil || floor.Sequence != 11 || len(entries) != 1 || entries[0] != entry {
		t.Fatalf("unexpected index: %+v %+v %v", entries, floor, err)
	}
	if _, _, err := verify(raw, 11, floor, false); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		name     string
		raw      []byte
		sequence uint64
		test     bool
	}{
		{"rollback", raw, 10, false},
		{"same sequence changed", append(append([]byte{}, raw...), '\n'), 11, false},
		{"test domain", raw, 12, true},
		{"missing array", []byte("null"), 12, false},
	} {
		t.Run(check.name, func(t *testing.T) {
			if _, _, err := verify(check.raw, check.sequence, floor, check.test); err == nil {
				t.Fatal("accepted invalid index")
			}
		})
	}
	signed := IndexSignature{Sequence: 12, Signatures: Sign(IndexPayload(raw, 12), signers, IndexPurpose, false)}
	signed.Sequence++
	if _, _, err := VerifyIndex(raw, jsonBytes(t, signed), roots, updateKeys, floor, false); err == nil {
		t.Fatal("signature did not bind the index sequence")
	}
	_, _, otherKey := keypair(t)
	other := IndexEntry{ID: "other", Name: "Other", Description: "Other module", Repository: "org/other", Symbol: "ngx_http_other_module", Keys: otherKey}
	for _, changed := range []IndexEntry{
		{ID: entry.ID, Name: other.Name, Description: other.Description, Repository: other.Repository, Symbol: other.Symbol, Keys: other.Keys},
		{ID: other.ID, Name: other.Name, Description: other.Description, Repository: "Org/GeoIP2", Symbol: other.Symbol, Keys: other.Keys},
		{ID: other.ID, Name: other.Name, Description: other.Description, Repository: other.Repository, Symbol: other.Symbol, Keys: moduleKey},
		{ID: other.ID, Name: other.Name, Description: other.Description, Repository: other.Repository, Symbol: other.Symbol, Keys: rootKey},
		{ID: other.ID, Name: other.Name, Description: other.Description, Repository: other.Repository, Symbol: other.Symbol, Keys: updateKey},
	} {
		if _, _, err := verify(jsonBytes(t, []IndexEntry{entry, changed}), 12, floor, false); err == nil {
			t.Fatalf("accepted invalid authority: %+v", changed)
		}
	}
	if entries, next, err := verify([]byte("[]"), 12, floor, false); err != nil || len(entries) != 0 || next.Sequence != 12 {
		t.Fatalf("could not revoke all modules: %+v %+v %v", entries, next, err)
	}
}

func TestReleaseTrustAndCompatibility(t *testing.T) {
	_, signers, publicKey := keypair(t)
	_, foreign, foreignKey := keypair(t)
	entry := IndexEntry{ID: GeoIP2, Name: "GeoIP2", Description: "Country lookup", Repository: "org/geoip2", Symbol: Symbol, Keys: publicKey}
	module := entry.DisplayModule()
	module.Release.Tag = "v2.0.0"
	digest := strings.Repeat("a", 64)
	module.Install = Script{Name: ScriptName(GeoIP2, "install", digest), Size: 12, SHA256: digest}
	module.Uninstall = Script{Name: ScriptName(GeoIP2, "uninstall", digest), Size: 12, SHA256: digest}
	target := Target{OS: "debian:12", Arch: "amd64", Runtime: "nginx", RuntimeVersion: "1.28.0", BinarySHA256: strings.Repeat("b", 64)}
	asset := Asset{Module: GeoIP2, Version: "v2.0.0", Target: target, Name: AssetName(GeoIP2, digest), Size: 1024, SHA256: digest}
	alpine := asset
	alpine.Target.OS = "alpine:3.21"
	alpine.Target.BinarySHA256 = strings.Repeat("c", 64)
	alpine.SHA256 = strings.Repeat("d", 64)
	alpine.Name = AssetName(GeoIP2, alpine.SHA256)
	original := Manifest{Protocol: Protocol, Module: module, Assets: []Asset{asset, alpine}}
	verify := func(manifest Manifest, accepted IndexEntry, tag string, keys map[string]ed25519.PrivateKey, purpose string, test bool) (Manifest, error) {
		raw := jsonBytes(t, manifest)
		return VerifyManifest(raw, jsonBytes(t, Sign(raw, keys, purpose, test)), accepted, tag, false)
	}
	verified, err := verify(original, entry, module.Release.Tag, signers, ReleasePurpose, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range original.Assets {
		if matched, ok := verified.Match(expected.Target); !ok || matched != expected {
			t.Fatalf("unexpected matching artifact for %s: %+v", expected.Target.OS, matched)
		}
	}
	wrongAlpineBinary := alpine.Target
	wrongAlpineBinary.BinarySHA256 = target.BinarySHA256
	if _, ok := verified.Match(wrongAlpineBinary); ok {
		t.Fatal("accepted the other distribution's binary fingerprint")
	}
	for _, change := range []func(*Target){
		func(target *Target) { target.Arch = "arm64" }, func(target *Target) { target.OS = "ubuntu:24.04" },
		func(target *Target) { target.BinarySHA256 = digest }, func(target *Target) { target.RuntimeVersion += ".1" },
		func(target *Target) { target.Runtime = "openresty" },
	} {
		probe := target
		change(&probe)
		if _, ok := verified.Match(probe); ok {
			t.Fatalf("accepted an incompatible build: %+v", probe)
		}
	}
	for _, change := range []func(*Manifest){
		func(value *Manifest) { value.Module.Release.Repository = "org/other" },
		func(value *Manifest) { value.Module.Release.Tag = "v1.0.0" },
		func(value *Manifest) { value.Module.Install = value.Module.Uninstall },
		func(value *Manifest) { value.Assets = append(value.Assets, value.Assets[0]) },
		func(value *Manifest) { value.Assets[0].Name = "../module.so" },
		func(value *Manifest) { value.Assets[0].Size = MaxAsset + 1 },
	} {
		probe := original
		probe.Assets = []Asset{asset}
		change(&probe)
		if _, err := verify(probe, entry, module.Release.Tag, signers, ReleasePurpose, false); err == nil {
			t.Fatalf("accepted invalid manifest: %+v", probe)
		}
	}
	for _, signatures := range []map[string]string{
		Sign(jsonBytes(t, original), foreign, ReleasePurpose, false),
		Sign(jsonBytes(t, original), signers, IndexPurpose, false),
		Sign(jsonBytes(t, original), signers, ReleasePurpose, true),
	} {
		if _, err := VerifyManifest(jsonBytes(t, original), jsonBytes(t, signatures), entry, module.Release.Tag, false); err == nil {
			t.Fatal("accepted untrusted release signature")
		}
	}
	history := original
	history.Module.Release.Tag = "v1.0.0"
	history.Assets = []Asset{asset}
	history.Assets[0].Version = "v1.0.0"
	if _, err := verify(history, entry, history.Module.Release.Tag, signers, ReleasePurpose, false); err != nil {
		t.Fatalf("verified new release prevents explicit historical choice: %v", err)
	}
	entry.Keys = foreignKey
	if _, err := verify(history, entry, history.Module.Release.Tag, signers, ReleasePurpose, false); err == nil {
		t.Fatal("historical release still verifies after the key was removed")
	}
}
