// agent 包 — Nginx 相关 RPC handlers
//
// 实现以下 agent 内部接口：
//   - POST /internal/v1/nginx/detect      — 检测 Nginx
//   - POST /internal/v1/nginx/test        — 执行 nginx -t
//   - POST /internal/v1/nginx/dump        — 执行 nginx -T
//   - POST /internal/v1/nginx/reload      — 执行 reload
//   - POST /internal/v1/nginx/ensure-include — 安装 include 入口
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/nginx"
)

// ============================================================
// Nginx detect handler
// ============================================================

// NginxDetectRequest 检测请求
type NginxDetectRequest struct {
	NginxBin string `json:"nginx_bin"` // 为空时由 agent 自动查找
}

// handleNginxDetect 检测 Nginx 安装情况
//
// 步骤：
//  1. 查找或使用指定的 nginx 二进制
//  2. 获取版本和配置路径
//  3. 验证 nginx -t 通过
//  4. 返回检测信息
func (s *Server) handleNginxDetect(w http.ResponseWriter, r *http.Request) {
	var req NginxDetectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeouts.Detect)
	defer cancel()

	result, err := s.executor.Detect(ctx, req.NginxBin)
	if err != nil {
		slog.Error("Nginx 检测失败", "error", err)
		writeAgentError(w, http.StatusInternalServerError, "Nginx 检测失败: "+err.Error())
		return
	}

	slog.Info("Nginx 检测完成",
		"bin", result.Bin,
		"version", result.Version,
		"conf_path", result.ConfPath,
		"test_ok", result.TestOK,
		"web_user", result.WebUser,
		"web_group", result.WebGroup,
	)

	// 持久化 web_user / web_group。安装脚本旧版本可能误写 root:root，detect 后应按 nginx.conf 修正。
	if isValidWebUser(result.WebUser) {
		s.cfg.Nginx.WebUser = result.WebUser
		s.cfg.Nginx.WebGroup = result.WebGroup
	}
	if result.Version != "" {
		s.cfg.Nginx.Version = result.Version
	}
	s.persistConfig()

	writeAgentOK(w, result)
}

func isValidWebUser(user string) bool {
	user = strings.TrimSpace(user)
	return user != "" && user != "root"
}

// ============================================================
// Nginx test handler
// ============================================================

// handleNginxTest 执行 nginx -t 配置测试
func (s *Server) handleNginxTest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.timeouts.Test)
	defer cancel()

	result, err := s.executor.Test(ctx)
	if err != nil {
		slog.Error("nginx -t 失败", "error", err, "stderr", result.Stderr)
		writeAgentError(w, http.StatusInternalServerError, result.Stderr)
		return
	}

	writeAgentOK(w, map[string]any{
		"ok":     true,
		"stdout": result.Stdout,
		"stderr": result.Stderr,
	})
}

// ============================================================
// Nginx dump handler
// ============================================================

// handleNginxDump 执行 nginx -T 输出全部配置
//
// 用于旧站只读扫描，将 stdout（合并后的配置）返回给 API 层解析。
func (s *Server) handleNginxDump(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.timeouts.Dump)
	defer cancel()

	result, err := s.executor.Dump(ctx)
	if err != nil {
		slog.Error("nginx -T 执行失败", "error", err, "stderr", result.Stderr)
		writeAgentError(w, http.StatusInternalServerError, "nginx -T 执行失败: "+result.Stderr)
		return
	}

	writeAgentOK(w, map[string]any{
		"stdout": result.Stdout,
		"stderr": result.Stderr,
	})
}

// ============================================================
// Nginx reload handler
// ============================================================

// NginxReloadRequest reload 请求
type NginxReloadRequest struct {
	TestBeforeReload bool `json:"test_before_reload"` // 是否在 reload 前先执行 nginx -t
}

