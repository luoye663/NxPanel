package hotlink

import (
	"testing"

	"github.com/luoye663/nxpanel/internal/db/repo"
)

func TestNormalizeRequestPartialUpdateKeepsExistingLists(t *testing.T) {
	enabled := false
	existing := &repo.SiteHotlinkRule{
		SiteID:            "site_1",
		Name:              "images",
		Enabled:           true,
		Extensions:        "jpg,png",
		Referers:          "server_names,*.example.com",
		AllowEmptyReferer: true,
		BlockStatus:       403,
	}

	rule, err := (&Service{}).normalizeRequest("site_1", &SaveRuleRequest{Enabled: &enabled}, existing)
	if err != nil {
		t.Fatalf("normalizeRequest() error = %v", err)
	}
	if rule.Enabled {
		t.Fatal("normalizeRequest() kept rule enabled")
	}
	if rule.Extensions != existing.Extensions {
		t.Fatalf("normalizeRequest() extensions = %q, want %q", rule.Extensions, existing.Extensions)
	}
	if rule.Referers != existing.Referers {
		t.Fatalf("normalizeRequest() referers = %q, want %q", rule.Referers, existing.Referers)
	}
}
