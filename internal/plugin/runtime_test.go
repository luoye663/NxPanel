package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type countingBroker struct{ calls int }

func (b *countingBroker) Call(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
	b.calls++
	return json.RawMessage(`{}`), nil
}

// A module named after another installation calls host_call from nxp_health.
// This must not reach the production broker during package inspection.
func TestInspectionCannotImpersonateInstalledPlugin(t *testing.T) {
	wasm := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	section := func(id byte, data []byte) { wasm = append(wasm, id, byte(len(data))); wasm = append(wasm, data...) }
	name := "com.example.victim"
	custom := append([]byte{4, 'n', 'a', 'm', 'e', 0, byte(len(name) + 1), byte(len(name))}, []byte(name)...)
	section(0, custom)
	section(1, []byte{2, 0x60, 6, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7e, 0x60, 0, 1, 0x7f})
	imports := append([]byte{1, 10}, []byte("nxpanel_v1")...)
	imports = append(imports, 9)
	imports = append(imports, []byte("host_call")...)
	section(2, append(imports, 0, 0))
	section(3, []byte{1, 1})
	section(5, []byte{1, 0, 1})
	exports := append([]byte{1, 10}, []byte("nxp_health")...)
	section(7, append(exports, 0, 1))
	body := []byte{0, 0x41, 0, 0x41, 6, 0x41, 6, 0x41, 11, 0x41, 32, 0x41, 32, 0x10, 0, 0x1a, 0x41, 0, 0x0b}
	section(10, append([]byte{1, byte(len(body))}, body...))
	data := []byte(`kv.get{"key":"x"}`)
	section(11, append([]byte{1, 0, 0x41, 0, 0x0b, byte(len(data))}, data...))
	path := filepath.Join(t.TempDir(), "untrusted.wasm")
	if err := os.WriteFile(path, wasm, 0600); err != nil {
		t.Fatal(err)
	}
	broker := &countingBroker{}
	r := NewWASMRuntimeWithBroker(context.Background(), broker)
	defer r.Close(context.Background())
	if err := r.Validate(context.Background(), &Manifest{ID: "com.example.upload"}, path); err != nil {
		t.Fatal(err)
	}
	if broker.calls != 0 {
		t.Fatalf("inspection reached production broker %d times", broker.calls)
	}
}

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
