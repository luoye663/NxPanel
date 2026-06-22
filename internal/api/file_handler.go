// api 包 — 文件管理 handler
//
// 处理站点文件管理接口：
//   - GET    /api/v1/sites/{site_id}/files?path=...       列出目录
//   - GET    /api/v1/sites/{site_id}/files/read?path=...   读取文件
//   - POST   /api/v1/sites/{site_id}/files/write           写入文件
//   - POST   /api/v1/sites/{site_id}/files/remove          删除文件/目录
//   - POST   /api/v1/sites/{site_id}/files/mkdir           创建目录
//   - POST   /api/v1/sites/{site_id}/files/move            移动/重命名
//   - GET    /api/v1/sites/{site_id}/files/download?path=.  下载文件（流式）
//   - GET    /api/v1/sites/{site_id}/files/archive?paths=.  打包下载（流式）
//   - POST   /api/v1/sites/{site_id}/files/upload          上传文件
//
// 所有路径操作均经过 agent.PathPolicy 白名单校验。
// site_id 仅用于操作审计上下文。
package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

func writeFileAgentError(w http.ResponseWriter, r *http.Request, action, pathKind, pathValue string, err error) {
	if app.IsPathDeniedError(err) {
		appErr := app.NewPathDeniedError(action, pathKind, pathValue)
		WriteError(w, r, http.StatusForbidden, appErr.Code, appErr.Message, appErr.Details)
		return
	}
	WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
}

// ============================================================
// 获取站点根路径（用于初始化文件浏览）
// ============================================================

func (s *Server) getSiteRootPath(siteID string) string {
	site, _, _, err := s.siteSvc.Get(siteID)
	if err != nil {
		return ""
	}
	return site.RootPath
}

// ============================================================
// List — 列出目录
// ============================================================

// handleFilesList 列出目录内容
//
// GET /api/v1/sites/{site_id}/files?path=/www/wwwroot/example.com
// path 为空时使用站点 root_path
func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")

	path := r.URL.Query().Get("path")
	if path == "" {
		path = s.getSiteRootPath(siteID)
		if path == "" {
			WriteError(w, r, http.StatusNotFound, app.ErrNotFound, "站点不存在", nil)
			return
		}
	}

	result, err := s.agentClient.FilesList(r.Context(), path)
	if err != nil {
		writeFileAgentError(w, r, "加载文件列表", "根目录", path, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"path":    path,
		"entries": result.Entries,
	})
}

// ============================================================
// Read — 读取文件
// ============================================================

// handleFilesRead 读取文件内容，返回 base64 编码
//
// GET /api/v1/sites/{site_id}/files/read?path=/www/wwwroot/example.com/index.html
func (s *Server) handleFilesRead(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "path 不能为空", nil)
		return
	}

	result, err := s.agentClient.FilesRead(r.Context(), path)
	if err != nil {
		writeFileAgentError(w, r, "读取文件", "根目录", path, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"path":           path,
		"content_base64": result.ContentBase64,
		"size":           result.Size,
	})
}

// ============================================================
// Write — 写入文件
// ============================================================

// handleFilesWrite 写入文件内容，接收 base64 编码
//
// POST /api/v1/sites/{site_id}/files/write
// Body: {"path":"...", "content_base64":"..."}
func (s *Server) handleFilesWrite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path          string `json:"path"`
		ContentBase64 string `json:"content_base64"`
	}
	if !DecodeJSON(w, r, &body) {
		return
	}

	if body.Path == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "path 不能为空", nil)
		return
	}

	if err := s.agentClient.FilesWrite(r.Context(), body.Path, body.ContentBase64); err != nil {
		writeFileAgentError(w, r, "写入文件", "根目录", body.Path, err)
		return
	}

	s.recordFileOperation(r.Context(), chi.URLParam(r, "site_id"), "file.write", body.Path)

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Remove — 删除文件/目录
// ============================================================

