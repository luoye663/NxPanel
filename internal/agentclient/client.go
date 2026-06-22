// agentclient 包 — Agent Unix Socket HTTP 客户端
//
// Client 是 API 层调用 agent 的客户端，通过 Unix Socket HTTP 通信。
//
// 使用方式：
//
//	client := agentclient.New("/run/nxpanel/agent.sock", "my-secret-token")
//	resp, err := client.Health(ctx)
//	result, err := client.ApplyTransaction(ctx, req)
package agentclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/accessanalysis"
)

// Client 是 agent 的 HTTP 客户端
type Client struct {
	socketPath string       // Unix Socket 路径
	token      string       // agent 认证 token
	httpClient *http.Client // HTTP 客户端（使用 Unix Socket 传输）
}

// New 创建 agent 客户端
//
// 参数：
//   - socketPath: agent Unix Socket 路径
//   - token: agent 共享密钥
func New(socketPath, token string, clientTimeout, dialTimeout, idleConnTimeout time.Duration) *Client {
	transport := newUnixSocketTransport(socketPath, dialTimeout, idleConnTimeout)
	return &Client{
		socketPath: socketPath,
		token:      token,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   clientTimeout,
		},
	}
}

// NewWithDefaults 创建使用默认超时的 agent 客户端（测试用）
func NewWithDefaults(socketPath, token string) *Client {
	return New(socketPath, token, 30*time.Second, 3*time.Second, 30*time.Second)
}

// Health 调用 agent 健康检查
//
// GET /internal/v1/health
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	var resp AgentResponse
	if err := c.get(ctx, "/internal/v1/health", &resp); err != nil {
		return nil, fmt.Errorf("agent 健康检查失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("agent 健康检查返回错误: %s", resp.Error)
	}

	// 将 resp.Data 解析为 HealthResponse
	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析健康检查响应失败: %w", err)
	}
	var health HealthResponse
	if err := json.Unmarshal(data, &health); err != nil {
		return nil, fmt.Errorf("解析健康检查响应失败: %w", err)
	}
	return &health, nil
}

// ApplyTransaction 调用 agent 应用文件事务
//
// POST /internal/v1/transactions/apply
func (c *Client) ApplyTransaction(ctx context.Context, req *TransactionRequest) (*TransactionResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/transactions/apply", req, &resp); err != nil {
		return nil, fmt.Errorf("文件事务请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("文件事务失败: %s", resp.Error)
	}

	// 将 resp.Data 解析为 TransactionResponse
	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析事务响应失败: %w", err)
	}
	var result TransactionResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析事务响应失败: %w", err)
	}
	return &result, nil
}

// ============================================================
// Nginx 操作方法
// ============================================================

// DetectNginx 调用 agent 检测 Nginx
//
// POST /internal/v1/nginx/detect
func (c *Client) DetectNginx(ctx context.Context, req *NginxDetectRequest) (*NginxDetectResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/nginx/detect", req, &resp); err != nil {
		return nil, fmt.Errorf("Nginx 检测请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("Nginx 检测失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析检测响应失败: %w", err)
	}
	var result NginxDetectResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析检测响应失败: %w", err)
	}
	return &result, nil
}

// TestNginx 调用 agent 执行 nginx -t
//
// POST /internal/v1/nginx/test
func (c *Client) TestNginx(ctx context.Context) (*NginxTestResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/nginx/test", nil, &resp); err != nil {
		return nil, fmt.Errorf("nginx -t 请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("nginx -t 失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析测试响应失败: %w", err)
	}
	var result NginxTestResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析测试响应失败: %w", err)
	}
	return &result, nil
}

// ReloadNginx 调用 agent 执行 nginx -s reload
//
// POST /internal/v1/nginx/reload
func (c *Client) ReloadNginx(ctx context.Context, req *NginxReloadRequest) (*NginxReloadResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/nginx/reload", req, &resp); err != nil {
		return nil, fmt.Errorf("nginx reload 请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("nginx reload 失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析 reload 响应失败: %w", err)
	}
	var result NginxReloadResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析 reload 响应失败: %w", err)
	}
	return &result, nil
}

