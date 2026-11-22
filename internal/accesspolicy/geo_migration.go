package accesspolicy

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/georuntime"
)

func (s *Service) PrepareGeoMigration(ctx context.Context, files map[string]georuntime.Snapshot, networks func() (map[string][]string, error)) ([]agentclient.FileChangeRequest, int, int, error) {
	items, err := s.store.List()
	if err != nil {
		return nil, 0, 0, err
	}
	known := make(map[string]StoredPolicy, len(items))
	for _, item := range items {
		known[s.globalPath(item.SiteID)] = item
	}
	var changes []agentclient.FileChangeRequest
	var migrated, blocked int
	paths := make([]string, 0, len(files))
	for path := range files {
		if strings.HasPrefix(filepath.Base(path), georuntime.PolicyIncludePrefix) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		snapshot := files[path]
		if !snapshot.Legacy {
			if snapshot.Shared {
				migrated++
			}
			continue
		}
		item, exists := known[path]
		if !exists || item.Policy.Mode != "unified" || item.Policy.ApplyStatus != "applied" {
			blocked++
			continue
		}
		applied, err := s.store.GetApplied(item.SiteID)
		if err != nil {
			return nil, 0, 0, err
		}
		if applied == nil || applied.Version != item.Policy.Version || !needsCountry(*applied) {
			blocked++
			continue
		}
		site, err := s.requireSite(item.SiteID)
		if err != nil {
			blocked++
			continue
		}
		renderCtx, _, err := s.renderContext(site, *applied)
		if err != nil {
			blocked++
			continue
		}
		compiled, err := Compile(site.ID, s.panelDir, *applied, renderCtx)
		if err != nil {
			blocked++
			continue
		}
		entries, err := networks()
		if err != nil {
			return nil, 0, 0, err
		}
		if previousPolicyHash(site.ID, compiled.GlobalContent, entries) != snapshot.Hash {
			blocked++
			continue
		}
		change := policyFile(path, compiled.GlobalContent, 0644)
		change.ExpectedHash = snapshot.Hash
		changes = append(changes, change)
		migrated++
	}
	return changes, migrated, blocked, ctx.Err()
}

func previousPolicyHash(siteID, compiled string, entries map[string][]string) string {
	suffix := fmt.Sprintf("%x", sha256.Sum256([]byte(siteID)))[:16]
	oldVariable := "$nx_policy_" + suffix + "_country"
	first, remainder, ok := strings.Cut(compiled, "\n")
	if !ok {
		return ""
	}
	second, remainder, ok := strings.Cut(remainder, "\n")
	if !ok {
		return ""
	}
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\n%s\ngeo %s {\n    default ZZ;\n", first, second, oldVariable)
	countries := make([]string, 0, len(entries))
	for country := range entries {
		countries = append(countries, country)
	}
	sort.Strings(countries)
	seen := make(map[string]string)
	for _, country := range countries {
		code := strings.ToUpper(country)
		for _, entry := range entries[country] {
			prefix, err := parseNetwork(entry)
			if err != nil {
				return ""
			}
			network := prefix.String()
			if previous, exists := seen[network]; exists {
				if previous != code {
					return ""
				}
				continue
			}
			seen[network] = code
			fmt.Fprintf(hash, "    %s %s;\n", network, code)
		}
	}
	_, _ = io.WriteString(hash, "}\n")
	_, _ = io.WriteString(hash, strings.ReplaceAll(remainder, "map "+georuntime.CountryVariable+" ", "map "+oldVariable+" "))
	return fmt.Sprintf("%x", hash.Sum(nil))
}