// handleFilesRemove 批量删除文件/目录
//
// POST /api/v1/sites/{site_id}/files/remove
// Body: {"paths":["...","..."]}
func (s *Server) handleFilesRemove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths []string `json:"paths"`
	}
	if !DecodeJSON(w, r, &body) {
		return
	}

	if len(body.Paths) == 0 {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "paths 不能为空", nil)
		return
	}

	if err := s.agentClient.FilesRemove(r.Context(), body.Paths); err != nil {
		writeFileAgentError(w, r, "删除文件", "根目录", strings.Join(body.Paths, ", "), err)
		return
	}

	for _, p := range body.Paths {
		s.recordFileOperation(r.Context(), chi.URLParam(r, "site_id"), "file.remove", p)
	}

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Mkdir — 创建目录
// ============================================================

// handleFilesMkdir 创建目录
//
// POST /api/v1/sites/{site_id}/files/mkdir
// Body: {"path":"..."}
func (s *Server) handleFilesMkdir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if !DecodeJSON(w, r, &body) {
		return
	}

	if body.Path == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "path 不能为空", nil)
		return
	}

	if err := s.agentClient.FilesMkdir(r.Context(), body.Path); err != nil {
		writeFileAgentError(w, r, "创建目录", "根目录", body.Path, err)
		return
	}

	s.recordFileOperation(r.Context(), chi.URLParam(r, "site_id"), "file.mkdir", body.Path)

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Move — 移动/重命名
// ============================================================

// handleFilesMove 移动或重命名文件/目录
//
// POST /api/v1/sites/{site_id}/files/move
// Body: {"source":"...", "destination":"..."}
func (s *Server) handleFilesMove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}
	if !DecodeJSON(w, r, &body) {
		return
	}

	if body.Source == "" || body.Destination == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "source 和 destination 不能为空", nil)
		return
	}

	if err := s.agentClient.FilesMove(r.Context(), body.Source, body.Destination); err != nil {
		writeFileAgentError(w, r, "移动文件", "根目录", fmt.Sprintf("%s -> %s", body.Source, body.Destination), err)
		return
	}

	s.recordFileOperation(r.Context(), chi.URLParam(r, "site_id"), "file.move",
		fmt.Sprintf("%s -> %s", body.Source, body.Destination))

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Copy — 批量复制
// ============================================================

// handleFilesCopy 批量复制文件/目录到目标目录
//
// POST /api/v1/sites/{site_id}/files/copy
// Body: {"paths":["...","..."], "dest_dir":"..."}
func (s *Server) handleFilesCopy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths   []string `json:"paths"`
		DestDir string   `json:"dest_dir"`
	}
	if !DecodeJSON(w, r, &body) {
		return
	}

	if len(body.Paths) == 0 || body.DestDir == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "paths 和 dest_dir 不能为空", nil)
		return
	}

	if err := s.agentClient.FilesCopy(r.Context(), body.Paths, body.DestDir); err != nil {
		writeFileAgentError(w, r, "复制文件", "根目录", fmt.Sprintf("%v -> %s", body.Paths, body.DestDir), err)
		return
	}

	detail := fmt.Sprintf("copy %v -> %s", body.Paths, body.DestDir)
	s.recordFileOperation(r.Context(), chi.URLParam(r, "site_id"), "file.copy", detail)

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Download — 下载文件（流式）
// ============================================================

// handleFilesDownload 下载文件，流式代理 agent 响应
//
// GET /api/v1/sites/{site_id}/files/download?path=...
func (s *Server) handleFilesDownload(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "path 不能为空", nil)
		return
	}

	agentResp, err := s.agentClient.FilesDownload(r.Context(), path)
	if err != nil {
		writeFileAgentError(w, r, "下载文件", "根目录", path, err)
		return
	}
	defer agentResp.Body.Close()

	// 透传 agent 的 Content-Type, Content-Disposition, Content-Length
	for _, h := range []string{"Content-Type", "Content-Disposition", "Content-Length"} {
		if v := agentResp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(http.StatusOK)
	io.Copy(w, agentResp.Body)
}

// ============================================================
// Archive — 打包下载（流式 ZIP）
// ============================================================

