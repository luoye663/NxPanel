package configpreflight

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

type memorySource map[string]string

func (m memorySource) Open(path string) (io.ReadCloser, error) {
	v, ok := m[path]
	if !ok {
		return nil, fmt.Errorf("missing %s", path)
	}
	return io.NopCloser(strings.NewReader(v)), nil
}
func (m memorySource) Includes(path string) ([]string, error) { return []string{path}, nil }

func TestScanBudgets(t *testing.T) {
	tests := []struct {
		name, content, scope, want string
		bytes, files, statements   int
	}{
		{name: "bytes", content: "# oversized comment", scope: "http", bytes: MaxBytes - 3, want: "64 MiB"},
		{name: "files", content: "", scope: "http", files: MaxFiles, want: "256"},
		{name: "statements", content: "set $x 1;", scope: "http", statements: MaxStatements, want: "指令总数"},
		{name: "token", content: "geo $x { " + strings.Repeat("x", maxToken+1) + "; }", scope: "http", want: "token"},
		{name: "depth", content: strings.Repeat("x {", 66) + strings.Repeat("}", 66), scope: "http", want: "64 层"},
		{name: "unfinished skipped block", content: "geo $x { default ZZ;", scope: "http", want: "未结束"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &scanner{ctx: context.Background(), bytes: tt.bytes, files: tt.files, statements: tt.statements}
			err := s.scan("test.conf", tt.scope, strings.NewReader(tt.content), 0)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want %s", err, tt.want)
			}
		})
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := checkError(ctx, nil, "site", "", strings.NewReader("server {}")); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestIncludesScopesAndLexer(t *testing.T) {
	source := memorySource{
		"/nginx/nginx.conf":  `events {} http { include data.conf; }`,
		"/nginx/data.conf":   `geo $country { default ZZ; } map $x $y { default "{};#"; } server { return 403; }`,
		"/nginx/nested.conf": `location / { set $x "escaped\";}"; root ${document_root}; }`,
	}
	main := `server { include nested.conf; }`
	if err := checkError(context.Background(), source, "/site.conf", "/nginx/nginx.conf", strings.NewReader(main)); err != nil {
		t.Fatal(err)
	}
	source["/nginx/nested.conf"] = `location / { include cycle.conf; }`
	source["/nginx/cycle.conf"] = `include nested.conf;`
	if err := checkError(context.Background(), source, "/site.conf", "/nginx/nginx.conf", strings.NewReader(main)); err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("cycle: %v", err)
	}
}

func checkError(ctx context.Context, source Source, sitePath, globalPath string, main io.Reader) error {
	report := Diagnose(ctx, source, sitePath, globalPath, main)
	if len(report.Warnings) > 0 {
		return fmt.Errorf("%+v", report.Warnings)
	}
	if !report.Complete {
		return fmt.Errorf("incomplete diagnostic")
	}
	return nil
}
