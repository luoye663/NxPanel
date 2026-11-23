package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/trust"
	"github.com/luoye663/nxpanel/internal/update"
)

func main() {
	keygen := flag.String("keygen", "", "directory for a new Ed25519 keypair")
	keyID := flag.String("key-id", "release-1", "key identifier for -keygen")
	output := flag.String("out", "release/updates", "output directory")
	version := flag.String("version", "", "signed release version")
	amd64 := flag.String("amd64", "", "directory with linux/amd64 binaries")
	arm64 := flag.String("arm64", "", "directory with linux/arm64 binaries")
	policyDir := flag.String("trust-dir", trust.Path, "directory with the signed root policy")
	flag.Parse()
	var err error
	if *keygen != "" {
		err = generateKey(*keygen, *keyID)
	} else {
		err = build(*output, *version, *amd64, *arm64, *policyDir)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generateKey(dir, id string) error {
	if id == "" || strings.ContainsAny(id, " ,:\r\n\t") {
		return errors.New("invalid key identifier")
	}
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "signing.key"), []byte(id+":"+base64.StdEncoding.EncodeToString(private.Seed())+"\n"), 0600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "trust.pub"), []byte(id+":"+base64.StdEncoding.EncodeToString(public)+"\n"), 0644)
}

func build(output, version, amd64, arm64, policyDir string) error {
	if !update.ValidVersion(version) || amd64 == "" && arm64 == "" {
		return errors.New("a valid version and at least one platform are required")
	}
	policy, err := trust.CurrentDir(policyDir, os.Getenv("NXPANEL_ROOT_PUBLIC_KEY"), time.Now())
	if err != nil {
		return err
	}
	keys, err := signingKeys(policy.Update)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	manifest := update.Manifest{Product: "nxpanel", Protocol: update.Protocol, Version: version, Channel: update.Channel(version)}
	for _, platform := range []struct{ arch, dir string }{{"amd64", amd64}, {"arm64", arm64}} {
		if platform.dir == "" {
			continue
		}
		asset, err := pack(output, platform.dir, "linux/"+platform.arch)
		if err != nil {
			return err
		}
		manifest.Assets = append(manifest.Assets, asset)
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	signatures := make(map[string]string, len(keys))
	for id, key := range keys {
		signatures[id] = base64.StdEncoding.EncodeToString(ed25519.Sign(key, update.SignatureMessage(update.Purpose, raw)))
	}
	signature, err := json.Marshal(signatures)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(output, update.ManifestName), raw, 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, update.SignatureName), signature, 0644)
}

func signingKeys(public string) (map[string]ed25519.PrivateKey, error) {
	keys := make(map[string]ed25519.PrivateKey)
	for _, entry := range strings.Split(strings.TrimSpace(os.Getenv("NXPANEL_UPDATE_SIGNING_KEYS")), ",") {
		id, key, err := trust.AuthorizedPrivate(entry, public)
		if err != nil || keys[id] != nil {
			return nil, errors.New("missing or invalid release signing secret")
		}
		keys[id] = key
	}
	return keys, nil
}

func pack(output, dir, platform string) (update.Asset, error) {
	asset := update.Asset{Platform: platform, Name: update.AssetName(platform)}
	dest, err := os.Create(filepath.Join(output, asset.Name))
	if err != nil {
		return asset, err
	}
	hash := sha256.New()
	compressed := gzip.NewWriter(io.MultiWriter(dest, hash))
	archive := tar.NewWriter(compressed)
	for _, name := range []string{"nxpanel-api", "nxpanel-agent"} {
		if err = addBinary(archive, dir, name); err != nil {
			break
		}
	}
	err = errors.Join(err, archive.Close(), compressed.Close())
	info, statErr := dest.Stat()
	err = errors.Join(err, statErr)
	err = errors.Join(err, dest.Close())
	if err != nil {
		return asset, err
	}
	asset.Size, asset.SHA256 = info.Size(), hex.EncodeToString(hash.Sum(nil))
	if asset.Size > update.PackageMax {
		return asset, errors.New("compressed update exceeds client limit")
	}
	return asset, nil
}

func addBinary(archive *tar.Writer, dir, name string) error {
	file, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > update.BinaryMax {
		return fmt.Errorf("invalid update binary: %s", name)
	}
	if err := archive.WriteHeader(&tar.Header{Name: name, Format: tar.FormatUSTAR, Typeflag: tar.TypeReg, Mode: 0755, Size: info.Size()}); err != nil {
		return err
	}
	_, err = io.Copy(archive, file)
	return err
}
