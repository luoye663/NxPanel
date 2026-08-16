package agent

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/upload"
)

// ============================================================
// 请求 / 响应类型
// ============================================================

type FilesListRequest struct {
	Path string `json:"path"`
}

type FileEntry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
	IsDir   bool   `json:"is_dir"`
	Mode    string `json:"mode"`
	Owner   string `json:"owner"`
	Group   string `json:"group"`
}

type FilesListResponse struct {
	Entries []FileEntry `json:"entries"`
}

type FilesReadRequest struct {
	Path string `json:"path"`
}

type FilesReadResponse struct {
	ContentBase64 string `json:"content_base64"`
	Size          int64  `json:"size"`
	Encoding      string `json:"encoding"`
}

type FilesWriteRequest struct {
	Path          string `json:"path"`
	ContentBase64 string `json:"content_base64"`
}

type FilesRemoveRequest struct {
	Paths []string `json:"paths"`
}

type FilesMkdirRequest struct {
	Path string `json:"path"`
}

type FilesMoveRequest struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type FilesUploadRequest struct {
	Path          string `json:"path"`
	ContentBase64 string `json:"content_base64"`
}

type FilesChmodRequest struct {
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	Recursive bool   `json:"recursive"`
}

type FilesChownRequest struct {
	Path      string `json:"path"`
	Owner     string `json:"owner"`
	Group     string `json:"group"`
	Recursive bool   `json:"recursive"`
}

type FilesCompressRequest struct {
	Paths      []string `json:"paths"`
	OutputPath string   `json:"output_path"`
	Format     string   `json:"format"`
}

type FilesExtractRequest struct {
	ArchivePath string `json:"archive_path"`
	DestDir     string `json:"dest_dir"`
}

type FilesCopyRequest struct {
	Paths   []string `json:"paths"`
	DestDir string   `json:"dest_dir"`
}

// ============================================================
// 文件列表
// ============================================================

func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	var req FilesListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	path, err := s.policy.Validate(req.Path)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不在白名单内: "+err.Error())
		return
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "读取目录失败: "+err.Error())
		return
	}

	resp := FilesListResponse{Entries: make([]FileEntry, 0, len(entries))}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		owner, group := lookupOwnerGroup(info)
		resp.Entries = append(resp.Entries, FileEntry{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format(time.RFC3339),
			IsDir:   e.IsDir(),
			Mode:    fmt.Sprintf("%o", info.Mode().Perm()),
			Owner:   owner,
			Group:   group,
		})
	}

	writeAgentOK(w, resp)
}

// ============================================================
// 文件读取
// ============================================================

func (s *Server) handleFilesRead(w http.ResponseWriter, r *http.Request) {
	var req FilesReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	path, err := s.policy.Validate(req.Path)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不在白名单内: "+err.Error())
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "获取文件信息失败: "+err.Error())
		return
	}
	if info.IsDir() {
		writeAgentError(w, http.StatusBadRequest, "不能读取目录")
		return
	}
	maxReadBytes := s.maxReadBytes()
	if info.Size() > maxReadBytes {
		writeAgentError(w, http.StatusBadRequest, fmt.Sprintf("文件过大，最多允许读取 %d MB", maxReadBytes/(1024*1024)))
		return
	}

	data, err := readFileWithinLimit(path, maxReadBytes)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "读取文件失败: "+err.Error())
		return
	}

	writeAgentOK(w, FilesReadResponse{
		ContentBase64: base64.StdEncoding.EncodeToString(data),
		Size:          int64(len(data)),
		Encoding:      "base64",
	})
}

// ============================================================
// 文件写入
// ============================================================

func (s *Server) handleFilesWrite(w http.ResponseWriter, r *http.Request) {
	var req FilesWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	path, err := s.policy.Validate(req.Path)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不在白名单内: "+err.Error())
		return
	}

	content, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, "base64 解码失败: "+err.Error())
		return
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "创建目录失败: "+err.Error())
		return
	}

	if err := writeFileAtomic(path, content, 0644); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "写入文件失败: "+err.Error())
		return
	}

	s.applyWebOwner(path)

	slog.Info("文件已写入", "path", path)
	writeAgentOK(w, map[string]any{"success": true})
}

