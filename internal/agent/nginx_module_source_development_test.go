//go:build module_dev

package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
)

func TestDevelopmentModuleSourceConstraints(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := "fixture:" + base64.StdEncoding.EncodeToString(public)
	local := "http://127.0.0.1:18081/sub/" + nginxmodule.IndexName
	alias := "http://module-feed/" + nginxmodule.IndexName
	for _, test := range []struct {
		feed, key string
		container bool
		wantErr   bool
	}{
		{local, key, false, false}, {alias, key, true, false}, {"http://[::1]/" + nginxmodule.IndexName, key, false, false},
		{alias, key, false, true}, {local, "", false, true}, {"", key, false, true}, {local, "wrong", false, true},
		{"http://host.invalid/" + nginxmodule.IndexName, key, true, true},
		{"http://user@127.0.0.1/" + nginxmodule.IndexName, key, false, true},
		{"http://127.0.0.1/../" + nginxmodule.IndexName, key, false, true},
		{local + "?test", key, false, true}, {local + "#test", key, false, true},
	} {
		source := parseModuleSource(test.feed, test.key, test.container)
		if !source.test || (source.err != nil) != test.wantErr || source.stateDir() != moduleTestStateDir {
			t.Errorf("unexpected settings for %q: %v", test.feed, source.err)
		}
	}
	if source := parseModuleSource("", "", false); source.test || source.stateDir() != moduleStateDir || source.err != nil {
		t.Fatal("test mode is not opt-in")
	}
	if source := parseModuleSource(local, key, false); source.base != "http://127.0.0.1:18081/sub/" {
		t.Fatalf("test assets not fetched beside catalog: %q", source.base)
	}
}

func TestDevelopmentModuleFetchDoesNotFollowRedirects(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+nginxmodule.IndexName {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		t.Errorf("unexpected HTTP request: %s", r.URL.Path)
		_, _ = fmt.Fprint(w, "not a directory")
	}))
	defer fixture.Close()
	source := moduleSource{test: true, base: fixture.URL + "/"}
	if _, err := source.fetch(t.Context(), nginxmodule.IndexName, nginxmodule.MaxEnvelope); err == nil {
		t.Fatal("accepted a redirected module catalog")
	}
}
