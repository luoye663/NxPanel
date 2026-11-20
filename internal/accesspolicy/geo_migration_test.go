package accesspolicy

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/georuntime"
)

type testCountryProvider struct{}

func (testCountryProvider) AccessPolicyData() (bool, []string, error)   { return true, nil, nil }
func (testCountryProvider) LookupCountry(netip.Addr) (string, error)    { return "CN", nil }
func (testCountryProvider) EnsureRuntime(context.Context, string) error { return nil }

func TestGeoMigrationRequiresMatchingConfirmedConfiguration(t *testing.T) {
	service, _, _, _ := policyServiceFixture(t)
	service.geo = testCountryProvider{}
	const siteID = "site_policy"
	policy := serviceAllowPolicy()
	policy.Rules = []Rule{{ID: "geo", Enabled: true, Match: "all", Conditions: []Condition{{Kind: "country", Values: []string{"CN"}}}, Action: Action{Type: "deny", StatusCode: 403}}}
	if _, err := service.store.SaveDesired(siteID, policy, 0); err != nil {
		t.Fatal(err)
	}
	if err := service.store.MarkApplied(siteID, 1); err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(siteID, service.panelDir, policy, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("%x", sha256.Sum256([]byte(siteID)))[:16]
	oldVar := "$nx_policy_" + suffix + "_country"
	lines := strings.SplitN(compiled.GlobalContent, "\n", 3)
	existing := strings.Join(lines[:2], "\n") + "\ngeo " + oldVar + " {\n    default ZZ;\n    1.0.1.0/24 CN;\n}\n" + strings.ReplaceAll(lines[2], georuntime.CountryVariable, oldVar)
	path := georuntime.PolicyPath(service.panelDir, siteID)
	prepare := func(content string, wantChanges, wantBlocked int) {
		t.Helper()
		snapshot, err := georuntime.ReadSnapshot(strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		changes, _, blocked, err := service.PrepareGeoMigration(context.Background(), map[string]georuntime.Snapshot{path: snapshot}, func() (map[string][]string, error) {
			return map[string][]string{"CN": {"1.0.1.0/24"}}, nil
		})
		if err != nil || len(changes) != wantChanges || blocked != wantBlocked {
			t.Fatalf("migration changes=%d blocked=%d err=%v", len(changes), blocked, err)
		}
		if len(changes) > 0 && changes[0].ExpectedHash != snapshot.Hash {
			t.Fatal("migration must compare the verified file before replacement")
		}
	}
	prepare(existing, 1, 0)
	prepare(compiled.GlobalContent, 0, 0)
	prepare(strings.Replace(existing, "/24 CN", "/25 CN", 1), 0, 1)
	if _, err := service.store.SaveDesired(siteID, serviceAllowPolicy(), 1); err != nil {
		t.Fatal(err)
	}
	prepare(existing, 0, 1)
	stored, err := service.store.Get(siteID)
	if err != nil || stored.Version != 2 || stored.ApplyStatus != "pending" {
		t.Fatalf("pending policy changed: %#v %v", stored, err)
	}
}