// handleNginxReload 执行 nginx -s reload
func (s *Server) handleNginxReload(w http.ResponseWriter, r *http.Request) {
	var req NginxReloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// 请求体为空时使用默认值
		req.TestBeforeReload = true
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeouts.Reload)
	defer cancel()

	// 如果要求先测试
	if req.TestBeforeReload {
		testResult, testErr := s.executor.Test(ctx)
		if testErr != nil {
			slog.Error("reload 前的 nginx -t 失败", "error", testErr, "stderr", testResult.Stderr)
			writeAgentError(w, http.StatusInternalServerError,
				fmt.Sprintf("nginx -t 失败: %s", testResult.Stderr))
			return
		}
	}

	// 执行 reload
	result, err := s.executor.Reload(ctx)
	if err != nil {
		if isNginxReloadPIDError(result, err) {
			slog.Warn("nginx reload 因 PID 无效失败，尝试启动 nginx", "error", err, "stderr", result.Stderr)
			startResult, startErr := s.executor.Start(ctx)
			if startErr == nil {
				slog.Info("nginx reload 失败但启动成功")
				writeAgentOK(w, map[string]any{
					"ok":     true,
					"action": "start",
				})
				return
			}

			slog.Error("nginx reload 失败且启动失败", "reload_error", err, "reload_stderr", result.Stderr, "start_error", startErr, "start_stderr", startResult.Stderr)
			startErrMsg := startResult.Stderr
			if startErrMsg == "" {
				startErrMsg = startErr.Error()
			}
			writeAgentError(w, http.StatusInternalServerError,
				fmt.Sprintf("nginx reload 失败: %s; nginx start 失败: %s", result.Stderr, startErrMsg))
			return
		}
		slog.Error("nginx reload 失败", "error", err, "stderr", result.Stderr)
		writeAgentError(w, http.StatusInternalServerError,
			fmt.Sprintf("nginx reload 失败: %s", result.Stderr))
		return
	}

	slog.Info("nginx reload 成功")
	writeAgentOK(w, map[string]any{
		"ok": true,
	})
}

// ============================================================
// Nginx ensure include handler
// ============================================================

// EnsureIncludeRequest 安装 include 入口的请求
type EnsureIncludeRequest struct {
	ConfirmModifyMainConf bool `json:"confirm_modify_main_conf"` // 修改主配置需要确认
}

// EnsureIncludeResponse 安装结果
type EnsureIncludeResponse struct {
	Installed bool   `json:"installed"`  // 是否已安装
	Changed   bool   `json:"changed"`    // 是否有变更（false 表示已经存在）
	EntryFile string `json:"entry_file"` // 入口文件路径
}

// handleNginxEnsureInclude 安装面板 include 入口
//
// 通过修改 nginx.conf 的 http {} 块插入 include 指令，
// 使 nginx 加载面板管理的站点配置（panel_dir/conf.d/ 和 panel_dir/sites-enabled/）。
func (s *Server) handleNginxEnsureInclude(w http.ResponseWriter, r *http.Request) {
	var req EnsureIncludeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	// 确保面板目录结构存在
	panelDir := s.cfg.Nginx.PanelDir
	if err := nginx.EnsurePanelDirectories(panelDir); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "创建面板目录失败: "+err.Error())
		return
	}

	confPath := s.executor.GetConfPath()

	// 检查 nginx.conf 是否已包含 include 入口
	if nginx.CheckIncludeInstalled(confPath) {
		if s.verifyIncludeLoadedByNginx() {
			if !s.cfg.Nginx.IncludeInstalled {
				s.cfg.Nginx.IncludeInstalled = true
				s.persistConfig()
			}
			writeAgentOK(w, EnsureIncludeResponse{
				Installed: true,
				Changed:   false,
				EntryFile: confPath,
			})
			return
		}
		slog.Warn("主配置中 marker 存在但 nginx -T 未验证通过，将重新安装",
			"conf_path", confPath)
	}

	// 需要确认才能修改主配置
	if !req.ConfirmModifyMainConf {
		writeAgentError(w, http.StatusForbidden,
			"需要修改主 nginx.conf，但未收到 confirm_modify_main_conf=true 确认")
		return
	}

	result, err := s.ensureIncludeViaMainConf(confPath)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.cfg.Nginx.IncludeInstalled = true
	s.persistConfig()
	writeAgentOK(w, result)
}

