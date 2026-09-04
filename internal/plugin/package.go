package plugin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type PackageLimits struct {
	CompressedBytes int64
	ExpandedBytes   int64
	Entries         int
}

var DefaultPackageLimits = PackageLimits{CompressedBytes: 64 << 20, ExpandedBytes: 256 << 20, Entries: 10_000}

func VerifyPackage(ctx context.Context, packagePath, expectedSHA256, destination string, limits PackageLimits) (*Manifest, error) {
	if limits.CompressedBytes <= 0 || limits.ExpandedBytes <= 0 || limits.Entries <= 0 {
		limits = DefaultPackageLimits
	}
	f, err := os.Open(packagePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Size() > limits.CompressedBytes {
		return nil, errors.New("plugin package exceeds compressed size limit")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	actualPackageHash := hex.EncodeToString(h.Sum(nil))
	if expectedSHA256 != "" && actualPackageHash != expectedSHA256 {
		return nil, errors.New("plugin package digest mismatch")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open plugin gzip: %w", err)
	}
	defer gz.Close()

	if err := os.MkdirAll(destination, 0o750); err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	var manifestBytes []byte
	actualFiles := make(map[string]FileDigest)
	var expanded int64
	entries := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read plugin archive: %w", err)
		}
		entries++
		if entries > limits.Entries {
			return nil, errors.New("plugin package has too many entries")
		}
		if err := validatePackagePath(hdr.Name); err != nil {
			return nil, fmt.Errorf("unsafe archive entry %q", hdr.Name)
		}
		target := filepath.Join(destination, filepath.FromSlash(hdr.Name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return nil, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if hdr.Size < 0 || expanded > limits.ExpandedBytes-hdr.Size {
				return nil, errors.New("plugin package exceeds expanded size limit")
			}
			expanded += hdr.Size
			if _, exists := actualFiles[hdr.Name]; exists || (hdr.Name == "manifest.json" && manifestBytes != nil) {
				return nil, fmt.Errorf("duplicate archive entry %q", hdr.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return nil, err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
			if err != nil {
				return nil, err
			}
			digest := sha256.New()
			reader := io.TeeReader(io.LimitReader(tr, hdr.Size+1), digest)
			var content []byte
			if hdr.Name == "manifest.json" {
				content, err = io.ReadAll(reader)
			} else {
				_, err = io.Copy(out, reader)
			}
			closeErr := out.Close()
			if err != nil || closeErr != nil {
				return nil, errors.Join(err, closeErr)
			}
			if hdr.Name == "manifest.json" {
				if err := os.WriteFile(target, content, 0o640); err != nil {
					return nil, err
				}
				manifestBytes = content
			} else {
				actualFiles[hdr.Name] = FileDigest{Path: hdr.Name, SHA256: hex.EncodeToString(digest.Sum(nil)), Size: hdr.Size}
			}
		default:
			return nil, fmt.Errorf("unsupported archive entry type for %q", hdr.Name)
		}
	}
	if manifestBytes == nil {
		return nil, errors.New("plugin package is missing manifest.json")
	}
	manifest, err := ParseManifest(manifestBytes)
	if err != nil {
		return nil, err
	}
	if len(actualFiles) != len(manifest.Files) {
		return nil, errors.New("archive files do not exactly match manifest")
	}
	for _, declared := range manifest.Files {
		actual, ok := actualFiles[declared.Path]
		if !ok || actual.Size != declared.Size || actual.SHA256 != declared.SHA256 {
			return nil, fmt.Errorf("file digest mismatch for %q", declared.Path)
		}
	}
	return manifest, nil
}
