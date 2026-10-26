package sites

import (
	"context"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginxpatch"
	"github.com/luoye663/nxpanel/internal/parser"
)

// The domain saved in Site is the durable selector; the scan hash prevents an
// import confirmation from silently selecting a different/stale server block.
func selectImportTarget(parsed []*parser.ParsedServer, req *ImportSiteRequest) (*parser.ParsedServer, error) {
	var matches []*parser.ParsedServer
	for _, p := range parsed {
		if p.SourceFile != req.SourceFile {
			continue
		}
		if req.ServerHash != "" && nginxpatch.Hash(p.RawBlock) != req.ServerHash {
			continue
		}
		if req.ServerName != "" {
			found := false
			for _, name := range p.ServerNames {
				if name == req.ServerName {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		matches = append(matches, p)
	}
	if len(matches) != 1 {
		return nil, app.NewAppError(app.ErrConflict, "server 目标缺失、已变化或存在歧义，请重新扫描并选择明确目标", nil)
	}
	return matches[0], nil
}

func (svc *Service) checkImportedOwnership(ctx context.Context, site *repo.Site) error {
	if site.ConfigPath != site.EnabledPath || site.Status == "disabled" {
		return nil
	}
	content, _, err := svc.agent.ReadFile(ctx, site.ConfigPath)
	if err != nil {
		return app.NewAppError(app.ErrAgentUnavailable, "无法确认导入配置文件归属，拒绝整文件禁用/删除", nil)
	}
	if err := nginxpatch.ValidateWholeFileOwnership(string(content), site.PrimaryDomain); err != nil {
		return app.NewAppError(app.ErrConflict, "导入文件包含多个或不明确的 server，不能整文件禁用/删除；请先拆分文件，可使用局部维护模式", nil)
	}
	return nil
}
