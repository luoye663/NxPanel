package nginxmodule

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSignedBuildBindsPublisherReleaseAndTarget(t *testing.T) {
	_, signers, buildKey := keypair(t)
	_, _, publisherKey := keypair(t)
	digest := strings.Repeat("a", 64)
	module := GeoIP2Module()
	module.Release = Release{Repository: "example/geoip2", Tag: "v3.4.0"}
	module.Install = Script{Name: ScriptName(module.ID, "install", digest), Size: 1, SHA256: digest}
	module.Uninstall = Script{Name: ScriptName(module.ID, "uninstall", digest), Size: 2, SHA256: digest}
	manifest := Manifest{Protocol: BuildProtocol, Module: module, Build: &BuildRecipe{Source: Script{Name: SourceName(module.ID, digest), Size: 5, SHA256: digest}}}
	raw := jsonBytes(t, manifest)
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	request := BuildRequest{ID: module.ID, Tag: module.Release.Tag, ManifestSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), Target: Target{OS: "ubuntu:24.04", Arch: "amd64", Runtime: "nginx", RuntimeVersion: "1.30.7", BinarySHA256: digest}, PackageVersion: "1.30.7-1~noble"}
	result := BuildResult{Request: request, Asset: Asset{Module: module.ID, Version: request.Tag, Target: request.Target, Name: AssetName(module.ID, digest), SHA256: digest, Size: 1024}}
	body := jsonBytes(t, result)
	signed := SignedBuild{Result: body, Signature: jsonBytes(t, Sign(body, signers, BuildPurpose, false))}
	if got, err := signed.Verify(manifest, raw, buildKey); err != nil || got.Request != request {
		t.Fatalf("verify: %+v %v", got, err)
	}
	if _, err := signed.Verify(manifest, raw, publisherKey); err == nil {
		t.Fatal("unrelated publisher key verified a build")
	}
	if _, err := signed.Verify(manifest, append(append([]byte{}, raw...), '\n'), buildKey); err == nil {
		t.Fatal("signature not bound to original manifest")
	}
	result.Asset.Target.RuntimeVersion = "1.30.8"
	modified, _ := json.Marshal(result)
	signed.Result = modified
	if _, err := signed.Verify(manifest, raw, buildKey); err == nil {
		t.Fatal("tampered target was accepted")
	}
}
