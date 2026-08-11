package repo

import (
	"context"
	"testing"
)

func createProxyTestSite(t *testing.T, databaseSiteRepo *SiteRepo) {
	t.Helper()
	if err := databaseSiteRepo.Create(&Site{
		ID: "site_proxy", PrimaryDomain: "proxy.example.com", DomainsJSON: `["proxy.example.com"]`,
		Status: "enabled", HTTPPort: 80, HTTPSPort: 443, RootPath: "/www/proxy",
		AccessLogPath: "/logs/proxy.access.log", ErrorLogPath: "/logs/proxy.error.log",
		ConfigPath: "/nginx/proxy.conf", EnabledPath: "/nginx/enabled/proxy.conf", RewritePath: "/nginx/rewrite/proxy.conf",
	}); err != nil {
		t.Fatal(err)
	}
}

func proxyRepoRecord(id string) *SiteProxy {
	return &SiteProxy{
		ID: id, SiteID: "site_proxy", Name: id, Enabled: true, LocationPath: "/" + id,
		UpstreamURL: "http://127.0.0.1:8080", UpstreamScheme: "http", HostHeader: "$host",
		ConnectTimeout: 60, SendTimeout: 60, ReadTimeout: 60, CacheType: "nginx", CacheTime: 60,
	}
}

func TestProxyRepoDirectAndManagedCRUDAndReferences(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	createProxyTestSite(t, NewSiteRepo(database))
	ctx := context.Background()
	upstreams := NewUpstreamRepo(database)
	upstream := testUpstream("up_proxy", "immutable_backend")
	if err := upstreams.Create(ctx, upstream); err != nil {
		t.Fatal(err)
	}
	proxies := NewProxyRepo(database)
	direct := proxyRepoRecord("direct")
	direct.UpstreamScheme = ""
	if err := proxies.Create(direct); err != nil {
		t.Fatal(err)
	}
	gotDirect, err := proxies.GetByID(direct.ID)
	if err != nil || gotDirect.UpstreamID != nil || gotDirect.UpstreamURL != direct.UpstreamURL || gotDirect.UpstreamScheme != "http" {
		t.Fatalf("direct=%#v err=%v", gotDirect, err)
	}
	id := upstream.ID
	managed := proxyRepoRecord("managed")
	managed.UpstreamID = &id
	managed.UpstreamScheme = "https"
	managed.UpstreamURL = "https://immutable_backend"
	managed.ProxySSLServerName = "backend.example.com"
	managed.ProxySSLVerify = true
	managed.ProxySSLTrustedCertificate = "/etc/ssl/certs/ca.pem"
	managed.ProxySSLVerifyDepth = 4
	if err := proxies.Create(managed); err != nil {
		t.Fatal(err)
	}
	gotManaged, err := proxies.GetByID(managed.ID)
	if err != nil || gotManaged.UpstreamID == nil || *gotManaged.UpstreamID != id || !gotManaged.ProxySSLVerify || gotManaged.ProxySSLVerifyDepth != 4 {
		t.Fatalf("managed=%#v err=%v", gotManaged, err)
	}
	gotManaged.ProxySSLVerify = false
	gotManaged.ProxySSLTrustedCertificate = ""
	gotManaged.ProxySSLVerifyDepth = 0
	if err := proxies.Update(gotManaged); err != nil {
		t.Fatal(err)
	}
	listed, err := proxies.ListBySiteID("site_proxy")
	if err != nil || len(listed) != 2 {
		t.Fatalf("list=%#v err=%v", listed, err)
	}
	refs, err := upstreams.CountReferences(ctx, id)
	if err != nil || refs != 1 {
		t.Fatalf("references=%d err=%v", refs, err)
	}
	aggregate, err := upstreams.GetByID(ctx, id)
	if err != nil || aggregate.ReferenceCount != 1 {
		t.Fatalf("aggregate=%#v err=%v", aggregate, err)
	}
	if err := upstreams.Delete(ctx, id); err == nil {
		t.Fatal("FK should reject referenced upstream delete")
	}
	if err := proxies.Delete(managed.ID); err != nil {
		t.Fatal(err)
	}
	if err := upstreams.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
}