// EnsureInclude 调用 agent 安装 include 入口
//
// POST /internal/v1/nginx/ensure-include
func (c *Client) EnsureInclude(ctx context.Context, req *EnsureIncludeRequest) (*EnsureIncludeResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/nginx/ensure-include", req, &resp); err != nil {
		return nil, fmt.Errorf("安装 include 请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("安装 include 失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析 include 响应失败: %w", err)
	}
	var result EnsureIncludeResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析 include 响应失败: %w", err)
	}
	return &result, nil
}

// ============================================================
// SSL 操作方法
// ============================================================

// SSLInspect 调用 agent 检查 SSL 证书（PEM 内容模式）
//
// POST /internal/v1/ssl/inspect
func (c *Client) SSLInspect(ctx context.Context, req *SSLInspectRequest) (*SSLInspectResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/ssl/inspect", req, &resp); err != nil {
		return nil, fmt.Errorf("SSL 证书检查请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("SSL 证书检查失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析 SSL 响应失败: %w", err)
	}
	var result SSLInspectResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析 SSL 响应失败: %w", err)
	}
	return &result, nil
}

// SSLInspectFiles 调用 agent 检查已有 SSL 文件
//
// POST /internal/v1/ssl/inspect
func (c *Client) SSLInspectFiles(ctx context.Context, req *SSLInspectFilesRequest) (*SSLInspectResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/ssl/inspect", req, &resp); err != nil {
		return nil, fmt.Errorf("SSL 文件检查请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("SSL 文件检查失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析 SSL 响应失败: %w", err)
	}
	var result SSLInspectResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析 SSL 响应失败: %w", err)
	}
	return &result, nil
}

// ============================================================
// 日志操作方法
// ============================================================

// LogTail 调用 agent 读取日志尾部
//
// POST /internal/v1/logs/tail
func (c *Client) LogTail(ctx context.Context, req *LogTailRequest) (*LogTailResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/logs/tail", req, &resp); err != nil {
		return nil, fmt.Errorf("日志 tail 请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("日志 tail 失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析日志 tail 响应失败: %w", err)
	}
	var result LogTailResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析日志 tail 响应失败: %w", err)
	}
	return &result, nil
}

// AccessAnalysisScan 调用 agent 流式扫描 access log，并只返回聚合结果。
// POST /internal/v1/logs/access-analysis/scan
func (c *Client) AccessAnalysisScan(ctx context.Context, req *accessanalysis.AgentScanRequest) (*accessanalysis.AgentScanResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/logs/access-analysis/scan", req, &resp); err != nil {
		return nil, fmt.Errorf("访问分析扫描请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("访问分析扫描失败: %s", resp.Error)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析访问分析扫描响应失败: %w", err)
	}
	var result accessanalysis.AgentScanResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析访问分析扫描响应失败: %w", err)
	}
	return &result, nil
}

// AccessAnalysisFormatDetect 调用 agent 读取少量日志样本并检测格式。
// POST /internal/v1/logs/access-analysis/format-detect
func (c *Client) AccessAnalysisFormatDetect(ctx context.Context, req *accessanalysis.AgentFormatDetectRequest) (*accessanalysis.FormatDetectResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/logs/access-analysis/format-detect", req, &resp); err != nil {
		return nil, fmt.Errorf("访问分析格式检测请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("访问分析格式检测失败: %s", resp.Error)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析访问分析格式检测响应失败: %w", err)
	}
	var result accessanalysis.FormatDetectResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析访问分析格式检测响应失败: %w", err)
	}
	return &result, nil
}

// LogTruncate 调用 agent 清空日志文件
//
// POST /internal/v1/logs/truncate
func (c *Client) LogTruncate(ctx context.Context, req *LogTruncateRequest) (*LogTruncateResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/logs/truncate", req, &resp); err != nil {
		return nil, fmt.Errorf("日志 truncate 请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("日志 truncate 失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析日志 truncate 响应失败: %w", err)
	}
	var result LogTruncateResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析日志 truncate 响应失败: %w", err)
	}
	return &result, nil
}

func (c *Client) LogSearch(ctx context.Context, req *LogSearchRequest) (*LogSearchResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/logs/search", req, &resp); err != nil {
		return nil, fmt.Errorf("日志搜索请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("日志搜索失败: %s", resp.Error)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析日志搜索响应失败: %w", err)
	}
	var result LogSearchResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析日志搜索响应失败: %w", err)
	}
	return &result, nil
}

