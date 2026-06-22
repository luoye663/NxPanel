// api 包 — system handler
//
// 处理系统概览接口：
//   - GET /api/v1/system/overview  系统概览
//
// 返回 API、Agent、Nginx 的综合状态信息。
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
)

// handleSystemOverview 系统概览
//
// 返回三个部分的信息：
//  1. api — API 服务自身状态（版本）
//  2. agent — Agent 连接状态（可用性、版本、socket 路径）
//  3. nginx — Nginx 检测状态（是否检测到、版本、路径、运行状态、include 状态）
func (s *Server) handleSystemOverview(w http.ResponseWriter, r *http.Request) {
	// 1. API 信息
	apiInfo := map[string]any{
		"version": app.Version,
		"user":    "openrest",
	}

	// 2. Agent 信息 — 尝试健康检查
	agentInfo := s.getAgentInfo(r)

	// 3. Nginx 信息 — 从 settings 读取缓存的检测信息
	nginxInfo := s.getNginxInfo()

	WriteOK(w, r, map[string]any{
		"api":   apiInfo,
		"agent": agentInfo,
		"nginx": nginxInfo,
	})
}

func (s *Server) handleUpgradeCheck(w http.ResponseWriter, r *http.Request) {
	if s.upgradeSvc == nil {
		WriteOK(w, r, map[string]any{
			"has_upgrade":     false,
			"current_version": app.Version,
			"checked_at":      "",
			"error":           "升级检测未配置",
		})
		return
	}
	WriteOK(w, r, s.upgradeSvc.GetStatus())
}

// handleUpgradeCheckTrigger 手动触发一次升级检查
//
// 同步向 GitHub Releases API 发起请求并返回最新状态。
// upgrade.Service 内置 60 秒冷却，冷却内重复调用直接返回缓存状态。
// 这里额外用 20 秒 context 超时兜底（httpClient 自身 15 秒超时）。
func (s *Server) handleUpgradeCheckTrigger(w http.ResponseWriter, r *http.Request) {
	if s.upgradeSvc == nil {
		WriteOK(w, r, map[string]any{
			"has_upgrade":     false,
			"current_version": app.Version,
			"checked_at":      "",
			"error":           "升级检测未配置",
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	WriteOK(w, r, s.upgradeSvc.CheckNow(ctx))
}

// getAgentInfo 获取 Agent 连接状态
func (s *Server) getAgentInfo(r *http.Request) map[string]any {
	if s.agentClient == nil {
		return map[string]any{
			"available": false,
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	health, err := s.agentClient.Health(ctx)
	if err != nil {
		slog.Debug("Agent 健康检查失败", "error", err)
		return map[string]any{
			"available": false,
			"socket":    s.cfg.Agent.SocketPath,
		}
	}

	return map[string]any{
		"available": true,
		"version":   health.Version,
		"socket":    s.cfg.Agent.SocketPath,
	}
}

// getNginxInfo 从 settings 表获取缓存的 Nginx 信息
func (s *Server) getNginxInfo() map[string]any {
	if s.cfg.Nginx.Bin == "" {
		if err := s.cfg.ReloadFromDisk(); err != nil {
			slog.Debug("从磁盘重读配置失败", "error", err)
		}
	}

	bin := s.cfg.Nginx.Bin
	version := s.cfg.Nginx.Version
	confPath := s.cfg.Nginx.ConfPath
	includeOK := s.cfg.Nginx.IncludeInstalled

	detected := bin != ""
	running := false

	if detected && s.agentClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.agentClient.TestNginx(ctx); err == nil {
			running = true
		}
	}

	return map[string]any{
		"detected":          detected,
		"bin":               bin,
		"version":           version,
		"conf_path":         confPath,
		"running":           running,
		"include_installed": includeOK,
	}
}
