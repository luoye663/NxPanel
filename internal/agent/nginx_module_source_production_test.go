//go:build !module_dev

package agent

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
)

func TestProductionModuleIgnoresTestEnvironment(t *testing.T) {
	t.Setenv("NXPANEL_MODULE_TEST_FEED", "http://127.0.0.1/modules.json")
	t.Setenv("NXPANEL_MODULE_TEST_KEYS", "must-not-be-read")
	t.Setenv("NXPANEL_MODULE_STORE_URL", "")
	previous := nginxmodule.StoreURL
	nginxmodule.StoreURL = ""
	t.Cleanup(func() { nginxmodule.StoreURL = previous })
	source := configuredModuleSource()
	if source.test || source.base != "" || source.err != nil || len(source.keys) != 0 || source.stateDir() != moduleStateDir {
		t.Fatalf("production module source reads test settings: %+v", source)
	}
}

func TestProductionModuleRequiresHTTPSOrigin(t *testing.T) {
	for _, input := range []string{"http://modules.example.org", "https://modules.example.org/path", "https://user@modules.example.org", "https://modules.example.org/?a=1"} {
		t.Setenv("NXPANEL_MODULE_STORE_URL", input)
		if configuredModuleSource().err == nil {
			t.Fatalf("accepted invalid module store: %s", input)
		}
	}
	t.Setenv("NXPANEL_MODULE_STORE_URL", "https://modules.example.org")
	if source := configuredModuleSource(); source.err != nil || source.base != "https://modules.example.org/v1/" {
		t.Fatalf("unexpected source: %+v", source)
	}
}

func TestProductionModuleRedirectsAreConstrained(t *testing.T) {
	for _, entry := range []string{"https://release-assets.githubusercontent.com/asset", "https://modules.example.org/asset", "http://modules.example.org/asset"} {
		address, err := url.Parse(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := moduleCatalogClient.CheckRedirect(&http.Request{URL: address}, nil); err != http.ErrUseLastResponse {
			t.Errorf("module store must not follow redirects: %v", err)
		}
	}
}
