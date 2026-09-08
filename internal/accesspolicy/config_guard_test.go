package accesspolicy

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

type configGuardAgent struct {
	*policyServiceAgent
	listErr error
}

func (a *configGuardAgent) FilesList(_ context.Context, dir string) (*agentclient.FilesListResponse, error) {
	if a.listErr != nil {
		return nil, a.listErr
	}
	result := &agentclient.FilesListResponse{}
	for path := range a.files {
		if filepath.Dir(path) == dir {
			result.Entries = append(result.Entries, agentclient.FileEntry{Name: filepath.Base(path)})
		}
	}
	return result, nil
}

type detectingConfigGuardAgent struct {
	*configGuardAgent
	detectErr error
}

func (a *detectingConfigGuardAgent) DetectNginx(context.Context, *agentclient.NginxDetectRequest) (*agentclient.NginxDetectResponse, error) {
	return &agentclient.NginxDetectResponse{ConfPath: "/etc/nginx/nginx.conf"}, a.detectErr
}

func configGuardFixture() (*Service, *configGuardAgent, *repo.Site) {
	agent := &configGuardAgent{policyServiceAgent: &policyServiceAgent{files: map[string][]byte{
		"/etc/nginx/nginx.conf": []byte("events {} http { include mime.types; include conf.d/*.conf; }"),
		"/etc/nginx/mime.types": []byte("types { text/html html; application/json json; }"),
	}}}
	service := &Service{agent: agent, nginxConfigPath: "/etc/nginx/nginx.conf"}
	return service, agent, &repo.Site{ID: "site", ConfigPath: "/panel/sites/site.conf"}
}

func TestConfigGuardFindsAccessInNestedIncludes(t *testing.T) {
	for _, directive := range []string{"auth_basic \"Private\";", "auth_request /check;", "allow 192.0.2.1;", "deny all;", "satisfy any;", "return 302 /login;"} {
		t.Run(strings.Fields(directive)[0], func(t *testing.T) {
			service, agent, site := configGuardFixture()
			agent.files["/panel/rewrite/site.conf"] = []byte("location /admin { include snippets/access.conf; }")
			agent.files["/etc/nginx/snippets/access.conf"] = []byte(directive)
			err := service.CheckConfig(context.Background(), site, []byte("server { include /panel/rewrite/site.conf; }"))
			if err == nil || !strings.Contains(err.Error(), "/etc/nginx/snippets/access.conf") || !strings.Contains(err.Error(), strings.Fields(directive)[0]) {
				t.Fatalf("nested conflict missed: %v", err)
			}
		})
	}
}

func TestConfigGuardFindsHTTPInheritanceAndSkipsOtherServers(t *testing.T) {
	service, agent, site := configGuardFixture()
	agent.files["/etc/nginx/conf.d/other.conf"] = []byte("server { auth_basic Other; include /missing-other-site.conf; location / { return 403; } }")
	agent.files["/etc/nginx/conf.d/data.conf"] = []byte("map $http_user_agent $x { default 0; return 1; } upstream backend { server 127.0.0.1:8080; }")
	main := []byte("server { location / { proxy_pass http://backend; } }")
	if err := service.CheckConfig(context.Background(), site, main); err != nil {
		t.Fatalf("other server/data falsely flagged: %v", err)
	}
	agent.files["/etc/nginx/conf.d/inherited.conf"] = []byte("include inherited-auth.conf;")
	agent.files["/etc/nginx/inherited-auth.conf"] = []byte("auth_basic \"Inherited\"; auth_basic_user_file /etc/nginx/passwords;")
	err := service.CheckConfig(context.Background(), site, main)
	if err == nil || !strings.Contains(err.Error(), "/etc/nginx/inherited-auth.conf") || !strings.Contains(err.Error(), "auth_basic") {
		t.Fatalf("HTTP inheritance missed: %v", err)
	}
}

func TestConfigGuardRelativeStandardIncludesUseMainConfigDirectory(t *testing.T) {
	service, agent, site := configGuardFixture()
	agent.files["/etc/nginx/fastcgi_params"] = []byte("fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name; fastcgi_param EMPTY \"\";")
	main := []byte("server { location / { include fastcgi_params; fastcgi_pass unix:/tmp/php.sock; } }")
	if err := service.CheckConfig(context.Background(), site, main); err != nil {
		t.Fatal(err)
	}
	delete(agent.files, "/etc/nginx/fastcgi_params")
	agent.files["/panel/sites/fastcgi_params"] = []byte("fastcgi_param WRONG_LOCATION 1;")
	if err := service.CheckConfig(context.Background(), site, main); err == nil || !strings.Contains(err.Error(), "/etc/nginx/fastcgi_params") {
		t.Fatalf("include resolved against site directory: %v", err)
	}
}

