package plugin

import "testing"

func TestManifestValidation(t *testing.T) {
	m := Manifest{SchemaVersion: 1, ID: "org.example.hello", Name: "Hello", Version: "1.0.0", Publisher: "Example", PluginAPIVersion: "v1", Backend: "backend/plugin.wasm", Permissions: []string{"panel.sites.read"}, Files: []FileDigest{{Path: "backend/plugin.wasm", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 8}}}
	if err := m.Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	m.Backend = "../escape.wasm"
	if err := m.Validate(); err == nil {
		t.Fatal("unsafe backend accepted")
	}
}

func TestManifestRejectsUnknownNativeProvider(t *testing.T) {
	m := Manifest{SchemaVersion: 1, ID: "com.example.provider", Name: "Provider", Version: "1.0.0", Publisher: "Example", PluginAPIVersion: "v1", Backend: "plugin.wasm", Providers: []string{"arbitrary.native.provider"}, Files: []FileDigest{{Path: "plugin.wasm", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 8}}}
	if err := m.Validate(); err == nil {
		t.Fatal("unknown native provider accepted")
	}
}

func TestManifestRejectsUnknownCapability(t *testing.T) {
	m := Manifest{SchemaVersion: 1, ID: "org.nxpanel.test", Name: "Test", Version: "1.0.0", Publisher: "nxPanel", PluginAPIVersion: "v1", Backend: "plugin.wasm", Permissions: []string{"shell.exec"}, Files: []FileDigest{{Path: "plugin.wasm", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 8}}}
	if err := m.Validate(); err == nil {
		t.Fatal("unknown capability accepted")
	}
}