// handleFilesArchive 打包下载 ZIP，流式代理 agent 响应
//
// GET /api/v1/sites/{site_id}/files/archive?paths=path1,path2
func (s *Server) handleFilesArchive(w http.ResponseWriter, r *http.Request) {
	rawPaths := r.URL.Query().Get("paths")
	if rawPaths == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "paths 不能为空", nil)
		return
	}

	paths := strings.Split(rawPaths, ",")

	agentResp, err := s.agentClient.FilesArchive(r.Context(), paths)
	if err != nil {
		writeFileAgentError(w, r, "打包文件", "根目录", strings.Join(paths, ", "), err)
		return
	}
	defer agentResp.Body.Close()

	for _, h := range []string{"Content-Type", "Content-Disposition", "Content-Length"} {
		if v := agentResp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(http.StatusOK)
	io.Copy(w, agentResp.Body)
}

// ============================================================
// Upload — 上传文件
// ============================================================

// handleFilesUpload 上传文件（接收 base64 编码的文件内容）
//
// POST /api/v1/sites/{site_id}/files/upload
// Body: {"path":"...", "content_base64":"..."}
// 使用 api.upload_timeout 配置覆盖默认超时，支持大文件上传
func (s *Server) handleFilesUpload(w http.ResponseWriter, r *http.Request) {
	uploadTimeout := app.ParseDurationOrDefault(s.cfg.API.UploadTimeout, 300*time.Second)
	ctx, cancel := context.WithTimeout(r.Context(), uploadTimeout)
	defer cancel()

	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/form-data") {
		// multipart 上传模式
		if err := r.ParseMultipartForm(100 << 20); err != nil { // 100MB max
			WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "解析上传表单失败: "+err.Error(), nil)
			return
		}

		targetPath := r.FormValue("path")
		if targetPath == "" {
			WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "path 字段不能为空", nil)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "读取上传文件失败: "+err.Error(), nil)
			return
		}
		defer file.Close()

		// 如果 path 是目录，拼接文件名
		if strings.HasSuffix(targetPath, "/") {
			targetPath = targetPath + header.Filename
		}

		data, err := io.ReadAll(file)
		if err != nil {
			WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, "读取文件内容失败: "+err.Error(), nil)
			return
		}

		contentBase64 := base64.StdEncoding.EncodeToString(data)
		if err := s.agentClient.FilesUploadWithTimeout(ctx, targetPath, contentBase64, uploadTimeout); err != nil {
			writeFileAgentError(w, r, "上传文件", "根目录", targetPath, err)
			return
		}

		s.recordFileOperation(r.Context(), chi.URLParam(r, "site_id"), "file.upload", targetPath)

		WriteOK(w, r, map[string]any{"success": true, "path": targetPath, "size": len(data)})
		return
	}

	// JSON 模式
	var body struct {
		Path          string `json:"path"`
		ContentBase64 string `json:"content_base64"`
	}
	if !DecodeJSON(w, r, &body) {
		return
	}

	if body.Path == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "path 不能为空", nil)
		return
	}

	if err := s.agentClient.FilesUploadWithTimeout(ctx, body.Path, body.ContentBase64, uploadTimeout); err != nil {
		writeFileAgentError(w, r, "上传文件", "根目录", body.Path, err)
		return
	}

	s.recordFileOperation(r.Context(), chi.URLParam(r, "site_id"), "file.upload", body.Path)

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Chmod — 修改文件/目录权限
// ============================================================

// handleFilesChmod 修改文件/目录权限
//
// POST /api/v1/sites/{site_id}/files/chmod
func (s *Server) handleFilesChmod(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")

	var body struct {
		Path      string `json:"path"`
		Mode      string `json:"mode"`
		Recursive bool   `json:"recursive"`
	}
	if !DecodeJSON(w, r, &body) {
		return
	}
	if body.Path == "" || body.Mode == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "path 和 mode 不能为空", nil)
		return
	}

	if err := s.agentClient.FilesChmod(r.Context(), body.Path, body.Mode, body.Recursive); err != nil {
		writeFileAgentError(w, r, "修改权限", "根目录", body.Path, err)
		return
	}

	detail := fmt.Sprintf("%s → %s (recursive=%v)", body.Path, body.Mode, body.Recursive)
	s.recordFileOperation(r.Context(), siteID, "file.chmod", detail)

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Chown — 修改文件/目录所有者
// ============================================================

