//go:build !update_dev

package update

import "github.com/luoye663/nxpanel/internal/trust"

func TestFeed() string { return "" }

func ActiveRoot() string { return trust.RootPublicKey }

func RejectTrial(string) bool { return false }
