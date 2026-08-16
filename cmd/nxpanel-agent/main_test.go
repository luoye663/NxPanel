package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
)

func TestNewAgentHTTPServerTimeouts(t *testing.T) {
	cfg := app.DefaultConfig()
	cfg.API.UploadTimeout = "4m"
	server := newAgentHTTPServer(cfg, http.NotFoundHandler())
	if server.ReadHeaderTimeout != 5*time.Second || server.ReadTimeout != 4*time.Minute {
		t.Fatalf("timeouts = %s/%s", server.ReadHeaderTimeout, server.ReadTimeout)
	}
}
