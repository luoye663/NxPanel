package nginxpatch

import (
	"strings"
	"testing"
)

const simple = `# braces in comments } {
server {
  listen 443 ssl;
  server_name example.com www.example.com;
  ssl_certificate "/external/cert.pem"; # preserve comment
  ssl_certificate_key '/external/key.pem';
  location / { proxy_pass http://backend; add_header X-Test "{;#}"; }
}
`

func TestInspectAndReplace(t *testing.T) {
	ref := InspectCertificate(simple, "example.com")
	if ref.ReadOnlyReason != "" || ref.CertPath != "/external/cert.pem" || ref.KeyPath != "/external/key.pem" {
		t.Fatalf("%+v", ref)
	}
	got, err := ReplaceCertificate(simple, "example.com", ref.FileHash, "/panel/new/cert.pem", "/panel/new/key.pem")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.ReplaceAll(strings.ReplaceAll(simple, `"/external/cert.pem"`, "/panel/new/cert.pem"), `'/external/key.pem'`, "/panel/new/key.pem")
	if got != want {
		t.Fatalf("non-local change:\n%s", got)
	}
	if _, err := ReplaceCertificate(simple+"\n", "example.com", ref.FileHash, "/c", "/k"); err == nil {
		t.Fatal("accepted stale preview")
	}
}

func TestReadOnlyConfigurations(t *testing.T) {
	cases := map[string]string{
		"two certs":          strings.Replace(simple, "listen 443 ssl;", "listen 443 ssl; ssl_certificate /second;", 1),
		"variable":           strings.Replace(simple, "/external/cert.pem", "/external/$name.pem", 1),
		"include":            strings.Replace(simple, "listen 443 ssl;", "listen 443 ssl; include /external/ssl.conf;", 1),
		"nested include":     strings.Replace(simple, "proxy_pass", "include /external/location.conf; proxy_pass", 1),
		"inherited":          `ssl_certificate /cert; ssl_certificate_key /key; server { server_name example.com; }`,
		"ambiguous":          simple + simple,
		"missing":            `server { server_name other.example; }`,
		"unclosed":           simple[:len(simple)-3],
		"relative":           strings.Replace(simple, "/external/cert.pem", "cert.pem", 1),
		"parent traversal":   strings.Replace(simple, "/external/cert.pem", "/external/../cert.pem", 1),
		"escaped":            strings.Replace(simple, "/external/cert.pem", `/external/cert\.pem`, 1),
		"nul":                simple + "\x00",
		"unterminated quote": simple + `set $foo "bad;`,
		"budget":             strings.Repeat(" ", MaxConfigBytes+1),
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			ref := InspectCertificate(config, "example.com")
			if ref.ReadOnlyReason == "" {
				t.Fatalf("accepted unsafe config: %+v", ref)
			}
			if _, err := ReplaceCertificate(config, "example.com", Hash(config), "/panel/cert", "/panel/key"); err == nil {
				t.Fatal("patched unsafe config")
			}
		})
	}
}

func TestTargetAndScope(t *testing.T) {
	config := `upstream backend { server localhost:8080; }` + strings.Replace(simple, "example.com www.example.com", "other.example", 1) + `http {` + simple + `}`
	ref := InspectCertificate(config, "example.com")
	if ref.ReadOnlyReason != "" {
		t.Fatal(ref.ReadOnlyReason)
	}
	got, err := ReplaceCertificate(config, "example.com", ref.FileHash, "/new/cert", "/new/key")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "/external/cert.pem") != 1 || strings.Count(got, "/new/cert") != 1 {
		t.Fatal("patched wrong server")
	}
	for _, path := range []string{"/cert;return 200;", "/cert\nfoo", "/cert$foo", "/cert#foo", "/cert{foo}", "/cert key", "relative"} {
		if _, err := ReplaceCertificate(simple, "example.com", Hash(simple), path, "/key"); err == nil {
			t.Fatalf("accepted path %q", path)
		}
	}
}

