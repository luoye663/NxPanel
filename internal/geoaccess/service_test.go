package geoaccess

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

type geoServiceTestAgent struct {
	files map[string][]byte
	last  *agentclient.TransactionRequest
}

func (a *geoServiceTestAgent) ReadFile(_ context.Context, path string) ([]byte, string, error) {
	content, ok := a.files[path]
	if !ok {
		return nil, "", fmt.Errorf("file not found: %s", path)
	}
	return append([]byte(nil), content...), "", nil
}

func (a *geoServiceTestAgent) ApplyTransaction(_ context.Context, req *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error) {
	a.last = req
	return &agentclient.TransactionResponse{}, nil
}

func newGeoServiceTest(t *testing.T, mainConfig string) (*Service, *repo.GeoAccessRepo, *geoServiceTestAgent) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	const (
		siteID          = "site_geo_service"
		configPath      = "/panel/sites-available/example.com.conf"
		accessLimitPath = "/panel/access-limit/example.com.conf"
	)
	if _, err := database.Exec(`INSERT INTO sites
		(id, primary_domain, domains_json, status, root_path, access_log_path, error_log_path,
		 config_path, enabled_path, rewrite_path, access_limit_path)
		VALUES (?, 'example.com', '["example.com"]', 'enabled', '/www/example', '/logs/access.log',
		'/logs/error.log', ?, '/panel/sites-enabled/example.com.conf', '/panel/rewrite/example.com.conf', ?)`,
		siteID, configPath, accessLimitPath); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	cachePath := filepath.Join(dataDir, "countries.json")
	if err := writeCache(cachePath, &countryCache{BuildEpoch: 1, Networks: map[string][]string{"CN": {"1.0.1.0/24"}}}); err != nil {
		t.Fatal(err)
	}
	geoRepo := repo.NewGeoAccessRepo(database)
	geoSettings, err := geoRepo.GetGeoIPSettings()
	if err != nil {
		t.Fatal(err)
	}
	geoSettings.ActiveCachePath = cachePath
	geoSettings.ActiveDBPath = filepath.Join(dataDir, "GeoLite2-Country.mmdb")
	geoSettings.Checksum = strings.Repeat("a", 64)
	geoSettings.CountriesJSON = `["CN","ZZ"]`
	if err := geoRepo.SaveGeoIPSettings(geoSettings); err != nil {
		t.Fatal(err)
	}
	agent := &geoServiceTestAgent{files: map[string][]byte{
		configPath:      []byte(mainConfig),
		accessLimitPath: []byte("# existing access rules\n"),
	}}
	service, err := NewService(geoRepo, repo.NewSiteRepo(database), repo.NewIPWhitelistRuleRepo(database),
		repo.NewOperationRepo(database), agent, dataDir, "/panel")
	if err != nil {
		t.Fatal(err)
	}
	return service, geoRepo, agent
}

func TestEnableSiteInjectsAccessLimitMarkerInSameTransaction(t *testing.T) {
	mainConfig := `#NXPANEL-SITE-START site_id=site_geo_service primary_domain=example.com
server {
    #NXPANEL-REWRITE-START
    include /panel/rewrite/example.com.conf;
    #NXPANEL-REWRITE-END
}
#NXPANEL-SITE-END
`
	service, _, agent := newGeoServiceTest(t, mainConfig)
	if _, err := service.EnableSite(context.Background(), "site_geo_service", "request-1"); err != nil {
		t.Fatal(err)
	}
	if agent.last == nil || !agent.last.TestNginx || !agent.last.ReloadNginx {
		t.Fatalf("expected tested reload transaction, got %#v", agent.last)
	}
	var patchedMain string
	for _, change := range agent.last.Changes {
		if change.Type != "write" || change.Path != "/panel/sites-available/example.com.conf" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(change.ContentBase64)
		if err != nil {
			t.Fatal(err)
		}
		patchedMain = string(raw)
	}
	if !strings.Contains(patchedMain, "#NXPANEL-ACCESS-LIMIT-START") ||
		!strings.Contains(patchedMain, "include /panel/access-limit/example.com.conf;") {
		t.Fatalf("site config does not reference access-limit config:\n%s", patchedMain)
	}
}

func TestEnableSiteRejectsUnreachableAccessLimitConfigAndRollsBack(t *testing.T) {
	service, geoRepo, agent := newGeoServiceTest(t, "server { listen 80; }\n")
	if _, err := service.EnableSite(context.Background(), "site_geo_service", "request-2"); err == nil {
		t.Fatal("enable must fail when the site config cannot accept the access-limit marker")
	}
	if agent.last != nil {
		t.Fatal("agent transaction must not run with an unreachable access-limit config")
	}
	settings, err := geoRepo.GetSiteSettings("site_geo_service")
	if err != nil {
		t.Fatal(err)
	}
	if settings.Enabled {
		t.Fatalf("failed enable was not rolled back: %#v", settings)
	}
}

