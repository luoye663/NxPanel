package nginx

import (
	"bytes"
	"errors"
	"testing"
)

func TestReplaceMarkerBlock_NormalReplace(t *testing.T) {
	content := []byte(`#NXPANEL-SERVER-NAME-START
server_name old.com;
#NXPANEL-SERVER-NAME-END
`)
	newBody := []byte("server_name new.com;\n")
	result, err := ReplaceMarkerBlock(content, "SERVER-NAME", newBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `#NXPANEL-SERVER-NAME-START
server_name new.com;
#NXPANEL-SERVER-NAME-END
`
	if string(result) != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, string(result))
	}
}

func TestReplaceMarkerBlock_PreservesContentOutside(t *testing.T) {
	content := []byte(`# header comment
#NXPANEL-SERVER-NAME-START
server_name old.com;
#NXPANEL-SERVER-NAME-END
# footer comment
`)
	newBody := []byte("server_name new.com;\n")
	result, err := ReplaceMarkerBlock(content, "SERVER-NAME", newBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(result, []byte("# header comment")) {
		t.Error("header comment should be preserved")
	}
	if !bytes.Contains(result, []byte("# footer comment")) {
		t.Error("footer comment should be preserved")
	}
	if !bytes.Contains(result, []byte("server_name new.com;")) {
		t.Error("new body should be present")
	}
	if bytes.Contains(result, []byte("server_name old.com;")) {
		t.Error("old body should be replaced")
	}
}

func TestReplaceMarkerBlock_StartMissing(t *testing.T) {
	content := []byte(`server_name old.com;
#NXPANEL-SERVER-NAME-END
`)
	_, err := ReplaceMarkerBlock(content, "SERVER-NAME", []byte("new"))
	if err == nil {
		t.Fatal("expected error for missing START marker")
	}
	if !errors.Is(err, ErrMarkerMissing) {
		t.Errorf("expected ErrMarkerMissing, got: %v", err)
	}
}

func TestReplaceMarkerBlock_EndMissing(t *testing.T) {
	content := []byte(`#NXPANEL-SERVER-NAME-START
server_name old.com;
`)
	_, err := ReplaceMarkerBlock(content, "SERVER-NAME", []byte("new"))
	if err == nil {
		t.Fatal("expected error for missing END marker")
	}
	if !errors.Is(err, ErrMarkerMissing) {
		t.Errorf("expected ErrMarkerMissing, got: %v", err)
	}
}

func TestReplaceMarkerBlock_Duplicated(t *testing.T) {
	content := []byte(`#NXPANEL-SERVER-NAME-START
server_name a.com;
#NXPANEL-SERVER-NAME-END
#NXPANEL-SERVER-NAME-START
server_name b.com;
#NXPANEL-SERVER-NAME-END
`)
	_, err := ReplaceMarkerBlock(content, "SERVER-NAME", []byte("new"))
	if err == nil {
		t.Fatal("expected error for duplicated marker")
	}
	if !errors.Is(err, ErrMarkerDuplicated) {
		t.Errorf("expected ErrMarkerDuplicated, got: %v", err)
	}
}

func TestReplaceMarkerBlock_InvalidName(t *testing.T) {
	_, err := ReplaceMarkerBlock([]byte("content"), "invalid name!", []byte("new"))
	if err == nil {
		t.Fatal("expected error for invalid marker name")
	}
}

func TestReplaceMarkerBlock_StartWithMetadata(t *testing.T) {
	content := []byte(`#NXPANEL-SITE-START site_id=site_001 primary_domain=example.com
server {
}
#NXPANEL-SITE-END
`)
	newBody := []byte("server {\n    listen 80;\n}\n")
	result, err := ReplaceMarkerBlock(content, "SITE", newBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(result, []byte("#NXPANEL-SITE-START site_id=site_001 primary_domain=example.com")) {
		t.Error("START line with metadata should be preserved")
	}
	if !bytes.Contains(result, []byte("listen 80;")) {
		t.Error("new body should be present")
	}
}

func TestReplaceMarkerBlock_NoTrailingNewline(t *testing.T) {
	content := []byte(`#NXPANEL-ROOT-START
root /old;
#NXPANEL-ROOT-END
`)
	newBody := []byte("root /new;")
	result, err := ReplaceMarkerBlock(content, "ROOT", newBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(result, []byte("root /new;\n")) {
		t.Error("trailing newline should be auto-added")
	}
}

func TestReplaceMarkerBlock_PreservesIndentedEndMarker(t *testing.T) {
	content := []byte(`server {
    #NXPANEL-SSL-START
    ssl_certificate /old/fullchain.pem;
    #NXPANEL-SSL-END
}
`)
	result, err := ReplaceMarkerBlock(content, "SSL", []byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `server {
    #NXPANEL-SSL-START

    #NXPANEL-SSL-END
}
`
	if string(result) != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, string(result))
	}
}

func TestExtractMarkerBlock(t *testing.T) {
	content := []byte(`#NXPANEL-SERVER-NAME-START
server_name example.com www.example.com;
#NXPANEL-SERVER-NAME-END
`)
	body, err := ExtractMarkerBlock(content, "SERVER-NAME")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "server_name example.com www.example.com;\n"
	if string(body) != expected {
		t.Errorf("expected %q, got %q", expected, string(body))
	}
}

func TestExtractMarkerBlock_ExcludesIndentedEndMarkerPrefix(t *testing.T) {
	content := []byte(`    #NXPANEL-LISTEN-START
    listen 80;
    #NXPANEL-LISTEN-END
`)
	body, err := ExtractMarkerBlock(content, "LISTEN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "    listen 80;\n"
	if string(body) != expected {
		t.Errorf("expected %q, got %q", expected, string(body))
	}
}

func TestExtractMarkerBlock_InvalidCount(t *testing.T) {
	content := []byte(`#NXPANEL-SERVER-NAME-START
a
#NXPANEL-SERVER-NAME-END
#NXPANEL-SERVER-NAME-START
b
#NXPANEL-SERVER-NAME-END
`)
	_, err := ExtractMarkerBlock(content, "SERVER-NAME")
	if err == nil {
		t.Fatal("expected error for duplicated markers")
	}
}

func TestValidateRequiredMarkers_AllPresent(t *testing.T) {
	content := []byte(`#NXPANEL-SITE-START
#NXPANEL-LISTEN-START
#NXPANEL-LISTEN-END
#NXPANEL-SERVER-NAME-START
#NXPANEL-SERVER-NAME-END
#NXPANEL-SITE-END
`)
	status := ValidateRequiredMarkers(content, []string{"SITE", "LISTEN", "SERVER-NAME"})
	if !status.Valid {
		t.Errorf("expected valid, got missing=%v duplicated=%v", status.Missing, status.Duplicated)
	}
}

func TestValidateRequiredMarkers_Missing(t *testing.T) {
	content := []byte(`#NXPANEL-SITE-START
#NXPANEL-SITE-END
`)
	status := ValidateRequiredMarkers(content, []string{"SITE", "LISTEN"})
	if status.Valid {
		t.Error("expected invalid")
	}
	if len(status.Missing) != 1 || status.Missing[0] != "LISTEN" {
		t.Errorf("expected LISTEN missing, got %v", status.Missing)
	}
}

func TestValidateRequiredMarkers_Duplicated(t *testing.T) {
	content := []byte(`#NXPANEL-SITE-START
#NXPANEL-SITE-END
#NXPANEL-SITE-START
#NXPANEL-SITE-END
`)
	status := ValidateRequiredMarkers(content, []string{"SITE"})
	if status.Valid {
		t.Error("expected invalid")
	}
	if len(status.Duplicated) != 1 || status.Duplicated[0] != "SITE" {
		t.Errorf("expected SITE duplicated, got %v", status.Duplicated)
	}
}

func TestApplyMarkerPatches(t *testing.T) {
	content := []byte(`#NXPANEL-SERVER-NAME-START
old.com;
#NXPANEL-SERVER-NAME-END
#NXPANEL-ROOT-START
/old;
#NXPANEL-ROOT-END
`)
	result, err := ApplyMarkerPatches(content, []BlockPatch{
		{Name: "SERVER-NAME", Body: []byte("new.com;\n")},
		{Name: "ROOT", Body: []byte("/new;\n")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(result, []byte("new.com;")) {
		t.Error("SERVER-NAME should be patched")
	}
	if !bytes.Contains(result, []byte("/new;")) {
		t.Error("ROOT should be patched")
	}
	if bytes.Contains(result, []byte("old.com;")) {
		t.Error("old SERVER-NAME should be gone")
	}
	if bytes.Contains(result, []byte("/old;")) {
		t.Error("old ROOT should be gone")
	}
}

func TestApplyMarkerPatches_StopsOnError(t *testing.T) {
	content := []byte(`#NXPANEL-SERVER-NAME-START
old;
#NXPANEL-SERVER-NAME-END
`)
	_, err := ApplyMarkerPatches(content, []BlockPatch{
		{Name: "SERVER-NAME", Body: []byte("new\n")},
		{Name: "MISSING", Body: []byte("x\n")},
	})
	if err == nil {
		t.Fatal("expected error for missing marker")
	}
}

func TestEnsureMarkerBlockInjectsOptional(t *testing.T) {
	content := []byte(`#NXPANEL-SITE-START site_id=site_001
server {
    #NXPANEL-LISTEN-START
    listen 80;
    #NXPANEL-LISTEN-END

    #NXPANEL-SERVER-NAME-START
    server_name example.com;
    #NXPANEL-SERVER-NAME-END

    #NXPANEL-ROOT-START
    root /www/wwwroot/example.com;
    #NXPANEL-ROOT-END

    #NXPANEL-REWRITE-START
    include /opt/nxpanel/nginx/rewrite/example.com.conf;
    #NXPANEL-REWRITE-END

    #NXPANEL-DOCUMENT-START
    autoindex off;
    #NXPANEL-DOCUMENT-END

    #NXPANEL-LOG-START
    access_log /www/wwwlogs/example.com.access.log;
    error_log /www/wwwlogs/example.com.error.log;
    #NXPANEL-LOG-END
}
#NXPANEL-SITE-END
`)

	patched, err := EnsureMarkerBlock(content, MarkerNameHotlink, []byte("    include /opt/nxpanel/nginx/hotlink/example.com.conf;\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertMarkerOrder(t, string(patched), []string{
		MarkerNameRewrite,
		MarkerNameHotlink,
		MarkerNameDocument,
		MarkerNameLog,
	})
}

func TestApplyOptionalMarkerPatchesInjectsDependentMarkers(t *testing.T) {
	content := []byte(`#NXPANEL-SITE-START site_id=site_001
server {
    #NXPANEL-LISTEN-START
    listen 80;
    #NXPANEL-LISTEN-END

    #NXPANEL-SERVER-NAME-START
    server_name example.com;
    #NXPANEL-SERVER-NAME-END
}
#NXPANEL-SITE-END
`)

	patched, err := ApplyOptionalMarkerPatches(content, []BlockPatch{
		{Name: MarkerNameSSL, Body: []byte("    ssl_certificate /cert.pem;\n")},
		{Name: MarkerNameForceHTTPS, Body: []byte("    return 301 https://$host$request_uri;\n")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertMarkerOrder(t, string(patched), []string{
		MarkerNameListen,
		MarkerNameSSL,
		MarkerNameForceHTTPS,
		MarkerNameServerName,
	})
}

func assertMarkerOrder(t *testing.T, content string, names []string) {
	t.Helper()
	last := -1
	for _, name := range names {
		idx := bytes.Index([]byte(content), []byte(markerStart(name)))
		if idx < 0 {
			t.Fatalf("marker %s missing in:\n%s", name, content)
		}
		if idx <= last {
			t.Fatalf("marker %s is out of order in:\n%s", name, content)
		}
		last = idx
	}
}
