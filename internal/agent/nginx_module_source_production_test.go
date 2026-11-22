//go:build !module_dev

package agent

import (
	"net/http"
	"net/url"
	"testing"
)

func TestProductionModuleIgnoresTestEnvironment(t *testing.T) {
	t.Setenv("NXPANEL_MODULE_TEST_FEED", "http://127.0.0.1/modules.json")
	t.Setenv("NXPANEL_MODULE_TEST_KEYS", "must-not-be-read")
	source := configuredModuleSource()
	if source.test || source.base != "" || source.err != nil || len(source.keys) != 0 || source.stateDir() != moduleStateDir {
		t.Fatalf("production module source reads test settings: %+v", source)
	}
}

func TestProductionModuleRedirectsAreConstrained(t *testing.T) {
	for _, entry := range []struct {
		url   string
		valid bool
	}{
		{"https://release-assets.githubusercontent.com/asset", true},
		{"https://objects.githubusercontent.com/asset", true},
		{"https://example.org/asset", false},
		{"http://release-assets.githubusercontent.com/asset", false},
		{"https://objects.githubusercontent.com:8080/asset", false},
		{"https://user@objects.githubusercontent.com/asset", false},
	} {
		address, err := url.Parse(entry.url)
		if err != nil {
			t.Fatal(err)
		}
		if err := moduleHTTPClient.CheckRedirect(&http.Request{URL: address}, nil); (err == nil) != entry.valid {
			t.Errorf("asset redirect %s: %v", entry.url, err)
		}
		if err := moduleCatalogClient.CheckRedirect(&http.Request{URL: address}, nil); err != http.ErrUseLastResponse {
			t.Errorf("catalog must not follow redirects: %v", err)
		}
	}
}
