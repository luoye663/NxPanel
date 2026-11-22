//go:build !update_dev

package update

func TestFeed() string { return "" }

func ActiveKeys() string { return TrustKeys }

func RejectTrial(string) bool { return false }
