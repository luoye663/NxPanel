// agent 包 — RPC handlers
//
// 实现 agent 的内部 RPC 接口。
package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
)

// ============================================================
// Agent 响应类型
// ============================================================

// AgentResponse agent 的统一响应格式
// 与 API 的统一响应格式不同，agent 使用更简单的格式
type AgentResponse struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// TransactionRequest 文件事务请求体
type TransactionRequest struct {
	OperationID    string       `json:"operation_id"`    // 操作 ID
	Changes        []FileChange `json:"changes"`         // 文件变更列表
	TestNginx      bool         `json:"test_nginx"`      // 是否在写入后执行 nginx -t
	ReloadNginx    bool         `json:"reload_nginx"`    // 是否在写入后执行 nginx -s reload
	TimeoutSeconds int          `json:"timeout_seconds"` // 超时时间（秒）
}

// TransactionResponse 文件事务响应
type TransactionResponse struct {
	Backups []BackupRecord `json:"backups"` // 备份记录
}

// handleHealth agent 健康检查
//
// 返回：
//
//	{"ok":true,"data":{"status":"ok","service":"nxpanel-agent","version":"0.1.0-dev"}}
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeAgentOK(w, map[string]any{
		"status":  "ok",
		"service": "nxpanel-agent",
		"version": app.Version,
	})
}

// handleTransactionApply 处理文件事务请求
//
// 流程：
//  1. 解析请求体
//  2. 解码 base64 编码的文件内容
//  3. 创建事务并执行
//  4. 返回备份记录
func (s *Server) handleTransactionApply(w http.ResponseWriter, r *http.Request) {
	var req TransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	// 参数校验
	if req.OperationID == "" {
		writeAgentError(w, http.StatusBadRequest, "operation_id 不能为空")
		return
	}
	if len(req.Changes) == 0 {
		writeAgentError(w, http.StatusBadRequest, "changes 不能为空")
		return
	}

	// 解码 base64 编码的文件内容
	for i := range req.Changes {
		if req.Changes[i].ContentBase64 != "" {
			decoded, err := base64.StdEncoding.DecodeString(req.Changes[i].ContentBase64)
			if err != nil {
				writeAgentError(w, http.StatusBadRequest,
					fmt.Sprintf("changes[%d].content_base64 解码失败: %v", i, err))
				return
			}
			req.Changes[i].Content = decoded
		}
	}

	// 设置超时
	timeout := 15 * time.Second
	if req.TimeoutSeconds > 0 {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	// 创建并执行事务
	tx := NewTransaction(req.OperationID, s.cfg.Nginx.PanelDir+"/backups", s.policy,
		s.cfg.Nginx.WebUser, s.cfg.Nginx.WebGroup)
	if err := tx.Apply(ctx, req.Changes); err != nil {
		slog.Error("文件事务执行失败", "operation_id", req.OperationID, "error", err)
		writeAgentError(w, http.StatusInternalServerError, "事务执行失败: "+err.Error())
		return
	}

	slog.Info("文件事务执行成功", "operation_id", req.OperationID, "changes", len(req.Changes))

	// 如果请求要求 nginx -t，执行配置测试。
	if req.TestNginx {
		testResult, testErr := s.executor.Test(ctx)
		if testErr != nil {
			errMsg := testResult.Stderr
			if errMsg == "" {
				errMsg = testErr.Error()
			}
			slog.Error("事务后 nginx -t 失败，开始回滚",
				"operation_id", req.OperationID,
				"stderr", testResult.Stderr,
				"error", testErr.Error())
			_ = tx.Rollback(ctx)
			writeAgentError(w, http.StatusInternalServerError,
				fmt.Sprintf("nginx -t 失败（已回滚）: %s", errMsg))
			return
		}
	}

	// 如果请求要求 nginx reload，执行重新加载。
	if req.ReloadNginx {
		reloadResult, reloadErr := s.executor.Reload(ctx)
		if reloadErr != nil {
			if isNginxReloadPIDError(reloadResult, reloadErr) {
				slog.Warn("事务后 nginx reload 因 PID 无效失败，尝试启动 nginx",
					"operation_id", req.OperationID,
					"stderr", reloadResult.Stderr,
					"error", reloadErr.Error())
				startResult, startErr := s.executor.Start(ctx)
				if startErr == nil {
					slog.Info("事务后 nginx reload 失败但启动成功", "operation_id", req.OperationID)
					writeAgentOK(w, TransactionResponse{
						Backups: tx.Backups,
					})
					go cleanupOldBackups(
						s.cfg.Nginx.PanelDir+"/backups",
						s.cfg.Nginx.BackupMaxCount,
						app.ParseDurationOrDefault(s.cfg.Nginx.BackupMaxAge, 168*time.Hour),
					)
					return
				}

				slog.Error("事务后 nginx reload 失败且启动失败，开始回滚",
					"operation_id", req.OperationID,
					"reload_stderr", reloadResult.Stderr,
					"reload_error", reloadErr.Error(),
					"start_stderr", startResult.Stderr,
					"start_error", startErr.Error())
				errMsg := startResult.Stderr
				if errMsg == "" {
					errMsg = startErr.Error()
				}
				_ = tx.Rollback(ctx)
				writeAgentError(w, http.StatusInternalServerError,
					fmt.Sprintf("nginx reload 失败且 start 失败（已回滚）: %s", errMsg))
				return
			}
			errMsg := reloadResult.Stderr
			if errMsg == "" {
				errMsg = reloadErr.Error()
			}
			slog.Error("事务后 nginx reload 失败，开始回滚",
				"operation_id", req.OperationID,
				"stderr", reloadResult.Stderr,
				"error", reloadErr.Error())
			_ = tx.Rollback(ctx)
			writeAgentError(w, http.StatusInternalServerError,
				fmt.Sprintf("nginx reload 失败（已回滚）: %s", errMsg))
			return
		}
	}

	writeAgentOK(w, TransactionResponse{
		Backups: tx.Backups,
	})

	go cleanupOldBackups(
		s.cfg.Nginx.PanelDir+"/backups",
		s.cfg.Nginx.BackupMaxCount,
		app.ParseDurationOrDefault(s.cfg.Nginx.BackupMaxAge, 168*time.Hour),
	)
}

// ============================================================
// 响应写入工具
// ============================================================

// writeAgentOK 写入成功响应
func writeAgentOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(AgentResponse{
		OK:   true,
		Data: data,
	})
}

// writeAgentError 写入错误响应
func writeAgentError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(AgentResponse{
		OK:    false,
		Error: msg,
	})
}
