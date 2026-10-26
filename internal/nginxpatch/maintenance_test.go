package nginxpatch

import (
	"strings"
	"testing"
)

func TestMaintenanceLocalAndReversible(t *testing.T) {
	page := "/panel/maintenance/site/index.html"
	on, err := SetMaintenance(simple, "example.com", []string{"www.example.com"}, "site", page, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(on, "return 503;") || !strings.Contains(on, "Retry-After 300 always") || !strings.Contains(on, "Cache-Control no-store always") {
		t.Fatal("missing maintenance response configuration")
	}
	if err := ValidateMaintenanceWrite(simple, on, true); err != nil {
		t.Fatal(err)
	}
	updated, err := ReplaceCertificate(on, "example.com", Hash(on), "/panel/new/cert", "/panel/new/key")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateMaintenanceWrite(on, updated, false); err != nil {
		t.Fatal(err)
	}
	off, err := SetMaintenance(updated, "example.com", nil, "site", page, false)
	if err != nil {
		t.Fatal(err)
	}
	want, err := ReplaceCertificate(simple, "example.com", Hash(simple), "/panel/new/cert", "/panel/new/key")
	if err != nil {
		t.Fatal(err)
	}
	if off != want {
		t.Fatal("restore reverted a certificate or changed business configuration")
	}
	if err := ValidateMaintenanceWrite(on, strings.Replace(on, "proxy_pass http://backend", "return 200", 1), false); err == nil {
		t.Fatal("allowed business rewrite during maintenance")
	}
	if err := ValidateMaintenanceWrite(on, simple, false); err == nil {
		t.Fatal("removed maintenance without explicit transition")
	}
}

func TestMaintenanceSharedFileAndOwnership(t *testing.T) {
	other := strings.ReplaceAll(simple, "example.com", "other.test")
	a, err := SetMaintenance(simple+other, "example.com", []string{"www.example.com"}, "first", "/panel/first.html", true)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SetMaintenance(a, "other.test", []string{"www.other.test"}, "second", "/panel/second.html", true)
	if err != nil {
		t.Fatal(err)
	}
	c, err := SetMaintenance(b, "example.com", nil, "first", "/panel/first.html", false)
	if err != nil {
		t.Fatal(err)
	}
	want, err := SetMaintenance(simple+other, "other.test", []string{"www.other.test"}, "second", "/panel/second.html", true)
	if err != nil {
		t.Fatal(err)
	}
	if c != want {
		t.Fatal("restore touched another site's maintenance block")
	}
	if _, err := SetMaintenance(simple, "example.com", nil, "site", "/page", true); err == nil {
		t.Fatal("claimed an unrecorded domain")
	}
	if _, err := SetMaintenance(simple, "example.com", nil, "../escape", "/page", true); err == nil {
		t.Fatal("accepted unsafe identity")
	}
	if _, _, err := StripMaintenance(simple + "\n    " + MaintenanceStart + "broken"); err == nil {
		t.Fatal("accepted malformed block")
	}
}