func TestReconcileRepairsMissingAccessLimitMarker(t *testing.T) {
	mainConfig := `#NXPANEL-SITE-START site_id=site_geo_service primary_domain=example.com
server {
    #NXPANEL-REWRITE-START
    include /panel/rewrite/example.com.conf;
    #NXPANEL-REWRITE-END
}
#NXPANEL-SITE-END
`
	service, geoRepo, agent := newGeoServiceTest(t, mainConfig)
	settings, err := geoRepo.GetSiteSettings("site_geo_service")
	if err != nil {
		t.Fatal(err)
	}
	settings.Enabled = true
	settings.ApplyStatus = "applied"
	if err := geoRepo.SaveSiteSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if agent.last == nil {
		t.Fatal("reconcile did not apply a repair transaction")
	}
	for _, change := range agent.last.Changes {
		if change.Path != "/panel/sites-available/example.com.conf" || change.Type != "write" {
			continue
		}
		raw, decodeErr := base64.StdEncoding.DecodeString(change.ContentBase64)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if strings.Contains(string(raw), "#NXPANEL-ACCESS-LIMIT-START") {
			return
		}
	}
	t.Fatal("reconcile transaction did not repair the site access-limit marker")
}

func TestNormalizeDefaultResponse(t *testing.T) {
	tests := []struct {
		name       string
		req        UpdateSiteAccessRequest
		wantAction string
		wantStatus int
		wantType   string
		wantBody   string
		wantErr    bool
	}{
		{name: "html", req: UpdateSiteAccessRequest{DefaultAction: ActionRespond, DefaultStatusCode: 451, DefaultResponseType: ResponseHTML, DefaultResponseBody: "<h1>x</h1>"}, wantAction: ActionRespond, wantStatus: 451, wantType: ResponseHTML, wantBody: "<h1>x</h1>"},
		{name: "empty body", req: UpdateSiteAccessRequest{DefaultAction: ActionRespond, DefaultStatusCode: 403, DefaultResponseType: ResponseText}, wantAction: ActionRespond, wantStatus: 403, wantType: ResponseText},
		{name: "legacy 403", req: UpdateSiteAccessRequest{DefaultAction: ActionDeny403}, wantAction: ActionRespond, wantStatus: 403, wantType: ResponseText},
		{name: "444 clears body", req: UpdateSiteAccessRequest{DefaultAction: ActionRespond, DefaultStatusCode: 444, DefaultResponseType: ResponseHTML, DefaultResponseBody: "ignored"}, wantAction: ActionRespond, wantStatus: 444, wantType: ResponseText},
		{name: "low status", req: UpdateSiteAccessRequest{DefaultAction: ActionRespond, DefaultStatusCode: 399, DefaultResponseType: ResponseText}, wantErr: true},
		{name: "too large", req: UpdateSiteAccessRequest{DefaultAction: ActionRespond, DefaultStatusCode: 403, DefaultResponseType: ResponseText, DefaultResponseBody: strings.Repeat("界", maxBodyBytes/3+1)}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, status, responseType, body, err := normalizeDefaultResponse(tt.req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.wantErr && (action != tt.wantAction || status != tt.wantStatus || responseType != tt.wantType || body != tt.wantBody) {
				t.Fatalf("unexpected normalized response: %q %d %q %q", action, status, responseType, body)
			}
		})
	}
}

func TestUpdateEnabledSiteWritesAndRemovesResponseArtifact(t *testing.T) {
	mainConfig := `#NXPANEL-SITE-START site_id=site_geo_service primary_domain=example.com
server {
    #NXPANEL-ACCESS-LIMIT-START
    include /panel/access-limit/example.com.conf;
    #NXPANEL-ACCESS-LIMIT-END
}
#NXPANEL-SITE-END
`
	service, _, agent := newGeoServiceTest(t, mainConfig)
	if _, err := service.EnableSite(context.Background(), "site_geo_service", "request-enable"); err != nil {
		t.Fatal(err)
	}
	result, err := service.UpdateSiteAccess(context.Background(), "site_geo_service", UpdateSiteAccessRequest{
		DefaultAction: ActionRespond, DefaultStatusCode: 451, DefaultResponseType: ResponseHTML,
		DefaultResponseBody: "<h1>Unavailable</h1>",
	}, "request-response")
	if err != nil {
		t.Fatal(err)
	}
	if result.DefaultStatusCode != 451 || result.DefaultResponseType != ResponseHTML {
		t.Fatalf("unexpected saved response: %#v", result)
	}
	responsePath := "/panel/geo-access/responses/site_geo_service.body"
	var wroteResponse bool
	for _, change := range agent.last.Changes {
		if change.Path == responsePath && change.Type == "write" {
			body, decodeErr := base64.StdEncoding.DecodeString(change.ContentBase64)
			if decodeErr != nil || string(body) != "<h1>Unavailable</h1>" {
				t.Fatalf("unexpected response artifact: %q err=%v", body, decodeErr)
			}
			wroteResponse = true
		}
	}
	if !wroteResponse {
		t.Fatal("custom response was not written in the apply transaction")
	}
	if _, err := service.UpdateSiteAccess(context.Background(), "site_geo_service", UpdateSiteAccessRequest{
		DefaultAction: ActionAllow, DefaultStatusCode: 403, DefaultResponseType: ResponseText,
	}, "request-allow"); err != nil {
		t.Fatal(err)
	}
	for _, change := range agent.last.Changes {
		if change.Path == responsePath && change.Type == "remove" {
			return
		}
	}
	t.Fatal("switching to allow did not remove the response artifact")
}
