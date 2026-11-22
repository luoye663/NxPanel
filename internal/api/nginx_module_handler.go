package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginxmodule"
)

const nginxModuleInstallAction = "nginx.module.install"
const nginxModuleUninstallAction = "nginx.module.uninstall"
const nginxModuleTargetType = "nginx_module"
const nginxModuleUnconfirmed = "NGINX_MODULE_UNCONFIRMED"

type nginxModuleJob struct {
	ID           string `json:"id"`
	ModuleID     string `json:"module_id"`
	Action       string `json:"action"`
	State        string `json:"state"`
	Message      string `json:"message,omitempty"`
	ScriptDetail string `json:"script_detail,omitempty"`
}

func moduleJobFromOperation(op *repo.Operation) nginxModuleJob {
	job := nginxModuleJob{ID: op.ID, ModuleID: op.TargetID, Action: "install", State: op.Status, Message: op.ErrorMessage, ScriptDetail: op.Stderr}
	if op.Action == nginxModuleUninstallAction {
		job.Action = "uninstall"
	}
	if op.ErrorCode == nginxModuleUnconfirmed {
		job.State = "needs_attention"
	}
	return job
}

func (s *Server) handleNginxModules(w http.ResponseWriter, r *http.Request) {
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "Agent 不可用", nil)
		return
	}
	listing, err := s.agentClient.NginxModuleList(r.Context(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "无法获取模块状态，请确认 Agent 已升级且运行正常", nil)
		return
	}
	var latest *nginxModuleJob
	if operation, err := s.opRepo.LatestByActions(r.Context(), nginxModuleTargetType, nginxModuleInstallAction, nginxModuleUninstallAction); err == nil && operation != nil {
		job := moduleJobFromOperation(operation)
		latest = &job
	}
	WriteOK(w, r, struct {
		nginxmodule.Listing
		Latest *nginxModuleJob `json:"latest_job,omitempty"`
	}{listing, latest})
}

func (s *Server) handleNginxModuleInstall(w http.ResponseWriter, r *http.Request) {
	s.handleNginxModuleChange(w, r, false)
}

func (s *Server) handleNginxModuleReleases(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 || page > nginxmodule.MaxReleasePage || !nginxmodule.ValidID(id) {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "无效的模块或分页参数", nil)
		return
	}
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "Agent 不可用", nil)
		return
	}
	data, err := s.agentClient.NginxModuleReleases(r.Context(), id, page)
	writeModuleLookup(w, r, data, err)
}

func (s *Server) handleNginxModulePreflight(w http.ResponseWriter, r *http.Request) {
	id, tag := r.URL.Query().Get("id"), r.URL.Query().Get("tag")
	if !nginxmodule.ValidID(id) || !nginxmodule.ValidTag(tag) {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "无效的模块或 Release tag", nil)
		return
	}
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "Agent 不可用", nil)
		return
	}
	data, err := s.agentClient.NginxModulePreflight(r.Context(), id, tag)
	writeModuleLookup(w, r, data, err)
}

func writeModuleLookup(w http.ResponseWriter, r *http.Request, data any, err error) {
	if err == nil {
		WriteOK(w, r, data)
		return
	}
	var response *agentclient.HTTPError
	if errors.As(err, &response) && (response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusBadGateway) {
		WriteError(w, r, response.StatusCode, "NGINX_MODULE_LOOKUP_FAILED", response.Message, nil)
		return
	}
	WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "无法查询模块版本，请确认 Agent 和 GitHub 连接正常", nil)
}

func (s *Server) handleNginxModuleUninstall(w http.ResponseWriter, r *http.Request) {
	s.handleNginxModuleChange(w, r, true)
}

