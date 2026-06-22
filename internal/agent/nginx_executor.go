// agent 包 — Nginx 命令执行器
//
// NginxExecutor 负责执行 nginx 相关命令（test、reload、reopen、version）。
// 使用 exec.CommandContext 执行命令，不使用 shell 拼接，支持超时。
//
// 安全要求：
//   - 禁止 sh -c 拼接用户输入
//   - nginx_bin 必须是绝对路径
//   - 命令参数固定，不允许用户提交额外参数
package agent

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/luoye663/nxpanel/internal/nginx"
)

// CmdResult 保存命令执行的输出
type CmdResult struct {
	Stdout string `json:"stdout"` // 标准输出
	Stderr string `json:"stderr"` // 标准错误输出
}

// NginxTimeouts nginx 命令超时配置
type NginxTimeouts struct {
	Test   time.Duration
	Reload time.Duration
	Dump   time.Duration
	Reopen time.Duration
	Detect time.Duration
}

// DefaultNginxTimeouts 返回默认超时配置
func DefaultNginxTimeouts() NginxTimeouts {
	return NginxTimeouts{
		Test:   15 * time.Second,
		Reload: 15 * time.Second,
		Dump:   30 * time.Second,
		Reopen: 15 * time.Second,
		Detect: 10 * time.Second,
	}
}

// NginxExecutor 执行 nginx 相关命令
//
// 字段说明：
//   - Bin: nginx 二进制路径（绝对路径）
//   - ConfPath: nginx 主配置文件路径
//
// Bin 和 ConfPath 在 Detect 后会被更新，因此需要用互斥锁保护。
type NginxExecutor struct {
	mu       sync.Mutex
	bin      string
	confPath string
	timeouts NginxTimeouts
}

// NewNginxExecutor 创建 Nginx 执行器
func NewNginxExecutor(bin, confPath string, timeouts NginxTimeouts) *NginxExecutor {
	return &NginxExecutor{
		bin:      bin,
		confPath: confPath,
		timeouts: timeouts,
	}
}

// NewNginxExecutorWithDefaults 创建使用默认超时的 Nginx 执行器（测试用）
func NewNginxExecutorWithDefaults(bin, confPath string) *NginxExecutor {
	return NewNginxExecutor(bin, confPath, DefaultNginxTimeouts())
}

// GetBin 线程安全地获取当前 nginx 二进制路径
func (e *NginxExecutor) GetBin() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.bin
}

// GetConfPath 线程安全地获取当前 nginx 配置路径
func (e *NginxExecutor) GetConfPath() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.confPath
}

// Update 更新 nginx 二进制和配置路径（线程安全）
// 通常在 detect 成功后调用
func (e *NginxExecutor) Update(bin, confPath string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.bin = bin
	e.confPath = confPath
	slog.Info("Nginx 执行器已更新", "bin", bin, "conf_path", confPath)
}

// Test 执行 nginx -t 测试配置
//
// 默认超时为 15 秒。
// 命令：nginx -t -c <conf_path>（confPath 为空时不传 -c，使用编译时默认路径）
func (e *NginxExecutor) Test(ctx context.Context) (CmdResult, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeouts.Test)
	defer cancel()

	e.mu.Lock()
	bin := e.bin
	confPath := e.confPath
	e.mu.Unlock()

	if bin == "" {
		return CmdResult{}, fmt.Errorf("nginx 二进制路径未设置，请先执行 detect")
	}

	if confPath != "" {
		return e.run(ctx, bin, "-t", "-c", confPath)
	}
	return e.run(ctx, bin, "-t")
}

// Reload 执行 nginx -s reload 重新加载配置
//
// 默认超时为 15 秒。
// 命令：nginx -c <conf_path> -s reload（confPath 为空时不传 -c，使用编译时默认路径）
func (e *NginxExecutor) Reload(ctx context.Context) (CmdResult, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeouts.Reload)
	defer cancel()

	e.mu.Lock()
	bin := e.bin
	confPath := e.confPath
	e.mu.Unlock()

	if bin == "" {
		return CmdResult{}, fmt.Errorf("nginx 二进制路径未设置，请先执行 detect")
	}

	if confPath != "" {
		return e.run(ctx, bin, "-c", confPath, "-s", "reload")
	}
	return e.run(ctx, bin, "-s", "reload")
}

