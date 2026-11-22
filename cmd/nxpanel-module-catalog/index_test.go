package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
)

func TestIndexSigningAndPublicVerification(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NXPANEL_MODULE_TRUST_KEYS", "index:"+base64.StdEncoding.EncodeToString(public))
	t.Setenv("NXPANEL_UPDATE_TRUST_KEYS", "")
	signingKey := "index:" + base64.StdEncoding.EncodeToString(private.Seed())
	t.Setenv("NXPANEL_MODULE_SIGNING_KEYS", signingKey)
	base, unsigned, signed, next := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	empty := map[string][]byte{nginxmodule.IndexName: []byte("[]\n")}
	if err := writeFiles(base, empty); err != nil {
		t.Fatal(err)
	}
	if err := writeFiles(unsigned, empty); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(unsigned, nginxmodule.IndexName)
	if err := signIndex(input, signed, base, 0); err != nil {
		t.Fatal(err)
	}
	if err := signIndex(input, base, "", math.MaxUint64-1); err != nil {
		t.Fatal(err)
	}
	if err := signIndex(input, next, base, 0); err != nil {
		t.Fatal(err)
	}
	if err := signIndex(input, t.TempDir(), next, 0); err == nil || !strings.Contains(err.Error(), "cannot be incremented") {
		t.Fatalf("expected sequence overflow rejection, got %v", err)
	}
	t.Setenv("NXPANEL_MODULE_SIGNING_KEYS", "")
	if err := verifyIndex(next, base); err != nil {
		t.Fatalf("public-only verification: %v", err)
	}
	for _, candidate := range []string{signed, base} {
		if err := verifyIndex(candidate, base); err == nil || !strings.Contains(err.Error(), "must exceed") {
			t.Fatalf("expected a strictly greater sequence, got %v", err)
		}
	}
	if err := verifyIndex(unsigned, ""); !os.IsNotExist(err) {
		t.Fatalf("unsigned candidates must fail verification, got %v", err)
	}
	if err := verifyIndex(signed, unsigned); err != nil {
		t.Fatalf("signed candidate should verify against an unsigned empty base: %v", err)
	}
	if err := os.WriteFile(input, []byte("null\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyIndex(next, unsigned); err == nil || !strings.Contains(err.Error(), "unsigned base index must be an empty JSON array") {
		t.Fatalf("invalid unsigned base must fail verification, got %v", err)
	}
	if err := os.WriteFile(filepath.Join(next, nginxmodule.IndexName), []byte("[ ]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyIndex(next, base); err == nil {
		t.Fatal("modified index bytes unexpectedly passed verification")
	}
}
