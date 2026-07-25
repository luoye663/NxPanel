package agent

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
)

func tinyArchiveLimits() archiveLimits {
	return archiveLimits{maxEntries: 10, maxInputBytes: 1024, maxArchiveBytes: 1024, maxExtracted: 1024, maxEntryBytes: 128, maxDepth: 4, maxRatio: 100, timeout: time.Second}
}

func TestArchiveLimitsRejectEntriesDepthRatioAndEntrySize(t *testing.T) {
	dir := t.TempDir()
	policy := NewPathPolicy([]string{dir})
	tests := []struct {
		name    string
		entry   string
		content string
		limits  func() archiveLimits
	}{
		{name: "entry-size", entry: "large.txt", content: strings.Repeat("x", 32), limits: func() archiveLimits { l := tinyArchiveLimits(); l.maxEntryBytes = 8; return l }},
		{name: "depth", entry: "a/b/c/d.txt", content: "x", limits: func() archiveLimits { l := tinyArchiveLimits(); l.maxDepth = 3; return l }},
		{name: "ratio", entry: "bomb.txt", content: strings.Repeat("0", 128), limits: func() archiveLimits { l := tinyArchiveLimits(); l.maxRatio = 2; return l }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archivePath := filepath.Join(dir, tt.name+".zip")
			writeTestZip(t, archivePath, map[string]string{tt.entry: tt.content})
			dest := filepath.Join(dir, "dest-"+tt.name)
			if err := extractZipWithLimits(context.Background(), policy, archivePath, dest, tt.limits()); !errors.Is(err, ErrArchiveBudgetExceeded) {
				t.Fatalf("expected archive budget error, got %v", err)
			}
			if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(tt.entry))); !os.IsNotExist(err) {
				t.Fatalf("rejected entry must not leave output, stat err=%v", err)
			}
		})
	}

	archivePath := filepath.Join(dir, "entries.zip")
	writeTestZip(t, archivePath, map[string]string{"one": "1", "two": "2"})
	limits := tinyArchiveLimits()
	limits.maxEntries = 1
	if err := extractZipWithLimits(context.Background(), policy, archivePath, filepath.Join(dir, "entry-dest"), limits); !errors.Is(err, ErrArchiveBudgetExceeded) {
		t.Fatalf("expected entry count error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "entry-dest", "one")); !os.IsNotExist(err) {
		t.Fatalf("entry count rejection left an earlier output: %v", err)
	}
}

func TestTarAndTarGzShareExtractionBudgets(t *testing.T) {
	dir := t.TempDir()
	policy := NewPathPolicy([]string{dir})
	for _, gzipped := range []bool{false, true} {
		name := "test.tar"
		if gzipped {
			name += ".gz"
		}
		archivePath := filepath.Join(dir, name)
		writeTestTar(t, archivePath, gzipped, map[string]string{"large.txt": strings.Repeat("x", 20)})
		limits := tinyArchiveLimits()
		limits.maxEntryBytes = 8
		err := extractTarWithLimits(context.Background(), policy, archivePath, filepath.Join(dir, fmt.Sprintf("dest-%v", gzipped)), gzipped, limits)
		if !errors.Is(err, ErrArchiveBudgetExceeded) {
			t.Fatalf("gzipped=%v expected budget error, got %v", gzipped, err)
		}
	}
}

func TestArchiveSelfOutputAndPartialCleanup(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte(strings.Repeat("abcdef", 30)), 0644); err != nil {
		t.Fatal(err)
	}
	selfOutput := filepath.Join(root, "inside.zip")
	if err := compressToZipWithLimits(context.Background(), []string{root}, selfOutput, tinyArchiveLimits()); err == nil {
		t.Fatal("archive output inside source must be rejected")
	}
	if _, err := os.Stat(selfOutput); !os.IsNotExist(err) {
		t.Fatalf("self output must not be created: %v", err)
	}

	output := filepath.Join(filepath.Dir(root), filepath.Base(root)+".zip")
	limits := tinyArchiveLimits()
	limits.maxArchiveBytes = 16
	if err := compressToZipWithLimits(context.Background(), []string{root}, output, limits); !errors.Is(err, ErrArchiveBudgetExceeded) {
		t.Fatalf("expected produced archive budget error, got %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("failed archive must be cleaned: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(output), ".nxpanel-archive-*.tmp")); len(matches) != 0 {
		t.Fatalf("temporary archives leaked: %v", matches)
	}
}

func TestArchiveWalkClosesFilesBeforeCallbackReturns(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fd regression uses /proc")
	}
	root := t.TempDir()
	for i := 0; i < 300; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%04d.txt", i)), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	before := openFDCount(t)
	output := filepath.Join(t.TempDir(), "many.zip")
	limits := defaultArchiveLimits()
	limits.timeout = 10 * time.Second
	if err := compressToZipWithLimits(context.Background(), []string{root}, output, limits); err != nil {
		t.Fatal(err)
	}
	after := openFDCount(t)
	if delta := after - before; delta > 8 {
		t.Fatalf("file descriptors grew by %d; walk callbacks likely retained files", delta)
	}
}

func openFDCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestSiteBackupProducedLimitCleansTemporaryOutput(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data"), []byte(strings.Repeat("x", 2*1024*1024)), 0644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "backup.tar.gz")
	s := &Server{cfg: testArchiveConfig("1M"), policy: NewPathPolicy([]string{dir})}
	_, err := s.createSiteBackup(context.Background(), &SiteBackupCreateRequest{SiteID: "site", BackupType: "root", OutputPath: output, RootPath: root})
	if !errors.Is(err, ErrArchiveBudgetExceeded) {
		t.Fatalf("expected site backup budget error, got %v", err)
	}
	for _, path := range []string{output, output + ".tmp"} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("partial site backup leaked at %s: %v", path, statErr)
		}
	}
}

