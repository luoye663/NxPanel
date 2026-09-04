package plugin

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseCatalogIndex(t *testing.T) {
	raw := []byte(`{"schema_version":1,"version":2,"generated_at":"2030-01-01T00:00:00Z","plugins":[{"id":"org.nxpanel.test","name":"Test","summary":"demo","publisher":"NxPanel","access":"free","versions":[{"version":"1.0.0","target":"packages/org.nxpanel.test/1.0.0.nxp","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","length":42,"plugin_api_version":"v1"}]}]}`)
	index, err := parseCatalogIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	entries := flattenCatalog(index)
	if len(entries) != 1 || entries[0].Access != CatalogAccessFree || entries[0].PackageSHA256 == "" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestParseCatalogIndexRejectsDuplicateTarget(t *testing.T) {
	idx := CatalogIndex{SchemaVersion: 1, Version: 1, GeneratedAt: time.Now(), Plugins: []CatalogPlugin{{ID: "org.nxpanel.one", Name: "one", Publisher: "NxPanel", Access: CatalogAccessFree, Versions: []CatalogVersion{{Version: "1.0.0", Target: "same.nxp", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Length: 1}}}, {ID: "org.nxpanel.two", Name: "two", Publisher: "NxPanel", Access: CatalogAccessFree, Versions: []CatalogVersion{{Version: "1.0.0", Target: "same.nxp", SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Length: 1}}}}}
	data, _ := json.Marshal(idx)
	if _, err := parseCatalogIndex(data); err == nil {
		t.Fatal("duplicate target accepted")
	}
}