// handleFilesChown 修改文件/目录所有者
//
// POST /api/v1/sites/{site_id}/files/chown
func (s *Server) handleFilesChown(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")

	var body struct {
		Path      string `json:"path"`
		Owner     string `json:"owner"`
		Group     string `json:"group"`
		Recursive bool   `json:"recursive"`
	}
	if !DecodeJSON(w, r, &body) {
		return
	}
	if body.Path == "" || body.Owner == "" || body.Group == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "path、owner、group 不能为空", nil)
		return
	}

	if err := s.agentClient.FilesChown(r.Context(), body.Path, body.Owner, body.Group, body.Recursive); err != nil {
		writeFileAgentError(w, r, "修改所有者", "根目录", body.Path, err)
		return
	}

	detail := fmt.Sprintf("%s → %s:%s (recursive=%v)", body.Path, body.Owner, body.Group, body.Recursive)
	s.recordFileOperation(r.Context(), siteID, "file.chown", detail)

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Compress — 压缩文件/目录
// ============================================================

// ============================================================
// Compress — 压缩文件/目录
// ============================================================

// handleFilesCompress 压缩文件/目录到存档
//
// POST /api/v1/sites/{site_id}/files/compress
func (s *Server) handleFilesCompress(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")

	var body struct {
		Paths      []string `json:"paths"`
		OutputPath string   `json:"output_path"`
		Format     string   `json:"format"`
	}
	if !DecodeJSON(w, r, &body) {
		return
	}
	if len(body.Paths) == 0 || body.OutputPath == "" || body.Format == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "paths、output_path、format 不能为空", nil)
		return
	}

	if err := s.agentClient.FilesCompress(r.Context(), body.Paths, body.OutputPath, body.Format); err != nil {
		writeFileAgentError(w, r, "压缩文件", "根目录", body.OutputPath, err)
		return
	}

	detail := fmt.Sprintf("compress %v → %s (%s)", body.Paths, body.OutputPath, body.Format)
	s.recordFileOperation(r.Context(), siteID, "file.compress", detail)

	WriteOK(w, r, map[string]any{"success": true, "output_path": body.OutputPath})
}

// ============================================================
// Extract — 解压文件
// ============================================================

// handleFilesExtract 解压存档到目录
//
// POST /api/v1/sites/{site_id}/files/extract
func (s *Server) handleFilesExtract(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")

	var body struct {
		ArchivePath string `json:"archive_path"`
		DestDir     string `json:"dest_dir"`
	}
	if !DecodeJSON(w, r, &body) {
		return
	}
	if body.ArchivePath == "" || body.DestDir == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "archive_path 和 dest_dir 不能为空", nil)
		return
	}

	if err := s.agentClient.FilesExtract(r.Context(), body.ArchivePath, body.DestDir); err != nil {
		writeFileAgentError(w, r, "解压文件", "根目录", body.DestDir, err)
		return
	}

	detail := fmt.Sprintf("extract %s → %s", body.ArchivePath, body.DestDir)
	s.recordFileOperation(r.Context(), siteID, "file.extract", detail)

	WriteOK(w, r, map[string]any{"success": true, "dest_dir": body.DestDir})
}

// ============================================================
// 文件操作记录（写操作写入操作日志）
// ============================================================

func (s *Server) recordFileOperation(ctx context.Context, siteID, action, detail string) {
	if s.opRepo == nil || siteID == "" {
		return
	}

	op := &repo.Operation{
		ID:         app.NewOperationID(),
		Action:     action,
		TargetType: "site",
		TargetID:   siteID,
		Status:     "success",
		RequestID:  middleware.GetRequestID(ctx),
		Message:    detail,
	}
	if err := s.opRepo.Create(op); err != nil {
		// 记录失败不影响主流程
	}
}

// ============================================================
// 辅助 — 构建下载 URL
// ============================================================

// buildDownloadURL 构建前端可用的文件下载 API URL
func buildDownloadURL(siteID, filePath string) string {
	return fmt.Sprintf("/api/v1/sites/%s/files/download?%s",
		siteID, url.Values{"path": {filePath}}.Encode())
}

// buildArchiveURL 构建前端可用的打包下载 API URL
func buildArchiveURL(siteID string, paths []string) string {
	return fmt.Sprintf("/api/v1/sites/%s/files/archive?%s",
		siteID, url.Values{"paths": {strings.Join(paths, ",")}}.Encode())
}
