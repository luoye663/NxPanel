package upstream

import (
	"fmt"
	"sort"
	"strings"

	"github.com/luoye663/nxpanel/internal/db/repo"
)

const managedHeader = "# Managed by nxPanel. Changes to this file will be overwritten.\n"

func RenderBlock(u *repo.NginxUpstream) string {
	var b strings.Builder
	fmt.Fprintf(&b, "upstream %s {\n", u.Name)
	switch u.Algorithm {
	case "least_conn":
		b.WriteString("    least_conn;\n")
	case "ip_hash":
		b.WriteString("    ip_hash;\n")
	case "hash":
		fmt.Fprintf(&b, "    hash %s", u.HashKey)
		if u.Consistent {
			b.WriteString(" consistent")
		}
		b.WriteString(";\n")
	}
	servers := append([]*repo.NginxUpstreamServer(nil), u.Servers...)
	sort.SliceStable(servers, func(i, j int) bool {
		if servers[i].SortOrder != servers[j].SortOrder {
			return servers[i].SortOrder < servers[j].SortOrder
		}
		ai, aj := strings.ToLower(servers[i].Address), strings.ToLower(servers[j].Address)
		if ai != aj {
			return ai < aj
		}
		return servers[i].ID < servers[j].ID
	})
	for _, s := range servers {
		fmt.Fprintf(&b, "    server %s weight=%d max_fails=%d fail_timeout=%ds", s.Address, s.Weight, s.MaxFails, s.FailTimeoutSeconds)
		if s.Backup {
			b.WriteString(" backup")
		}
		if s.Down {
			b.WriteString(" down")
		}
		b.WriteString(";\n")
	}
	if u.Keepalive > 0 {
		fmt.Fprintf(&b, "    keepalive %d;\n", u.Keepalive)
	}
	if u.KeepaliveRequests > 0 {
		fmt.Fprintf(&b, "    keepalive_requests %d;\n", u.KeepaliveRequests)
	}
	if u.KeepaliveTimeoutSeconds > 0 {
		fmt.Fprintf(&b, "    keepalive_timeout %ds;\n", u.KeepaliveTimeoutSeconds)
	}
	for _, line := range strings.Split(strings.TrimSuffix(u.AdvancedDirectives, "\n"), "\n") {
		if line != "" {
			b.WriteString("    ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	b.WriteString("}\n")
	return b.String()
}

func RenderFile(upstreams []*repo.NginxUpstream) string {
	items := append([]*repo.NginxUpstream(nil), upstreams...)
	sort.SliceStable(items, func(i, j int) bool {
		ni, nj := strings.ToLower(items[i].Name), strings.ToLower(items[j].Name)
		if ni != nj {
			return ni < nj
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].ID < items[j].ID
	})
	var b strings.Builder
	b.WriteString(managedHeader)
	for _, u := range items {
		b.WriteByte('\n')
		b.WriteString(RenderBlock(u))
	}
	return b.String()
}