// Start 启动 nginx。用于 reload 因 PID 文件无效或 nginx 未运行失败时的受限回退。
func (e *NginxExecutor) Start(ctx context.Context) (CmdResult, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeouts.Reload)
	defer cancel()

	e.mu.Lock()
	bin := e.bin
	confPath := e.confPath
	e.mu.Unlock()

	if bin == "" {
		return CmdResult{}, fmt.Errorf("nginx 二进制路径未设置，请先执行 detect")
	}

	if confPath != "" {
		return e.run(ctx, bin, "-c", confPath)
	}
	return e.run(ctx, bin)
}

// Dump 执行 nginx -T 输出全部配置（用于旧站扫描）
//
// 超时：30 秒（配置文件较多时可能需要更长时间）
// 命令：nginx -T -c <conf_path>（confPath 为空时不传 -c，使用编译时默认路径）
func (e *NginxExecutor) Dump(ctx context.Context) (CmdResult, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeouts.Dump)
	defer cancel()

	e.mu.Lock()
	bin := e.bin
	confPath := e.confPath
	e.mu.Unlock()

	if bin == "" {
		return CmdResult{}, fmt.Errorf("nginx 二进制路径未设置，请先执行 detect")
	}

	if confPath != "" {
		return e.run(ctx, bin, "-T", "-c", confPath)
	}
	return e.run(ctx, bin, "-T")
}

// Reopen 执行 nginx -s reopen 重新打开日志文件
//
// 超时：15 秒
func (e *NginxExecutor) Reopen(ctx context.Context) (CmdResult, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeouts.Reopen)
	defer cancel()

	e.mu.Lock()
	bin := e.bin
	confPath := e.confPath
	e.mu.Unlock()

	if bin == "" {
		return CmdResult{}, fmt.Errorf("nginx 二进制路径未设置，请先执行 detect")
	}

	if confPath != "" {
		return e.run(ctx, bin, "-c", confPath, "-s", "reopen")
	}
	return e.run(ctx, bin, "-s", "reopen")
}

func isNginxReloadPIDError(result CmdResult, err error) bool {
	msg := result.Stderr
	if err != nil {
		msg += "\n" + err.Error()
	}
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "invalid pid number") ||
		strings.Contains(msg, "open()") && strings.Contains(msg, "nginx.pid") && strings.Contains(msg, "failed") ||
		strings.Contains(msg, "no such process")
}

// DetectResult 保存检测到的 Nginx 信息
type DetectResult struct {
	Bin      string `json:"bin"`       // 二进制路径
	Version  string `json:"version"`   // 版本号（如 "nginx/1.24.0"）
	ConfPath string `json:"conf_path"` // 主配置文件路径
	Prefix   string `json:"prefix"`    // Nginx prefix 路径
	TestOK   bool   `json:"test_ok"`   // nginx -t 是否通过
	Stderr   string `json:"stderr"`    // nginx -t 的 stderr 输出
	WebUser  string `json:"web_user"`  // Nginx 运行用户
	WebGroup string `json:"web_group"` // Nginx 运行组
}

