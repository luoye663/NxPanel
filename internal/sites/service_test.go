package sites

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

// allInsideRoots 构造一个位于 root 下的完整站点路径集合。
func allInsideRoots(t *testing.T, root string) (configPath, rootPath, accessLogPath, errorLogPath string) {
	t.Helper()
	configPath = filepath.Join(root, "vhost", "example.com.conf")
	rootPath = filepath.Join(root, "wwwroot", "example.com")
	accessLogPath = filepath.Join(root, "logs", "example.com.access.log")
	errorLogPath = filepath.Join(root, "logs", "example.com.error.log")
	return
}

func TestImportWarningsForPaths_AllInside(t *testing.T) {
	root := t.TempDir()
	roots := []string{root}
	configPath, rootPath, accessLogPath, errorLogPath := allInsideRoots(t, root)

	warnings := importWarningsForPaths(roots, configPath, rootPath, accessLogPath, errorLogPath)
	if len(warnings) != 0 {
		t.Fatalf("期望无警告，实际: %v", warnings)
	}
}

func TestImportWarningsForPaths_ConfigOutside(t *testing.T) {
	inside := t.TempDir()
	outside := t.TempDir()
	roots := []string{inside}

	configPath := filepath.Join(outside, "vhost", "example.com.conf")
	rootPath := filepath.Join(inside, "wwwroot", "example.com")

	warnings := importWarningsForPaths(roots, configPath, rootPath, "", "")
	if len(warnings) != 1 {
		t.Fatalf("期望仅 1 条警告，实际: %v", warnings)
	}
	if !containsString(warnings, "配置文件") {
		t.Fatalf("期望包含配置文件警告，实际: %v", warnings)
	}
}

func TestImportWarningsForPaths_RootOutside(t *testing.T) {
	inside := t.TempDir()
	outside := t.TempDir()
	roots := []string{inside}

	configPath := filepath.Join(inside, "vhost", "example.com.conf")
	rootPath := filepath.Join(outside, "wwwroot", "example.com")

	warnings := importWarningsForPaths(roots, configPath, rootPath, "", "")
	if len(warnings) != 1 {
		t.Fatalf("期望仅 1 条警告，实际: %v", warnings)
	}
	if !containsString(warnings, "根目录") {
		t.Fatalf("期望包含根目录警告，实际: %v", warnings)
	}
}

func TestImportWarningsForPaths_LogsOutsideAndEmptySkipped(t *testing.T) {
	inside := t.TempDir()
	outside := t.TempDir()
	roots := []string{inside}

	configPath := filepath.Join(inside, "vhost", "example.com.conf")
	rootPath := filepath.Join(inside, "wwwroot", "example.com")
	accessLogPath := filepath.Join(outside, "logs", "access.log")
	errorLogPath := filepath.Join(outside, "logs", "error.log")

	warnings := importWarningsForPaths(roots, configPath, rootPath, accessLogPath, errorLogPath)
	if len(warnings) != 2 {
		t.Fatalf("期望 2 条日志警告，实际: %v", warnings)
	}
	if !containsString(warnings, "访问日志") || !containsString(warnings, "错误日志") {
		t.Fatalf("期望包含访问/错误日志警告，实际: %v", warnings)
	}

	// 空日志路径应跳过，不应产生警告。
	warningsEmpty := importWarningsForPaths(roots, configPath, rootPath, "  ", "")
	if len(warningsEmpty) != 0 {
		t.Fatalf("空日志路径期望无警告，实际: %v", warningsEmpty)
	}
}

// TestAgentRoots_LivePreferred 验证 agentRoots 优先返回 Agent 实时白名单，
// 而非 API 本地配置快照。这是本次修复的核心：用户在 Agent 侧补充白名单后，
// 即使 API 未重启也能立即生效。
func TestAgentRoots_LivePreferred(t *testing.T) {
	cfg := &app.Config{}

	liveRoot := t.TempDir()
	svc := &Service{
		cfg: cfg,
		rootsFn: func(ctx context.Context) ([]string, error) {
			return []string{liveRoot}, nil
		},
	}

	got := svc.agentRoots(context.Background())
	if len(got) != 1 || got[0] != liveRoot {
		t.Fatalf("期望 agentRoots 返回实时 roots %v，实际: %v", []string{liveRoot}, got)
	}

	// 实时 roots 下路径不应触发警告。
	configPath := filepath.Join(liveRoot, "vhost", "example.com.conf")
	warnings := importWarningsForPaths(got, configPath, filepath.Join(liveRoot, "www"), "", "")
	if len(warnings) != 0 {
		t.Fatalf("实时白名单覆盖的路径期望无警告，实际: %v", warnings)
	}
}

// TestAgentRoots_FallbackOnAgentError 验证 Agent 不可达时降级到本地配置快照。
func TestAgentRoots_FallbackOnAgentError(t *testing.T) {
	cfg := &app.Config{}
	expected := app.BuildAllowedPathRoots(cfg)

	svc := &Service{
		cfg: cfg,
		rootsFn: func(ctx context.Context) ([]string, error) {
			return nil, context.DeadlineExceeded
		},
	}

	got := svc.agentRoots(context.Background())
	if !sameSet(got, expected) {
		t.Fatalf("期望降级回本地快照 %v，实际: %v", expected, got)
	}
}

// TestAgentRoots_FallbackOnEmpty 验证 Agent 返回空 roots 时也降级。
func TestAgentRoots_FallbackOnEmpty(t *testing.T) {
	cfg := &app.Config{}
	expected := app.BuildAllowedPathRoots(cfg)

	svc := &Service{
		cfg: cfg,
		rootsFn: func(ctx context.Context) ([]string, error) {
			return []string{}, nil
		},
	}

	got := svc.agentRoots(context.Background())
	if !sameSet(got, expected) {
		t.Fatalf("期望空 roots 降级回本地快照 %v，实际: %v", expected, got)
	}
}

// TestImportWarnings_NilAndNonImported 验证非导入站点不产生警告。
func TestImportWarnings_NilAndNonImported(t *testing.T) {
	svc := &Service{cfg: &app.Config{}}

	if got := svc.ImportWarnings(context.Background(), nil); got != nil {
		t.Fatalf("nil site 期望 nil，实际: %v", got)
	}

	// ConfigPath != EnabledPath 表示非导入站点。
	site := &repo.Site{ConfigPath: "/a/conf", EnabledPath: "/different/enabled"}
	if got := svc.ImportWarnings(context.Background(), site); got != nil {
		t.Fatalf("非导入站点期望 nil，实际: %v", got)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		if !seen[s] {
			return false
		}
	}
	return true
}
