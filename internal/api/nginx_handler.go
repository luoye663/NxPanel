// api 包 — nginx handler
//
// 处理 Nginx 管理接口：
//   - POST /api/v1/nginx/detect           检测 Nginx
//   - POST /api/v1/nginx/test             测试 Nginx 配置
//   - POST /api/v1/nginx/reload           reload Nginx
//   - POST /api/v1/nginx/include/ensure   安装 include 入口
//
// 所有操作都会写入 operations 表做审计记录。
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginxconf"
)

// ============================================================
// Nginx detect
// ============================================================

// nginxDetectRequest 检测请求体。
// API 层不再接收 nginx_bin，避免 Web 请求指定 root Agent 要执行的二进制路径。
type nginxDetectRequest struct{}

// handleNginxDetect 检测 Nginx
//
// 流程：
//  1. 创建操作记录
//  2. 调用 agent 检测
//  3. 将检测结果保存到 settings 表
//  4. 更新操作记录状态
func (s *Server) handleNginxDetect(w http.ResponseWriter, r *http.Request) {
	var req nginxDetectRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	// 检查 agent 是否可用
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable,
			"Agent 不可用", nil)
		return
	}

	// 创建操作记录
	opID := app.NewOperationID()
	op := &repo.Operation{
		ID:         opID,
		Action:     "nginx.detect",
		TargetType: "system",
		TargetID:   "nginx",
		Status:     "pending",
		RequestID:  middleware.GetRequestID(r.Context()),
		Actor:      "admin",
		Message:    "检测 Nginx",
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.opRepo.Create(op); err != nil {
		slog.Error("创建操作记录失败", "error", err)
	}

	// 调用 agent 检测
	result, err := s.agentClient.DetectNginx(r.Context(), &agentclient.NginxDetectRequest{
		NginxBin: "",
	})
	if err != nil {
		slog.Error("Nginx 检测失败", "error", err)
		_ = s.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, err.Error(), "")
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable,
			"Nginx 检测失败: "+err.Error(), nil)
		return
	}

	s.cfg.Nginx.Bin = result.Bin
	s.cfg.Nginx.Version = result.Version
	s.cfg.Nginx.ConfPath = result.ConfPath

	_ = s.opRepo.UpdateStatus(opID, "success")

	WriteOK(w, r, map[string]any{
		"bin":       result.Bin,
		"version":   result.Version,
		"conf_path": result.ConfPath,
		"prefix":    result.Prefix,
		"test_ok":   result.TestOK,
		"stderr":    result.Stderr,
	})
}

// ============================================================
// Nginx test
// ============================================================

// handleNginxTest 测试 Nginx 配置
func (s *Server) handleNginxTest(w http.ResponseWriter, r *http.Request) {
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable,
			"Agent 不可用", nil)
		return
	}

	result, err := s.agentClient.TestNginx(r.Context())
	if err != nil {
		slog.Error("nginx -t 失败", "error", err)
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrNginxTestFailed,
			"Nginx 配置测试失败", map[string]any{
				"stderr": err.Error(),
			})
		return
	}

	WriteOK(w, r, map[string]any{
		"ok":     result.OK,
		"stdout": result.Stdout,
		"stderr": result.Stderr,
	})
}

// ============================================================
// Nginx reload
// ============================================================

// nginxReloadRequest reload 请求体
type nginxReloadRequest struct {
	TestBeforeReload bool `json:"test_before_reload"` // 是否在 reload 前先测试
}

// handleNginxReload reload Nginx
func (s *Server) handleNginxReload(w http.ResponseWriter, r *http.Request) {
	var req nginxReloadRequest
	req.TestBeforeReload = true
	if !DecodeJSONOptional(w, r, &req) {
		return
	}

	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable,
			"Agent 不可用", nil)
		return
	}

	// 创建操作记录
	opID := app.NewOperationID()
	op := &repo.Operation{
		ID:         opID,
		Action:     "nginx.reload",
		TargetType: "system",
		TargetID:   "nginx",
		Status:     "pending",
		RequestID:  middleware.GetRequestID(r.Context()),
		Actor:      "admin",
		Message:    "Reload Nginx",
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.opRepo.Create(op); err != nil {
		slog.Error("创建操作记录失败", "error", err)
	}

	result, err := s.agentClient.ReloadNginx(r.Context(), &agentclient.NginxReloadRequest{
		TestBeforeReload: req.TestBeforeReload,
	})
	if err != nil {
		slog.Error("nginx reload 失败", "error", err)
		_ = s.opRepo.UpdateError(opID, "failed", app.ErrNginxReloadFailed, err.Error(), "")
		WriteError(w, r, http.StatusInternalServerError, app.ErrNginxReloadFailed,
			"Nginx reload 失败", map[string]any{
				"stderr": err.Error(),
			})
		return
	}

	_ = s.opRepo.UpdateStatus(opID, "success")

	WriteOK(w, r, map[string]any{
		"ok":           result.OK,
		"operation_id": opID,
	})
}

