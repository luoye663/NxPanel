package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/trust"
)

func TestUpdaterRechecksStagedSignatureAfterRevocation(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("release packages are built for supported Linux platforms")
	}
	root, rootPrivate, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	updatePublic, updatePrivate, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	buildPublic, buildPrivate, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	oldRoot := trust.RootPublicKey
	trust.RootPublicKey = "root:" + base64.StdEncoding.EncodeToString(root)
	t.Cleanup(func() { trust.RootPublicKey = oldRoot })
	updater := &Updater{Home: t.TempDir()}
	if err := os.MkdirAll(updater.private(), 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	policy := trust.Policy{Version: 1, Sequence: 1, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Update: "release:" + base64.StdEncoding.EncodeToString(updatePublic), Build: "builder:" + base64.StdEncoding.EncodeToString(buildPublic), Modules: []trust.Module{}}
	sign := func() trust.Signed {
		t.Helper()
		raw, err := json.Marshal(policy)
		if err != nil {
			t.Fatal(err)
		}
		signed, err := trust.Sign(raw, rootPrivate)
		if err != nil {
			t.Fatal(err)
		}
		return signed
	}
	initial := sign()
	if err := updater.acceptTrust(initial, now); err != nil {
		t.Fatal(err)
	}
	platform := runtime.GOOS + "/" + runtime.GOARCH
	manifest := Manifest{Product: "nxpanel", Protocol: Protocol, Version: "v2.0.0", Channel: "stable", Assets: []Asset{{Platform: platform, Name: AssetName(platform), Size: 1024, SHA256: strings.Repeat("a", 64)}}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signature := func(id string, key ed25519.PrivateKey) []byte {
		t.Helper()
		wire, err := json.Marshal(map[string]string{id: base64.StdEncoding.EncodeToString(ed25519.Sign(key, SignatureMessage(Purpose, raw)))})
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	if _, _, err := updater.verifyWithTrust(raw, signature("builder", buildPrivate), now); err == nil {
		t.Fatal("authorized builder key could verify update")
	}
	state := State{ID: "fixture", Version: manifest.Version}
	if err := updater.writeTrustRecord("prepared.json", preparedTrust{ID: state.ID, Manifest: raw, Signature: signature("release", updatePrivate)}); err != nil {
		t.Fatal(err)
	}
	if err := updater.verifyPrepared(state); err != nil {
		t.Fatal(err)
	}
	policy.Sequence++
	policy.Update = ""
	if err := updater.acceptTrust(sign(), now); err != nil {
		t.Fatal(err)
	}
	restarted := &Updater{Home: filepath.Clean(updater.Home)}
	if err := restarted.acceptTrust(initial, now); err == nil {
		t.Fatal("restarted updater accepted previous authorization")
	}
	if err := restarted.verifyPrepared(state); err == nil {
		t.Fatal("previously staged release remained authorized")
	}
}
