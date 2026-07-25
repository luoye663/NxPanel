package agent

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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

type SiteBackupRemoveRequest struct {
	BackupPath string `json:"backup_path"`
}

type siteBackupMetadata struct {
	Version       int                    `json:"version"`
	SiteID        string                 `json:"site_id"`
	PrimaryDomain string                 `json:"primary_domain"`
	BackupType    string                 `json:"backup_type"`
	CreatedAt     string                 `json:"created_at"`
	Files         []siteBackupFileRecord `json:"files"`
}

type siteBackupFileRecord struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func (s *Server) handleSiteBackupCreate(w http.ResponseWriter, r *http.Request) {
	var req SiteBackupCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}
	resp, err := s.createSiteBackup(r.Context(), &req)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAgentOK(w, resp)
}

func (s *Server) handleSiteBackupDownload(w http.ResponseWriter, r *http.Request) {
	path, err := s.policy.Validate(r.URL.Query().Get("path"))
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "备份路径不在白名单内: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(path)))
	http.ServeFile(w, r, path)
}

func (s *Server) handleSiteBackupRestore(w http.ResponseWriter, r *http.Request) {
	var req SiteBackupRestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}
	if err := s.restoreSiteBackup(r.Context(), &req); err != nil {
		writeAgentError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAgentOK(w, map[string]any{"success": true})
}

func (s *Server) handleSiteBackupRemove(w http.ResponseWriter, r *http.Request) {
	var req SiteBackupRemoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}
	path, err := s.policy.Validate(req.BackupPath)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "备份路径不在白名单内: "+err.Error())
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		writeAgentError(w, http.StatusInternalServerError, "删除备份文件失败: "+err.Error())
		return
	}
	writeAgentOK(w, map[string]any{"success": true})
}

