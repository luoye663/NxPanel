package accesspolicy

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Set NXPANEL_POLICY_NGINX_IMAGE to a locally available Nginx/OpenResty image.
// The test publishes only a random loopback port and always removes its container.
func TestNginxHTTP(t *testing.T) {
	image := os.Getenv("NXPANEL_POLICY_NGINX_IMAGE")
	if image == "" {
		t.Skip("set NXPANEL_POLICY_NGINX_IMAGE for native HTTP integration")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	p := Policy{Mode: "unified", DefaultAction: Action{Type: "allow"}, Rules: []Rule{
		{ID: "allow", Enabled: true, Match: "all", Conditions: []Condition{{Kind: "path", Operator: "exact", Values: []string{"/rewrite", "/admin/public"}}}, Action: Action{Type: "allow"}},
		{ID: "deny", Enabled: true, Match: "all", Conditions: []Condition{{Kind: "path", Operator: "prefix", Values: []string{"/blocked"}}}, Action: Action{Type: "deny", StatusCode: 451, ResponseType: "html", ResponseBody: "<p>literal $uri \"quoted\" \\ slash</p>"}},
		{ID: "drop", Enabled: true, Match: "all", Conditions: []Condition{{Kind: "path", Operator: "exact", Values: []string{"/drop"}}}, Action: Action{Type: "deny", StatusCode: 444}},
		{ID: strings.Repeat("a", 64), Enabled: true, Match: "all", Conditions: []Condition{{Kind: "path", Operator: "prefix", Values: []string{"/admin", "/proxy"}}}, Action: Action{Type: "auth", AccountIDs: []string{"user"}}},
		{ID: "referer", Enabled: true, Match: "all", Conditions: []Condition{{Kind: "extension", Values: []string{"jpg"}}, {Kind: "referer", Values: []string{"example.com", "none", "blocked"}, Negate: true}}, Action: Action{Type: "deny", StatusCode: 403, ResponseBody: "hotlink"}},
	}}
	p.Rules = append([]Rule{
		{ID: "office", Enabled: true, Match: "all", Conditions: []Condition{{Kind: "ip", Values: []string{"10.0.0.0/8", "2001:db8::/48"}}}, Action: Action{Type: "allow"}},
		{ID: "region", Enabled: true, Match: "all", Conditions: []Condition{{Kind: "country", Values: []string{"CN"}}}, Action: Action{Type: "deny", StatusCode: 452, ResponseBody: "region"}},
	}, p.Rules...)
	ctx := RenderContext{CountryNetworks: map[string][]string{"CN": {"10.0.0.0/8", "2001:db8::/32"}}, AuthFiles: map[string]string{strings.Repeat("a", 64): "user:{SHA}W6ph5Mm5Pz8GgiULbPgzG37mj9g=\n"}}
	compiled, err := Compile("native-test", "/policy", p, ctx)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		file := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("global.conf", compiled.GlobalContent)
	write("server.conf", compiled.ServerContent)
	for name, body := range compiled.Files {
		write(strings.TrimPrefix(name, "/policy/"), body)
	}
	for _, name := range []string{"admin/private", "admin/public", "photo.jpg", "ok"} {
		write("www/"+name, "content")
	}
	config := `worker_processes 1;
pid /tmp/nginx-policy.pid;
error_log /dev/stderr info;
events { worker_connections 64; }
http {
 access_log off;
 set_real_ip_from 0.0.0.0/0;
 real_ip_header X-Forwarded-For;
 real_ip_recursive on;
 include /policy/global.conf;
 server {
  listen 8080;
  root /policy/www;
  include /policy/server.conf;
  location = /rewrite { try_files /missing /admin/private; }
  location /proxy { proxy_pass http://127.0.0.1:8081; }
 }
 server { listen 8081; location / { return 200 "upstream"; } }
}`
	write("nginx.conf", config)
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	name := "nxpolicy-" + filepath.Base(dir) + fmt.Sprint(time.Now().UnixNano())
	run("run", "--detach", "--rm", "--name", name, "--publish", "127.0.0.1::8080", "--volume", dir+":/policy:ro", "--entrypoint", "nginx", image, "-c", "/policy/nginx.conf", "-g", "daemon off;")
	defer exec.Command("docker", "rm", "-f", name).Run()
	address := run("port", name, "8080/tcp")
	client := &http.Client{Timeout: 3 * time.Second}
	var ready bool
	for i := 0; i < 50; i++ {
		resp, err := client.Get("http://" + address + "/ok")
		if err == nil {
			resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("nginx did not start: %s", run("logs", name))
	}
	cases := []struct {
		path, referer, password string
		code                    int
		body, mime              string
	}{
		{"/ok", "", "", 200, "content", ""},
		{"/blocked/test", "", "", 451, p.Rules[3].Action.ResponseBody, "text/html"},
		{"/admin/private", "", "", 401, "", ""},
		{"/admin/private", "", "wrong", 401, "", ""},
		{"/admin/private", "", "password", 200, "content", ""},
		{"/admin/public", "", "", 200, "content", ""},
		{"/rewrite", "", "", 200, "content", ""},
		{"/proxy", "", "", 401, "", ""},
		{"/proxy", "", "password", 200, "upstream", ""},
		{"/photo.jpg", "https://evil.example/", "", 403, "hotlink", "text/plain"},
		{"/photo.jpg", "https://example.com/", "", 200, "content", ""},
		{"/photo.jpg", "", "", 200, "content", ""},
		{"/photo.jpg", "about:blank", "", 200, "content", ""},
		{"/photo.jpg", "HTTPS://EXAMPLE.COM/a", "", 200, "content", ""},
	}
	for _, tt := range cases {
		t.Run(tt.path+tt.referer+tt.password, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "http://"+address+tt.path, nil)
			if tt.referer != "" {
				req.Header.Set("Referer", tt.referer)
			}
			if tt.password != "" {
				req.SetBasicAuth("user", tt.password)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tt.code {
				t.Fatalf("status %d want %d body %s", resp.StatusCode, tt.code, body)
			}
			if tt.body != "" && string(body) != tt.body {
				t.Errorf("body %q want %q", body, tt.body)
			}
			if tt.mime != "" && !strings.HasPrefix(resp.Header.Get("Content-Type"), tt.mime) {
				t.Errorf("Content-Type %q", resp.Header.Get("Content-Type"))
			}
		})
	}
	checkIP := func(ip string, want int) bool {
		t.Helper()
		req, _ := http.NewRequest("GET", "http://"+address+"/ok", nil)
		req.Header.Set("X-Forwarded-For", ip)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode == want
	}
	if !checkIP("10.1.2.3", 200) || !checkIP("2001:db8::1", 200) {
		t.Fatal("IP allow did not win against region deny")
	}
	if !checkIP("2001:db8:1::1", 452) {
		t.Fatal("IPv6 country rule did not match outside allowed prefix")
	}
	p.Rules[0], p.Rules[1] = p.Rules[1], p.Rules[0]
	p.DefaultAction = Action{Type: "deny", StatusCode: 449, ResponseBody: "default deny"}
	updated, err := Compile("native-test", "/policy", p, ctx)
	if err != nil {
		t.Fatal(err)
	}
	write("global.conf", updated.GlobalContent)
	write("server.conf", updated.ServerContent)
	run("exec", name, "nginx", "-c", "/policy/nginx.conf", "-s", "reload")
	client.CloseIdleConnections()
	applied := false
	for i := 0; i < 50; i++ {
		client.CloseIdleConnections()
		if checkIP("10.1.2.3", 452) && checkIP("2001:db8::1", 452) && checkIP("8.8.8.8", 449) {
			applied = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !applied {
		t.Fatal("moving country above IP did not change native HTTP outcome")
	}

	p.DefaultAction = Action{Type: "auth", AccountIDs: []string{"user"}}
	ctx.AuthFiles["default"] = ctx.AuthFiles[strings.Repeat("a", 64)]
	updated, err = Compile("native-test", "/policy", p, ctx)
	if err != nil {
		t.Fatal(err)
	}
	write("global.conf", updated.GlobalContent)
	write("server.conf", updated.ServerContent)
	for file, body := range updated.Files {
		write(strings.TrimPrefix(file, "/policy/"), body)
	}
	run("exec", name, "nginx", "-c", "/policy/nginx.conf", "-s", "reload")
	applied = false
	for i := 0; i < 50; i++ {
		client.CloseIdleConnections()
		if checkIP("8.8.8.8", 401) {
			applied = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !applied {
		t.Fatal("default authentication did not require a password")
	}
	authReq, _ := http.NewRequest("GET", "http://"+address+"/ok", nil)
	authReq.SetBasicAuth("user", "password")
	authResponse, err := client.Do(authReq)
	if err != nil {
		t.Fatal(err)
	}
	authResponse.Body.Close()
	if authResponse.StatusCode != 200 {
		t.Fatalf("default authentication success status %d", authResponse.StatusCode)
	}

	if resp, err := client.Get("http://" + address + "/drop"); err == nil {
		resp.Body.Close()
		t.Fatalf("444 unexpectedly returned HTTP %d", resp.StatusCode)
	}
}
