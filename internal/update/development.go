//go:build update_dev

package update

import "os"

func TestFeed() string { return os.Getenv("NXPANEL_UPDATE_TEST_FEED") }

func ActiveKeys() string { return os.Getenv("NXPANEL_UPDATE_TEST_KEYS") }

func RejectTrial(version string) bool {
	return os.Getenv("NXPANEL_UPDATE_TEST_FAIL_VERSION") == version
}