func (s *Server) verifyIncludeLoadedByNginx() bool {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeouts.Test)
	defer cancel()

	result, err := s.executor.Dump(ctx)
	if err != nil {
		slog.Warn("nginx -T 执行失败，无法验证 include 加载状态", "error", err)
		return false
	}

	dump := result.Stdout
	return strings.Contains(dump, nginx.MarkerIncludeStart)
}

// ensureIncludeViaMainConf 通过修改主配置安装 include 入口
func (s *Server) ensureIncludeViaMainConf(confPath string) (*EnsureIncludeResponse, error) {
	// 读取主配置
	data, err := os.ReadFile(confPath)
	if err != nil {
		return nil, fmt.Errorf("读取 nginx.conf 失败: %w", err)
	}
	originalContent := string(data)

	// 在 http 块中插入 include
	newContent, err := nginx.InsertIncludeInHTTPBlock(originalContent, s.cfg.Nginx.PanelDir)
	if err != nil {
		return nil, fmt.Errorf("在 http 块中插入 include 失败: %w", err)
	}

	// 备份原始配置
	backupDir := filepath.Join(s.cfg.Nginx.PanelDir, "backups", fmt.Sprintf("include_%d", time.Now().Unix()))
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return nil, fmt.Errorf("创建备份目录失败: %w", err)
	}
	backupPath := filepath.Join(backupDir, filepath.Base(confPath))
	if err := writeFileAtomic(backupPath, data, 0600); err != nil {
		return nil, fmt.Errorf("备份 nginx.conf 失败: %w", err)
	}

	// 写入新配置
	if err := writeFileAtomic(confPath, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("写入 nginx.conf 失败: %w", err)
	}

	// 执行 nginx -t 验证
	ctx, cancel := context.WithTimeout(context.Background(), s.timeouts.Test)
	defer cancel()
	if result, testErr := s.executor.Test(ctx); testErr != nil {
		// 测试失败，从备份恢复
		slog.Error("修改主配置后 nginx -t 失败，开始回滚", "stderr", result.Stderr)
		if restoreErr := writeFileAtomic(confPath, data, 0644); restoreErr != nil {
			slog.Error("回滚 nginx.conf 失败", "error", restoreErr)
		}
		return nil, fmt.Errorf("修改主配置后 nginx -t 失败（已回滚）: %s", result.Stderr)
	}

	slog.Info("面板 include 入口已通过修改主配置安装", "conf_path", confPath, "backup", backupPath)

	cleanupOldBackups(
		s.cfg.Nginx.PanelDir+"/backups",
		s.cfg.Nginx.BackupMaxCount,
		app.ParseDurationOrDefault(s.cfg.Nginx.BackupMaxAge, 168*time.Hour),
	)

	return &EnsureIncludeResponse{
		Installed: true,
		Changed:   true,
		EntryFile: confPath,
	}, nil
}

func (s *Server) handleNginxReopen(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.timeouts.Reopen)
	defer cancel()

	result, err := s.executor.Reopen(ctx)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "nginx -s reopen 失败: "+err.Error())
		return
	}
	writeAgentOK(w, map[string]any{
		"ok":     true,
		"stdout": result.Stdout,
		"stderr": result.Stderr,
	})
}

func (s *Server) handleConfigReload(w http.ResponseWriter, r *http.Request) {
	if err := s.ReloadConfig(); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "重载配置失败: "+err.Error())
		return
	}
	writeAgentOK(w, map[string]any{"ok": true})
}

type configWriteBackField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type configWriteBackRequest struct {
	Fields []configWriteBackField `json:"fields"`
}

