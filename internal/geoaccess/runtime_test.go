package geoaccess

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/georuntime"
	"github.com/luoye663/nxpanel/internal/nginx"
)

type geoRuntimeTestAgent struct {
	*geoServiceTestAgent
	applyErr error
	applied  int
}

func (a *geoRuntimeTestAgent) FilesList(_ context.Context, dir string) (*agentclient.FilesListResponse, error) {
	list := &agentclient.FilesListResponse{}
	for path, body := range a.files {
		if filepath.Dir(path) == dir {
			list.Entries = append(list.Entries, agentclient.FileEntry{Name: filepath.Base(path), Size: int64(len(body))})
		}
	}
	return list, nil
}

func (a *geoRuntimeTestAgent) FilesDownload(ctx context.Context, path string) (*http.Response, error) {
	data, _, err := a.ReadFile(ctx, path)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(data))}, nil
}

func (*geoRuntimeTestAgent) NginxStatus(context.Context) (*nginx.RuntimeStatus, error) {
	return &nginx.RuntimeStatus{Running: true}, nil
}

func (a *geoRuntimeTestAgent) ApplyTransaction(_ context.Context, request *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error) {
	a.last = request
	if a.applyErr != nil {
		return nil, a.applyErr
	}
	a.applied++
	for _, change := range request.Changes {
		switch change.Type {
		case "write":
			content, err := base64.StdEncoding.DecodeString(change.ContentBase64)
			if err != nil {
				return nil, err
			}
			a.files[change.Path] = content
		case "remove":
			delete(a.files, change.Path)
		}
	}
	return &agentclient.TransactionResponse{}, nil
}

func TestLegacyGeoMigrationPreservesActiveRulesWithoutModule(t *testing.T) {
	service, store, baseAgent := newGeoServiceTest(t, "server {}")
	agent := &geoRuntimeTestAgent{geoServiceTestAgent: baseAgent}
	service.agent = agent
	service.ConfigureRuntime("", "", func(context.Context, map[string]georuntime.Snapshot, func() (map[string][]string, error)) ([]agentclient.FileChangeRequest, int, int, error) {
		return nil, 0, 0, nil
	})
	const siteID = "site_geo_service"
	if err := store.CreateRule(&repo.SiteGeoRule{ID: "geo", SiteID: siteID, Name: "country", CountriesJSON: `["CN"]`, Action: ActionDeny403, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	settings, err := store.GetSiteSettings(siteID)
	if err != nil {
		t.Fatal(err)
	}
	settings.Enabled, settings.ApplyStatus = true, "applied"
	rules, err := store.ListRules(siteID)
	if err != nil {
		t.Fatal(err)
	}
	old, err := renderConfig(service.panelDir, []string{}, 0, []*repo.SiteGeoSettings{settings}, map[string][]*repo.SiteGeoRule{siteID: rules}, nil,
		&countryCache{Networks: map[string][]string{"CN": {"1.0.1.0/24"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings.AppliedHash, settings.DesiredHash = old.Sites[siteID].Hash, old.Sites[siteID].Hash
	if err := store.SaveSiteSettings(settings); err != nil {
		t.Fatal(err)
	}
	agent.files[old.GlobalPath] = []byte(old.GlobalContent)
	agent.files["/panel/conf.d/custom.conf"] = []byte("# untouched")
	agent.applyErr = &agentclient.HTTPError{StatusCode: 500, Message: `nginx -t 失败（已回滚）: unknown directive "geoip2"`}
	if err := service.Migrate(context.Background(), "test"); err == nil {
		t.Fatal("missing module was not reported")
	}
	status, err := service.GetStatus()
	if err != nil || status.Runtime.State != "blocked" || status.Runtime.Module != "unavailable" || status.Runtime.LegacyIncludes != 1 {
		t.Fatalf("wrong blocked state: %+v, %v", status, err)
	}
	if string(agent.files[old.GlobalPath]) != old.GlobalContent || agent.applied != 0 {
		t.Fatal("existing geo rules must still be applied")
	}
	agent.applyErr = nil
	if err := service.Migrate(context.Background(), "retry"); err != nil {
		t.Fatal(err)
	}
	if !agent.last.PreserveBackup || !agent.last.ConfirmReload || len(agent.last.Changes) != 2 || agent.last.Changes[0].ExpectedHash != georuntime.Hash(old.GlobalContent) {
		t.Fatalf("unsafe migration transaction: %#v", agent.last)
	}
	if got := string(agent.files[old.GlobalPath]); !strings.Contains(got, "map "+georuntime.CountryVariable) || strings.Contains(got, "1.0.1.0/24") {
		t.Fatalf("legacy include was not converted: %s", got)
	}
	if err := service.Migrate(context.Background(), "idempotent"); err != nil || agent.applied != 1 || string(agent.files["/panel/conf.d/custom.conf"]) != "# untouched" {
		t.Fatalf("migration is not idempotent: transactions=%d err=%v", agent.applied, err)
	}
}
