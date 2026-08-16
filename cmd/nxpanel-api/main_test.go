package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
)

func TestNewAPIHTTPServerUsesIndependentReadTimeouts(t *testing.T) {
	cfg := app.DefaultConfig()
	cfg.API.ReadHeaderTimeout = "7s"
	cfg.API.ReadTimeout = "11s"
	cfg.API.UploadTimeout = "5m"
	server := newAPIHTTPServer(cfg, http.NotFoundHandler())
	if server.ReadHeaderTimeout != 7*time.Second || server.ReadTimeout != 11*time.Second {
		t.Fatalf("timeouts = %s/%s", server.ReadHeaderTimeout, server.ReadTimeout)
	}
}
