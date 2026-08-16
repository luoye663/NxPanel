package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
)

var ErrArchiveBudgetExceeded = errors.New("archive resource budget exceeded")

type archiveLimits struct {
	maxEntries      int64
	maxInputBytes   int64
	maxArchiveBytes int64
	maxExtracted    int64
	maxEntryBytes   int64
	maxDepth        int
	maxRatio        int64
	timeout         time.Duration
}

func defaultArchiveLimits() archiveLimits {
	return archiveLimits{100000, 2 << 30, 1 << 30, 2 << 30, 512 << 20, 64, 100, 30 * time.Minute}
}

func (s *Server) archiveLimits() archiveLimits {
	d := defaultArchiveLimits()
	if s == nil || s.cfg == nil {
		return d
	}
	c := s.cfg.Agent.Archive
	return archiveLimits{
		maxEntries:      clampInt64(c.MaxEntries, d.maxEntries, 1, 1000000),
		maxInputBytes:   clampSize(c.MaxInputSize, d.maxInputBytes, 1<<20, 16<<30),
		maxArchiveBytes: clampSize(c.MaxArchiveSize, d.maxArchiveBytes, 1<<20, 8<<30),
		maxExtracted:    clampSize(c.MaxExtractedSize, d.maxExtracted, 1<<20, 16<<30),
		maxEntryBytes:   clampSize(c.MaxEntrySize, d.maxEntryBytes, 1<<10, 4<<30),
		maxDepth:        int(clampInt64(int64(c.MaxDepth), int64(d.maxDepth), 1, 256)),
		maxRatio:        clampInt64(c.MaxCompressionRatio, d.maxRatio, 1, 10000),
		timeout:         clampDuration(c.Timeout, d.timeout, time.Second, 2*time.Hour),
	}
}

func clampSize(value string, fallback, min, max int64) int64 {
	return clampInt64(app.ParseSizeOrDefault(value, fallback), fallback, min, max)
}

func clampInt64(value, fallback, min, max int64) int64 {
	if value <= 0 {
		value = fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func clampDuration(value string, fallback, min, max time.Duration) time.Duration {
	d := app.ParseDurationOrDefault(value, fallback)
	if d < min {
		return min
	}
	if d > max {
		return max
	}
	return d
}

type archiveBudget struct {
	limits    archiveLimits
	entries   int64
	input     int64
	produced  int64
	extracted int64
}

func newArchiveBudget(limits archiveLimits) *archiveBudget { return &archiveBudget{limits: limits} }

func checkArchiveFileSize(path string, limits archiveLimits) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("archive is not a regular file: %s", path)
	}
	if info.Size() > limits.maxArchiveBytes {
		return nil, budgetError("compressed archive size exceeds %d", limits.maxArchiveBytes)
	}
	return info, nil
}

func (b *archiveBudget) checkEntry(name string, size int64) error {
	b.entries++
	if b.entries > b.limits.maxEntries {
		return budgetError("entry count exceeds %d", b.limits.maxEntries)
	}
	if archiveNameDepth(name) > b.limits.maxDepth {
		return budgetError("entry depth exceeds %d: %s", b.limits.maxDepth, name)
	}
	if size < 0 || size > b.limits.maxEntryBytes {
		return budgetError("entry size exceeds %d: %s", b.limits.maxEntryBytes, name)
	}
	return nil
}

func (b *archiveBudget) addInput(n int64) error {
	b.input += n
	if b.input > b.limits.maxInputBytes {
		return budgetError("input size exceeds %d", b.limits.maxInputBytes)
	}
	return nil
}

func (b *archiveBudget) addProduced(n int64) error {
	b.produced += n
	if b.produced > b.limits.maxArchiveBytes {
		return budgetError("archive size exceeds %d", b.limits.maxArchiveBytes)
	}
	return nil
}

func (b *archiveBudget) addExtracted(n int64) error {
	b.extracted += n
	if b.extracted > b.limits.maxExtracted {
		return budgetError("extracted size exceeds %d", b.limits.maxExtracted)
	}
	return nil
}

func (b *archiveBudget) checkRatio(uncompressed, compressed int64, name string) error {
	if uncompressed <= 0 {
		return nil
	}
	if compressed <= 0 || compressed <= math.MaxInt64/b.limits.maxRatio && uncompressed > compressed*b.limits.maxRatio {
		return budgetError("compression ratio exceeds %d:1: %s", b.limits.maxRatio, name)
	}
	return nil
}

func budgetError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrArchiveBudgetExceeded, fmt.Sprintf(format, args...))
}

func joinCopyCloseErrors(copyErr error, closeErrs ...error) error {
	errs := make([]error, 0, len(closeErrs)+1)
	if copyErr != nil {
		errs = append(errs, copyErr)
	}
	for _, err := range closeErrs {
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func archiveNameDepth(name string) int {
	clean := strings.Trim(filepath.ToSlash(filepath.Clean(name)), "/")
	if clean == "" || clean == "." {
		return 0
	}
	return strings.Count(clean, "/") + 1
}

type budgetWriter struct {
	dst    io.Writer
	budget *archiveBudget
}

func (w *budgetWriter) Write(p []byte) (int, error) {
	originalLen := len(p)
	remaining := w.budget.limits.maxArchiveBytes - w.budget.produced
	if remaining <= 0 {
		return 0, budgetError("archive size exceeds %d", w.budget.limits.maxArchiveBytes)
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := w.dst.Write(p)
	if budgetErr := w.budget.addProduced(int64(n)); budgetErr != nil {
		return n, budgetErr
	}
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	if err == nil && len(p) < originalLen {
		err = budgetError("archive size exceeds %d", w.budget.limits.maxArchiveBytes)
	}
	return n, err
}

func copyArchiveInput(ctx context.Context, dst io.Writer, src io.Reader, b *archiveBudget, entryRemaining int64) (int64, error) {
	return copyBudgeted(ctx, dst, src, func(n int64) error {
		entryRemaining -= n
		if entryRemaining < 0 {
			return budgetError("entry size exceeds %d", b.limits.maxEntryBytes)
		}
		return b.addInput(n)
	})
}

func copyArchiveOutput(ctx context.Context, dst io.Writer, src io.Reader, b *archiveBudget, entrySize int64) (int64, error) {
	var copied int64
	return copyBudgeted(ctx, dst, src, func(n int64) error {
		copied += n
		if copied > entrySize || copied > b.limits.maxEntryBytes {
			return budgetError("entry extracted size exceeds limit")
		}
		return b.addExtracted(n)
	})
}

func copyBudgeted(ctx context.Context, dst io.Writer, src io.Reader, account func(int64) error) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if err := account(int64(n)); err != nil {
				return total, err
			}
			wn, writeErr := dst.Write(buf[:n])
			total += int64(wn)
			if writeErr != nil {
				return total, writeErr
			}
			if wn != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return total, nil
			}
			return total, readErr
		}
	}
}

func pathsOverlap(a, b string) bool {
	a = canonicalArchivePath(a)
	b = canonicalArchivePath(b)
	return a == b || pathWithin(a, b) || pathWithin(b, a)
}

func canonicalArchivePath(path string) string {
	path = filepath.Clean(path)
	current := path
	missing := []string{}
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
	return path
}

func cleanupExtractedPaths(paths []string, destDir string) {
	for i := len(paths) - 1; i >= 0; i-- {
		_ = os.Remove(paths[i])
		for dir := filepath.Dir(paths[i]); pathWithin(destDir, dir); dir = filepath.Dir(dir) {
			if err := os.Remove(dir); err != nil {
				break
			}
		}
	}
}

func pathWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