// ============================================================
// 文件/目录删除
// ============================================================

func (s *Server) handleFilesRemove(w http.ResponseWriter, r *http.Request) {
	var req FilesRemoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	if len(req.Paths) == 0 {
		writeAgentError(w, http.StatusBadRequest, "paths 不能为空")
		return
	}

	var removed int
	for _, p := range req.Paths {
		path, err := s.policy.Validate(p)
		if err != nil {
			slog.Warn("删除路径不在白名单内，跳过", "path", p)
			continue
		}

		if err := os.RemoveAll(path); err != nil {
			slog.Error("删除失败", "path", path, "error", err)
			continue
		}
		removed++
	}

	slog.Info("文件/目录已删除", "total", len(req.Paths), "removed", removed)
	writeAgentOK(w, map[string]any{"removed": removed})
}

// ============================================================
// 创建目录
// ============================================================

func (s *Server) handleFilesMkdir(w http.ResponseWriter, r *http.Request) {
	var req FilesMkdirRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	path, err := s.policy.Validate(req.Path)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不在白名单内: "+err.Error())
		return
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "创建目录失败: "+err.Error())
		return
	}

	s.applyWebOwner(path)

	slog.Info("目录已创建", "path", path)
	writeAgentOK(w, map[string]any{"success": true})
}

// ============================================================
// 移动/重命名
// ============================================================

func (s *Server) handleFilesMove(w http.ResponseWriter, r *http.Request) {
	var req FilesMoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	source, err := s.policy.Validate(req.Source)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "源路径不在白名单内: "+err.Error())
		return
	}

	dest, err := s.policy.Validate(req.Destination)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "目标路径不在白名单内: "+err.Error())
		return
	}

	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "创建目录失败: "+err.Error())
		return
	}

	if err := os.Rename(source, dest); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "移动失败: "+err.Error())
		return
	}

	slog.Info("文件/目录已移动", "source", source, "dest", dest)
	writeAgentOK(w, map[string]any{"success": true})
}

func (s *Server) handleFilesCopy(w http.ResponseWriter, r *http.Request) {
	var req FilesCopyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	if len(req.Paths) == 0 || req.DestDir == "" {
		writeAgentError(w, http.StatusBadRequest, "paths 和 dest_dir 不能为空")
		return
	}

	destDir, err := s.policy.Validate(req.DestDir)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "目标路径不在白名单内: "+err.Error())
		return
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "创建目标目录失败: "+err.Error())
		return
	}

	var copied int
	for _, p := range req.Paths {
		srcPath, err := s.policy.Validate(p)
		if err != nil {
			slog.Warn("复制源路径不在白名单内，跳过", "path", p, "error", err)
			continue
		}
		targetPath := filepath.Join(destDir, filepath.Base(srcPath))
		if pathsOverlap(srcPath, targetPath) {
			writeAgentError(w, http.StatusBadRequest, "复制目标不能与源路径重叠")
			return
		}
		if err := copyRecursive(srcPath, targetPath); err != nil {
			slog.Error("复制失败", "source", srcPath, "target", targetPath, "error", err)
			writeAgentError(w, http.StatusInternalServerError, fmt.Sprintf("复制 %s 失败: %s", filepath.Base(srcPath), err.Error()))
			return
		}
		s.applyWebOwner(targetPath)
		copied++
	}

	slog.Info("文件/目录已复制", "total", len(req.Paths), "copied", copied, "dest", destDir)
	writeAgentOK(w, map[string]any{"success": true, "copied": copied})
}

