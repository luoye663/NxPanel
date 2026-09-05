package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/luoye663/nxpanel/internal/plugin"
)

type versionTestCatalog struct{ uploadTestCatalog }

func (versionTestCatalog) List(context.Context) ([]plugin.CatalogEntry, error) {
	return []plugin.CatalogEntry{{ID: "com.example.versions", Version: "1.9.0"}, {ID: "com.example.versions", Version: "1.10.0"}}, nil
}

func TestCatalogShowsOneLatestVersionAndNeverOffersDowngrade(t *testing.T) {
	db := newTestDB(t)
	svc := plugin.NewService(db, t.TempDir(), versionTestCatalog{}, nil)
	defer svc.Close(context.Background())
	h := NewPluginHandler(svc)
	for _, tc := range []struct {
		installed string
		update    bool
	}{{"1.9.0", true}, {"1.10.0", false}, {"1.11.0", false}} {
		m := &plugin.Manifest{ID: "com.example.versions", Version: tc.installed}
		if err := plugin.NewRepository(db).Activate(context.Background(), m, "hash", "/unused", nil); err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		h.catalog(rec, httptest.NewRequest(http.MethodGet, "/plugins/catalog", nil))
		var result struct {
			Data struct {
				Items []pluginCatalogDTO `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		items := result.Data.Items
		if rec.Code != http.StatusOK || len(items) != 1 || items[0].Version != "1.10.0" || items[0].UpdateAvailable != tc.update {
			t.Fatalf("installed %s: %s", tc.installed, rec.Body.String())
		}
	}
}
