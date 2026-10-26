package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/luoye663/nxpanel/internal/configpreflight"
)

// preflightSource never uses the general-purpose file read RPC or its limit.
// Nonblocking open prevents FIFOs/devices from pinning a privileged worker.
type preflightSource struct{ policy *PathPolicy }

func (s preflightSource) Open(path string) (io.ReadCloser, error) {
	path, err := s.policy.Validate(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("预检仅允许普通文件")
	}
	return f, nil
}

func (s preflightSource) Includes(path string) ([]string, error) {
	if !strings.ContainsAny(path, "*?[") {
		return []string{path}, nil
	}
	dir, pattern := filepath.Dir(path), filepath.Base(path)
	if strings.ContainsAny(dir, "*?[") {
		return nil, fmt.Errorf("暂不支持目录包含通配符的 include")
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, err
	}
	dir, err := s.policy.Validate(dir)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(dir, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(65537)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 65536 {
		return nil, fmt.Errorf("include 目录条目过多")
	}
	paths := []string{}
	for _, e := range entries {
		match, _ := filepath.Match(pattern, e.Name())
		if match {
			if e.IsDir() {
				return nil, fmt.Errorf("include 匹配目录: %s", e.Name())
			}
			paths = append(paths, filepath.Join(dir, e.Name()))
			if len(paths) > configpreflight.MaxFiles {
				return nil, fmt.Errorf("include 匹配文件超过 256 个")
			}
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *Server) handleConfigPreflight(w http.ResponseWriter, r *http.Request) {
	// Only the small, already prepared site configuration crosses the socket.
	r.Body = http.MaxBytesReader(w, r.Body, 24<<20)
	var req configpreflight.Request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "预检请求格式错误: "+err.Error())
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeAgentError(w, http.StatusBadRequest, "预检只接受一个 JSON 对象")
		return
	}
	if len(req.Main) > 16<<20 {
		writeAgentError(w, http.StatusBadRequest, "站点配置超过 16 MiB")
		return
	}
	if _, err := s.policy.Validate(req.SitePath); err != nil {
		writeAgentError(w, http.StatusForbidden, err.Error())
		return
	}
	if req.GlobalPath == "" {
		writeAgentError(w, http.StatusBadRequest, "主配置路径不能为空")
		return
	}
	if _, err := s.policy.Validate(req.GlobalPath); err != nil {
		writeAgentError(w, http.StatusForbidden, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := configpreflight.Check(ctx, preflightSource{s.policy}, req.SitePath, req.GlobalPath, strings.NewReader(req.Main)); err != nil {
		writeAgentError(w, http.StatusBadRequest, "访问策略配置预检失败: "+err.Error())
		return
	}
	writeAgentOK(w, struct{}{})
}