func TestCertificateSplitHTTPAndHTTPS(t *testing.T) {
	http := `server { listen 80; server_name example.com; return 301 https://$host$request_uri; }` + "\n"
	for _, content := range []string{http + simple, simple + http, "http {" + http + simple + "}"} {
		for _, resolved := range []bool{false, true} {
			inspect, replace := InspectCertificate, ReplaceCertificate
			if resolved {
				inspect, replace = InspectResolvedCertificate, ReplaceResolvedCertificate
			}
			ref := inspect(content, "example.com")
			if ref.ReadOnlyReason != "" || ref.ServerHash != Hash(strings.TrimSpace(strings.TrimPrefix(simple, "# braces in comments } {\n"))) {
				t.Fatalf("unexpected TLS target: %+v", ref)
			}
			got, err := replace(content, "example.com", ref.FileHash, "/new/cert", "/new/key")
			want := strings.ReplaceAll(strings.ReplaceAll(content, `"/external/cert.pem"`, "/new/cert"), `'/external/key.pem'`, "/new/key")
			if err != nil || got != want {
				t.Fatalf("patch changed HTTP sibling: %v\n%s", err, got)
			}
		}
		if _, err := locate(content, "example.com"); err == nil {
			t.Fatal("certificate selection relaxed general server ownership")
		}
	}
	withInclude := http + strings.Replace(simple, "listen 443 ssl;", "listen 443 ssl; include /tls/options.conf;", 1)
	paths, err := CertificateIncludes(withInclude, "example.com")
	if err != nil || len(paths) != 1 || paths[0] != "/tls/options.conf" {
		t.Fatalf("wrong TLS include graph: %v %v", paths, err)
	}
	for _, sibling := range []string{
		`server { listen 8443 ssl; server_name example.com; }`,
		`server { listen 443 quic; server_name example.com; }`,
		`server { listen 80; server_name example.com; include /tls.conf; }`,
		`server { listen 443; server_name example.com; ssl on; }`,
		simple,
	} {
		content := sibling + simple
		if ref := InspectResolvedCertificate(content, "example.com"); ref.ReadOnlyReason == "" {
			t.Fatalf("accepted ambiguous TLS sibling: %s", sibling)
		}
		if _, err := ReplaceResolvedCertificate(content, "example.com", Hash(content), "/new/cert", "/new/key"); err == nil {
			t.Fatal("patched ambiguous TLS sibling")
		}
	}
}

func FuzzInspectCertificate(f *testing.F) {
	f.Add(simple)
	f.Add(`server { server_name example.com; set $x "}"; }`)
	f.Fuzz(func(t *testing.T, content string) {
		ref := InspectCertificate(content, "example.com")
		if ref.ReadOnlyReason == "" {
			if _, err := ReplaceCertificate(content, "example.com", ref.FileHash, "/new/cert", "/new/key"); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestCertificateIncludeResolutionIsBoundedAndExplicit(t *testing.T) {
	content := strings.Replace(simple, "listen 443 ssl;", "listen 443 ssl; include /panel/rewrite.conf;", 1)
	paths, err := CertificateIncludes(content, "example.com")
	if err != nil || len(paths) != 1 || paths[0] != "/panel/rewrite.conf" {
		t.Fatalf("includes: %v %v", paths, err)
	}
	if _, err := InspectIncludeFragment(`location / { proxy_pass http://backend; }`); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`ssl_certificate /unexpected;`, `ssl_certificate_key /unexpected;`, `include /wildcard/*.conf;`, `include relative.conf;`, `include /path/$variable;`, `server {server_name other;}`} {
		if _, err := InspectIncludeFragment(fragment); err == nil {
			t.Fatalf("accepted unresolved include: %s", fragment)
		}
	}
	if ref := InspectResolvedCertificate(content, "example.com"); ref.ReadOnlyReason != "" {
		t.Fatal(ref.ReadOnlyReason)
	}
	if _, err := MaintenanceIncludePaths(`location / { rewrite_by_lua_block { return } }`); err == nil {
		t.Fatal("allowed scripted request phase")
	}
}
