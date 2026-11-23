//go:build update_dev

package update

import (
	"os"

	"github.com/luoye663/nxpanel/internal/trust"
)

func TestFeed() string { return os.Getenv("NXPANEL_UPDATE_TEST_FEED") }

func ActiveRoot() string {
	if TestFeed() != "" {
		return os.Getenv("NXPANEL_UPDATE_TEST_KEYS")
	}
	return trust.RootPublicKey
}

func RejectTrial(version string) bool {
	return os.Getenv("NXPANEL_UPDATE_TEST_FAIL_VERSION") == version
}
