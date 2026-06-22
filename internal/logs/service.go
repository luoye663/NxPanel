// logs 包 — 日志操作
//
// 提供日志尾部读取和清空功能。
// 实际文件操作由 agent 通过 Unix Socket RPC 执行，
// API 层的 logs.Service 负责业务校验并调用 agent。
//
// 约束：
//   - tail access/error log
//   - truncate access/error log
//   - 避免大文件读爆内存
//   - 最大读取 4MB
//   - 默认 200 行，最大 1000 行
//   - 路径由站点记录确定，不接受用户路径
package logs

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

// maxLines 日志最大行数
const defaultMaxLines = 200

// maxLinesLimit 日志行数上限
const maxLinesLimit = 1000

// maxBytes 日志最大读取字节数 4MB
const maxBytes = 4 * 1024 * 1024

// searchMaxBytes 关键词搜索默认最多扫描尾部 8MB，避免高流量日志造成内存压力。
const searchMaxBytes = 8 * 1024 * 1024

// agentCaller 定义调用 agent 的能力（由 agentclient.Client 实现）
type agentCaller interface {
	LogTail(ctx context.Context, req *agentclient.LogTailRequest) (*agentclient.LogTailResponse, error)
	LogTruncate(ctx context.Context, req *agentclient.LogTruncateRequest) (*agentclient.LogTruncateResponse, error)
	LogSearch(ctx context.Context, req *agentclient.LogSearchRequest) (*agentclient.LogSearchResponse, error)
	RotatedLogList(ctx context.Context, req *agentclient.RotatedLogListRequest) (*agentclient.RotatedLogListResponse, error)
	RotatedLogTail(ctx context.Context, req *agentclient.RotatedLogTailRequest) (*agentclient.LogTailResponse, error)
	RotatedLogRemove(ctx context.Context, req *agentclient.RotatedLogRemoveRequest) error
	LogDownload(ctx context.Context, path string) (*http.Response, error)
}

// siteGetter 获取站点信息
type siteGetter interface {
	GetByID(id string) (*repo.Site, error)
}

// opRecorder 操作记录
type opRecorder interface {
	Create(o *repo.Operation) error
	UpdateStatus(id, status string) error
	UpdateError(id, status, errorCode, errorMessage, stderr string) error
}

// Service 日志业务服务
type Service struct {
	siteRepo siteGetter
	opRepo   opRecorder
	agent    agentCaller
}

// NewService 创建日志服务
func NewService(siteRepo siteGetter, opRepo opRecorder, agent agentCaller) *Service {
	return &Service{
		siteRepo: siteRepo,
		opRepo:   opRepo,
		agent:    agent,
	}
}

