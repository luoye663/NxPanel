package trust

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func testKey(t *testing.T, id string) (string, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return id + ":" + base64.StdEncoding.EncodeToString(public), private
}

func signTest(t *testing.T, policy Policy, key ed25519.PrivateKey) Signed {
	t.Helper()
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := Sign(raw, key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestTrustPolicyRevocationAndRollback(t *testing.T) {
	now := time.Now()
	root, private := testKey(t, "root")
	publisher, publisherPrivate := testKey(t, "module")
	policy := Policy{Version: 1, Sequence: 7, IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Modules: []Module{{ID: "geoip2", Name: "GeoIP2", Description: "GeoIP2 module", Repository: "example/geoip2", Symbol: "ngx_http_geoip2_module", Keys: publisher}}}
	initial := signTest(t, policy, private)
	verified, floor, err := initial.Verify(root, Floor{}, now)
	if err != nil || verified.RequireCurrent(now) != nil || floor.Sequence != 7 {
		t.Fatalf("trust policy failed verification: %v", err)
	}
	if expired, _, err := initial.Verify(root, floor, policy.ExpiresAt); err != nil || expired.RequireCurrent(policy.ExpiresAt) == nil {
		t.Fatal("expiry did not distinguish offline proof from authorization for new actions")
	}
	if _, _, err := initial.Verify(root, Floor{}, policy.IssuedAt.Add(-time.Second)); err == nil {
		t.Fatal("accepted future policy")
	}
	policy.Modules = []Module{}
	conflicting := signTest(t, policy, private)
	if _, _, err := conflicting.Verify(root, floor, now); err == nil {
		t.Fatal("same sequence accepted with different contents")
	}
	policy.Sequence++
	revocation := signTest(t, policy, private)
	verified, floor, err = revocation.Verify(root, floor, now)
	if _, ok := verified.Module("geoip2"); err != nil || ok {
		t.Fatal("revoked publisher remains authorized")
	}
	if _, _, err := initial.Verify(root, floor, now); err == nil {
		t.Fatal("replayed policy restored a revoked key")
	}
	if _, _, err := signTest(t, policy, publisherPrivate).Verify(root, floor, now); err == nil {
		t.Fatal("publisher could sign a policy")
	}
	modified := bytes.Replace(revocation.Policy, []byte(`"sequence":8`), []byte(`"sequence":9`), 1)
	if _, _, err := (Signed{Policy: modified, Signature: revocation.Signature}).Verify(root, floor, now); err == nil {
		t.Fatal("modified policy verified")
	}
}

func TestTrustPolicyRejectsReusedRoleKeys(t *testing.T) {
	root, private := testKey(t, "root")
	publisher, _ := testKey(t, "module")
	now := time.Now()
	policy := Policy{Version: 1, Sequence: 1, IssuedAt: now, ExpiresAt: now.Add(time.Hour), Modules: []Module{{ID: "geoip2", Name: "GeoIP2", Description: "GeoIP2 module", Repository: "example/geoip2", Symbol: "ngx_http_geoip2_module", Keys: publisher}}}
	for _, key := range []string{root, publisher, "different-id:" + publisher[len("module:"):]} {
		policy.Update = key
		raw, err := json.Marshal(policy)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Sign(raw, private); err == nil {
			t.Fatal("policy allows reuse of a root or module signing key for updates")
		}
	}
}

func TestDefaultValidityAndAmbiguousPolicy(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	root, private := testKey(t, "root")
	raw := []byte(`{"version":1,"sequence":1,"update_keys":"","build_keys":"","modules":[]}`)
	body, err := WithDefaultValidity(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := Sign(body, private)
	if err != nil {
		t.Fatal(err)
	}
	policy, _, err := signed.Verify(root, Floor{}, now)
	if err != nil || !policy.IssuedAt.Equal(now) || !policy.ExpiresAt.Equal(now.Add(Lifetime)) {
		t.Fatalf("wrong defaults: %+v, %v", policy, err)
	}
	if _, err := WithDefaultValidity([]byte(`{"version":1,"Sequence":2,"sequence":1,"modules":[]}`), now); err == nil {
		t.Fatal("accepted ambiguous field")
	}
}