func copyRecursive(src, dst string) error {
	if pathsOverlap(src, dst) {
		return fmt.Errorf("复制目标不能与源路径重叠")
	}
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("获取源文件信息失败: %w", err)
	}
	if err := rejectSymlinkForArchive(src, info); err != nil {
		return err
	}

	if !info.IsDir() {
		return copyFile(src, dst, info)
	}

	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("读取目录失败: %w", err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyRecursive(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			fi, err := entry.Info()
			if err != nil {
				return err
			}
			if err := copyFile(srcPath, dstPath, fi); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string, fi os.FileInfo) error {
	if err := rejectSymlinkForArchive(src, fi); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开源文件失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		_ = srcFile.Close()
		return fmt.Errorf("创建目标目录失败: %w", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fi.Mode())
	if err != nil {
		_ = srcFile.Close()
		return fmt.Errorf("创建目标文件失败: %w", err)
	}
	_, copyErr := io.Copy(dstFile, srcFile)
	dstCloseErr := dstFile.Close()
	srcCloseErr := srcFile.Close()
	if err := joinCopyCloseErrors(copyErr, dstCloseErr, srcCloseErr); err != nil {
		return fmt.Errorf("复制文件失败: %w", err)
	}
	return nil
}

func rejectSymlinkForArchive(path string, fi os.FileInfo) error {
	// Agent 以 root 运行，复制/打包时必须拒绝符号链接，避免跟随到白名单外读取敏感文件。
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("拒绝处理符号链接: %s", path)
	}
	return nil
}

// ============================================================
// 文件下载（流式）
// ============================================================

func (s *Server) handleFilesDownload(w http.ResponseWriter, r *http.Request) {
	rawPath := r.URL.Query().Get("path")
	if rawPath == "" {
		writeAgentError(w, http.StatusBadRequest, "path 参数不能为空")
		return
	}

	path, err := s.policy.Validate(rawPath)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不在白名单内: "+err.Error())
		return
	}

	f, err := os.Open(path)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "打开文件失败: "+err.Error())
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "获取文件信息失败: "+err.Error())
		return
	}
	if stat.IsDir() {
		writeAgentError(w, http.StatusBadRequest, "不能下载目录")
		return
	}
	maxDownloadBytes := s.maxDownloadBytes()
	if stat.Size() > maxDownloadBytes {
		writeAgentError(w, http.StatusBadRequest, fmt.Sprintf("文件过大，最多允许下载 %d MB", maxDownloadBytes/(1024*1024)))
		return
	}

	filename := filepath.Base(path)
	filename = strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r == '\n' || r == '\r' {
			return '_'
		}
		return r
	}, filename)
	contentType := detectContentType(filename)

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.WriteHeader(http.StatusOK)

	ctx, cancel := context.WithTimeout(r.Context(), s.downloadTimeout())
	defer cancel()
	if _, err := copyWithContext(ctx, w, io.LimitReader(f, stat.Size())); err != nil {
		slog.Warn("文件下载中断", "path", path, "error", err)
	}
}

func readFileWithinLimit(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// 即使 Stat 后文件被追加，这里也只多读 1 字节用于判断超限，避免竞态导致一次性读入超大文件。
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("文件超过读取上限 %d MB", maxBytes/(1024*1024))
	}
	return data, nil
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	var written int64
	for {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}

		n, readErr := src.Read(buf)
		if n > 0 {
			wn, writeErr := dst.Write(buf[:n])
			written += int64(wn)
			if writeErr != nil {
				return written, writeErr
			}
			if wn != n {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return written, nil
			}
			return written, readErr
		}
	}
}

// ============================================================
// 打包下载（ZIP 流式）
// ============================================================