// Detect 检测 Nginx 安装情况
//
// 步骤：
//  1. 如果提供了 nginxBin，使用它；否则自动搜索
//  2. 执行 nginx -V 获取版本和配置参数
//  3. 执行 nginx -t 验证配置
//  4. 更新执行器内部的 bin 和 confPath
func (e *NginxExecutor) Detect(ctx context.Context, nginxBin string) (*DetectResult, error) {
	// 查找 nginx 二进制
	bin := nginxBin
	if bin == "" {
		bin = nginx.FindNginxBin()
	}
	if bin == "" {
		return nil, fmt.Errorf("未找到 Nginx 二进制，请手动指定路径")
	}

	// 安全校验：必须是绝对路径，不能包含特殊字符
	cleanBin := filepath.Clean(bin)
	if !filepath.IsAbs(cleanBin) {
		return nil, fmt.Errorf("nginx 二进制路径必须是绝对路径: %s", bin)
	}
	if strings.ContainsAny(cleanBin, " \t\n\r;{}$`|&<>") {
		return nil, fmt.Errorf("nginx 二进制路径包含非法字符: %s", bin)
	}
	bin = cleanBin

	// 执行 nginx -V 获取版本和配置参数
	// 注意：nginx -V 的输出在 stderr 中
	ctxV, cancelV := context.WithTimeout(ctx, e.timeouts.Detect)
	defer cancelV()
	vResult, err := e.run(ctxV, bin, "-V")
	if err != nil {
		return nil, fmt.Errorf("执行 nginx -V 失败: %w", err)
	}

	// 解析版本号
	version := nginx.ParseVersion(vResult.Stderr)
	if version == "" {
		// 某些版本可能输出到 stdout
		version = nginx.ParseVersion(vResult.Stdout)
	}

	// 解析配置路径
	confPath := nginx.ParseConfigurePath(vResult.Stderr)
	if confPath == "" {
		confPath = nginx.ParseConfigurePath(vResult.Stdout)
	}

	if confPath == "" {
		ctxT, cancelT := context.WithTimeout(ctx, e.timeouts.Detect)
		tResult, _ := e.run(ctxT, bin, "-t")
		cancelT()
		confPath = nginx.ParseConfPathFromTestOutput(tResult.Stderr)
		if confPath == "" {
			confPath = nginx.ParseConfPathFromTestOutput(tResult.Stdout)
		}
	}

	prefix := nginx.ParsePrefix(vResult.Stderr)
	if prefix == "" {
		prefix = nginx.ParsePrefix(vResult.Stdout)
	}

	oldConfPath := e.GetConfPath()
	e.Update(bin, confPath)

	// 解析 web_user / web_group
	webUser, webGroup := e.detectWebUser(confPath)

	// 执行 nginx -t 验证
	testResult, testErr := e.Test(ctx)
	testOK := testErr == nil

	if !testOK {
		slog.Warn("Nginx 配置测试失败", "error", testErr, "stderr", testResult.Stderr)
		e.Update(bin, oldConfPath)
	}

	return &DetectResult{
		Bin:      bin,
		Version:  version,
		ConfPath: confPath,
		Prefix:   prefix,
		TestOK:   testOK,
		Stderr:   testResult.Stderr,
		WebUser:  webUser,
		WebGroup: webGroup,
	}, nil
}

// detectWebUser 从 nginx.conf 解析 web_user，找不到则尝试常见默认用户
func (e *NginxExecutor) detectWebUser(confPath string) (user, group string) {
	data, err := os.ReadFile(confPath)
	if err != nil {
		slog.Warn("读取 nginx.conf 解析 web_user 失败", "path", confPath, "error", err)
		user, group = resolveDefaultWebUser()
		return user, group
	}

	user, group = nginx.ParseWebUser(string(data))
	if user == "" {
		slog.Warn("nginx.conf 中未找到 user 指令，尝试默认用户")
		user, group = resolveDefaultWebUser()
	} else {
		slog.Info("从 nginx.conf 解析到 web_user", "user", user, "group", group)
	}
	return user, group
}

// resolveDefaultWebUser 遍历常见 web 用户，返回第一个系统中存在的
func resolveDefaultWebUser() (string, string) {
	for _, name := range nginx.DefaultWebUserOptions {
		if _, err := user.Lookup(name); err == nil {
			slog.Info("使用默认 web 用户", "user", name)
			return name, name
		}
	}
	slog.Warn("未找到任何常见 web 用户，使用 nobody")
	return "nobody", "nobody"
}

// run 执行 nginx 命令
//
// 安全措施：
//   - 使用 exec.CommandContext，不使用 shell
//   - 参数固定，不接受用户输入的参数
//   - 有超时保护
func (e *NginxExecutor) run(ctx context.Context, bin string, args ...string) (CmdResult, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	err := cmd.Run()
	result := CmdResult{
		Stdout: out.String(),
		Stderr: errb.String(),
	}

	if err != nil {
		return result, fmt.Errorf("命令执行失败 %s %v: %w", bin, args, err)
	}

	return result, nil
}