// ============================================================
// Nginx include ensure
// ============================================================

// nginxIncludeEnsureRequest 安装 include 入口请求体
type nginxIncludeEnsureRequest struct {
	ConfirmModifyMainConf bool `json:"confirm_modify_main_conf"` // 是否确认修改主配置
}

// handleNginxIncludeEnsure 安装面板 include 入口
func (s *Server) handleNginxIncludeEnsure(w http.ResponseWriter, r *http.Request) {
	var req nginxIncludeEnsureRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable,
			"Agent 不可用", nil)
		return
	}

	// 创建操作记录
	opID := app.NewOperationID()
	op := &repo.Operation{
		ID:         opID,
		Action:     "nginx.ensure_include",
		TargetType: "system",
		TargetID:   "nginx_include",
		Status:     "pending",
		RequestID:  middleware.GetRequestID(r.Context()),
		Actor:      "admin",
		Message:    "安装面板 include 入口",
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.opRepo.Create(op); err != nil {
		slog.Error("创建操作记录失败", "error", err)
	}

	result, err := s.agentClient.EnsureInclude(r.Context(), &agentclient.EnsureIncludeRequest{
		ConfirmModifyMainConf: req.ConfirmModifyMainConf,
	})
	if err != nil {
		slog.Error("安装 include 失败", "error", err)

		// 判断错误类型
		errCode := app.ErrInternalError
		httpStatus := http.StatusInternalServerError
		if isAgentDenied(err) {
			errCode = app.ErrAgentDenied
			httpStatus = http.StatusForbidden
		}
		_ = s.opRepo.UpdateError(opID, "failed", errCode, err.Error(), "")
		WriteError(w, r, httpStatus, errCode, err.Error(), nil)
		return
	}

	s.cfg.Nginx.IncludeInstalled = true
	_ = s.opRepo.UpdateStatus(opID, "success")

	WriteOK(w, r, map[string]any{
		"installed":    result.Installed,
		"changed":      result.Changed,
		"entry_file":   result.EntryFile,
		"operation_id": opID,
	})
}

// isAgentDenied 判断是否是 agent 拒绝操作（如需要确认但未确认）
func isAgentDenied(err error) bool {
	if err == nil {
		return false
	}
	return true
}

// ============================================================
// Nginx.conf 编辑
// ============================================================

func (s *Server) handleNginxConfGet(w http.ResponseWriter, r *http.Request) {
	if s.nginxconfSvc == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "服务不可用", nil)
		return
	}

	result, err := s.nginxconfSvc.GetNginxConf(r.Context())
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

func (s *Server) handleNginxConfSave(w http.ResponseWriter, r *http.Request) {
	if s.nginxconfSvc == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "服务不可用", nil)
		return
	}

	var req nginxconf.SaveNginxConfRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, err := s.nginxconfSvc.SaveNginxConf(r.Context(), &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

// ============================================================
// Nginx 常用参数管理
// ============================================================

func (s *Server) handleNginxParametersGet(w http.ResponseWriter, r *http.Request) {
	if s.nginxconfSvc == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "服务不可用", nil)
		return
	}

	result, err := s.nginxconfSvc.GetNginxParameters(r.Context())
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

func (s *Server) handleNginxParametersSave(w http.ResponseWriter, r *http.Request) {
	if s.nginxconfSvc == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "服务不可用", nil)
		return
	}

	var req nginxconf.SaveNginxParametersRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, err := s.nginxconfSvc.SaveNginxParameters(r.Context(), &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}