func (c *Client) RotatedLogList(ctx context.Context, req *RotatedLogListRequest) (*RotatedLogListResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/logs/rotated/list", req, &resp); err != nil {
		return nil, fmt.Errorf("历史日志列表请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("历史日志列表失败: %s", resp.Error)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析历史日志列表失败: %w", err)
	}
	var result RotatedLogListResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析历史日志列表失败: %w", err)
	}
	return &result, nil
}

func (c *Client) RotatedLogTail(ctx context.Context, req *RotatedLogTailRequest) (*LogTailResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/logs/rotated/tail", req, &resp); err != nil {
		return nil, fmt.Errorf("历史日志 tail 请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("历史日志 tail 失败: %s", resp.Error)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析历史日志 tail 响应失败: %w", err)
	}
	var result LogTailResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析历史日志 tail 响应失败: %w", err)
	}
	return &result, nil
}

func (c *Client) RotatedLogRemove(ctx context.Context, req *RotatedLogRemoveRequest) error {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/logs/rotated/remove", req, &resp); err != nil {
		return fmt.Errorf("删除历史日志请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("删除历史日志失败: %s", resp.Error)
	}
	return nil
}

func (c *Client) NginxLogRotateRun(ctx context.Context, req *NginxLogRotateRunRequest) (*NginxLogRotateRunResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/nginx/logs/rotate-run", req, &resp); err != nil {
		return nil, fmt.Errorf("Nginx 日志切割请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("Nginx 日志切割失败: %s", resp.Error)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析 Nginx 日志切割响应失败: %w", err)
	}
	var result NginxLogRotateRunResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析 Nginx 日志切割响应失败: %w", err)
	}
	return &result, nil
}

func (c *Client) LogDownload(ctx context.Context, path string) (*http.Response, error) {
	u := "http://unix/internal/v1/logs/download?" + url.Values{"path": {path}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("创建日志下载请求失败: %w", err)
	}
	req.Header.Set("X-NxPanel-Agent-Token", c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("日志下载请求失败: %w", err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent 日志下载返回 HTTP %d: %s", resp.StatusCode, string(body))
	}
	return resp, nil
}

func (c *Client) SiteBackupCreate(ctx context.Context, req *SiteBackupCreateRequest) (*SiteBackupCreateResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/backups/site/create", req, &resp); err != nil {
		return nil, fmt.Errorf("站点备份创建请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("站点备份创建失败: %s", resp.Error)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析站点备份创建响应失败: %w", err)
	}
	var result SiteBackupCreateResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析站点备份创建响应失败: %w", err)
	}
	return &result, nil
}

func (c *Client) SiteBackupDownload(ctx context.Context, path string) (*http.Response, error) {
	u := "http://unix/internal/v1/backups/site/download?" + url.Values{"path": {path}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("创建站点备份下载请求失败: %w", err)
	}
	req.Header.Set("X-NxPanel-Agent-Token", c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("站点备份下载请求失败: %w", err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent 站点备份下载返回 HTTP %d: %s", resp.StatusCode, string(body))
	}
	return resp, nil
}

func (c *Client) SiteBackupRestore(ctx context.Context, req *SiteBackupRestoreRequest) error {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/backups/site/restore", req, &resp); err != nil {
		return fmt.Errorf("站点备份恢复请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("站点备份恢复失败: %s", resp.Error)
	}
	return nil
}

func (c *Client) SiteBackupRemove(ctx context.Context, path string) error {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/backups/site/remove", map[string]string{"backup_path": path}, &resp); err != nil {
		return fmt.Errorf("站点备份删除请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("站点备份删除失败: %s", resp.Error)
	}
	return nil
}

// ============================================================
// 文件管理操作方法（文件管理器）
// ============================================================

// FilesRoots 获取白名单根目录列表
//
// GET /internal/v1/files/roots
func (c *Client) FilesRoots(ctx context.Context) (*FilesRootsResponse, error) {
	var resp AgentResponse
	if err := c.get(ctx, "/internal/v1/files/roots", &resp); err != nil {
		return nil, fmt.Errorf("获取根目录列表失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("获取根目录列表失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析根目录列表响应失败: %w", err)
	}
	var result FilesRootsResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析根目录列表响应失败: %w", err)
	}
	return &result, nil
}

// FilesList 列出目录内容
//
// POST /internal/v1/files/list
func (c *Client) FilesList(ctx context.Context, path string) (*FilesListResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/files/list", map[string]string{"path": path}, &resp); err != nil {
		return nil, fmt.Errorf("列出目录请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("列出目录失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析列表响应失败: %w", err)
	}
	var result FilesListResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析列表响应失败: %w", err)
	}
	return &result, nil
}

