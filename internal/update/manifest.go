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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/mod/semver"
)

const (
	Protocol      = 1
	MetadataMax   = 64 << 10
	PackageMax    = 128 << 20
	BinaryMax     = 128 << 20
	ManifestName  = "nxpanel-update.json"
	SignatureName = "nxpanel-update.sig"
)

var TrustKeys string

type Manifest struct {
	Product  string  `json:"product"`
	Protocol int     `json:"protocol"`
	Version  string  `json:"version"`
	Channel  string  `json:"channel"`
	Assets   []Asset `json:"assets"`
}

type Asset struct {
	Platform string `json:"platform"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

func AssetName(platform string) string {
	return "nxpanel-update-" + strings.ReplaceAll(platform, "/", "-") + ".tar.gz"
}

func Channel(version string) string {
	if semver.Prerelease(version) != "" {
		return "preview"
	}
	return "stable"
}

func ValidVersion(version string) bool {
	return len(version) <= 100 && semver.IsValid(version) && semver.Canonical(version) == version
}

func Keys(value string) (map[string]ed25519.PublicKey, error) {
	keys := make(map[string]ed25519.PublicKey)
	for _, entry := range strings.FieldsFunc(value, func(char rune) bool { return char == ',' }) {
		id, encoded, found := strings.Cut(entry, ":")
		key, err := base64.StdEncoding.DecodeString(encoded)
		if !found || id == "" || err != nil || len(key) != ed25519.PublicKeySize || keys[id] != nil {
			return nil, errors.New("invalid update trust configuration")
		}
		keys[id] = key
	}
	if len(keys) > 8 {
		return nil, errors.New("too many update trust keys")
	}
	return keys, nil
}

func Decode(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return errors.New("expected exactly one JSON value")
	}
	return nil
}

func Verify(raw, signature []byte, keys map[string]ed25519.PublicKey) (Manifest, Asset, error) {
	var manifest Manifest
	var signatures map[string]string
	if len(raw) > MetadataMax || len(signature) > MetadataMax || Decode(bytes.NewReader(signature), &signatures) != nil || len(signatures) > 8 {
		return manifest, Asset{}, errors.New("invalid update signature envelope")
	}
	verified := false
	for id, encoded := range signatures {
		key := keys[id]
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if len(key) == ed25519.PublicKeySize && err == nil && ed25519.Verify(key, raw, decoded) {
			verified = true
			break
		}
	}
	if !verified {
		return manifest, Asset{}, errors.New("update signature not trusted")
	}
	if err := Decode(bytes.NewReader(raw), &manifest); err != nil {
		return manifest, Asset{}, fmt.Errorf("invalid signed update manifest: %w", err)
	}
	if manifest.Product != "nxpanel" || manifest.Protocol != Protocol || !ValidVersion(manifest.Version) || manifest.Channel != Channel(manifest.Version) {
		return manifest, Asset{}, errors.New("incompatible update manifest")
	}
	var selected Asset
	seen := make(map[string]bool)
	for _, asset := range manifest.Assets {
		digest, err := hex.DecodeString(asset.SHA256)
		if (asset.Platform != "linux/amd64" && asset.Platform != "linux/arm64") || seen[asset.Platform] || asset.Name != AssetName(asset.Platform) || asset.Size <= 0 || asset.Size > PackageMax || err != nil || len(digest) != sha256.Size || asset.SHA256 != strings.ToLower(asset.SHA256) {
			return manifest, Asset{}, errors.New("invalid signed update asset")
		}
		seen[asset.Platform] = true
		if asset.Platform == runtime.GOOS+"/"+runtime.GOARCH {
			selected = asset
		}
	}
	if selected.Name == "" {
		return manifest, Asset{}, errors.New("update unavailable for this platform")
	}
	return manifest, selected, nil
}

func CopyPackage(dst io.Writer, src io.Reader, asset Asset) error {
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(src, asset.Size+1))
	if err != nil {
		return err
	}
	if written != asset.Size || hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return errors.New("update archive size or digest mismatch")
	}
	return nil
}

func unpack(src io.Reader, dir string) error {
	compressed, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer compressed.Close()
	bounded := &io.LimitedReader{R: compressed, N: 2*BinaryMax + 16<<10}
	archive := tar.NewReader(bounded)
	seen := make(map[string]bool)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if (header.Name != "nxpanel-api" && header.Name != "nxpanel-agent") || seen[header.Name] || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > BinaryMax || header.Format != tar.FormatUSTAR {
			return errors.New("unexpected update archive member")
		}
		seen[header.Name] = true
		file, err := os.OpenFile(filepath.Join(dir, header.Name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
		if err != nil {
			return err
		}
		_, err = io.Copy(file, archive)
		if err := errors.Join(err, file.Sync(), file.Close()); err != nil {
			return err
		}
	}
	remaining, err := io.Copy(io.Discard, bounded)
	if err != nil || remaining != 0 || bounded.N <= 0 || len(seen) != 2 {
		return errors.New("incomplete or oversized update archive")
	}
	return nil
}
