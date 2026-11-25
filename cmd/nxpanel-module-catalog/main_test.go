package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
	"github.com/luoye663/nxpanel/internal/trust"
)

func TestIndependentReleaseRoundTrip(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded := "test:" + base64.StdEncoding.EncodeToString(public)
	t.Setenv("NXPANEL_MODULE_RELEASE_SIGNING_KEYS", "test:"+base64.StdEncoding.EncodeToString(private.Seed()))
	target := nginxmodule.Target{OS: "rocky:9", Arch: "arm64", Runtime: "openresty", RuntimeVersion: "1.27.1.2", BinarySHA256: strings.Repeat("e", 64)}
	built := buildTarget{Target: target, Compatibility: []nginxmodule.Compatibility{{
		OSID: "rocky", Arch: "arm64", Runtime: "openresty",
		OSVersion:      nginxmodule.VersionRange{Min: "9.8", Max: "9.10"},
		RuntimeVersion: nginxmodule.VersionRange{Min: "1.27.1.2", Max: "1.31.1.1"},
	}}}
	input, prepared, output := t.TempDir(), t.TempDir(), t.TempDir()
	builds := map[string][]byte{
		"geoip2/install.sh":                   []byte("#!/bin/sh\nexit 0\n"),
		"geoip2/uninstall.sh":                 []byte("#!/bin/sh\nexit 0\n"),
		"geoip2/host/" + nginxmodule.Filename: []byte("tested build"),
	}
	if err := addJSON(builds, "geoip2/host/target.json", built); err != nil {
		t.Fatal(err)
	}
	if err := writeFiles(input, builds); err != nil {
		t.Fatal(err)
	}
	module := nginxmodule.GeoIP2Module()
	module.Release = nginxmodule.Release{Repository: "official/geoip2", Tag: "v3.4.0"}
	manifest, files, err := assembleManifest(input, moduleDefinition{Module: module, Version: "v3.4.0"})
	if err != nil {
		t.Fatal(err)
	}
	candidate := releaseCandidate{Modules: []nginxmodule.Module{manifest.Module}, Assets: manifest.Assets}
	if err := addJSON(files, "candidate.json", candidate); err != nil {
		t.Fatal(err)
	}
	if err := writeFiles(prepared, files); err != nil {
		t.Fatal(err)
	}
	entry := indexEntry(manifest, encoded)
	root, rootPrivate, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NXPANEL_ROOT_PUBLIC_KEY", "root:"+base64.StdEncoding.EncodeToString(root))
	policyDir := t.TempDir()
	now := time.Now()
	policy, err := json.Marshal(trust.Policy{Version: 1, Sequence: 1, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Modules: []trust.Module{trust.Module(entry)}})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := trust.Sign(policy, rootPrivate)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFiles(policyDir, map[string][]byte{trust.Name: signed.Policy, trust.Signature: signed.Signature}); err != nil {
		t.Fatal(err)
	}
	if err := signRelease(prepared, output, policyDir); err == nil || !strings.Contains(err.Error(), "不得包含预编译模块") {
		t.Fatalf("precompiled candidate must not be signed: %v", err)
	}
	sourceBody := []byte("fixture source archive")
	if err := writeFiles(input, map[string][]byte{"geoip2/source.tar.gz": sourceBody}); err != nil {
		t.Fatal(err)
	}
	mixed, mixedFiles, err := assembleManifest(input, moduleDefinition{Module: module, Version: "v3.4.0", BuildFromSource: true})
	if err != nil {
		t.Fatal(err)
	}
	mixedDir := t.TempDir()
	sourceCandidate := releaseCandidate{Modules: []nginxmodule.Module{mixed.Module}, Assets: mixed.Assets, Build: mixed.Build}
	if err := addJSON(mixedFiles, "candidate.json", sourceCandidate); err != nil {
		t.Fatal(err)
	}
	if err := writeFiles(mixedDir, mixedFiles); err != nil {
		t.Fatal(err)
	}
	if err := signRelease(mixedDir, output, policyDir); err == nil || !strings.Contains(err.Error(), "不得包含预编译模块") {
		t.Fatalf("mixed candidate must not be signed: %v", err)
	}
	sourceDir := t.TempDir()
	sourceCandidate.Assets = []nginxmodule.Asset{}
	delete(mixedFiles, mixed.Assets[0].Name)
	if err := addJSON(mixedFiles, "candidate.json", sourceCandidate); err != nil {
		t.Fatal(err)
	}
	if err := writeFiles(sourceDir, mixedFiles); err != nil {
		t.Fatal(err)
	}
	if err := signRelease(sourceDir, output, policyDir); err != nil {
		t.Fatal(err)
	}
	read := func(dir, name string) []byte {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	verified, err := nginxmodule.VerifyManifest(read(output, nginxmodule.ManifestName), read(output, nginxmodule.ManifestSig), entry, module.Release.Tag, false)
	if err != nil || verified.RequireSourceOnly() != nil {
		t.Fatalf("expected a signed source-only manifest: %+v %v", verified, err)
	}
	if string(read(output, verified.Build.Source.Name)) != string(sourceBody) {
		t.Fatal("signed release did not contain the expected source")
	}
	modified := strings.Replace(string(read(output, nginxmodule.ManifestName)), "v3.4.0", "v3.4.1", 1)
	if _, err := nginxmodule.VerifyManifest([]byte(modified), read(output, nginxmodule.ManifestSig), entry, module.Release.Tag, false); err == nil {
		t.Fatal("a modified manifest still verifies")
	}
	sourcePath := filepath.Join(sourceDir, verified.Build.Source.Name)
	priorWrite := time.Unix(123456789, 0)
	if err := os.Chtimes(sourcePath, priorWrite, priorWrite); err != nil {
		t.Fatal(err)
	}
	if err := signRelease(sourceDir, sourceDir, policyDir); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(sourcePath); err != nil || !info.ModTime().Equal(priorWrite) {
		t.Fatalf("signing in place rewrote the verified module source: %v %v", info, err)
	}
	if _, err := nginxmodule.VerifyManifest(read(sourceDir, nginxmodule.ManifestName), read(sourceDir, nginxmodule.ManifestSig), entry, module.Release.Tag, false); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(output, "fixture")
	if err := writeTestFixture(prepared, fixture, "", 41); err != nil {
		t.Fatal(err)
	}
	fixtureKeys, err := nginxmodule.Keys(strings.TrimSpace(string(read(fixture, "trust.pub"))), encoded)
	if err != nil {
		t.Fatal(err)
	}
	feed := filepath.Join(fixture, "feed")
	entries, _, err := nginxmodule.VerifyIndex(read(feed, nginxmodule.IndexName), read(feed, nginxmodule.IndexSigName), fixtureKeys, nil, nginxmodule.Floor{}, true)
	if err != nil || len(entries) != 1 || entries[0].Keys == encoded {
		t.Fatalf("invalid fixture index: %+v %v", entries, err)
	}
	release := filepath.Join(feed, "repos", module.Release.Repository, "releases", "download", module.Release.Tag)
	testManifest, err := nginxmodule.VerifyManifest(read(release, nginxmodule.ManifestName), read(release, nginxmodule.ManifestSig), entries[0], module.Release.Tag, true)
	if err != nil {
		t.Fatal(err)
	}
	observed := target
	observed.OS, observed.BinarySHA256, observed.RuntimeVersion = "rocky:9.9", strings.Repeat("f", 64), "1.31.1.1"
	if asset, err := testManifest.Match(observed); err != nil || asset.Target != target || string(read(release, asset.Name)) != "tested build" {
		t.Fatalf("test fixture did not contain the expected build: %+v %v", asset, err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, manifest.Module.Uninstall.Name), []byte("#!/bin/sh\nexit 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := signRelease(sourceDir, t.TempDir(), policyDir); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("modified script must not be signed: %v", err)
	}
	candidate.Modules = append(candidate.Modules, candidate.Modules[0])
	raw, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared, "candidate.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readCandidate(prepared); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("batch publication was accepted: %v", err)
	}
}
