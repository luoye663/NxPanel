package georuntime

import (
	"strings"
	"testing"
)

func TestConfigUsesOneDatabaseAndEffectiveClientAddress(t *testing.T) {
	config, err := Config("/data/geo DB/Country.mmdb")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(config, "geoip2 ") != 1 || strings.Count(config, "Country.mmdb") != 1 ||
		strings.Count(config, "source=$remote_addr") != 2 || strings.Contains(config, "forwarded_for") || !strings.Contains(config, "default ZZ;") {
		t.Fatalf("unexpected shared lookup: %s", config)
	}
}

func TestSnapshotDigestAndSharedLookupBeyondPrefix(t *testing.T) {
	content := strings.Repeat("# padding\n", 1000) + "map " + CountryVariable + " $site { default 0; }\n"
	snapshot, err := ReadSnapshot(strings.NewReader(content))
	if err != nil || !snapshot.Shared || snapshot.Legacy || snapshot.Hash != Hash(content) {
		t.Fatalf("unexpected snapshot shared=%t legacy=%t hash=%s err=%v", snapshot.Shared, snapshot.Legacy, snapshot.Hash, err)
	}
}