func (s *Server) createSiteBackup(ctx context.Context, req *SiteBackupCreateRequest) (*SiteBackupCreateResponse, error) {
	if req.SiteID == "" || req.OutputPath == "" || !validSiteBackupType(req.BackupType) {
		return nil, fmt.Errorf("site_id、backup_type、output_path 不能为空且类型必须合法")
	}
	outputPath, err := s.policy.Validate(req.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("输出路径不在白名单内: %w", err)
	}
	for _, source := range append(append([]string{}, req.ConfigPaths...), req.SSLPaths...) {
		if source != "" && pathsOverlap(source, outputPath) {
			return nil, fmt.Errorf("备份输出路径不能与备份源重叠")
		}
	}
	if req.RootPath != "" && pathsOverlap(req.RootPath, outputPath) {
		return nil, fmt.Errorf("备份输出路径不能位于站点根目录内")
	}
	limits := s.archiveLimits()
	ctx, cancel := context.WithTimeout(ctx, limits.timeout)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(outputPath), 0700); err != nil {
		return nil, fmt.Errorf("创建备份目录失败: %w", err)
	}

	tmpPath := outputPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("创建备份文件失败: %w", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	hash := sha256.New()
	budget := newArchiveBudget(limits)
	gw := gzip.NewWriter(&budgetWriter{dst: io.MultiWriter(out, hash), budget: budget})
	tw := tar.NewWriter(gw)
	metadata := siteBackupMetadata{Version: 1, SiteID: req.SiteID, PrimaryDomain: req.PrimaryDomain, BackupType: req.BackupType, CreatedAt: time.Now().UTC().Format(time.RFC3339)}

	// metadata 最后写入，这样可以记录实际归档成功的文件 hash 和大小。
	if err := s.addBackupEntries(ctx, tw, req, &metadata, budget); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		_ = out.Close()
		return nil, err
	}
	if err := writeSiteBackupMetadata(tw, metadata, budget); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		_ = out.Close()
		return nil, err
	}
	if err := tw.Close(); err != nil {
		_ = gw.Close()
		_ = out.Close()
		return nil, fmt.Errorf("关闭 tar writer 失败: %w", err)
	}
	if err := gw.Close(); err != nil {
		_ = out.Close()
		return nil, fmt.Errorf("关闭 gzip writer 失败: %w", err)
	}
	if err := out.Close(); err != nil {
		return nil, fmt.Errorf("关闭备份文件失败: %w", err)
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return nil, fmt.Errorf("保存备份文件失败: %w", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("读取备份文件信息失败: %w", err)
	}
	slog.Info("站点备份已创建", "path", outputPath, "type", req.BackupType, "site_id", req.SiteID)
	return &SiteBackupCreateResponse{Path: outputPath, Size: info.Size(), SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func (s *Server) addBackupEntries(ctx context.Context, tw *tar.Writer, req *SiteBackupCreateRequest, metadata *siteBackupMetadata, budget *archiveBudget) error {
	if req.BackupType == "config" || req.BackupType == "full" {
		for index, path := range req.ConfigPaths {
			if path == "" {
				continue
			}
			name := siteBackupConfigEntryName(index)
			if err := s.addFileToBackup(ctx, tw, path, name, metadata, budget); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	if req.BackupType == "ssl" || req.BackupType == "full" {
		for index, path := range req.SSLPaths {
			if path == "" {
				continue
			}
			name := siteBackupSSLEntryName(index)
			if err := s.addFileToBackup(ctx, tw, path, name, metadata, budget); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	if req.BackupType == "root" || req.BackupType == "full" {
		if req.RootPath == "" {
			return fmt.Errorf("根目录备份需要 root_path")
		}
		root, err := s.policy.Validate(req.RootPath)
		if err != nil {
			return fmt.Errorf("站点根目录不在白名单内: %w", err)
		}
		return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if path == root {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() && !info.IsDir() {
				return fmt.Errorf("根目录备份不支持链接或特殊文件: %s", path)
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(filepath.Join("root", rel))
			if info.IsDir() {
				if err := budget.checkEntry(name, 0); err != nil {
					return err
				}
				return writeTarDir(tw, info, name)
			}
			return s.addFileToBackup(ctx, tw, path, name, metadata, budget)
		})
	}
	return nil
}

func (s *Server) addFileToBackup(ctx context.Context, tw *tar.Writer, sourcePath, entryName string, metadata *siteBackupMetadata, budget *archiveBudget) error {
	path, err := s.policy.Validate(sourcePath)
	if err != nil {
		return fmt.Errorf("备份源路径不在白名单内: %s: %w", sourcePath, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("备份只允许普通文件: %s", sourcePath)
	}
	if err := budget.checkEntry(entryName, info.Size()); err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(entryName)
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := copyArchiveInput(ctx, tw, io.TeeReader(file, hash), budget, info.Size())
	closeErr := file.Close()
	if err := joinCopyCloseErrors(copyErr, closeErr); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	metadata.Files = append(metadata.Files, siteBackupFileRecord{Path: header.Name, SHA256: hex.EncodeToString(hash.Sum(nil)), Size: info.Size()})
	return nil
}

func writeTarDir(tw *tar.Writer, info os.FileInfo, name string) error {
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = strings.TrimSuffix(filepath.ToSlash(name), "/") + "/"
	return tw.WriteHeader(header)
}

func writeSiteBackupMetadata(tw *tar.Writer, metadata siteBackupMetadata, budget *archiveBudget) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	header := &tar.Header{Name: "metadata.json", Mode: 0600, Size: int64(len(data)), ModTime: time.Now()}
	if err := budget.checkEntry(header.Name, header.Size); err != nil {
		return err
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err = tw.Write(data)
	return err
}

func (s *Server) restoreSiteBackup(ctx context.Context, req *SiteBackupRestoreRequest) error {
	if req.SiteID == "" || req.BackupPath == "" {
		return fmt.Errorf("site_id 和 backup_path 不能为空")
	}
	backupPath, err := s.policy.Validate(req.BackupPath)
	if err != nil {
		return fmt.Errorf("备份路径不在白名单内: %w", err)
	}
	limits := s.archiveLimits()
	ctx, cancel := context.WithTimeout(ctx, limits.timeout)
	defer cancel()
	metadata, err := readSiteBackupMetadataWithLimits(ctx, backupPath, limits)
	if err != nil {
		return err
	}
	if metadata.SiteID != req.SiteID {
		return fmt.Errorf("备份所属站点不匹配，拒绝恢复")
	}
	snapshotPath := filepath.Join(filepath.Dir(backupPath), "restore-snapshot-"+time.Now().UTC().Format("20060102_150405")+".tar.gz")
	snapshotReq := SiteBackupCreateRequest{SiteID: req.SiteID, BackupType: snapshotType(req), OutputPath: snapshotPath, ConfigPaths: req.ConfigPaths, RootPath: req.RootPath, SSLPaths: req.SSLPaths}
	if _, err := s.createSiteBackup(ctx, &snapshotReq); err != nil {
		return fmt.Errorf("恢复前创建快照失败: %w", err)
	}
	if s.restoreSnapshotHook != nil {
		s.restoreSnapshotHook(snapshotPath)
	}
	if err := s.extractSiteBackup(ctx, backupPath, req); err != nil {
		rollbackErr := s.rollbackSiteBackup(ctx, snapshotPath, req, limits)
		if rollbackErr != nil {
			return fmt.Errorf("恢复备份失败: %w；快照回滚失败: %v", err, rollbackErr)
		}
		return fmt.Errorf("恢复备份失败，已从快照回滚: %w", err)
	}
	if req.RestoreConfig || req.RestoreSSL {
		if result, testErr := s.executor.Test(ctx); testErr != nil {
			rollbackErr := s.rollbackSiteBackup(ctx, snapshotPath, req, limits)
			if rollbackErr != nil {
				return fmt.Errorf("nginx -t 失败且快照回滚失败: %s %v；回滚错误: %v", result.Stderr, testErr, rollbackErr)
			}
			return fmt.Errorf("nginx -t 失败，已从快照回滚: %s %v", result.Stderr, testErr)
		}
		if req.ReloadNginx {
			if _, err := s.executor.Reload(ctx); err != nil {
				return fmt.Errorf("备份已恢复但 reload 失败: %w", err)
			}
		}
	}
	return nil
}

func (s *Server) rollbackSiteBackup(parent context.Context, snapshotPath string, req *SiteBackupRestoreRequest, limits archiveLimits) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), limits.timeout)
	defer cancel()
	rollbackReq := *req
	rollbackReq.BackupPath = snapshotPath
	return s.extractSiteBackup(rollbackCtx, snapshotPath, &rollbackReq)
}

func (s *Server) extractSiteBackup(ctx context.Context, backupPath string, req *SiteBackupRestoreRequest) error {
	if _, err := checkArchiveFileSize(backupPath, s.archiveLimits()); err != nil {
		return fmt.Errorf("备份文件资源检查失败: %w", err)
	}
	file, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("打开备份文件失败: %w", err)
	}
	defer file.Close()
	compressed := &countingReader{r: file}
	gr, err := gzip.NewReader(compressed)
	if err != nil {
		return fmt.Errorf("读取 gzip 失败: %w", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	budget := newArchiveBudget(s.archiveLimits())
	configTargets := mapConfigEntryNames(req.ConfigPaths)
	sslTargets := mapSSLEntryNames(req.SSLPaths)
	created := []string{}
	success := false
	defer func() {
		if !success {
			for i := len(created) - 1; i >= 0; i-- {
				_ = os.Remove(created[i])
			}
		}
	}()
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取备份条目失败: %w", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !safeBackupEntryName(header.Name) {
			return fmt.Errorf("备份包含非法条目: %s", header.Name)
		}
		if err := budget.checkEntry(header.Name, header.Size); err != nil {
			return err
		}
		if header.Name == "metadata.json" {
			if header.Size > 0 {
				if _, err := copyArchiveOutput(ctx, io.Discard, tr, budget, header.Size); err != nil {
					return err
				}
			}
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
			return fmt.Errorf("备份包含不支持的条目类型: %s", header.Name)
		}
		var target string
		switch {
		case req.RestoreConfig && strings.HasPrefix(header.Name, "config/"):
			target = configTargets[header.Name]
		case req.RestoreSSL && strings.HasPrefix(header.Name, "ssl/"):
			target = sslTargets[header.Name]
		case req.RestoreRoot && strings.HasPrefix(header.Name, "root/"):
			if req.RootPath == "" {
				return fmt.Errorf("恢复根目录需要 root_path")
			}
			target = filepath.Join(req.RootPath, strings.TrimPrefix(header.Name, "root/"))
		default:
			if header.Size > 0 {
				if _, err := copyArchiveOutput(ctx, io.Discard, tr, budget, header.Size); err != nil {
					return err
				}
				if err := budget.checkRatio(budget.extracted, compressed.n, header.Name); err != nil {
					return err
				}
			}
			continue
		}
		if target == "" {
			if header.Size > 0 {
				if _, err := copyArchiveOutput(ctx, io.Discard, tr, budget, header.Size); err != nil {
					return err
				}
			}
			continue
		}
		cleanTarget, err := safeRestoreTarget(s.policy, target)
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeDir {
			_, statErr := os.Stat(cleanTarget)
			if err := os.MkdirAll(cleanTarget, 0755); err != nil {
				return err
			}
			if os.IsNotExist(statErr) {
				created = append(created, cleanTarget)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cleanTarget), 0755); err != nil {
			return err
		}
		_, statErr := os.Stat(cleanTarget)
		if err := extractEntryAtomically(ctx, cleanTarget, os.FileMode(header.Mode), tr, budget, header.Size); err != nil {
			return err
		}
		if err := budget.checkRatio(budget.extracted, compressed.n, header.Name); err != nil {
			return err
		}
		s.applyWebOwner(cleanTarget)
		if os.IsNotExist(statErr) {
			created = append(created, cleanTarget)
		}
	}
	success = true
	return nil
}

func validSiteBackupType(value string) bool {
	switch value {
	case "config", "root", "ssl", "full":
		return true
	default:
		return false
	}
}

func snapshotType(req *SiteBackupRestoreRequest) string {
	if req.RestoreConfig && req.RestoreRoot && req.RestoreSSL {
		return "full"
	}
	if req.RestoreRoot {
		return "root"
	}
	if req.RestoreSSL {
		return "ssl"
	}
	return "config"
}

func siteBackupConfigEntryName(index int) string {
	names := []string{"config/site.conf", "config/rewrite.conf", "config/access-limit.conf", "config/hotlink.conf"}
	if index >= 0 && index < len(names) {
		return names[index]
	}
	return fmt.Sprintf("config/extra-%d.conf", index)
}

func siteBackupSSLEntryName(index int) string {
	if index == 0 {
		return "ssl/fullchain.pem"
	}
	if index == 1 {
		return "ssl/privkey.pem"
	}
	return fmt.Sprintf("ssl/extra-%d.pem", index)
}

func mapConfigEntryNames(paths []string) map[string]string {
	result := make(map[string]string, len(paths))
	for index, path := range paths {
		if path != "" {
			result[siteBackupConfigEntryName(index)] = path
		}
	}
	return result
}

func mapSSLEntryNames(paths []string) map[string]string {
	result := make(map[string]string, len(paths))
	for index, path := range paths {
		if path != "" {
			result[siteBackupSSLEntryName(index)] = path
		}
	}
	return result
}

func readSiteBackupMetadata(backupPath string) (*siteBackupMetadata, error) {
	return readSiteBackupMetadataWithLimits(context.Background(), backupPath, defaultArchiveLimits())
}

func readSiteBackupMetadataWithLimits(ctx context.Context, backupPath string, limits archiveLimits) (*siteBackupMetadata, error) {
	if _, err := checkArchiveFileSize(backupPath, limits); err != nil {
		return nil, fmt.Errorf("备份文件资源检查失败: %w", err)
	}
	file, err := os.Open(backupPath)
	if err != nil {
		return nil, fmt.Errorf("打开备份文件失败: %w", err)
	}
	defer file.Close()
	compressed := &countingReader{r: file}
	gr, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("读取 gzip 失败: %w", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	budget := newArchiveBudget(limits)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取备份条目失败: %w", err)
		}
		if err := budget.checkEntry(header.Name, header.Size); err != nil {
			return nil, err
		}
		if header.Name != "metadata.json" {
			if header.Size > 0 {
				if _, err := copyArchiveOutput(ctx, io.Discard, tr, budget, header.Size); err != nil {
					return nil, err
				}
				if err := budget.checkRatio(budget.extracted, compressed.n, header.Name); err != nil {
					return nil, err
				}
			}
			continue
		}
		var metadata siteBackupMetadata
		limited := io.LimitReader(tr, header.Size)
		if err := json.NewDecoder(limited).Decode(&metadata); err != nil {
			return nil, fmt.Errorf("解析备份 metadata 失败: %w", err)
		}
		if metadata.Version != 1 || metadata.SiteID == "" {
			return nil, fmt.Errorf("备份 metadata 不合法")
		}
		return &metadata, nil
	}
	return nil, fmt.Errorf("备份缺少 metadata.json")
}

func safeBackupEntryName(name string) bool {
	if name == "" || strings.Contains(name, "\\") || filepath.IsAbs(name) {
		return false
	}
	clean := filepath.Clean(name)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func safeRestoreTarget(policy *PathPolicy, target string) (string, error) {
	cleanTarget, err := policy.Validate(target)
	if err != nil {
		return "", fmt.Errorf("恢复目标路径不在白名单内: %s: %w", target, err)
	}
	return cleanTarget, nil
}
