package nginxmodule

import (
	"errors"
	"strings"
	"testing"
)

func TestMatchCompatibilityRanges(t *testing.T) {
	build := Target{OS: "ubuntu:22.04", Arch: "arm64", Runtime: "openresty", RuntimeVersion: "1.25.3.1", BinarySHA256: strings.Repeat("1", 64)}
	rule := Compatibility{OSID: "alpine", Arch: "amd64", Runtime: "nginx", OSVersion: VersionRange{"3.9.2", "4.1.2"}, RuntimeVersion: VersionRange{"1.9.10", "2.1.3"}}
	asset := Asset{Target: build, Compatibility: []Compatibility{rule}, SHA256: strings.Repeat("a", 64)}
	manifest := Manifest{Assets: []Asset{asset}}
	for _, scenario := range []struct {
		os, arch, runtime, version string
		want                       bool
	}{
		{"alpine:3.9.2", "amd64", "nginx", "1.9.10", true},
		{"alpine:4.1.2", "amd64", "nginx", "2.1.3", true},
		{"alpine:3.10.1", "amd64", "nginx", "1.10.1.4", true},
		{"alpine:3.9.1", "amd64", "nginx", "1.10.1", false},
		{"alpine:4.1.3", "amd64", "nginx", "1.10.1", false},
		{"alpine:3.10.1", "amd64", "nginx", "1.9.9", false},
		{"alpine:3.10.1", "amd64", "nginx", "2.1.3.1", false},
		{"alpine:3.10.1", "amd64", "nginx", "2.1.3-rc1", false},
		{"alpine:edge", "amd64", "nginx", "1.10.1", false},
		{"debian:4.1", "amd64", "nginx", "1.10.1", false},
		{"alpine:3.10.1", "arm64", "nginx", "1.10.1", false},
		{"alpine:3.10.1", "amd64", "openresty", "1.10.1", false},
	} {
		probe := Target{OS: scenario.os, Arch: scenario.arch, Runtime: scenario.runtime, RuntimeVersion: scenario.version, BinarySHA256: strings.Repeat("2", 64)}
		matched, err := manifest.Match(probe)
		if scenario.want && (err != nil || matched.SHA256 != asset.SHA256) || !scenario.want && !errors.Is(err, ErrNoMatch) {
			t.Errorf("Match(%+v) = %s, %v, want match %v", probe, matched.SHA256, err, scenario.want)
		}
	}
	if _, err := manifest.Match(build); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("exact build bypassed its declared ranges: %v", err)
	}
	other := rule
	other.OSID = "ubuntu"
	other.OSVersion = VersionRange{"24.04", "24.04.0"}
	manifest.Assets[0].Compatibility = append(manifest.Assets[0].Compatibility, other)
	probe := Target{OS: "ubuntu:24.4", Arch: "amd64", Runtime: "nginx", RuntimeVersion: "1.10.0"}
	if _, err := manifest.Match(probe); err != nil {
		t.Fatalf("second rule did not match numeric version: %v", err)
	}
}

func TestMatchPrefersLegacyExactAndRejectsAmbiguity(t *testing.T) {
	probe := Target{OS: "rocky:9.8", Arch: "amd64", Runtime: "nginx", RuntimeVersion: "1.30.7", BinarySHA256: strings.Repeat("1", 64)}
	exact := Asset{Target: probe, SHA256: strings.Repeat("a", 64)}
	exact.Target.OS = "rocky:9"
	first := Asset{Target: probe, SHA256: strings.Repeat("b", 64), Compatibility: []Compatibility{{
		OSID: "rocky", Arch: "amd64", Runtime: "nginx", OSVersion: VersionRange{"9.8", "9.8"}, RuntimeVersion: VersionRange{"1.30.7", "1.30.7"},
	}}}
	second := first
	second.Target.BinarySHA256 = strings.Repeat("2", 64)
	manifest := Manifest{Assets: []Asset{first, second}}
	if matched, err := manifest.Match(probe); err != nil || matched.SHA256 != first.SHA256 {
		t.Fatalf("same bytes should not be ambiguous: %+v %v", matched, err)
	}
	manifest.Assets[1].SHA256 = strings.Repeat("c", 64)
	if _, err := manifest.Match(probe); !errors.Is(err, errAmbiguous) {
		t.Fatalf("overlapping assets should be ambiguous: %v", err)
	}
	manifest.Assets = append(manifest.Assets, exact)
	if matched, err := manifest.Match(probe); err != nil || matched.SHA256 != exact.SHA256 {
		t.Fatalf("legacy exact match did not take priority: %+v %v", matched, err)
	}
}

func TestValidateCompatibility(t *testing.T) {
	rule := Compatibility{OSID: "debian", Arch: "amd64", Runtime: "openresty", OSVersion: VersionRange{"12.8", "13.1"}, RuntimeVersion: VersionRange{"1.27.1.2", "2.1.3.4"}}
	if err := validateCompatibility([]Compatibility{rule}); err != nil {
		t.Fatal(err)
	}
	for _, bounds := range []VersionRange{{"1.10", "1.9"}, {"1.2", ""}, {"1.2.0-beta", "1.2.0"}, {"1..2", "2"}} {
		invalid := rule
		invalid.RuntimeVersion = bounds
		if err := validateCompatibility([]Compatibility{invalid}); err == nil {
			t.Errorf("accepted malformed bounds %+v", bounds)
		}
	}
	if err := validateCompatibility([]Compatibility{}); err == nil {
		t.Fatal("accepted an empty compatibility declaration")
	}
}
