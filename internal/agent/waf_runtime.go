package agent

import (
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/waf"
)

var configureArgumentsPattern = regexp.MustCompile(`configure arguments:\s*(.*)`)

func (e *NginxExecutor) WAFFingerprint(ctx context.Context) (waf.RuntimeFingerprint, error) {
	bin := e.GetBin()
	if bin == "" {
		return waf.RuntimeFingerprint{}, fmt.Errorf("nginx binary is not configured")
	}
	result, err := e.run(ctx, bin, "-V")
	if err != nil {
		return waf.RuntimeFingerprint{}, fmt.Errorf("nginx -V failed: %w", err)
	}
	versionOutput := strings.TrimSpace(result.Stdout + "\n" + result.Stderr)
	version := nginx.ParseVersion(versionOutput)
	if version == "" {
		return waf.RuntimeFingerprint{}, fmt.Errorf("nginx version could not be parsed")
	}
	kind := "nginx"
	if strings.Contains(strings.ToLower(versionOutput), "openresty") {
		kind = "openresty"
	}
	configureArgs := ""
	if match := configureArgumentsPattern.FindStringSubmatch(versionOutput); len(match) == 2 {
		fields := strings.Fields(match[1])
		sort.Strings(fields)
		configureArgs = strings.Join(fields, "\n")
	}
	argsSum := sha256.Sum256([]byte(configureArgs))
	binarySHA, err := hashRegularFile(bin, 512*1024*1024)
	if err != nil {
		return waf.RuntimeFingerprint{}, fmt.Errorf("hash nginx binary: %w", err)
	}
	return waf.NewRuntimeFingerprint(kind, version, hex.EncodeToString(argsSum[:]), binarySHA, runtime.GOOS, runtime.GOARCH, detectELFLibc(bin))
}

func hashRegularFile(path string, maxBytes int64) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("not a regular file")
	}
	if info.Size() > maxBytes {
		return "", fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, maxBytes+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func detectELFLibc(path string) string {
	f, err := elf.Open(path)
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	for _, program := range f.Progs {
		if program.Type != elf.PT_INTERP {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(program.Open(), 4096))
		if err != nil {
			return "unknown"
		}
		interpreter := strings.ToLower(strings.TrimRight(string(data), "\x00"))
		switch {
		case strings.Contains(interpreter, "musl"):
			return "musl"
		case strings.Contains(interpreter, "ld-linux"), strings.Contains(interpreter, "glibc"):
			return "glibc"
		default:
			return interpreter
		}
	}
	return "static-or-unknown"
}
