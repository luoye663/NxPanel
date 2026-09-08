package accesspolicy

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
)

func newSitePolicy() Policy {
	return Policy{Version: 1, Mode: "unified", Rules: []Rule{}, DefaultAction: Action{Type: "allow"}, ApplyStatus: "applied", Warnings: []string{}}
}

// PrepareSiteCreate adds the initial empty policy to the site's original file
// transaction. It does not publish database state before the site row exists.
func (s *Service) PrepareSiteCreate(site *repo.Site, original []agentclient.FileChangeRequest) ([]agentclient.FileChangeRequest, error) {
	if site == nil || site.ID == "" || site.AccessLimitPath == "" {
		return nil, fmt.Errorf("新站点缺少访问策略路径")
	}
	var main []byte
	for _, change := range original {
		if change.Type == "write" && change.Path == site.ConfigPath {
			var err error
			main, err = base64.StdEncoding.DecodeString(change.ContentBase64)
			if err != nil {
				return nil, err
			}
			break
		}
	}
	if len(main) == 0 {
		return nil, fmt.Errorf("新站点缺少待写入配置")
	}
	patched, err := nginx.ApplyOptionalMarkerPatches(main, []nginx.BlockPatch{
		{Name: nginx.MarkerNameAccessLimit, Body: []byte("    include " + site.AccessLimitPath + ";\n")},
		{Name: nginx.MarkerNameHotlink, Body: nil},
	})
	if err != nil {
		return nil, err
	}
	p := newSitePolicy()
	compiled, err := Compile(site.ID, s.panelDir, p, RenderContext{})
	if err != nil {
		return nil, err
	}
	// The empty initial policy must not cancel authentication inherited from a
	// hand-written http configuration. Subsequent edits run the complete config
	// preflight before selecting explicit per-rule authentication.
	compiled.ServerContent = strings.ReplaceAll(compiled.ServerContent, "auth_basic off;\n", "")
	policyChanges, err := s.buildFiles(site, p, compiled, nil, map[string]string{}, site.AccessLimitPath, patched)
	if err != nil {
		return nil, err
	}
	replaced := map[string]bool{}
	for _, change := range policyChanges {
		replaced[change.Type+"\x00"+change.Path] = true
	}
	changes := make([]agentclient.FileChangeRequest, 0, len(original)+len(policyChanges))
	for _, change := range original {
		if !replaced[change.Type+"\x00"+change.Path] {
			changes = append(changes, change)
		}
	}
	return append(changes, policyChanges...), nil
}

func (s *Service) FinishSiteCreate(siteID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	saved, err := s.store.SaveDesired(siteID, newSitePolicy(), 0)
	if err != nil {
		return err
	}
	return s.store.MarkApplied(siteID, saved.Version)
}

// SiteDeleteFiles names individual generated dependencies, including unused
// authentication slots; it never recursively removes a shared policy directory.
func (s *Service) SiteDeleteFiles(siteID string) []agentclient.FileChangeRequest {
	paths := s.backupPaths(siteID)
	changes := make([]agentclient.FileChangeRequest, 0, len(paths))
	for _, path := range paths {
		changes = append(changes, agentclient.FileChangeRequest{Type: "remove", Path: path})
	}
	return changes
}
