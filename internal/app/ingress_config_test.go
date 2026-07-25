package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUploadAndIngressConfigDefaultsAreBackwardCompatible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("api:\n  login_path: /nx-testgate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.API.ReadHeaderTimeout != "5s" || cfg.API.ReadTimeout != "15s" || cfg.API.MaxUploadSize != "100M" {
		t.Fatalf("unexpected defaults: %+v", cfg.API)
	}
	if cfg.API.Ingress.RequestRate != 10 || cfg.API.Ingress.RequestBurst != 40 || cfg.API.Ingress.MaxTrackedIPs != 4096 || cfg.API.Ingress.MaxConnections != 256 || cfg.API.Ingress.MaxConnectionsPerIP != 20 {
		t.Fatalf("unexpected ingress defaults: %+v", cfg.API.Ingress)
	}
}

func TestUploadAndIngressEnvironmentOverrides(t *testing.T) {
	t.Setenv("NXPANEL_API_MAX_UPLOAD_SIZE", "12M")
	t.Setenv("NXPANEL_API_READ_HEADER_TIMEOUT", "3s")
	t.Setenv("NXPANEL_API_INGRESS_REQUEST_RATE", "7.5")
	t.Setenv("NXPANEL_API_INGRESS_REQUEST_BURST", "21")
	t.Setenv("NXPANEL_API_INGRESS_MAX_TRACKED_IPS", "99")
	t.Setenv("NXPANEL_API_INGRESS_MAX_CONNECTIONS", "80")
	t.Setenv("NXPANEL_API_INGRESS_MAX_CONNECTIONS_PER_IP", "9")
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.API.MaxUploadSize != "12M" || cfg.API.ReadHeaderTimeout != "3s" || cfg.API.Ingress.RequestRate != 7.5 || cfg.API.Ingress.RequestBurst != 21 || cfg.API.Ingress.MaxTrackedIPs != 99 || cfg.API.Ingress.MaxConnections != 80 || cfg.API.Ingress.MaxConnectionsPerIP != 9 {
		t.Fatalf("environment overrides not applied: %+v", cfg.API)
	}
}
