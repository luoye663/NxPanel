package repo

import (
	"context"
	"testing"
)

func externalCertificate(id string) *Certificate {
	return &Certificate{ID: id, Name: "external", DomainsJSON: `["proxy.example.com"]`, Issuer: "issuer", Subject: "subject",
		CertSHA256: "fingerprint", KeySHA256: "", CertPath: "/external/cert", KeyPath: "/external/key"}
}

func TestCertificateAdoptionAtomicAndIdempotent(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	createProxyTestSite(t, NewSiteRepo(database))
	store := NewCertificateRepo(database)
	ctx := context.Background()
	id, err := store.AdoptExternal(ctx, externalCertificate("cert_1"), "site_proxy", "/nginx/proxy.conf", "hash", "server")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := store.AdoptExternal(ctx, externalCertificate("cert_2"), "site_proxy", "/nginx/proxy.conf", "hash2", "server2")
	if err != nil || id2 != id {
		t.Fatalf("idempotence: %s %v", id2, err)
	}
	all, err := store.List()
	if err != nil || len(all) != 1 || all[0].Source != "external" {
		t.Fatalf("list=%+v err=%v", all, err)
	}
	byHash, err := store.GetBySHA256("fingerprint")
	if err != nil || byHash.Source != "external" {
		t.Fatalf("get fingerprint: %+v %v", byHash, err)
	}
	sites, err := store.AssociatedSites(id)
	if err != nil || len(sites) != 1 || sites[0].PrimaryDomain != "proxy.example.com" {
		t.Fatalf("associated=%+v %v", sites, err)
	}
	if err := store.Delete(id); err == nil {
		t.Fatal("unconditional delete bypassed association FK")
	}
	if err := store.ForgetExternal(ctx, id); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetByID(id); err != nil || got != nil {
		t.Fatalf("forget: %+v %v", got, err)
	}
}

func TestCertificateAdoptionRollsBackOnAssociationFailure(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	createProxyTestSite(t, NewSiteRepo(database))
	store := NewCertificateRepo(database)
	if _, err := database.Exec(`CREATE TRIGGER fail_association BEFORE INSERT ON certificate_sites BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdoptExternal(context.Background(), externalCertificate("cert_1"), "site_proxy", "/site", "hash", "server"); err == nil {
		t.Fatal("injected failure ignored")
	}
	all, err := store.List()
	if err != nil || len(all) != 0 {
		t.Fatalf("partial certificate persisted: %+v %v", all, err)
	}
}

func TestCertificateAdoptionReusesPanelStoreWithoutChangingOwnership(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	createProxyTestSite(t, NewSiteRepo(database))
	store := NewCertificateRepo(database)
	c := externalCertificate("panel")
	c.CertPath, c.KeyPath = "/panel/cert", "/panel/key"
	if err := store.Create(c); err != nil {
		t.Fatal(err)
	}
	id, err := store.AdoptExternal(context.Background(), externalCertificate("external"), "site_proxy", "/config", "hash", "server")
	if err != nil || id != "panel" {
		t.Fatalf("reuse: %s %v", id, err)
	}
	got, err := store.GetByID(id)
	if err != nil || got.Source != "panel" || got.CertPath != "/panel/cert" {
		t.Fatalf("changed ownership: %+v %v", got, err)
	}
}
