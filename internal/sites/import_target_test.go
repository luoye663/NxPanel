package sites

import (
	"github.com/luoye663/nxpanel/internal/nginxpatch"
	"github.com/luoye663/nxpanel/internal/parser"
	"testing"
)

func TestImportTargetSelectionNeverChoosesFirstServer(t *testing.T) {
	a := &parser.ParsedServer{SourceFile: "/shared.conf", ServerNames: []string{"first.example.com"}, RawBlock: "first"}
	b := &parser.ParsedServer{SourceFile: "/shared.conf", ServerNames: []string{"second.example.com"}, RawBlock: "second"}
	parsed := []*parser.ParsedServer{a, b}
	if _, err := selectImportTarget(parsed, &ImportSiteRequest{SourceFile: a.SourceFile}); err == nil {
		t.Fatal("silently selected first server")
	}
	req := &ImportSiteRequest{SourceFile: b.SourceFile, ServerName: b.ServerNames[0], ServerHash: nginxpatch.Hash(b.RawBlock)}
	got, err := selectImportTarget(parsed, req)
	if err != nil || got != b {
		t.Fatalf("wrong selected target: %+v %v", got, err)
	}
	b.RawBlock = "changed"
	if _, err := selectImportTarget(parsed, req); err == nil {
		t.Fatal("accepted stale import scan")
	}
	if _, err := selectImportTarget([]*parser.ParsedServer{a, a}, &ImportSiteRequest{SourceFile: a.SourceFile, ServerName: a.ServerNames[0], ServerHash: nginxpatch.Hash(a.RawBlock)}); err == nil {
		t.Fatal("accepted ambiguous identical server blocks")
	}
	if err := nginxpatch.ValidateWholeFileOwnership(`server {server_name example.com;}`, "example.com"); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{`server {server_name example.com;} server {server_name other.example;}`, `upstream shared {server localhost:8000;} server {server_name example.com;}`} {
		if err := nginxpatch.ValidateWholeFileOwnership(content, "example.com"); err == nil {
			t.Fatal("allowed whole-file disable/delete of shared configuration")
		}
	}
}

func TestImportedLogPathsUseTheSelectedDomain(t *testing.T) {
	content := `server {server_name first.example.com; access_log /logs/first.access; error_log /logs/first.error;}
server {server_name second.example.com; access_log /logs/second.access; error_log /logs/second.error; location / {access_log /logs/nested;}}`
	access, errorLog, err := nginxpatch.ServerLogPaths(content, "second.example.com")
	if err != nil || access != "/logs/second.access" || errorLog != "/logs/second.error" {
		t.Fatalf("wrong target logs: %s %s %v", access, errorLog, err)
	}
	if _, _, err := nginxpatch.ServerLogPaths(content, "missing.example.com"); err == nil {
		t.Fatal("fell back to first server for missing target")
	}
}