func (s *Server) handleNginxModuleChange(w http.ResponseWriter, r *http.Request, uninstall bool) {
	var request nginxmodule.InstallRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if !DecodeJSON(w, r, &request) {
		return
	}
	if !request.Valid(uninstall) {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "无效的模块版本或操作确认", nil)
		return
	}
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "Agent 不可用", nil)
		return
	}
	s.updateMu.Lock()
	if s.updatePausing || s.Context().Err() != nil || s.nginxModuleInstalling {
		s.updateMu.Unlock()
		WriteError(w, r, http.StatusConflict, app.ErrBadRequest, "已有安装任务或面板正在更新，请稍后重试", nil)
		return
	}
	s.nginxModuleInstalling = true
	s.updateWrites++
	s.backgroundWG.Add(1)
	s.updateMu.Unlock()
	finish := func() {
		s.updateMu.Lock()
		s.nginxModuleInstalling = false
		s.updateWrites--
		s.updateMu.Unlock()
		s.backgroundWG.Done()
	}
	opID := app.NewOperationID()
	action, message, resultAction := nginxModuleInstallAction, "安装并启用 Nginx 模块", "install"
	if uninstall {
		action, message, resultAction = nginxModuleUninstallAction, "卸载 Nginx 模块", "uninstall"
	} else {
		message += "（Release " + request.Tag + "）"
	}
	if err := s.opRepo.CreateContext(r.Context(), &repo.Operation{ID: opID, Action: action,
		TargetType: nginxModuleTargetType, TargetID: request.ID, Status: "pending", Actor: "admin",
		RequestID: middleware.GetRequestID(r.Context()), Message: message, CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		finish()
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, "未建立安装审计记录，未执行安装", nil)
		return
	}
	go func() {
		defer finish()
		var result nginxmodule.Result
		var err error
		switch {
		case !uninstall:
			result, err = s.agentClient.InstallNginxModule(s.Context(), request)
		case request.ID != nginxmodule.GeoIP2:
			result, err = s.agentClient.UninstallNginxModule(s.Context(), request)
		case s.geoAccessSvc != nil && s.accessPolicySvc != nil:
			result, err = s.geoAccessSvc.WithModuleRemoval(s.accessPolicySvc.CheckGeoIP2Removal, func() (nginxmodule.Result, error) {
				return s.agentClient.UninstallNginxModule(s.Context(), request)
			})
		default:
			result = nginxmodule.Result{State: "failed", Message: "无法核验地域访问依赖，未卸载"}
		}
		saveCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		detail := ""
		if result.ExitCode != nil && nginxmodule.ValidDigest(result.ScriptSHA256) {
			detail = fmt.Sprintf("脚本版本 %s，SHA-256 %s，退出状态 %d", result.Version, result.ScriptSHA256, *result.ExitCode)
		}
		switch {
		case err != nil:
			slog.Warn("模块操作未收到 Agent 确认", "operation_id", opID, "error", err)
			err = s.opRepo.UpdateErrorContext(saveCtx, opID, "failed", nginxModuleUnconfirmed, "未收到 Agent 操作确认，请刷新模块状态并检查 Nginx", "")
		case result.State == "success":
			err = s.opRepo.UpdateErrorContext(saveCtx, opID, "success", "", "", detail)
		case result.State == "needs_attention":
			err = s.opRepo.UpdateErrorContext(saveCtx, opID, "failed", nginxModuleUnconfirmed, result.Message, detail)
		default:
			err = s.opRepo.UpdateErrorContext(saveCtx, opID, "failed", "NGINX_MODULE_ACTION_FAILED", result.Message, detail)
		}
		if err != nil {
			slog.Error("写入模块安装结果失败", "operation_id", opID, "error", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, Response{RequestID: middleware.GetRequestID(r.Context()), Success: true, Data: nginxModuleJob{ID: opID, ModuleID: request.ID, Action: resultAction, State: "pending"}})
}

func (s *Server) handleNginxModuleJob(w http.ResponseWriter, r *http.Request) {
	op, err := s.opRepo.GetByID(chi.URLParam(r, "operation_id"))
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, "无法查询模块安装任务", nil)
		return
	}
	if op == nil || op.TargetType != nginxModuleTargetType || op.Action != nginxModuleInstallAction && op.Action != nginxModuleUninstallAction {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "模块安装任务不存在", nil)
		return
	}
	WriteOK(w, r, moduleJobFromOperation(op))
}
