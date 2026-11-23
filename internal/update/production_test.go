//go:build !update_dev

package update

import (
	"testing"

	"github.com/luoye663/nxpanel/internal/trust"
)

func TestProductionIgnoresFixtureOverrides(t *testing.T) {
	t.Setenv("NXPANEL_UPDATE_TEST_FEED", "http://example.test/index.json")
	t.Setenv("NXPANEL_UPDATE_TEST_KEYS", "fixture-key")
	t.Setenv("NXPANEL_UPDATE_TEST_FAIL_VERSION", "v1.2.3")
	if TestFeed() != "" || ActiveRoot() != trust.RootPublicKey || RejectTrial("v1.2.3") {
		t.Fatal("production accepted a development update override")
	}
}