func (s *Server) handleFilesArchive(w http.ResponseWriter, r *http.Request) {
	rawPaths := r.URL.Query().Get("paths")
	if rawPaths == "" {
		writeAgentError(w, http.StatusBadRequest, "paths 参数不能为空")
		return
	}

	requestedPaths := strings.Split(rawPaths, ",")

	var validPaths []string
	for _, p := range requestedPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		cleaned, err := s.policy.Validate(p)
		if err != nil {
			slog.Warn("打包路径不在白名单内，跳过", "path", p)
			continue
		}
		validPaths = append(validPaths, cleaned)
	}

	if len(validPaths) == 0 {
		writeAgentError(w, http.StatusBadRequest, "没有有效的打包路径")
		return
	}

	limits := s.archiveLimits()
	ctx, cancel := context.WithTimeout(r.Context(), limits.timeout)
	defer cancel()
	if err := preflightArchiveSources(ctx, validPaths, limits); err != nil {
		writeAgentError(w, http.StatusBadRequest, "打包资源检查失败: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="archive.zip"`)
	w.WriteHeader(http.StatusOK)

	budget := newArchiveBudget(limits)
	zw := zip.NewWriter(&budgetWriter{dst: w, budget: budget})
	if err := writeZipSources(ctx, zw, validPaths, budget); err != nil {
		slog.Warn("流式打包中断", "error", err)
		_ = zw.Close()
		return
	}
	if err := zw.Close(); err != nil {
		slog.Warn("关闭流式压缩包失败", "error", err)
	}
}

// ============================================================
// 文件上传（接收 base64，写入文件）
// ============================================================

func (s *Server) handleFilesUpload(w http.ResponseWriter, r *http.Request) {
	maxBytes := app.ParseSizeOrDefault(s.cfg.API.MaxUploadSize, 100*1024*1024)
	if maxBytes <= 0 {
		maxBytes = 100 * 1024 * 1024
	}
	file, err := upload.Parse(r, maxBytes)
	if err != nil {
		if errors.Is(err, upload.ErrTooLarge) {
			writeAgentError(w, http.StatusRequestEntityTooLarge, "上传文件超过大小限制")
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(r.Context().Err(), context.DeadlineExceeded) || isNetworkTimeout(err) {
			writeAgentError(w, http.StatusRequestTimeout, "上传读取超时")
		} else {
			writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		}
		return
	}
	defer file.Close()

	path, err := s.policy.Validate(file.Path)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不在白名单内: "+err.Error())
		return
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "创建目录失败: "+err.Error())
		return
	}

	if err := writeFileAtomicFromReader(r.Context(), path, file.File, 0644); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "写入文件失败: "+err.Error())
		return
	}

	s.applyWebOwner(path)

	slog.Info("文件已上传", "path", path, "size", file.Size)
	writeAgentOK(w, map[string]any{"success": true, "size": file.Size})
}

func isNetworkTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// ============================================================
// 修改权限
// ============================================================

func (s *Server) handleFilesChmod(w http.ResponseWriter, r *http.Request) {
	var req FilesChmodRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	path, err := s.policy.Validate(req.Path)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不在白名单内: "+err.Error())
		return
	}

	mode, err := strconv.ParseUint(req.Mode, 8, 32)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, "权限格式错误: "+err.Error())
		return
	}

	if req.Recursive {
		info, err := os.Stat(path)
		if err != nil {
			writeAgentError(w, http.StatusInternalServerError, "获取文件信息失败: "+err.Error())
			return
		}
		if !info.IsDir() {
			req.Recursive = false
		}
	}

	chmodFn := func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chmod(p, os.FileMode(mode))
	}

	if req.Recursive {
		err = filepath.Walk(path, chmodFn)
	} else {
		err = os.Chmod(path, os.FileMode(mode))
	}

	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "修改权限失败: "+err.Error())
		return
	}

	slog.Info("权限已修改", "path", path, "mode", req.Mode, "recursive", req.Recursive)
	writeAgentOK(w, map[string]any{"success": true})
}

// ============================================================
// 修改所有者
// ============================================================

func resolveUser(name string) (int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		uid, err := strconv.Atoi(name)
		if err != nil {
			return 0, fmt.Errorf("无法解析用户: %s", name)
		}
		return uid, nil
	}
	return strconv.Atoi(u.Uid)
}

func resolveGroup(name string) (int, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		gid, err := strconv.Atoi(name)
		if err != nil {
			return 0, fmt.Errorf("无法解析用户组: %s", name)
		}
		return gid, nil
	}
	return strconv.Atoi(g.Gid)
}

func (s *Server) handleFilesChown(w http.ResponseWriter, r *http.Request) {
	var req FilesChownRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	path, err := s.policy.Validate(req.Path)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不在白名单内: "+err.Error())
		return
	}

	uid, err := resolveUser(req.Owner)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, "所有者解析失败: "+err.Error())
		return
	}

	gid, err := resolveGroup(req.Group)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, "用户组解析失败: "+err.Error())
		return
	}

	if req.Recursive {
		info, err := os.Stat(path)
		if err != nil {
			writeAgentError(w, http.StatusInternalServerError, "获取文件信息失败: "+err.Error())
			return
		}
		if !info.IsDir() {
			req.Recursive = false
		}
	}

	chownFn := func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(p, uid, gid)
	}

	if req.Recursive {
		err = filepath.Walk(path, chownFn)
	} else {
		err = os.Chown(path, uid, gid)
	}

	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "修改所有者失败: "+err.Error())
		return
	}

	slog.Info("所有者已修改", "path", path, "owner", req.Owner, "group", req.Group, "recursive", req.Recursive)
	writeAgentOK(w, map[string]any{"success": true})
}