var configWriteBackAllowlist = map[string]bool{
	"api.login_path":                     true,
	"api.public_health":                  true,
	"api.rate_limit.max_failures":        true,
	"api.rate_limit.window":              true,
	"api.max_sessions":                   true,
	"api.bind_session_ip":                true,
	"api.bind_session_ua":                true,
	"api.trusted_proxies":                true,
	"api.captcha.provider":               true,
	"api.captcha.site_key":               true,
	"api.captcha.secret_key":             true,
	"api.captcha.trigger_after_failures": true,
	"api.tls.enabled":                    true,
	"api.tls.cert":                       true,
	"api.tls.key":                        true,
	"api.tls.cert_validity":              true,
	"nginx.web_user":                     true,
	"nginx.web_group":                    true,
}

func (s *Server) handleConfigWriteBack(w http.ResponseWriter, r *http.Request) {
	var req configWriteBackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	// 先校验整批字段，再修改内存配置，避免前半批成功、后半批失败导致配置处于半更新状态。
	for _, f := range req.Fields {
		if !configWriteBackAllowlist[f.Key] {
			writeAgentError(w, http.StatusForbidden, "不允许回写的配置字段: "+f.Key)
			return
		}
		if err := validateConfigWriteBackField(f); err != nil {
			writeAgentError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	for _, f := range req.Fields {
		switch f.Key {
		case "api.login_path":
			s.cfg.API.LoginPath = f.Value
		case "api.public_health":
			s.cfg.API.PublicHealth = f.Value == "true" || f.Value == "1"
		case "api.rate_limit.max_failures":
			n, err := strconv.Atoi(f.Value)
			if err != nil {
				writeAgentError(w, http.StatusBadRequest, "max_failures 无效: "+f.Value)
				return
			}
			s.cfg.API.RateLimit.MaxFailures = n
		case "api.rate_limit.window":
			s.cfg.API.RateLimit.Window = f.Value
		case "api.max_sessions":
			n, err := strconv.Atoi(f.Value)
			if err != nil {
				writeAgentError(w, http.StatusBadRequest, "max_sessions 无效: "+f.Value)
				return
			}
			s.cfg.API.MaxSessions = n
		case "api.bind_session_ip":
			s.cfg.API.BindSessionIP = f.Value == "true" || f.Value == "1"
		case "api.bind_session_ua":
			s.cfg.API.BindSessionUA = f.Value == "true" || f.Value == "1"
		case "api.trusted_proxies":
			proxies := parseTrustedProxiesValue(f.Value)
			if len(proxies) == 0 {
				s.cfg.API.TrustedProxies = nil
			} else {
				s.cfg.API.TrustedProxies = proxies
			}
		case "api.captcha.provider":
			s.cfg.API.Captcha.Provider = f.Value
		case "api.captcha.site_key":
			s.cfg.API.Captcha.SiteKey = f.Value
		case "api.captcha.secret_key":
			s.cfg.API.Captcha.SecretKey = f.Value
		case "api.captcha.trigger_after_failures":
			n, err := strconv.Atoi(f.Value)
			if err != nil {
				writeAgentError(w, http.StatusBadRequest, "trigger_after_failures 无效: "+f.Value)
				return
			}
			s.cfg.API.Captcha.TriggerAfterFailures = n
		case "api.tls.enabled":
			s.cfg.API.TLS.Enabled = f.Value == "true" || f.Value == "1"
		case "api.tls.cert":
			s.cfg.API.TLS.Cert = f.Value
		case "api.tls.key":
			s.cfg.API.TLS.Key = f.Value
		case "api.tls.cert_validity":
			s.cfg.API.TLS.CertValidity = f.Value
		case "nginx.web_user":
			s.cfg.Nginx.WebUser = f.Value
		case "nginx.web_group":
			s.cfg.Nginx.WebGroup = f.Value
		}
	}

	if err := s.cfg.WriteBack(); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "写回配置文件失败: "+err.Error())
		return
	}

	slog.Info("配置已通过 RPC 回写", "fields", len(req.Fields))
	writeAgentOK(w, map[string]any{"ok": true})
}

