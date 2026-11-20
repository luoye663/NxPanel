package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVerifySignedManifest(t *testing.T) {
	platform := runtime.GOOS + "/" + runtime.GOARCH
	other := "linux/arm64"
	if platform == other {
		other = "linux/amd64"
	}
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := Keys("release:" + base64.StdEncoding.EncodeToString(public))
	if err != nil {
		t.Fatal(err)
	}
	asset := Asset{Platform: platform, Name: AssetName(platform), Size: 10, SHA256: hex.EncodeToString(make([]byte, 32))}
	base := Manifest{Product: "nxpanel", Protocol: Protocol, Version: "v1.2.3-rc.1", Channel: "preview", Assets: []Asset{asset}}
	cases := []struct {
		name   string
		mutate func(*Manifest)
		alter  bool
		valid  bool
	}{
		{name: "preview", valid: true},
		{name: "unsigned edit", alter: true},
		{name: "incorrect channel", mutate: func(manifest *Manifest) { manifest.Channel = "stable" }},
		{name: "unsupported protocol", mutate: func(manifest *Manifest) { manifest.Protocol++ }},
		{name: "wrong architecture", mutate: func(manifest *Manifest) {
			manifest.Assets[0].Platform, manifest.Assets[0].Name = other, AssetName(other)
		}},
		{name: "duplicate architecture", mutate: func(manifest *Manifest) { manifest.Assets = append(manifest.Assets, asset) }},
	}
	for _, sample := range cases {
		t.Run(sample.name, func(t *testing.T) {
			manifest := base
			manifest.Assets = []Asset{asset}
			if sample.mutate != nil {
				sample.mutate(&manifest)
			}
			raw, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			signature, err := json.Marshal(map[string]string{"release": base64.StdEncoding.EncodeToString(ed25519.Sign(private, raw))})
			if err != nil {
				t.Fatal(err)
			}
			if sample.alter {
				raw = append(raw, '\n')
			}
			_, selected, err := Verify(raw, signature, keys)
			if sample.valid != (err == nil) || sample.valid && selected != asset {
				t.Fatalf("unexpected manifest result: %+v, %v", selected, err)
			}
			if _, _, err := Verify(raw, signature, nil); err == nil {
				t.Fatal("accepted a manifest without trusted keys")
			}
		})
	}
}

func TestArchiveVerificationAndMembers(t *testing.T) {
	fixtures := []struct {
		name  string
		files []string
		valid bool
	}{
		{"two binaries", []string{"nxpanel-api", "nxpanel-agent"}, true},
		{"unexpected path", []string{"nxpanel-api", "../nxpanel-agent"}, false},
		{"duplicate", []string{"nxpanel-api", "nxpanel-api"}, false},
		{"missing", []string{"nxpanel-api"}, false},
		{"extra", []string{"nxpanel-api", "nxpanel-agent", "install.sh"}, false},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			var compressed bytes.Buffer
			gzipWriter := gzip.NewWriter(&compressed)
			tarWriter := tar.NewWriter(gzipWriter)
			for _, name := range fixture.files {
				if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0755, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR, Size: int64(len(name))}); err != nil {
					t.Fatal(err)
				}
				if _, err := io.WriteString(tarWriter, name); err != nil {
					t.Fatal(err)
				}
			}
			if err := tarWriter.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gzipWriter.Close(); err != nil {
				t.Fatal(err)
			}
			data := compressed.Bytes()
			digest := sha256.Sum256(data)
			asset := Asset{Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}
			if err := CopyPackage(io.Discard, bytes.NewReader(data), asset); err != nil {
				t.Fatal(err)
			}
			if err := CopyPackage(io.Discard, io.MultiReader(bytes.NewReader(data), bytes.NewReader([]byte{0})), asset); err == nil {
				t.Fatal("accepted extra package data")
			}
			dir := t.TempDir()
			if err := unpack(bytes.NewReader(data), dir); fixture.valid != (err == nil) {
				t.Fatalf("unexpected archive result: %v", err)
			}
			if fixture.valid {
				for _, name := range fixture.files {
					content, err := os.ReadFile(filepath.Join(dir, name))
					if err != nil || string(content) != name {
						t.Fatalf("unexpected contents of %s: %q %v", name, content, err)
					}
				}
			}
		})
	}
}
