package accesspolicy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/configpreflight"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
)

type nginxConfigDetector interface {
	DetectNginx(context.Context, *agentclient.NginxDetectRequest) (*agentclient.NginxDetectResponse, error)
}
type nginxConfigLister interface {
	FilesList(context.Context, string) (*agentclient.FilesListResponse, error)
}

type nginxConfigPreflight interface {
	CheckAccessConfig(context.Context, *agentclient.ConfigPreflightRequest) error
}

// SetNginxConfigPath is configured once during dependency initialization.
func (s *Service) SetNginxConfigPath(path string) { s.nginxConfigPath = path }

// CheckConfig rejects independent checks that native policy variables would
// override or that could finish a request before the ordered rules run.
func (s *Service) CheckConfig(ctx context.Context, site *repo.Site, main []byte) error {
	global := s.nginxConfigPath
	if global == "" {
		if detector, ok := s.agent.(nginxConfigDetector); ok {
			info, err := detector.DetectNginx(ctx, &agentclient.NginxDetectRequest{})
			if err != nil {
				return configGuardError("nginx.conf", "无法确定主配置路径: "+err.Error())
			}
			if info == nil || info.ConfPath == "" {
				return configGuardError("nginx.conf", "Agent 未返回主配置路径")
			}
			global = info.ConfPath
		}
	}
	if global != "" && !filepath.IsAbs(global) {
		return configGuardError(global, "主配置路径必须是绝对路径")
	}
	if checker, ok := s.agent.(nginxConfigPreflight); ok {
		err := checker.CheckAccessConfig(ctx, &agentclient.ConfigPreflightRequest{
			SitePath: site.ConfigPath, GlobalPath: global,
			Main: string(stripOwnedAccessBlocks(main)),
		})
		if err != nil {
			return configGuardError(site.ConfigPath, err.Error())
		}
		return nil
	}
	if err := configpreflight.Check(ctx, guardSource{s, ctx}, site.ConfigPath, global, bytes.NewReader(stripOwnedAccessBlocks(main))); err != nil {
		return configGuardError(site.ConfigPath, err.Error())
	}
	return nil
}

func configGuardError(path, detail string) error {
	return app.ErrValidationFailedMsg("访问策略配置预检失败（"+path+"）："+detail, nil)
}

func stripOwnedAccessBlocks(main []byte) []byte {
	clean := main
	for _, name := range []string{nginx.MarkerNameAccessLimit, nginx.MarkerNameHotlink, nginx.MarkerNameForceHTTPS} {
		if patched, err := nginx.ReplaceMarkerBlock(clean, name, nil); err == nil {
			clean = patched
		}
	}
	return clean
}

// guardSource supports in-process implementations; the production client always
// performs preflight on the Agent, and RPC failures never fall back to ReadFile.
type guardSource struct {
	service *Service
	ctx     context.Context
}

func (g guardSource) Open(path string) (io.ReadCloser, error) {
	content, _, err := g.service.agent.ReadFile(g.ctx, path)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func checkCustomAccess(main []byte) error {
	return configpreflight.Check(context.Background(), nil, "站点配置", "", bytes.NewReader(stripOwnedAccessBlocks(main)))
}

func (g guardSource) Includes(path string) ([]string, error) {
	if path == "" || strings.ContainsAny(path, "$\x00\r\n") {
		return nil, configGuardError(path, "无法安全解析动态 include "+path)
	}
	path = filepath.Clean(path)
	if !strings.ContainsAny(path, "*?[") {
		return []string{path}, nil
	}
	dir, pattern := filepath.Dir(path), filepath.Base(path)
	if strings.ContainsAny(dir, "*?[") {
		return nil, configGuardError(path, "暂不支持目录包含通配符的 include: "+path)
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, configGuardError(path, "include 通配符无效: "+path)
	}
	lister, ok := g.service.agent.(nginxConfigLister)
	if !ok {
		return nil, configGuardError(path, "Agent 无法展开 include 通配符: "+path)
	}
	listing, err := lister.FilesList(g.ctx, dir)
	if err != nil || listing == nil {
		return nil, configGuardError(path, fmt.Sprintf("无法列出 include 目录 %s: %v", dir, err))
	}
	if len(listing.Entries) > 65536 {
		return nil, configGuardError(path, "include 目录条目过多")
	}
	paths := []string{}
	for _, entry := range listing.Entries {
		if filepath.Base(entry.Name) != entry.Name || entry.Name == "." || entry.Name == ".." {
			return nil, configGuardError(path, "Agent 返回无效 include 文件名")
		}
		matched, _ := filepath.Match(pattern, entry.Name)
		if matched {
			if entry.IsDir {
				return nil, configGuardError(path, "include 匹配目录: "+entry.Name)
			}
			paths = append(paths, filepath.Join(dir, entry.Name))
			if len(paths) > configpreflight.MaxFiles {
				return nil, configGuardError(path, "include 匹配文件超过 256 个")
			}
		}
	}
	sort.Strings(paths)
	return paths, nil
}
