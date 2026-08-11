package upstream

import (
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/db/repo"
)

func validRequest() SaveRequest {
	return SaveRequest{
		Name: "api_backend", Algorithm: "round_robin",
		Servers: []ServerRequest{{Address: "127.0.0.1:8080", Weight: 1, MaxFails: 1, FailTimeoutSeconds: 10}},
	}
}

func TestStructuredAlgorithmsAndAddresses(t *testing.T) {
	cases := []struct {
		name       string
		algorithm  string
		hashKey    string
		consistent bool
		address    string
	}{
		{"round robin hostname", "round_robin", "", false, "backend.example.com:80"},
		{"least conn ipv4", "least_conn", "", false, "192.0.2.1:443"},
		{"ip hash ipv6", "ip_hash", "", false, "[2001:db8::1]:8443"},
		{"hash", "hash", "$request_uri", true, "localhost:8080"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			req.Algorithm, req.HashKey, req.Consistent = tc.algorithm, tc.hashKey, tc.consistent
			req.Servers[0].Address = tc.address
			if err := NormalizeAndValidate(&req); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStructuredValidationRejectsUnsafeCombinations(t *testing.T) {
	cases := map[string]func(*SaveRequest){
		"unix socket":             func(r *SaveRequest) { r.Servers[0].Address = "unix:/tmp/app.sock" },
		"missing port":            func(r *SaveRequest) { r.Servers[0].Address = "backend.example.com" },
		"port range":              func(r *SaveRequest) { r.Servers[0].Address = "127.0.0.1:65536" },
		"invalid ipv4":            func(r *SaveRequest) { r.Servers[0].Address = "999.999.999.999:80" },
		"unbracketed ipv6":        func(r *SaveRequest) { r.Servers[0].Address = "2001:db8::1:80" },
		"ip hash backup":          func(r *SaveRequest) { r.Algorithm = "ip_hash"; r.Servers[0].Backup = true },
		"hash backup":             func(r *SaveRequest) { r.Algorithm = "hash"; r.HashKey = "$uri"; r.Servers[0].Backup = true },
		"backup down":             func(r *SaveRequest) { r.Servers[0].Backup, r.Servers[0].Down = true, true },
		"consistent without hash": func(r *SaveRequest) { r.Consistent = true },
		"keepalive dependent":     func(r *SaveRequest) { r.KeepaliveRequests = 100 },
		"duplicate address":       func(r *SaveRequest) { r.Servers = append(r.Servers, ServerRequest{Address: "127.0.0.1:8080"}) },
		"no active primary":       func(r *SaveRequest) { r.Servers[0].Down = true },
		"backup only":             func(r *SaveRequest) { r.Servers[0].Backup = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := validRequest()
			mutate(&req)
			if err := NormalizeAndValidate(&req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestAdvancedDirectivesLexer(t *testing.T) {
	valid := validRequest()
	valid.AdvancedDirectives = `zone api 64k; queue 100 timeout=30s; sticky cookie route expires=1h domain="example.com";`
	if err := NormalizeAndValidate(&valid); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(valid.AdvancedDirectives, "sticky cookie") {
		t.Fatalf("normalized directives=%q", valid.AdvancedDirectives)
	}
	rejected := []string{
		`server 127.0.0.1:9000;`, `LeAsT_CoNn;`, `random two least_conn;`, `keepalive_timeout 10s;`,
		`include /tmp/evil.conf;`, `zone x 64k; # comment`, `zone x { server y; };`,
		"zone x 64k\x00;", `zone "unterminated;`, `zone x 64k`, `zone x 64k;;`,
	}
	for _, directives := range rejected {
		t.Run(directives, func(t *testing.T) {
			req := validRequest()
			req.AdvancedDirectives = directives
			if err := NormalizeAndValidate(&req); err == nil {
				t.Fatal("expected advanced directive rejection")
			}
		})
	}
}

func TestRenderAllAlgorithms(t *testing.T) {
	cases := []struct {
		algorithm  string
		hashKey    string
		consistent bool
		expected   string
		forbidden  string
	}{
		{algorithm: "round_robin", forbidden: "least_conn;"},
		{algorithm: "least_conn", expected: "    least_conn;\n"},
		{algorithm: "ip_hash", expected: "    ip_hash;\n"},
		{algorithm: "hash", hashKey: "$request_uri", consistent: true, expected: "    hash $request_uri consistent;\n"},
	}
	for _, tc := range cases {
		t.Run(tc.algorithm, func(t *testing.T) {
			u := &repo.NginxUpstream{Name: "backend", Algorithm: tc.algorithm, HashKey: tc.hashKey, Consistent: tc.consistent,
				Servers: []*repo.NginxUpstreamServer{{Address: "127.0.0.1:80", Weight: 1, MaxFails: 1, FailTimeoutSeconds: 10}}}
			got := RenderBlock(u)
			if tc.expected != "" && !strings.Contains(got, tc.expected) {
				t.Fatalf("missing %q in:\n%s", tc.expected, got)
			}
			if tc.forbidden != "" && strings.Contains(got, tc.forbidden) {
				t.Fatalf("round_robin emitted an algorithm directive:\n%s", got)
			}
		})
	}
}

func TestRenderFileIsDeterministicAndKeepsEmptyFile(t *testing.T) {
	if got := RenderFile(nil); got != managedHeader {
		t.Fatalf("empty managed file=%q", got)
	}
	u1 := &repo.NginxUpstream{ID: "2", Name: "zeta", Algorithm: "round_robin", Servers: []*repo.NginxUpstreamServer{
		{ID: "b", Address: "b.example.com:80", Weight: 1, MaxFails: 1, FailTimeoutSeconds: 10, SortOrder: 2},
		{ID: "a", Address: "a.example.com:80", Weight: 2, MaxFails: 0, FailTimeoutSeconds: 20, SortOrder: 1, Backup: true},
	}}
	u2 := &repo.NginxUpstream{ID: "1", Name: "Alpha", Algorithm: "hash", HashKey: "$uri", Consistent: true,
		Keepalive: 32, KeepaliveRequests: 1000, KeepaliveTimeoutSeconds: 60,
		AdvancedDirectives: "zone alpha 64k;\n", Servers: []*repo.NginxUpstreamServer{{ID: "c", Address: "[2001:db8::1]:80", Weight: 1, MaxFails: 1, FailTimeoutSeconds: 10}},
	}
	a := RenderFile([]*repo.NginxUpstream{u1, u2})
	b := RenderFile([]*repo.NginxUpstream{u2, u1})
	if a != b {
		t.Fatal("render changed with input order")
	}
	if strings.Index(a, "upstream Alpha") > strings.Index(a, "upstream zeta") || strings.Index(a, "a.example.com") > strings.Index(a, "b.example.com") {
		t.Fatalf("render order is unstable:\n%s", a)
	}
	for _, expected := range []string{"hash $uri consistent;", "keepalive 32;", "zone alpha 64k;", " backup;"} {
		if !strings.Contains(a, expected) {
			t.Fatalf("missing %q in:\n%s", expected, a)
		}
	}
}
