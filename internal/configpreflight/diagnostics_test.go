package configpreflight

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestDiagnosticsPositionsAndScopes(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		source := memorySource{
			"/nginx/nginx.conf":  "http { include shared.conf; server { return 403; } }",
			"/nginx/shared.conf": "geo $country { default ZZ; }\nmap $x $y { return 1; }\nauth_basic Private;",
			"/nginx/nested.conf": strings.ReplaceAll("# return 403;\n\nlocation / {\n set $x \"auth_basic return;\";\n if ($request_method = 'OPTIONS') {\n  return\n   204;\n }\n auth_basic off; deny all;\n}\n", "\n", newline),
		}
		report := Diagnose(context.Background(), source, "/site.conf", "/nginx/nginx.conf", strings.NewReader("server { include nested.conf; }"))
		if !report.Complete || report.Truncated || len(report.Warnings) != 4 {
			t.Fatalf("report: %+v", report)
		}
		for i, want := range []struct {
			path, name string
			line       int
		}{
			{"/nginx/nested.conf", "return", 6}, {"/nginx/nested.conf", "auth_basic", 9},
			{"/nginx/nested.conf", "deny", 9}, {"/nginx/shared.conf", "auth_basic", 3},
		} {
			got := report.Warnings[i]
			if got.Path != want.path || got.Directive != want.name || got.Line != want.line {
				t.Fatalf("warning: %+v want %+v", got, want)
			}
		}
	}
}

func TestDiagnosticsUserOPTIONSLine32(t *testing.T) {
	content, err := os.ReadFile("testdata/cors.conf")
	if err != nil {
		t.Fatal(err)
	}
	path := "/opt/nxpanel/nginx/rewrite/aihub2.326333.xyz.conf"
	source := memorySource{path: string(content)}
	report := Diagnose(context.Background(), source, "/site.conf", "", strings.NewReader("server { include "+path+"; }"))
	if !report.Complete || len(report.Warnings) != 1 || report.Warnings[0].Path != path || report.Warnings[0].Line != 32 || report.Warnings[0].Directive != "return" {
		t.Fatalf("wrong production location: %+v", report)
	}
	if source[path] != string(content) {
		t.Fatal("diagnostic modified source")
	}
}

func TestDiagnosticsEscapesAndMultilineTokens(t *testing.T) {
	content := "server {\nset $x \"line1\nline2\";\nset $y escaped\\\nvalue;\nset $z ${document_root};\nreturn 204;\n}"
	report := Diagnose(context.Background(), nil, "/site.conf", "", strings.NewReader(content))
	if !report.Complete || len(report.Warnings) != 1 || report.Warnings[0].Line != 7 {
		t.Fatalf("bad line: %+v", report)
	}
}

func TestDiagnosticsLimitsAndPartialResults(t *testing.T) {
	report := Diagnose(context.Background(), nil, "/site.conf", "", strings.NewReader("server {"+strings.Repeat("return 204;\n", MaxWarnings+10)+"}"))
	if !report.Complete || !report.Truncated || len(report.Warnings) != MaxWarnings {
		t.Fatalf("unbounded report: %+v", report)
	}
	source := memorySource{"/nested.conf": "return 204;\ninclude /missing.conf;"}
	report = Diagnose(context.Background(), source, "/site.conf", "", strings.NewReader("server { include /nested.conf; }"))
	if report.Complete || len(report.Warnings) != 2 || report.Warnings[0].Line != 1 || report.Warnings[1].Path != "/missing.conf" {
		t.Fatalf("partial report: %+v", report)
	}
}