// FilesRead 读取文件内容
//
// POST /internal/v1/files/read
func (c *Client) FilesRead(ctx context.Context, path string) (*FilesReadResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/files/read", map[string]string{"path": path}, &resp); err != nil {
		return nil, fmt.Errorf("读取文件请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("读取文件失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析读响应失败: %w", err)
	}
	var result FilesReadResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析读响应失败: %w", err)
	}
	return &result, nil
}

// FilesWrite 写入文件内容
//
// POST /internal/v1/files/write
func (c *Client) FilesWrite(ctx context.Context, path, contentBase64 string) error {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/files/write", map[string]string{
		"path": path, "content_base64": contentBase64,
	}, &resp); err != nil {
		return fmt.Errorf("写入文件请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("写入文件失败: %s", resp.Error)
	}
	return nil
}

// FilesRemove 删除文件/目录
//
// POST /internal/v1/files/remove
func (c *Client) FilesRemove(ctx context.Context, paths []string) error {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/files/remove", map[string]any{"paths": paths}, &resp); err != nil {
		return fmt.Errorf("删除文件请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("删除文件失败: %s", resp.Error)
	}
	return nil
}

// FilesMkdir 创建目录
//
// POST /internal/v1/files/mkdir
func (c *Client) FilesMkdir(ctx context.Context, path string) error {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/files/mkdir", map[string]string{"path": path}, &resp); err != nil {
		return fmt.Errorf("创建目录请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("创建目录失败: %s", resp.Error)
	}
	return nil
}

// FilesMove 移动/重命名
//
// POST /internal/v1/files/move
func (c *Client) FilesMove(ctx context.Context, source, dest string) error {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/files/move", map[string]string{
		"source": source, "destination": dest,
	}, &resp); err != nil {
		return fmt.Errorf("移动文件请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("移动文件失败: %s", resp.Error)
	}
	return nil
}

// FilesCopy 批量复制文件/目录到目标目录
//
// POST /internal/v1/files/copy
func (c *Client) FilesCopy(ctx context.Context, paths []string, destDir string) error {
	var resp AgentResponse
	req := FilesCopyRequest{Paths: paths, DestDir: destDir}
	if err := c.postJSON(ctx, "/internal/v1/files/copy", req, &resp); err != nil {
		return fmt.Errorf("复制文件请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("复制文件失败: %s", resp.Error)
	}
	return nil
}

// FilesDownload 下载文件（流式），返回 HTTP 响应体
// 调用方负责关闭 resp.Body
//
// GET /internal/v1/files/download?path=...
func (c *Client) FilesDownload(ctx context.Context, path string) (*http.Response, error) {
	u := "http://unix/internal/v1/files/download?" + url.Values{"path": {path}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("创建下载请求失败: %w", err)
	}
	req.Header.Set("X-NxPanel-Agent-Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载请求失败: %w", err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent 下载返回 HTTP %d: %s", resp.StatusCode, string(body))
	}
	return resp, nil
}

// FilesArchive 打包下载 ZIP（流式），返回 HTTP 响应体
// 调用方负责关闭 resp.Body
//
// GET /internal/v1/files/archive?paths=...
func (c *Client) FilesArchive(ctx context.Context, paths []string) (*http.Response, error) {
	u := "http://unix/internal/v1/files/archive?" + url.Values{"paths": {strings.Join(paths, ",")}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("创建打包请求失败: %w", err)
	}
	req.Header.Set("X-NxPanel-Agent-Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("打包请求失败: %w", err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent 打包返回 HTTP %d: %s", resp.StatusCode, string(body))
	}
	return resp, nil
}