func TestGenericExtractionPreflightPreservesExistingTarget(t *testing.T) {
	formats := []struct {
		name    string
		write   func(*testing.T, string, []orderedArchiveEntry)
		extract func(context.Context, *PathPolicy, string, string, archiveLimits) error
	}{
		{name: "zip", write: writeOrderedZip, extract: extractZipWithLimits},
		{name: "tar", write: func(t *testing.T, path string, entries []orderedArchiveEntry) {
			writeOrderedTar(t, path, false, entries)
		}, extract: func(ctx context.Context, policy *PathPolicy, archivePath, dest string, limits archiveLimits) error {
			return extractTarWithLimits(ctx, policy, archivePath, dest, false, limits)
		}},
		{name: "tar.gz", write: func(t *testing.T, path string, entries []orderedArchiveEntry) {
			writeOrderedTar(t, path, true, entries)
		}, extract: func(ctx context.Context, policy *PathPolicy, archivePath, dest string, limits archiveLimits) error {
			return extractTarWithLimits(ctx, policy, archivePath, dest, true, limits)
		}},
	}
	for _, format := range formats {
		t.Run(format.name, func(t *testing.T) {
			dir := t.TempDir()
			archivePath := filepath.Join(dir, "archive."+format.name)
			dest := filepath.Join(dir, "dest")
			if err := os.Mkdir(dest, 0755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dest, "existing.txt")
			if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
				t.Fatal(err)
			}
			format.write(t, archivePath, []orderedArchiveEntry{{name: "existing.txt", content: "replacement"}, {name: "later.txt", content: "rejected"}})
			limits := tinyArchiveLimits()
			limits.maxEntries = 1
			err := format.extract(context.Background(), NewPathPolicy([]string{dir}), archivePath, dest, limits)
			if !errors.Is(err, ErrArchiveBudgetExceeded) {
				t.Fatalf("expected preflight budget error, got %v", err)
			}
			content, readErr := os.ReadFile(target)
			if readErr != nil || string(content) != "original" {
				t.Fatalf("preflight failure changed existing target: %q err=%v", content, readErr)
			}
			if _, statErr := os.Stat(filepath.Join(dest, "later.txt")); !os.IsNotExist(statErr) {
				t.Fatalf("preflight failure created later target: %v", statErr)
			}
		})
	}
}

func TestPreExistingArchiveCompressedSizeCeiling(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "archive.zip")
	writeOrderedZip(t, archivePath, []orderedArchiveEntry{{name: "file.txt", content: "content"}})
	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	limits := tinyArchiveLimits()
	limits.maxArchiveBytes = info.Size() - 1
	err = extractZipWithLimits(context.Background(), NewPathPolicy([]string{dir}), archivePath, filepath.Join(dir, "dest"), limits)
	if !errors.Is(err, ErrArchiveBudgetExceeded) {
		t.Fatalf("expected compressed archive ceiling error, got %v", err)
	}
}

func TestSiteBackupRestoreRollsBackMutationAfterExtractFailure(t *testing.T) {
	server, req, target := setupFailingSiteRestore(t)
	err := server.restoreSiteBackup(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "已从快照回滚") {
		t.Fatalf("expected successful snapshot rollback error, got %v", err)
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil || string(content) != "original" {
		t.Fatalf("snapshot rollback did not restore target: %q err=%v", content, readErr)
	}
}

func TestSiteBackupRestoreReportsRollbackFailure(t *testing.T) {
	server, req, _ := setupFailingSiteRestore(t)
	server.restoreSnapshotHook = func(path string) { _ = os.Remove(path) }
	err := server.restoreSiteBackup(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "快照回滚失败") {
		t.Fatalf("expected rollback failure in error, got %v", err)
	}
}

type orderedArchiveEntry struct {
	name    string
	content string
}

func writeOrderedZip(t *testing.T, path string, entries []orderedArchiveEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	for _, entry := range entries {
		writer, err := zw.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, entry.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeOrderedTar(t *testing.T, path string, gzipped bool, entries []orderedArchiveEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	var output io.Writer = file
	var gw *gzip.Writer
	if gzipped {
		gw = gzip.NewWriter(file)
		output = gw
	}
	tw := tar.NewWriter(output)
	for _, entry := range entries {
		content := []byte(entry.content)
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if gw != nil {
		if err := gw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func setupFailingSiteRestore(t *testing.T) (*Server, *SiteBackupRestoreRequest, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "file.txt")
	if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	metadata, err := json.Marshal(siteBackupMetadata{Version: 1, SiteID: "site", BackupType: "root"})
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(dir, "malformed.tar.gz")
	writeOrderedTar(t, backupPath, true, []orderedArchiveEntry{{name: "root/file.txt", content: "replacement"}, {name: "../invalid", content: "fault"}, {name: "metadata.json", content: string(metadata)}})
	cfg := app.DefaultConfig()
	server := &Server{cfg: cfg, policy: NewPathPolicy([]string{dir})}
	req := &SiteBackupRestoreRequest{SiteID: "site", BackupPath: backupPath, RestoreRoot: true, RootPath: root}
	return server, req, target
}

func testArchiveConfig(maxInput string) *app.Config {
	cfg := app.DefaultConfig()
	cfg.Agent.Archive.MaxInputSize = maxInput
	return cfg
}
