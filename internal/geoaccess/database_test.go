package geoaccess

import "testing"

func TestCountryFallback(t *testing.T) {
	for _, test := range []struct{ primary, registered, want string }{
		{"CN", "US", "CN"}, {"", "US", "US"}, {"", "", "ZZ"}, {"invalid", "FR", "FR"},
	} {
		var record mmdbCountryRecord
		record.Country.ISOCode, record.RegisteredCountry.ISOCode = test.primary, test.registered
		if got := record.countryCode(); got != test.want {
			t.Errorf("countryCode(%q, %q) = %q, want %q", test.primary, test.registered, got, test.want)
		}
	}
}
