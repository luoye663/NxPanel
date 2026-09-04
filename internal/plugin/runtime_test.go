package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

var healthyWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7f,
	0x03, 0x02, 0x01, 0x00,
	0x07, 0x0e, 0x01, 0x0a, 'n', 'x', 'p', '_', 'h', 'e', 'a', 'l', 't', 'h', 0x00, 0x00,
	0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x00, 0x0b,
}

func TestWASMRuntimeValidateAndLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugin.wasm")
	if err := os.WriteFile(path, healthyWASM, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := NewWASMRuntime(context.Background())
	defer runtime.Close(context.Background())
	manifest := &Manifest{ID: "org.nxpanel.test"}
	if err := runtime.Validate(context.Background(), manifest, path); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := runtime.Enable(context.Background(), manifest, path); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := runtime.Disable(context.Background(), manifest.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
}

func TestWASMRuntimeRejectsMissingHealth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.wasm")
	if err := os.WriteFile(path, []byte{0, 0x61, 0x73, 0x6d, 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := NewWASMRuntime(context.Background())
	defer runtime.Close(context.Background())
	if err := runtime.Validate(context.Background(), &Manifest{}, path); err == nil {
		t.Fatal("expected missing health export error")
	}
}
