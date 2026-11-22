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
	DiagnoseAccessConfig(context.Context, *agentclient.ConfigPreflightRequest) (configpreflight.Report, error)
}

// SetNginxConfigPath is configured once during dependency initialization.
func (s *Service) SetNginxConfigPath(path string) { s.nginxConfigPath = path }

// DiagnoseSite reads a prepared snapshot without modifying configuration or policy.
// It deliberately stays outside the apply path: diagnostic failures never gate writes.
func (s *Service) DiagnoseSite(ctx context.Context, siteID string) (configpreflight.Report, error) {
	site, err := s.requireSite(siteID)
	if err != nil {
		return configpreflight.Report{}, err
	}
	var main []byte
	if s.proxy != nil {
		err = s.proxy.WithAccessPolicyConfig(ctx, siteID, func(content []byte) error {
			main = append([]byte(nil), content...)
			return nil
		})
	} else {
		main, _, err = s.agent.ReadFile(ctx, site.ConfigPath)
	}
	if err != nil {
		return incompleteReport(configGuardError(site.ConfigPath, err.Error())), nil
	}
	return s.DiagnoseConfig(ctx, site, main), nil
}

func incompleteReport(err error) configpreflight.Report {
	report := configpreflight.Report{Warnings: []configpreflight.Warning{}}
	report.Incomplete(err)
	return report
}

func (s *Service) DiagnoseConfig(ctx context.Context, site *repo.Site, main []byte) configpreflight.Report {
	global := s.nginxConfigPath
	if global == "" {
		if detector, ok := s.agent.(nginxConfigDetector); ok {
			info, err := detector.DetectNginx(ctx, &agentclient.NginxDetectRequest{})
			if err != nil {
				return incompleteReport(configGuardError("nginx.conf", "无法确定主配置路径: "+err.Error()))
			}
			if info == nil || info.ConfPath == "" {
				return incompleteReport(configGuardError("nginx.conf", "Agent 未返回主配置路径"))
			}
			global = info.ConfPath
		}
	}
	if global != "" && !filepath.IsAbs(global) {
		return incompleteReport(configGuardError(global, "主配置路径必须是绝对路径"))
	}
	if checker, ok := s.agent.(nginxConfigPreflight); ok {
		report, err := checker.DiagnoseAccessConfig(ctx, &agentclient.ConfigPreflightRequest{
			SitePath: site.ConfigPath, GlobalPath: global,
			Main: string(stripOwnedAccessBlocks(main)),
		})
		if err != nil {
			report.Incomplete(err)
		}
		return report
	}
	return configpreflight.Diagnose(ctx, guardSource{s, ctx}, site.ConfigPath, global, bytes.NewReader(stripOwnedAccessBlocks(main)))
}

func configGuardError(path, detail string) error {
	return fmt.Errorf("%s: %s", path, detail)
}

func stripOwnedAccessBlocks(main []byte) []byte {
	clean := main
	for _, name := range []string{nginx.MarkerNameAccessLimit, nginx.MarkerNameHotlink, nginx.MarkerNameForceHTTPS} {
		body, err := nginx.ExtractMarkerBlock(clean, name)
		if err != nil {
			continue
		}
		// Blank only owned content; preserve physical source lines for diagnostics.
		blank := bytes.Repeat([]byte("\n"), bytes.Count(body, []byte("\n")))
		if patched, err := nginx.ReplaceMarkerBlock(clean, name, blank); err == nil {
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
