// api 包 — 全局文件管理 handler
//
// 处理站点无关的文件管理接口，供侧边栏「文件管理」页面使用：
//   - GET    /api/v1/files/roots               列出白名单根目录
//   - GET    /api/v1/files/list?path=           列出目录
//   - GET    /api/v1/files/read?path=           读取文件
//   - POST   /api/v1/files/write                写入文件
//   - POST   /api/v1/files/remove               删除文件/目录
//   - POST   /api/v1/files/mkdir                创建目录
//   - POST   /api/v1/files/move                 移动/重命名
//   - GET    /api/v1/files/download?path=        下载文件（流式）
//   - GET    /api/v1/files/archive?paths=        打包下载（流式）
//   - POST   /api/v1/files/upload               上传文件
//
// 与 site-scoped 文件接口 (file_handler.go) 功能相同，
// 但不绑定 site_id，写操作使用 file_global 目标类型记录操作审计。
package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

// ============================================================
// Roots — 白名单根目录列表
// ============================================================

// handleGlobalFilesRoots 获取所有白名单根目录
//
// GET /api/v1/files/roots
func (s *Server) handleGlobalFilesRoots(w http.ResponseWriter, r *http.Request) {
	result, err := s.agentClient.FilesRoots(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
		return
	}
	WriteOK(w, r, map[string]any{"roots": result.Roots})
}

// ============================================================
// List — 列出目录
// ============================================================

// handleGlobalFilesList 列出目录内容
//
// GET /api/v1/files/list?path=/www/wwwroot
func (s *Server) handleGlobalFilesList(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "path 不能为空", nil)
		return
	}

	result, err := s.agentClient.FilesList(r.Context(), path)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
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

// handleGlobalFilesRead 读取文件内容，返回 base64 编码
//
// GET /api/v1/files/read?path=...
func (s *Server) handleGlobalFilesRead(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "path 不能为空", nil)
		return
	}

	result, err := s.agentClient.FilesRead(r.Context(), path)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
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

// handleGlobalFilesWrite 写入文件内容
//
// POST /api/v1/files/write
// Body: {"path":"...", "content_base64":"..."}
func (s *Server) handleGlobalFilesWrite(w http.ResponseWriter, r *http.Request) {
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
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
		return
	}
	s.recordGlobalFileOperation(r.Context(), "file.write", body.Path)

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Remove — 删除文件/目录
// ============================================================

// handleGlobalFilesRemove 批量删除
//
// POST /api/v1/files/remove
// Body: {"paths":[...]}
func (s *Server) handleGlobalFilesRemove(w http.ResponseWriter, r *http.Request) {
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
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
		return
	}
	for _, path := range body.Paths {
		s.recordGlobalFileOperation(r.Context(), "file.remove", path)
	}

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Mkdir — 创建目录
// ============================================================

// handleGlobalFilesMkdir 创建目录
//
// POST /api/v1/files/mkdir
// Body: {"path":"..."}
func (s *Server) handleGlobalFilesMkdir(w http.ResponseWriter, r *http.Request) {
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
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
		return
	}
	s.recordGlobalFileOperation(r.Context(), "file.mkdir", body.Path)

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Move — 移动/重命名
// ============================================================

// handleGlobalFilesMove 移动/重命名
//
// POST /api/v1/files/move
// Body: {"source":"...", "destination":"..."}
func (s *Server) handleGlobalFilesMove(w http.ResponseWriter, r *http.Request) {
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
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
		return
	}
	s.recordGlobalFileOperation(r.Context(), "file.move", fmt.Sprintf("%s -> %s", body.Source, body.Destination))

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Copy — 批量复制
// ============================================================

// handleGlobalFilesCopy 批量复制文件/目录到目标目录
//
// POST /api/v1/files/copy
// Body: {"paths":["...","..."], "dest_dir":"..."}
func (s *Server) handleGlobalFilesCopy(w http.ResponseWriter, r *http.Request) {
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
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
		return
	}
	s.recordGlobalFileOperation(r.Context(), "file.copy", fmt.Sprintf("copy %v -> %s", body.Paths, body.DestDir))

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Download — 下载文件（流式）
// ============================================================

// handleGlobalFilesDownload 下载文件
//
// GET /api/v1/files/download?path=...
func (s *Server) handleGlobalFilesDownload(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "path 不能为空", nil)
		return
	}

	agentResp, err := s.agentClient.FilesDownload(r.Context(), path)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
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
// Archive — 打包下载（流式 ZIP）
// ============================================================

