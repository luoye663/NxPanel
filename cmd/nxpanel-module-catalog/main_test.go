package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
)

func TestSignCatalogRoundTrip(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded := "test:" + base64.StdEncoding.EncodeToString(public)
	t.Setenv("NXPANEL_MODULE_TRUST_KEYS", encoded)
	t.Setenv("NXPANEL_MODULE_SIGNING_KEYS", "test:"+base64.StdEncoding.EncodeToString(private.Seed()))
	t.Setenv("NXPANEL_UPDATE_TRUST_KEYS", "")
	input, output := t.TempDir(), t.TempDir()
	target := nginxmodule.Target{OS: "rocky:9", Arch: "arm64", Runtime: "openresty", RuntimeVersion: "1.27.1.2", BinarySHA256: strings.Repeat("e", 64)}
	dir := filepath.Join(input, "geoip2", "host")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"install.sh", "uninstall.sh"} {
		if err := os.WriteFile(filepath.Join(filepath.Dir(dir), name), []byte("#!/bin/sh\nexit 0\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "target.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, nginxmodule.Filename), []byte("tested build"), 0644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	module := nginxmodule.GeoIP2Module()
	module.Release = nginxmodule.Release{Repository: "official/modules", Tag: "nginx-modules-v3"}
	prepared := t.TempDir()
	candidate := newCatalog(1, now, false)
	files, err := assembleCatalog(&candidate, input, []moduleDefinition{{Module: module, Version: "v3.4.0"}}, now, false)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(releaseCandidate{Modules: candidate.Modules, Assets: candidate.Assets})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFiles(prepared, files, "candidate.json", manifest); err != nil {
		t.Fatal(err)
	}
	if err := signCatalog(input, t.TempDir(), 41, now); !os.IsNotExist(err) {
		t.Fatalf("production signing must require a candidate manifest: %v", err)
	}
	if err := signCatalog(prepared, output, 42, now); err != nil {
		t.Fatal(err)
	}
	envelope, err := os.ReadFile(filepath.Join(output, nginxmodule.CatalogName))
	if err != nil {
		t.Fatal(err)
	}
	catalog, floor, err := nginxmodule.Verify(envelope, map[string]ed25519.PublicKey{"test": public}, now, nginxmodule.Floor{})
	if err != nil || floor.Sequence != 42 {
		t.Fatalf("signed catalog verification failed: %+v %v", floor, err)
	}
	asset, ok := catalog.Match(nginxmodule.GeoIP2, target)
	if !ok {
		t.Fatalf("catalog omitted target: %+v", target)
	}
	payload, err := os.ReadFile(filepath.Join(output, asset.Name))
	if err != nil || string(payload) != "tested build" || int64(len(payload)) != asset.Size {
		t.Fatalf("wrong publication payload: %q %v", payload, err)
	}

	fixture := filepath.Join(output, "test-fixture")
	if err := writeTestFixture(input, fixture, 41, now, []moduleDefinition{{Module: nginxmodule.GeoIP2Module(), Version: "v3.4.0"}}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(fixture)
	if err != nil || len(entries) != 2 || entries[0].Name() != "feed" || entries[1].Name() != "trust.pub" {
		t.Fatalf("test fixture persisted unexpected files: %v %v", entries, err)
	}
	keyFile, err := os.ReadFile(filepath.Join(fixture, "trust.pub"))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := nginxmodule.Keys(strings.TrimSpace(string(keyFile)), encoded)
	if err != nil {
		t.Fatal(err)
	}
	testEnvelope, err := os.ReadFile(filepath.Join(fixture, "feed", nginxmodule.CatalogName))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := nginxmodule.VerifyTest(testEnvelope, keys, now, nginxmodule.Floor{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := nginxmodule.Verify(testEnvelope, keys, now, nginxmodule.Floor{}); err == nil {
		t.Fatal("production trusted the local fixture")
	}

	if err := os.WriteFile(filepath.Join(prepared, candidate.Modules[0].Uninstall.Name), []byte("#!/bin/sh\nexit 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := signCatalog(prepared, t.TempDir(), 44, now); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("modified script must not be signed: %v", err)
	}
}