// GetResponse 日志尾部响应
// 对应文档 7.8.1 节
type GetResponse struct {
	Type      string   `json:"type"`
	Path      string   `json:"path"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
	MaxBytes  int64    `json:"max_bytes"`
}

// Get 获取站点日志尾部
//
// type 参数: access 或 error
// lines 参数: 返回行数，默认 200，最大 1000
func (svc *Service) Get(ctx context.Context, siteID, logType string, lines int) (*GetResponse, error) {
	// 校验日志类型
	if logType != "access" && logType != "error" {
		return nil, app.NewAppError(app.ErrBadRequest, "type 参数必须是 access 或 error", nil)
	}

	// 校验行数
	if lines <= 0 {
		lines = defaultMaxLines
	}
	if lines > maxLinesLimit {
		lines = maxLinesLimit
	}

	// 获取站点
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	// 确定日志路径
	logPath := site.AccessLogPath
	if logType == "error" {
		logPath = site.ErrorLogPath
	}
	if logPath == "" {
		return nil, app.NewAppError(app.ErrNotFound,
			fmt.Sprintf("站点未配置 %s log 路径", logType), nil)
	}

	// 调用 agent tail
	result, err := svc.agent.LogTail(ctx, &agentclient.LogTailRequest{
		Path:     logPath,
		MaxLines: lines,
		MaxBytes: maxBytes,
	})
	if err != nil {
		if app.IsPathDeniedError(err) {
			return nil, app.NewPathDeniedError("读取日志", "日志", logPath)
		}
		return nil, app.NewAppError(app.ErrAgentUnavailable, "读取日志失败: "+err.Error(), nil)
	}

	return &GetResponse{
		Type:      logType,
		Path:      logPath,
		Lines:     result.Lines,
		Truncated: result.Truncated,
		MaxBytes:  maxBytes,
	}, nil
}

// SearchResponse 日志关键词过滤响应。
type SearchResponse struct {
	Type      string   `json:"type"`
	Path      string   `json:"path"`
	Lines     []string `json:"lines"`
	Matched   int      `json:"matched"`
	Truncated bool     `json:"truncated"`
	MaxBytes  int64    `json:"max_bytes"`
}

// Search 在当前日志或用户选中的切割日志中按普通字符串过滤。
func (svc *Service) Search(ctx context.Context, siteID, logType, keyword, rotatedName string, lines int) (*SearchResponse, error) {
	logPath, err := svc.resolveLogPath(siteID, logType, rotatedName)
	if err != nil {
		return nil, err
	}
	if lines <= 0 {
		lines = defaultMaxLines
	}
	if lines > maxLinesLimit {
		lines = maxLinesLimit
	}
	result, err := svc.agent.LogSearch(ctx, &agentclient.LogSearchRequest{Path: logPath, Keyword: keyword, MaxLines: lines, MaxBytes: searchMaxBytes})
	if err != nil {
		if app.IsPathDeniedError(err) {
			return nil, app.NewPathDeniedError("搜索日志", "日志", logPath)
		}
		return nil, app.NewAppError(app.ErrAgentUnavailable, "搜索日志失败: "+err.Error(), nil)
	}
	return &SearchResponse{Type: logType, Path: logPath, Lines: result.Lines, Matched: result.Matched, Truncated: result.Truncated, MaxBytes: result.MaxBytes}, nil
}

type RotatedListResponse struct {
	Items []agentclient.RotatedLogItem `json:"items"`
}

func (svc *Service) RotatedList(ctx context.Context, siteID, logType string) (*RotatedListResponse, error) {
	logPath, err := svc.resolveLogPath(siteID, logType, "")
	if err != nil {
		return nil, err
	}
	result, err := svc.agent.RotatedLogList(ctx, &agentclient.RotatedLogListRequest{BasePath: logPath})
	if err != nil {
		if app.IsPathDeniedError(err) {
			return nil, app.NewPathDeniedError("读取历史日志列表", "日志", logPath)
		}
		return nil, app.NewAppError(app.ErrAgentUnavailable, "读取历史日志列表失败: "+err.Error(), nil)
	}
	return &RotatedListResponse{Items: result.Items}, nil
}

func (svc *Service) RotatedTail(ctx context.Context, siteID, logType, name string, lines int) (*GetResponse, error) {
	basePath, err := svc.resolveLogPath(siteID, logType, "")
	if err != nil {
		return nil, err
	}
	if lines <= 0 {
		lines = defaultMaxLines
	}
	if lines > maxLinesLimit {
		lines = maxLinesLimit
	}
	result, err := svc.agent.RotatedLogTail(ctx, &agentclient.RotatedLogTailRequest{BasePath: basePath, Name: name, MaxLines: lines, MaxBytes: maxBytes})
	if err != nil {
		if app.IsPathDeniedError(err) {
			return nil, app.NewPathDeniedError("读取历史日志", "日志", filepath.Join(filepath.Dir(basePath), name))
		}
		return nil, app.NewAppError(app.ErrAgentUnavailable, "读取历史日志失败: "+err.Error(), nil)
	}
	return &GetResponse{Type: logType, Path: filepath.Join(filepath.Dir(basePath), name), Lines: result.Lines, Truncated: result.Truncated, MaxBytes: maxBytes}, nil
}

func (svc *Service) DeleteRotated(ctx context.Context, siteID, logType, name, requestID string) error {
	basePath, err := svc.resolveLogPath(siteID, logType, "")
	if err != nil {
		return err
	}
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{ID: opID, Action: "site.log.delete_rotated", TargetType: "site", TargetID: siteID, Status: "pending", RequestID: requestID, Actor: "admin", Message: fmt.Sprintf("删除历史 %s 日志 %s", logType, name), CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	if err := svc.agent.RotatedLogRemove(ctx, &agentclient.RotatedLogRemoveRequest{BasePath: basePath, Name: name}); err != nil {
		if app.IsPathDeniedError(err) {
			_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentDenied, err.Error(), "")
			return app.NewPathDeniedError("删除历史日志", "日志", filepath.Join(filepath.Dir(basePath), name))
		}
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, err.Error(), "")
		return app.NewAppError(app.ErrAgentUnavailable, "删除历史日志失败: "+err.Error(), nil)
	}
	_ = svc.opRepo.UpdateStatus(opID, "success")
	return nil
}

func (svc *Service) Download(ctx context.Context, siteID, logType, rotatedName string) (*http.Response, string, error) {
	logPath, err := svc.resolveLogPath(siteID, logType, rotatedName)
	if err != nil {
		return nil, "", err
	}
	resp, err := svc.agent.LogDownload(ctx, logPath)
	if err != nil {
		if app.IsPathDeniedError(err) {
			return nil, "", app.NewPathDeniedError("下载日志", "日志", logPath)
		}
		return nil, "", app.NewAppError(app.ErrAgentUnavailable, "下载日志失败: "+err.Error(), nil)
	}
	return resp, filepath.Base(logPath), nil
}

// TruncateRequest 日志清空请求
type TruncateRequest struct {
	Type    string `json:"type"`
	Confirm bool   `json:"confirm"`
}

// TruncateResponse 日志清空响应
type TruncateResponse struct {
	Truncated   bool   `json:"truncated"`
	OperationID string `json:"operation_id"`
}

// Truncate 清空站点日志文件
//
// 必须传入 confirm=true 作为二次确认。
// 清空后记录操作。
func (svc *Service) Truncate(ctx context.Context, siteID string, req *TruncateRequest, requestID string) (*TruncateResponse, error) {
	// 校验日志类型
	if req.Type != "access" && req.Type != "error" {
		return nil, app.NewAppError(app.ErrBadRequest, "type 参数必须是 access 或 error", nil)
	}

	// 二次确认
	if !req.Confirm {
		return nil, app.NewAppError(app.ErrValidationFailed, "清空日志需要确认（confirm=true）", nil)
	}

	// 获取站点
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	// 确定日志路径
	logPath := site.AccessLogPath
	logLabel := "access"
	if req.Type == "error" {
		logPath = site.ErrorLogPath
		logLabel = "error"
	}
	if logPath == "" {
		return nil, app.NewAppError(app.ErrNotFound,
			fmt.Sprintf("站点未配置 %s log 路径", req.Type), nil)
	}

	// 操作记录
	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.truncate_log", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("清空 %s 日志 站点 %s", logLabel, site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	// 调用 agent truncate
	_, agentErr := svc.agent.LogTruncate(ctx, &agentclient.LogTruncateRequest{
		Path: logPath,
	})
	if agentErr != nil {
		if app.IsPathDeniedError(agentErr) {
			_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentDenied, agentErr.Error(), "")
			return nil, app.NewPathDeniedError("清空日志", "日志", logPath)
		}
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return nil, app.NewAppError(app.ErrAgentUnavailable, "清空日志失败: "+agentErr.Error(), nil)
	}

	_ = svc.opRepo.UpdateStatus(opID, "success")

	slog.Info("日志清空成功", "site_id", siteID, "type", req.Type, "path", logPath, "operation_id", opID)

	return &TruncateResponse{
		Truncated:   true,
		OperationID: opID,
	}, nil
}

// ResolveLogPath 根据 logType 返回站点的日志路径
func ResolveLogPath(site *repo.Site, logType string) string {
	logType = strings.ToLower(logType)
	if logType == "error" {
		return site.ErrorLogPath
	}
	return site.AccessLogPath
}

func (svc *Service) resolveLogPath(siteID, logType, rotatedName string) (string, error) {
	if logType != "access" && logType != "error" {
		return "", app.NewAppError(app.ErrBadRequest, "type 参数必须是 access 或 error", nil)
	}
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	basePath := ResolveLogPath(site, logType)
	if basePath == "" {
		return "", app.NewAppError(app.ErrNotFound, fmt.Sprintf("站点未配置 %s log 路径", logType), nil)
	}
	if rotatedName == "" {
		return basePath, nil
	}
	// 历史日志只接收文件名，绝对路径由站点当前日志路径反推，防止任意文件读取。
	if rotatedName != filepath.Base(rotatedName) || strings.Contains(rotatedName, "..") || strings.ContainsAny(rotatedName, "/\\") || !isRotatedLogName(basePath, rotatedName) {
		return "", app.NewAppError(app.ErrBadRequest, "历史日志名称非法", nil)
	}
	return filepath.Join(filepath.Dir(basePath), rotatedName), nil
}

func isRotatedLogName(basePath, name string) bool {
	baseName := filepath.Base(basePath)
	if !strings.HasPrefix(name, baseName+".") {
		return false
	}
	suffix := strings.TrimPrefix(name, baseName+".")
	suffix = strings.TrimSuffix(suffix, ".gz")
	_, err := time.Parse("20060102_150405", suffix)
	return err == nil
}
