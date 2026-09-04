package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/luoye663/nxpanel/internal/waf"
)

const (
	wafAuditMaxFiles   = 100000
	wafAuditMaxPreview = int64(1024 * 1024)
)

type wafAuditListRequest struct {
	SiteID string `json:"site_id"`
	Limit  int    `json:"limit"`
}

type wafAuditReadRequest struct {
	SiteID   string `json:"site_id"`
	EventID  string `json:"event_id"`
	MaxBytes int64  `json:"max_bytes"`
}

type wafAuditCleanupRequest struct {
	SiteID   string `json:"site_id"`
	MaxAge   string `json:"max_age"`
	MaxBytes int64  `json:"max_bytes"`
}

type wafAuditItem struct {
	EventID string    `json:"event_id"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

type wafAuditFile struct {
	wafAuditItem
	path string
}

func (s *Server) handleWAFAuditList(w http.ResponseWriter, r *http.Request) {
	var req wafAuditListRequest
	if err := decodeWAFJSON(r, &req); err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	root, err := s.wafAuditRoot(req.SiteID)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	files, err := scanWAFAuditFiles(r.Context(), root, req.SiteID)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime.After(files[j].ModTime) })
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if len(files) > limit {
		files = files[:limit]
	}
	items := make([]wafAuditItem, len(files))
	for i := range files {
		items[i] = files[i].wafAuditItem
	}
	writeAgentOK(w, map[string]any{"items": items})
}

func (s *Server) handleWAFAuditRead(w http.ResponseWriter, r *http.Request) {
	var req wafAuditReadRequest
	if err := decodeWAFJSON(r, &req); err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.EventID) != 64 || !validSHA256(req.EventID) {
		writeAgentError(w, http.StatusBadRequest, "event_id is invalid")
		return
	}
	root, err := s.wafAuditRoot(req.SiteID)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	files, err := scanWAFAuditFiles(r.Context(), root, req.SiteID)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var selected string
	for _, file := range files {
		if file.EventID == req.EventID {
			selected = file.path
			break
		}
	}
	if selected == "" {
		writeAgentError(w, http.StatusNotFound, "audit event not found")
		return
	}
	maxBytes := req.MaxBytes
	if maxBytes <= 0 || maxBytes > wafAuditMaxPreview {
		maxBytes = wafAuditMaxPreview
	}
	data, truncated, err := readAuditPreview(selected, maxBytes)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAgentOK(w, map[string]any{"event_id": req.EventID, "content": string(data), "truncated": truncated})
}

func (s *Server) handleWAFAuditCleanup(w http.ResponseWriter, r *http.Request) {
	var req wafAuditCleanupRequest
	if err := decodeWAFJSON(r, &req); err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	root, err := s.wafAuditRoot(req.SiteID)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	maxAge, err := time.ParseDuration(req.MaxAge)
	if err != nil || maxAge < 24*time.Hour || maxAge > 90*24*time.Hour {
		writeAgentError(w, http.StatusBadRequest, "max_age must be between 24h and 2160h")
		return
	}
	if req.MaxBytes < 256*1024*1024 || req.MaxBytes > 100*1024*1024*1024 {
		writeAgentError(w, http.StatusBadRequest, "max_bytes must be between 256 MiB and 100 GiB")
		return
	}
	files, err := scanWAFAuditFiles(r.Context(), root, req.SiteID)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, err.Error())
		return
	}
	removed, freed, err := cleanupWAFAuditFiles(r.Context(), files, time.Now().Add(-maxAge), req.MaxBytes)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAgentOK(w, map[string]any{"removed": removed, "freed_bytes": freed})
}

func (s *Server) wafAuditRoot(siteID string) (string, error) {
	if err := waf.ValidateSiteID(siteID); err != nil {
		return "", err
	}
	root := filepath.Join(s.cfg.Nginx.PanelDir, "waf", "audit", siteID)
	if _, err := s.policy.Validate(root); err != nil {
		return "", fmt.Errorf("audit root is outside the agent policy: %w", err)
	}
	return root, nil
}

func scanWAFAuditFiles(ctx context.Context, root, siteID string) ([]wafAuditFile, error) {
	files := make([]wafAuditFile, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if len(files) >= wafAuditMaxFiles {
			return errors.New("audit directory exceeds the file scan limit")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || filepath.IsAbs(rel) {
			return errors.New("invalid audit file path")
		}
		sum := sha256.Sum256([]byte(siteID + "\x00" + filepath.ToSlash(rel)))
		files = append(files, wafAuditFile{wafAuditItem: wafAuditItem{EventID: hex.EncodeToString(sum[:]), Size: info.Size(), ModTime: info.ModTime()}, path: path})
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return []wafAuditFile{}, nil
	}
	return files, err
}

func readAuditPreview(path string, maxBytes int64) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false, errors.New("audit event is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > maxBytes {
		return data[:maxBytes], true, nil
	}
	return data, false, nil
}

func cleanupWAFAuditFiles(ctx context.Context, files []wafAuditFile, cutoff time.Time, maxBytes int64) (int, int64, error) {
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime.Before(files[j].ModTime) })
	var total int64
	for _, file := range files {
		total += file.Size
	}
	removed := 0
	var freed int64
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return removed, freed, err
		}
		if !file.ModTime.Before(cutoff) && total <= maxBytes {
			continue
		}
		info, err := os.Lstat(file.path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, freed, err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := os.Remove(file.path); err != nil {
			return removed, freed, err
		}
		removed++
		freed += file.Size
		total -= file.Size
	}
	return removed, freed, nil
}
