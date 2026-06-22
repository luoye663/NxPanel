// agentclient 包 — API 调用 Agent 的客户端类型定义
//
// 定义 agent RPC 的请求和响应类型，供 client.go 使用。
package agentclient

import "github.com/luoye663/nxpanel/internal/accessanalysis"

// ============================================================
// 通用类型
// ============================================================

// AgentResponse agent 的统一响应格式
type AgentResponse struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// ============================================================
// 文件事务类型
// ============================================================

// FileChangeRequest 文件变更请求
type FileChangeRequest struct {
	Type          string `json:"type"`                     // write, remove, symlink, mkdir, truncate
	Path          string `json:"path"`                     // 目标文件路径
	Target        string `json:"target,omitempty"`         // symlink 目标
	ContentBase64 string `json:"content_base64,omitempty"` // base64 编码的内容
	Perm          uint32 `json:"perm,omitempty"`           // 文件权限
	RequireMarker string `json:"require_marker,omitempty"` // 要求文件包含的标记
}

// TransactionRequest 文件事务请求
type TransactionRequest struct {
	OperationID    string              `json:"operation_id"`
	Changes        []FileChangeRequest `json:"changes"`
	TestNginx      bool                `json:"test_nginx"`
	ReloadNginx    bool                `json:"reload_nginx"`
	TimeoutSeconds int                 `json:"timeout_seconds"`
}

// BackupRecord 文件备份记录
type BackupRecord struct {
	FilePath   string `json:"file_path"`
	BackupPath string `json:"backup_path"`
	Existed    bool   `json:"file_existed"`
}

// TransactionResponse 文件事务响应
type TransactionResponse struct {
	Backups []BackupRecord `json:"backups"`
}

// ============================================================
// 健康检查类型
// ============================================================

// HealthResponse 健康检查响应
type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

// ============================================================
// Nginx detect 类型
// ============================================================

// NginxDetectRequest Nginx 检测请求
type NginxDetectRequest struct {
	NginxBin string `json:"nginx_bin"` // 为空时由 agent 自动查找
}

// NginxDetectResponse Nginx 检测响应
type NginxDetectResponse struct {
	Bin      string `json:"bin"`       // 二进制路径
	Version  string `json:"version"`   // 版本号
	ConfPath string `json:"conf_path"` // 主配置文件路径
	Prefix   string `json:"prefix"`    // Nginx prefix 路径
	TestOK   bool   `json:"test_ok"`   // nginx -t 是否通过
	Stderr   string `json:"stderr"`    // nginx -t 的 stderr
	WebUser  string `json:"web_user"`  // Nginx 运行用户
	WebGroup string `json:"web_group"` // Nginx 运行组
}