// FilesUpload 上传文件（base64）
//
// POST /internal/v1/files/upload
func (c *Client) FilesUpload(ctx context.Context, path, contentBase64 string) error {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/files/upload", map[string]string{
		"path": path, "content_base64": contentBase64,
	}, &resp); err != nil {
		return fmt.Errorf("上传文件请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("上传文件失败: %s", resp.Error)
	}
	return nil
}

// FilesUploadWithTimeout 上传文件，使用独立超时（大文件上传场景）
//
// 创建临时 HTTP 客户端绕过默认 clientTimeout
func (c *Client) FilesUploadWithTimeout(ctx context.Context, path, contentBase64 string, timeout time.Duration) error {
	var resp AgentResponse
	body, err := json.Marshal(map[string]string{
		"path": path, "content_base64": contentBase64,
	})
	if err != nil {
		return fmt.Errorf("序列化上传请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/internal/v1/files/upload", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建上传请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", c.token)

	client := &http.Client{
		Transport: c.httpClient.Transport,
		Timeout:   timeout,
	}

	respHTTP, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("上传文件请求失败: %w", err)
	}
	defer respHTTP.Body.Close()

	if respHTTP.StatusCode >= 300 {
		respBody, _ := io.ReadAll(respHTTP.Body)
		return fmt.Errorf("上传文件失败: HTTP %d: %s", respHTTP.StatusCode, string(respBody))
	}

	if err := json.NewDecoder(respHTTP.Body).Decode(&resp); err != nil {
		return fmt.Errorf("解析上传响应失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("上传文件失败: %s", resp.Error)
	}
	return nil
}

// FilesChmod 修改文件/目录权限
//
// POST /internal/v1/files/chmod
func (c *Client) FilesChmod(ctx context.Context, path, mode string, recursive bool) error {
	var resp AgentResponse
	req := FilesChmodRequest{Path: path, Mode: mode, Recursive: recursive}
	if err := c.postJSON(ctx, "/internal/v1/files/chmod", req, &resp); err != nil {
		return fmt.Errorf("修改权限请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("修改权限失败: %s", resp.Error)
	}
	return nil
}

// FilesChown 修改文件/目录所有者
//
// POST /internal/v1/files/chown
func (c *Client) FilesChown(ctx context.Context, path, owner, group string, recursive bool) error {
	var resp AgentResponse
	req := FilesChownRequest{Path: path, Owner: owner, Group: group, Recursive: recursive}
	if err := c.postJSON(ctx, "/internal/v1/files/chown", req, &resp); err != nil {
		return fmt.Errorf("修改所有者请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("修改所有者失败: %s", resp.Error)
	}
	return nil
}

// FilesCompress 压缩文件/目录到存档
//
// POST /internal/v1/files/compress
func (c *Client) FilesCompress(ctx context.Context, paths []string, outputPath, format string) error {
	var resp AgentResponse
	req := FilesCompressRequest{Paths: paths, OutputPath: outputPath, Format: format}
	if err := c.postJSON(ctx, "/internal/v1/files/compress", req, &resp); err != nil {
		return fmt.Errorf("压缩请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("压缩失败: %s", resp.Error)
	}
	return nil
}

// FilesExtract 解压缩存档到目录
//
// POST /internal/v1/files/extract
func (c *Client) FilesExtract(ctx context.Context, archivePath, destDir string) error {
	var resp AgentResponse
	req := FilesExtractRequest{ArchivePath: archivePath, DestDir: destDir}
	if err := c.postJSON(ctx, "/internal/v1/files/extract", req, &resp); err != nil {
		return fmt.Errorf("解压请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("解压失败: %s", resp.Error)
	}
	return nil
}

// NginxDump 调用 agent 执行 nginx -T（用于旧站扫描）
//
// POST /internal/v1/nginx/dump
func (c *Client) NginxDump(ctx context.Context, req *NginxDumpRequest) (*NginxDumpResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/nginx/dump", req, &resp); err != nil {
		return nil, fmt.Errorf("nginx -T 请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("nginx -T 失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析 nginx -T 响应失败: %w", err)
	}
	var result NginxDumpResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析 nginx -T 响应失败: %w", err)
	}
	return &result, nil
}

func (c *Client) ReopenNginx(ctx context.Context) (*NginxReopenResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/nginx/reopen", nil, &resp); err != nil {
		return nil, fmt.Errorf("nginx -s reopen 请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("nginx -s reopen 失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析 reopen 响应失败: %w", err)
	}
	var result NginxReopenResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析 reopen 响应失败: %w", err)
	}
	return &result, nil
}

func (c *Client) ReloadConfig(ctx context.Context) error {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/config/reload", nil, &resp); err != nil {
		return fmt.Errorf("配置重载请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("配置重载失败: %s", resp.Error)
	}
	return nil
}

func (c *Client) WriteBackConfig(ctx context.Context, req *ConfigWriteBackRequest) error {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/config/write-back", req, &resp); err != nil {
		return fmt.Errorf("配置回写请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("配置回写失败: %s", resp.Error)
	}
	return nil
}

// ReadFile 读取文件内容并返回内容和 SHA256 hash
func (c *Client) ReadFile(ctx context.Context, path string) ([]byte, string, error) {
	resp, err := c.FilesRead(ctx, path)
	if err != nil {
		return nil, "", err
	}

	var content []byte
	if resp.Encoding == "base64" && resp.ContentBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(resp.ContentBase64)
		if err != nil {
			return nil, "", fmt.Errorf("base64 解码失败: %w", err)
		}
		content = decoded
	}

	hash := sha256Hex(content)
	return content, hash, nil
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func (c *Client) ServiceLogTail(ctx context.Context, req *ServiceLogRequest) (*LogTailResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/logs/service/tail", req, &resp); err != nil {
		return nil, fmt.Errorf("服务日志 tail 请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("服务日志 tail 失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析服务日志响应失败: %w", err)
	}
	var result LogTailResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析服务日志响应失败: %w", err)
	}
	return &result, nil
}

func (c *Client) ServiceLogTruncate(ctx context.Context, service string) error {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/logs/service/truncate", map[string]string{"service": service}, &resp); err != nil {
		return fmt.Errorf("服务日志清空请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("服务日志清空失败: %s", resp.Error)
	}
	return nil
}

func (c *Client) TaskLogList(ctx context.Context) (*TaskLogListResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/logs/tasks/list", nil, &resp); err != nil {
		return nil, fmt.Errorf("任务日志列表请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("任务日志列表失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析任务日志列表失败: %w", err)
	}
	var result TaskLogListResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析任务日志列表失败: %w", err)
	}
	return &result, nil
}

func (c *Client) TaskLogTail(ctx context.Context, req *TaskLogRequest) (*LogTailResponse, error) {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/logs/tasks/tail", req, &resp); err != nil {
		return nil, fmt.Errorf("任务日志 tail 请求失败: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("任务日志 tail 失败: %s", resp.Error)
	}

	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("解析任务日志响应失败: %w", err)
	}
	var result LogTailResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析任务日志响应失败: %w", err)
	}
	return &result, nil
}

func (c *Client) TaskLogTruncate(ctx context.Context, name string) error {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/logs/tasks/truncate", map[string]string{"name": name}, &resp); err != nil {
		return fmt.Errorf("任务日志清空请求失败: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("任务日志清空失败: %s", resp.Error)
	}
	return nil
}

// ============================================================
// HTTP 工具方法
// ============================================================

// postJSON 发送 JSON POST 请求
func (c *Client) postJSON(ctx context.Context, path string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("序列化请求体失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		var agentResp AgentResponse
		if json.Unmarshal(respBody, &agentResp) == nil && agentResp.Error != "" {
			return fmt.Errorf("agent 返回 HTTP %d: %s", resp.StatusCode, agentResp.Error)
		}
		return fmt.Errorf("agent 返回 HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

// get 发送 GET 请求
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+path, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("X-NxPanel-Agent-Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		var agentResp AgentResponse
		if json.Unmarshal(respBody, &agentResp) == nil && agentResp.Error != "" {
			return fmt.Errorf("agent 返回 HTTP %d: %s", resp.StatusCode, agentResp.Error)
		}
		return fmt.Errorf("agent 返回 HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