// handleGlobalFilesArchive 打包下载 ZIP
//
// GET /api/v1/files/archive?paths=path1,path2
func (s *Server) handleGlobalFilesArchive(w http.ResponseWriter, r *http.Request) {
	rawPaths := r.URL.Query().Get("paths")
	if rawPaths == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "paths 不能为空", nil)
		return
	}
	paths := strings.Split(rawPaths, ",")

	agentResp, err := s.agentClient.FilesArchive(r.Context(), paths)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
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

// handleGlobalFilesUpload 上传文件
//
// POST /api/v1/files/upload
// Body (JSON): {"path":"...", "content_base64":"..."}
// Or Body (multipart): path + file
// 使用 api.upload_timeout 配置覆盖默认超时，支持大文件上传
func (s *Server) handleGlobalFilesUpload(w http.ResponseWriter, r *http.Request) {
	uploadTimeout := app.ParseDurationOrDefault(s.cfg.API.UploadTimeout, 300*time.Second)
	ctx, cancel := context.WithTimeout(r.Context(), uploadTimeout)
	defer cancel()

	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(100 << 20); err != nil {
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
			WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
			return
		}
		s.recordGlobalFileOperation(r.Context(), "file.upload", targetPath)

		WriteOK(w, r, map[string]any{"success": true, "path": targetPath, "size": len(data)})
		return
	}

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
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
		return
	}
	s.recordGlobalFileOperation(r.Context(), "file.upload", body.Path)

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Chmod — 修改文件/目录权限
// ============================================================

// handleGlobalFilesChmod 修改文件/目录权限（全局）
//
// POST /api/v1/files/chmod
func (s *Server) handleGlobalFilesChmod(w http.ResponseWriter, r *http.Request) {
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
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
		return
	}
	s.recordGlobalFileOperation(r.Context(), "file.chmod", fmt.Sprintf("%s -> %s (recursive=%v)", body.Path, body.Mode, body.Recursive))

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Chown — 修改文件/目录所有者
// ============================================================

// handleGlobalFilesChown 修改文件/目录所有者（全局）
//
// POST /api/v1/files/chown
func (s *Server) handleGlobalFilesChown(w http.ResponseWriter, r *http.Request) {
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
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
		return
	}
	s.recordGlobalFileOperation(r.Context(), "file.chown", fmt.Sprintf("%s -> %s:%s (recursive=%v)", body.Path, body.Owner, body.Group, body.Recursive))

	WriteOK(w, r, map[string]any{"success": true})
}

// ============================================================
// Compress — 压缩文件/目录
// ============================================================

// handleGlobalFilesCompress 压缩文件/目录到存档（全局）
//
// POST /api/v1/files/compress
func (s *Server) handleGlobalFilesCompress(w http.ResponseWriter, r *http.Request) {
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
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
		return
	}
	s.recordGlobalFileOperation(r.Context(), "file.compress", fmt.Sprintf("compress %v -> %s (%s)", body.Paths, body.OutputPath, body.Format))

	WriteOK(w, r, map[string]any{"success": true, "output_path": body.OutputPath})
}

// ============================================================
// Extract — 解压文件
// ============================================================

// handleGlobalFilesExtract 解压存档到目录（全局）
//
// POST /api/v1/files/extract
func (s *Server) handleGlobalFilesExtract(w http.ResponseWriter, r *http.Request) {
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
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
		return
	}
	s.recordGlobalFileOperation(r.Context(), "file.extract", fmt.Sprintf("extract %s -> %s", body.ArchivePath, body.DestDir))

	WriteOK(w, r, map[string]any{"success": true, "dest_dir": body.DestDir})
}

// recordGlobalFileOperation 记录全局文件写操作审计。
// 这里只保存路径、权限、目标等元信息，避免把文件正文、密钥或 token 写入审计表。
func (s *Server) recordGlobalFileOperation(ctx context.Context, action, detail string) {
	if s.opRepo == nil {
		return
	}

	op := &repo.Operation{
		ID:         app.NewOperationID(),
		Action:     action,
		TargetType: "file_global",
		TargetID:   "global",
		Status:     "success",
		RequestID:  middleware.GetRequestID(ctx),
		Message:    detail,
	}
	if err := s.opRepo.Create(op); err != nil {
		// 审计失败不能回滚已经成功的 root 文件操作，主流程保持成功返回。
	}
}