// NginxTestResponse nginx -t 测试响应
type NginxTestResponse struct {
	OK     bool   `json:"ok"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// NginxReloadRequest nginx reload 请求
type NginxReloadRequest struct {
	TestBeforeReload bool `json:"test_before_reload"` // 是否在 reload 前先测试
}

// NginxReloadResponse nginx reload 响应
type NginxReloadResponse struct {
	OK bool `json:"ok"`
}

// EnsureIncludeRequest 安装 include 入口请求
type EnsureIncludeRequest struct {
	ConfirmModifyMainConf bool `json:"confirm_modify_main_conf"` // 是否确认修改主配置
}

// EnsureIncludeResponse 安装 include 入口响应
type EnsureIncludeResponse struct {
	Installed bool   `json:"installed"`  // 是否已安装
	Changed   bool   `json:"changed"`    // 是否有变更
	EntryFile string `json:"entry_file"` // 入口文件路径
}

// ============================================================
// SSL inspect 类型
// ============================================================

// SSLInspectRequest SSL 证书检查请求（PEM 内容模式）
type SSLInspectRequest struct {
	CertPEM string `json:"cert_pem"` // PEM 编码的证书内容
	KeyPEM  string `json:"key_pem"`  // PEM 编码的私钥内容
}

// SSLInspectFilesRequest SSL 证书检查请求（文件路径模式）
type SSLInspectFilesRequest struct {
	CertPath string `json:"cert_path"` // 证书文件路径
	KeyPath  string `json:"key_path"`  // 私钥文件路径
}

// SSLInspectResponse SSL 证书检查响应
type SSLInspectResponse struct {
	Subject    string   `json:"subject"`
	Issuer     string   `json:"issuer"`
	NotBefore  string   `json:"not_before"`
	NotAfter   string   `json:"not_after"`
	DNSNames   []string `json:"dns_names"`
	CertSHA256 string   `json:"cert_sha256"`
	KeySHA256  string   `json:"key_sha256,omitempty"`
}

// ============================================================
// 日志操作类型
// ============================================================

// LogTailRequest 日志尾部读取请求
type LogTailRequest struct {
	Path     string `json:"path"`      // 日志文件路径（由站点记录确定）
	MaxLines int    `json:"max_lines"` // 最大行数
	MaxBytes int64  `json:"max_bytes"` // 最大读取字节数
}

// LogTailResponse 日志尾部读取响应
type LogTailResponse struct {
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
	Path      string   `json:"path,omitempty"`
}

// LogTruncateRequest 日志清空请求
type LogTruncateRequest struct {
	Path string `json:"path"` // 日志文件路径（由站点记录确定）
}

// LogTruncateResponse 日志清空响应
type LogTruncateResponse struct {
	OK bool `json:"ok"`
}

type LogSearchRequest struct {
	Path          string `json:"path"`
	Keyword       string `json:"keyword"`
	MaxBytes      int64  `json:"max_bytes"`
	MaxLines      int    `json:"max_lines"`
	CaseSensitive bool   `json:"case_sensitive"`
}

type LogSearchResponse struct {
	Lines     []string `json:"lines"`
	Matched   int      `json:"matched"`
	Truncated bool     `json:"truncated"`
	MaxBytes  int64    `json:"max_bytes"`
}

type RotatedLogListRequest struct {
	BasePath string `json:"base_path"`
}

type RotatedLogTailRequest struct {
	BasePath string `json:"base_path"`
	Name     string `json:"name"`
	MaxLines int    `json:"max_lines"`
	MaxBytes int64  `json:"max_bytes"`
}

type RotatedLogRemoveRequest struct {
	BasePath string `json:"base_path"`
	Name     string `json:"name"`
}

type RotatedLogItem struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	ModTime    string `json:"mod_time"`
	Compressed bool   `json:"compressed"`
}

type RotatedLogListResponse struct {
	Items []RotatedLogItem `json:"items"`
}

type NginxLogRotateRunRequest struct {
	MinSize  string `json:"min_size"`
	MaxCount int    `json:"max_count"`
	MaxAge   string `json:"max_age"`
}

type NginxLogRotateRunResponse struct {
	RotatedCount int    `json:"rotated_count"`
	RemovedCount int    `json:"removed_count"`
	ReopenOK     bool   `json:"reopen_ok"`
	Message      string `json:"message"`
}

type AccessAnalysisScanRequest = accessanalysis.AgentScanRequest
type AccessAnalysisScanResponse = accessanalysis.AgentScanResponse
type AccessAnalysisFormatDetectRequest = accessanalysis.AgentFormatDetectRequest
type AccessAnalysisFormatDetectResponse = accessanalysis.FormatDetectResponse

// ============================================================
// 站点备份类型
// ============================================================

type SiteBackupCreateRequest struct {
	SiteID        string   `json:"site_id"`
	PrimaryDomain string   `json:"primary_domain"`
	BackupType    string   `json:"backup_type"`
	OutputPath    string   `json:"output_path"`
	ConfigPaths   []string `json:"config_paths"`
	RootPath      string   `json:"root_path"`
	SSLPaths      []string `json:"ssl_paths"`
}

type SiteBackupCreateResponse struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type SiteBackupRestoreRequest struct {
	SiteID        string   `json:"site_id"`
	BackupPath    string   `json:"backup_path"`
	RestoreConfig bool     `json:"restore_config"`
	RestoreRoot   bool     `json:"restore_root"`
	RestoreSSL    bool     `json:"restore_ssl"`
	ConfigPaths   []string `json:"config_paths"`
	RootPath      string   `json:"root_path"`
	SSLPaths      []string `json:"ssl_paths"`
	ReloadNginx   bool     `json:"reload_nginx"`
}

// ============================================================
// 文件管理类型（文件管理器）
// ============================================================

// FileEntry 文件/目录条目
type FileEntry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
	IsDir   bool   `json:"is_dir"`
	Mode    string `json:"mode"`
	Owner   string `json:"owner"`
	Group   string `json:"group"`
}

// FilesListResponse 列出目录响应
type FilesListResponse struct {
	Entries []FileEntry `json:"entries"`
}

// FilesRootsResponse 白名单根目录列表响应
type FilesRootsResponse struct {
	Roots []string `json:"roots"`
}

// FilesReadResponse 读取文件响应
type FilesReadResponse struct {
	ContentBase64 string `json:"content_base64"`
	Size          int64  `json:"size"`
	Encoding      string `json:"encoding"`
}

// FilesChmodRequest 修改权限请求
type FilesChmodRequest struct {
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	Recursive bool   `json:"recursive"`
}

// FilesChownRequest 修改所有者请求
type FilesChownRequest struct {
	Path      string `json:"path"`
	Owner     string `json:"owner"`
	Group     string `json:"group"`
	Recursive bool   `json:"recursive"`
}

// FilesCompressRequest 压缩请求
type FilesCompressRequest struct {
	Paths      []string `json:"paths"`
	OutputPath string   `json:"output_path"`
	Format     string   `json:"format"`
}

// FilesExtractRequest 解压请求
type FilesExtractRequest struct {
	ArchivePath string `json:"archive_path"`
	DestDir     string `json:"dest_dir"`
}

// FilesCopyRequest 批量复制请求
type FilesCopyRequest struct {
	Paths   []string `json:"paths"`
	DestDir string   `json:"dest_dir"`
}

// NginxDumpRequest nginx -T 输出请求
type NginxDumpRequest struct{}

// NginxDumpResponse nginx -T 输出响应
type NginxDumpResponse struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// NginxReopenResponse nginx -s reopen 响应
type NginxReopenResponse struct {
	OK     bool   `json:"ok"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// ConfigWriteBackField 单个配置回写字段
type ConfigWriteBackField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ConfigWriteBackRequest 配置回写请求
type ConfigWriteBackRequest struct {
	Fields []ConfigWriteBackField `json:"fields"`
}

// ServiceLogRequest 服务运行日志请求
type ServiceLogRequest struct {
	Service  string `json:"service"`
	MaxLines int    `json:"max_lines"`
}

// TaskLogListResponse 任务日志类型列表响应
type TaskLogListResponse struct {
	Tasks []TaskLogEntry `json:"tasks"`
}

// TaskLogEntry 任务日志条目
type TaskLogEntry struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Label   string `json:"label"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

// TaskLogRequest 任务日志读取/清空请求
type TaskLogRequest struct {
	Name     string `json:"name"`
	MaxLines int    `json:"max_lines"`
}