// ============================================================
// 压缩文件/目录
// ============================================================

func (s *Server) handleFilesCompress(w http.ResponseWriter, r *http.Request) {
	var req FilesCompressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	if len(req.Paths) == 0 || req.OutputPath == "" {
		writeAgentError(w, http.StatusBadRequest, "paths 和 output_path 不能为空")
		return
	}

	var validPaths []string
	for _, p := range req.Paths {
		cleaned, err := s.policy.Validate(p)
		if err != nil {
			writeAgentError(w, http.StatusForbidden, "路径不在白名单内: "+p)
			return
		}
		validPaths = append(validPaths, cleaned)
	}

	outputPath, err := s.policy.Validate(req.OutputPath)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "输出路径不在白名单内: "+err.Error())
		return
	}

	format := strings.ToLower(req.Format)
	switch format {
	case "zip", "tar.gz", "tgz":
	default:
		writeAgentError(w, http.StatusBadRequest, "不支持的格式，仅支持 zip / tar.gz")
		return
	}

	for _, source := range validPaths {
		if pathsOverlap(source, outputPath) {
			writeAgentError(w, http.StatusBadRequest, "输出路径不能与压缩源重叠")
			return
		}
	}
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "创建输出目录失败: "+err.Error())
		return
	}

	switch format {
	case "zip":
		if err := compressToZipWithLimits(r.Context(), validPaths, outputPath, s.archiveLimits()); err != nil {
			writeAgentError(w, http.StatusInternalServerError, "压缩失败: "+err.Error())
			return
		}
	case "tar.gz", "tgz":
		if err := compressToTarGzWithLimits(r.Context(), validPaths, outputPath, s.archiveLimits()); err != nil {
			writeAgentError(w, http.StatusInternalServerError, "压缩失败: "+err.Error())
			return
		}
	}

	slog.Info("压缩完成", "output", outputPath, "files", len(validPaths), "format", format)
	writeAgentOK(w, map[string]any{"success": true, "output_path": outputPath})
}

func compressToZip(paths []string, outputPath string) error {
	return compressToZipWithLimits(context.Background(), paths, outputPath, defaultArchiveLimits())
}

func compressToTarGz(paths []string, outputPath string) error {
	return compressToTarGzWithLimits(context.Background(), paths, outputPath, defaultArchiveLimits())
}

// ============================================================
// 解压文件
// ============================================================

func (s *Server) handleFilesExtract(w http.ResponseWriter, r *http.Request) {
	var req FilesExtractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	if req.ArchivePath == "" || req.DestDir == "" {
		writeAgentError(w, http.StatusBadRequest, "archive_path 和 dest_dir 不能为空")
		return
	}

	archivePath, err := s.policy.Validate(req.ArchivePath)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "压缩包路径不在白名单内: "+err.Error())
		return
	}

	destDir, err := s.policy.Validate(req.DestDir)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "目标目录不在白名单内: "+err.Error())
		return
	}

	if pathsOverlap(archivePath, destDir) {
		writeAgentError(w, http.StatusBadRequest, "压缩包路径与解压目录不能重叠")
		return
	}
	ext := strings.ToLower(filepath.Ext(archivePath))
	ctx, cancel := context.WithTimeout(r.Context(), s.archiveLimits().timeout)
	defer cancel()
	switch ext {
	case ".zip":
		err = extractZipWithLimits(ctx, s.policy, archivePath, destDir, s.archiveLimits())
	case ".tar":
		err = extractTarWithLimits(ctx, s.policy, archivePath, destDir, false, s.archiveLimits())
	case ".gz", ".tgz":
		err = extractTarWithLimits(ctx, s.policy, archivePath, destDir, true, s.archiveLimits())
	default:
		writeAgentError(w, http.StatusBadRequest, "不支持的压缩格式，仅支持 zip / tar / tar.gz")
		return
	}

	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "解压失败: "+err.Error())
		return
	}

	// 将解压目录及所有文件设为 web 所有者
	if isWebRoot(destDir, s.cfg.Nginx.AllowedRootPrefixes) {
		filepath.Walk(destDir, func(fp string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			s.applyWebOwner(fp)
			return nil
		})
	}

	slog.Info("解压完成", "archive", archivePath, "dest", destDir)
	writeAgentOK(w, map[string]any{"success": true, "dest_dir": destDir})
}