func TestConfigGuardOnlySkipsOwnedAccessBlocks(t *testing.T) {
	service, agent, site := configGuardFixture()
	main := []byte("server {\n#NXPANEL-ACCESS-LIMIT-START\ninclude /missing-old-ip.conf;\n#NXPANEL-ACCESS-LIMIT-END\n#NXPANEL-HOTLINK-START\ninclude /missing-old-hotlink.conf;\n#NXPANEL-HOTLINK-END\n#NXPANEL-FORCE-HTTPS-START\nreturn 301 https://$host$request_uri;\n#NXPANEL-FORCE-HTTPS-END\n#NXPANEL-REWRITE-START\ninclude /panel/rewrite/site.conf;\n#NXPANEL-REWRITE-END\n}\n")
	agent.files["/panel/rewrite/site.conf"] = []byte("rewrite ^/old$ /new last;")
	if err := service.CheckConfig(context.Background(), site, main); err != nil {
		t.Fatal(err)
	}
	agent.files["/panel/rewrite/site.conf"] = []byte("if ($request_method = POST) { return 403; }")
	if err := service.CheckConfig(context.Background(), site, main); err == nil {
		t.Fatal("REWRITE include was skipped as owned access block")
	}
}

func TestConfigGuardRejectsUnresolvedIncludes(t *testing.T) {
	for _, path := range []string{"$dynamic.conf", "${dynamic}.conf", "conf.*/access.conf", "missing.conf", "conf.d/[invalid"} {
		t.Run(path, func(t *testing.T) {
			service, _, site := configGuardFixture()
			if err := service.CheckConfig(context.Background(), site, []byte("server { include "+path+"; }")); err == nil {
				t.Fatal("unresolved include accepted")
			}
		})
	}
	service, agent, site := configGuardFixture()
	service.agent = agent.policyServiceAgent // No glob listing capability.
	if err := service.CheckConfig(context.Background(), site, []byte("server {}")); err == nil || !strings.Contains(err.Error(), "通配符") {
		t.Fatalf("glob silently ignored: %v", err)
	}
}

func TestConfigGuardCyclesAndBudgets(t *testing.T) {
	service, agent, site := configGuardFixture()
	agent.files["/etc/nginx/a.conf"] = []byte("include b.conf;")
	agent.files["/etc/nginx/b.conf"] = []byte("include a.conf;")
	if err := service.CheckConfig(context.Background(), site, []byte("server { include a.conf; }")); err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("cycle: %v", err)
	}
	for i := 0; i < 18; i++ {
		agent.files[fmt.Sprintf("/etc/nginx/depth%d.conf", i)] = []byte(fmt.Sprintf("include depth%d.conf;", i+1))
	}
	if err := service.CheckConfig(context.Background(), site, []byte("server { include depth0.conf; }")); err == nil || !strings.Contains(err.Error(), "16") {
		t.Fatalf("depth: %v", err)
	}
	guard := &configGuard{service: service, ctx: context.Background(), files: configGuardMaxFiles, active: map[string]bool{}, seen: map[string]bool{}}
	if err := guard.scanFile("/etc/nginx/a.conf", "site", 0); err == nil {
		t.Fatal("file budget ignored")
	}
	guard = &configGuard{bytes: configGuardMaxBytes}
	if err := guard.scanBytes("large.conf", []byte("server {}"), "site", 0); err == nil {
		t.Fatal("byte budget ignored")
	}
}

func TestConfigGuardTokenizerUnderstandsStatementsQuotesAndVariables(t *testing.T) {
	for _, text := range []string{
		`server { set $x "auth_basic deny return; {}"; # deny all;
location / { proxy_set_header Test "quote\" ; return"; root ${document_root}; } }`,
		`server { set $x ''; set $x ""; location / { fastcgi_param SCRIPT_FILENAME ${document_root}/index.php; } }`,
	} {
		if err := checkCustomAccess([]byte(text)); err != nil {
			t.Fatalf("quoted content false positive: %v", err)
		}
	}
	for _, text := range []string{`server { location / { "auth_basic" "Private"; } }`, `server { set $x "safe";auth_basic Private; }`, `server { if ($a) {return 403;} }`} {
		if err := checkCustomAccess([]byte(text)); err == nil {
			t.Fatalf("inline/quoted-name directive missed: %s", text)
		}
	}
	for _, text := range []string{`server {`, `server { set $x "unterminated; }`, `server { deny all }`, `}`} {
		if err := checkCustomAccess([]byte(text)); err == nil {
			t.Fatalf("malformed config accepted: %s", text)
		}
	}
}

func TestConfigGuardDetectFallbackAndFakeCompatibility(t *testing.T) {
	service, agent, site := configGuardFixture()
	service.nginxConfigPath = ""
	service.agent = &detectingConfigGuardAgent{configGuardAgent: agent}
	agent.files["/etc/nginx/nginx.conf"] = []byte("http { auth_basic inherited; }")
	if err := service.CheckConfig(context.Background(), site, []byte("server {}")); err == nil || !strings.Contains(err.Error(), "auth_basic") {
		t.Fatalf("detect fallback did not inspect inherited config: %v", err)
	}
	service.agent = agent.policyServiceAgent
	if err := service.CheckConfig(context.Background(), site, []byte("server {}")); err != nil {
		t.Fatalf("minimal fake compatibility: %v", err)
	}
}