func validateConfigWriteBackField(f configWriteBackField) error {
	switch f.Key {
	case "api.bind_session_ip", "api.bind_session_ua", "api.tls.enabled", "api.public_health":
		return validateBoolValue(f.Key, f.Value)
	case "api.login_path":
		return validateLoginPathValue(f.Value)
	case "api.rate_limit.window", "api.tls.cert_validity":
		return validatePositiveDuration(f.Key, f.Value)
	case "api.rate_limit.max_failures", "api.max_sessions":
		return validateIntRange(f.Key, f.Value, 1, 10000)
	case "api.captcha.trigger_after_failures":
		return validateIntRange(f.Key, f.Value, 0, 10000)
	case "nginx.web_user", "nginx.web_group":
		return validateWebUserValue(f.Key, f.Value)
	case "api.trusted_proxies":
		return validateTrustedProxies(f.Value)
	case "api.captcha.provider":
		return validateCaptchaProvider(f.Value)
	case "api.tls.cert", "api.tls.key":
		return validateTLSPath(f.Key, f.Value)
	case "api.captcha.site_key", "api.captcha.secret_key":
		return validatePlainSecretField(f.Key, f.Value)
	default:
		return fmt.Errorf("不允许回写的配置字段: %s", f.Key)
	}
}

func validateWebUserValue(key, value string) error {
	if !isValidWebUser(value) {
		return fmt.Errorf("%s 不能为 root 或空", key)
	}
	for _, r := range value {
		if !(r == '_' || r == '-' || r == '.' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return fmt.Errorf("%s 包含非法字符", key)
		}
	}
	return nil
}

func validateLoginPathValue(value string) error {
	if err := app.ValidateLoginPath(value); err != nil {
		return fmt.Errorf("api.login_path 无效: %w", err)
	}
	return nil
}

func validateBoolValue(key, value string) error {
	switch value {
	case "true", "false", "1", "0":
		return nil
	default:
		return fmt.Errorf("%s 必须是 true/false 或 1/0", key)
	}
}

func validatePositiveDuration(key, value string) error {
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return fmt.Errorf("%s 时长无效: %s", key, value)
	}
	return nil
}

func validateIntRange(key, value string, min, max int) error {
	n, err := strconv.Atoi(value)
	if err != nil || n < min || n > max {
		return fmt.Errorf("%s 必须在 %d 到 %d 之间", key, min, max)
	}
	return nil
}

func validateTrustedProxies(value string) error {
	for _, proxy := range parseTrustedProxiesValue(value) {
		if net.ParseIP(proxy) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(proxy); err == nil {
			continue
		}
		return fmt.Errorf("api.trusted_proxies 包含无效 IP/CIDR: %s", proxy)
	}
	return nil
}

func parseTrustedProxiesValue(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	proxies := make([]string, 0, len(parts))
	for _, part := range parts {
		proxy := strings.TrimSpace(part)
		if proxy != "" {
			proxies = append(proxies, proxy)
		}
	}
	return proxies
}

func validateCaptchaProvider(value string) error {
	switch value {
	case "none", "turnstile", "hcaptcha":
		return nil
	default:
		return fmt.Errorf("api.captcha.provider 无效，可选: none / turnstile / hcaptcha")
	}
}

func validateTLSPath(key, value string) error {
	if value == "" {
		return nil
	}
	if strings.ContainsAny(value, "\x00\n\r") {
		return fmt.Errorf("%s 包含非法字符", key)
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("%s 必须是绝对路径", key)
	}
	return nil
}

func validatePlainSecretField(key, value string) error {
	// 这些字段会直接写入 YAML，禁止换行和空字节，避免破坏配置结构或污染日志。
	if strings.ContainsAny(value, "\x00\n\r") {
		return fmt.Errorf("%s 包含非法字符", key)
	}
	return nil
}
