//go:build !update_dev

package update

import "testing"

func TestProductionIgnoresFixtureOverrides(t *testing.T) {
	t.Setenv("NXPANEL_UPDATE_TEST_FEED", "http://example.test/index.json")
	t.Setenv("NXPANEL_UPDATE_TEST_KEYS", "fixture-key")
	t.Setenv("NXPANEL_UPDATE_TEST_FAIL_VERSION", "v1.2.3")
	if TestFeed() != "" || ActiveKeys() != TrustKeys || RejectTrial("v1.2.3") {
		t.Fatal("production accepted a development update override")
	}
}