func extractZip(policy *PathPolicy, archivePath, destDir string) error {
	return extractZipWithLimits(context.Background(), policy, archivePath, destDir, defaultArchiveLimits())
}

func extractTar(policy *PathPolicy, archivePath, destDir string, gzipped bool) error {
	return extractTarWithLimits(context.Background(), policy, archivePath, destDir, gzipped, defaultArchiveLimits())
}

func safeExtractTarget(policy *PathPolicy, destDir, entryName string) (string, error) {
	// 归档条目来自外部输入，先拒绝空名、绝对路径、.. 和 Windows 反斜杠穿越。
	if entryName == "" || strings.Contains(entryName, "\\") || filepath.IsAbs(entryName) {
		return "", fmt.Errorf("非法文件路径: %s", entryName)
	}
	cleanEntry := filepath.Clean(entryName)
	if cleanEntry == "." || cleanEntry == ".." || strings.HasPrefix(cleanEntry, "../") {
		return "", fmt.Errorf("非法文件路径: %s", entryName)
	}

	target := filepath.Join(destDir, cleanEntry)
	// 第一层限制：目标必须仍在本次解压目录内，防止条目逃逸到同一白名单的其它目录。
	if _, err := NewPathPolicy([]string{destDir}).Validate(target); err != nil {
		return "", fmt.Errorf("非法文件路径: %s", entryName)
	}
	// 第二层限制：复用全局 PathPolicy，解析最近存在父目录，阻断 destDir/link-out/new 这类 symlink 绕出。
	cleanTarget, err := policy.Validate(target)
	if err != nil {
		return "", fmt.Errorf("非法文件路径: %s", entryName)
	}
	return cleanTarget, nil
}

// ============================================================
// 白名单根目录列表
// ============================================================

func (s *Server) handleFilesRoots(w http.ResponseWriter, r *http.Request) {
	roots := s.policy.Roots()
	writeAgentOK(w, map[string]any{"roots": roots})
}

// ============================================================
// 辅助
// ============================================================

// isWebRoot 检查路径是否在 web 目录下
func isWebRoot(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// applyWebOwner 将文件/目录的所有者设置为 web_user/web_group
// 只在路径属于 web 根目录时执行
func (s *Server) applyWebOwner(path string) {
	prefixes := s.cfg.Nginx.AllowedRootPrefixes
	if len(prefixes) == 0 {
		prefixes = []string{"/www/wwwroot", "/var/www"}
	}
	if s.cfg.Nginx.WebUser == "" || !isWebRoot(path, prefixes) {
		return
	}
	uid, err := resolveUser(s.cfg.Nginx.WebUser)
	if err != nil {
		slog.Warn("web_user 解析失败，跳过 chown", "user", s.cfg.Nginx.WebUser, "error", err)
		return
	}
	group := s.cfg.Nginx.WebGroup
	if group == "" {
		group = s.cfg.Nginx.WebUser
	}
	gid, err := resolveGroup(group)
	if err != nil {
		slog.Warn("web_group 解析失败，跳过 chown", "group", group, "error", err)
		return
	}
	if err := os.Chown(path, uid, gid); err != nil {
		slog.Warn("设置文件所有者失败", "path", path, "error", err)
	}
}

func lookupOwnerGroup(fi os.FileInfo) (string, string) {
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		uid := strconv.FormatUint(uint64(stat.Uid), 10)
		gid := strconv.FormatUint(uint64(stat.Gid), 10)
		owner := uid
		group := gid
		if u, err := user.LookupId(uid); err == nil {
			owner = u.Username
		}
		if g, err := user.LookupGroupId(gid); err == nil {
			group = g.Name
		}
		return owner, group
	}
	return "", ""
}

func detectContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".tar", ".gz":
		return "application/gzip"
	case ".txt", ".md":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
