package sitebackup

import "testing"

func TestPolicyArchiveSlotsPreserveLegacyPositions(t *testing.T) {
	for _, legacy := range [][]string{{"/site.conf"}, {"/site.conf", "/rewrite.conf"}, {"/site.conf", "/rewrite.conf", "/access.conf", "/hotlink.conf"}} {
		result := appendPolicyConfigPaths(legacy, []string{"/snapshot.json", "/global.conf", "/auth.htpasswd"})
		for i, path := range legacy {
			if result[i] != path {
				t.Fatalf("legacy slot %d changed", i)
			}
		}
		if len(result) != 7 || result[4] != "/snapshot.json" || result[5] != "/global.conf" {
			t.Fatalf("unstable slots %#v", result)
		}
	}
}
