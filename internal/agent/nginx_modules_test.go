package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/nginxmodule"
	"github.com/luoye663/nxpanel/internal/nginxpatch"
)

func TestModuleOSPointReleases(t *testing.T) {
	for _, scenario := range []struct {
		id        string
		versionID string
		pretty    string
		want      string
	}{
		{"debian", "13", "Debian GNU/Linux 13 (trixie)", "debian:13"},
		{"debian", "13.1", "Debian GNU/Linux 13.1 (trixie)", "debian:13"},
		{"ubuntu", "24.04", "Ubuntu 24.04.2 LTS", "ubuntu:24.04"},
		{"rocky", "9.8", "Rocky Linux 9.8", "rocky:9"},
	} {
		t.Run(scenario.id+":"+scenario.versionID, func(t *testing.T) {
			release := fmt.Sprintf("ID=%s\nVERSION_ID=%q\nPRETTY_NAME=%q\n", scenario.id, scenario.versionID, scenario.pretty)
			if got := moduleOS(release); got != scenario.want {
				t.Fatalf("moduleOS() = %q; want %q", got, scenario.want)
			}
		})
	}
}

func TestModuleManagedConfig(t *testing.T) {
	source := moduleSource{}
	digest := strings.Repeat("a", 64)
	path := source.assetPath(nginxmodule.GeoIP2, digest)
	fragment := []byte(moduleFragmentMark + digest + " " + nginxmodule.Symbol + "\nload_module \"" + path + "\";\n")
	if parsed, ok := source.parseFragment(nginxmodule.GeoIP2, fragment); !ok || parsed.path != path || parsed.binaryHash != digest {
		t.Fatal("owned GeoIP2 fragment not recognized")
	}
	if _, ok := source.parseFragment(nginxmodule.GeoIP2, []byte(strings.Replace(string(fragment), path, "/tmp/geoip2.so", 1))); ok {
		t.Fatal("manual module accepted as an owned fragment")
	}
}

func TestModuleManagedConfigRejectsMismatchedLoadEntry(t *testing.T) {
	official := moduleSource{}
	testFeed := moduleSource{test: true}
	for _, scenario := range []struct {
		name   string
		source moduleSource
		conf   string
		reason string
	}{
		{"test to official", official, testFeed.includeBlock(), "引用本地测试源的模块加载目录，当前 Agent 使用正式源"},
		{"official to test", testFeed, official.includeBlock(), "引用正式源的模块加载目录，当前 Agent 使用本地测试源"},
		{"modified include", official, moduleIncludeMark + "include /custom/*.conf;\n", "模块加载入口已被修改"},
		{"moved include", official, "# Nginx config\n" + official.includeBlock(), "模块加载入口已被修改"},
		{"duplicate marker", official, testFeed.includeBlock() + official.includeBlock(), "模块加载入口已被修改"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			managed, err := scenario.source.managedModules(moduleRuntime{conf: []byte(scenario.conf + "events {}\n")})
			if managed != nil || err == nil || !strings.Contains(err.Error(), scenario.reason) {
				t.Fatalf("managedModules() = %v, %v; want rejection containing %q", managed, err, scenario.reason)
			}
		})
	}
}

func TestModuleRPCRejectsUnknownInputs(t *testing.T) {
	s := &Server{}
	digest := strings.Repeat("a", 64)
	for _, body := range []string{
		`{"id":"../other","confirmation":"` + digest + `"}`,
		`{"id":"geoip2","confirmation":"` + digest + `","url":"https://example.com"}`,
		`{"id":"geoip2","confirmation":"` + digest + `"} {}`,
		`{"id":"geoip2","confirmation":"not-a-ticket"}`,
	} {
		recorder := httptest.NewRecorder()
		s.handleNginxModuleInstall(recorder, httptest.NewRequest(http.MethodPost, "/internal/v1/nginx/modules/install", strings.NewReader(body)))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("accepted unsafe request %s: %d", body, recorder.Code)
		}
	}
}

func TestModuleConfirmationAndArtifactDigest(t *testing.T) {
	instance := moduleRuntime{binaryHash: strings.Repeat("a", 64), confPath: "/etc/nginx/nginx.conf", conf: []byte("events {}")}
	instance.selection.status.RestartGeneration = "generation-1"
	ticket := moduleConfirmation(instance, "catalog-1")
	if moduleConfirmation(instance, "catalog-2") == ticket {
		t.Fatal("confirmation reused across catalogs")
	}
	instance.conf = []byte("events {} http {}")
	if moduleConfirmation(instance, "catalog-1") == ticket {
		t.Fatal("confirmation reused across configuration changes")
	}
	body := []byte("verified bytes")
	asset := nginxmodule.Asset{Size: int64(len(body)), SHA256: sha256Text(body)}
	if !moduleAssetMatches(body, asset) || moduleAssetMatches([]byte("modified bytes"), asset) || moduleAssetMatches(append(body, '\n'), asset) {
		t.Fatal("incorrect artifact size or digest check")
	}
}

func TestModuleConfigTopLevelInclude(t *testing.T) {
	dir := t.TempDir()
	writeRestartFixture(t, filepath.Join(dir, "module.conf"), `load_module "modules/ngx_http_geoip2_module.so";`)
	paths := []string{}
	config := runtimeConfig{base: dir, budget: &runtimeConfigBudget{2048, 10, 40}, active: make(map[string]bool), module: func(value string) error {
		paths = append(paths, value)
		return nil
	}}
	if err := config.scan("# load_module ignored;\ninclude module*.conf;\nevents {}\nhttp { include unrelated-site.conf; }"); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "modules/ngx_http_geoip2_module.so" {
		t.Fatalf("module paths = %v", paths)
	}
}

func TestModuleWorkersDoNotRequireReadingWorkerExecutable(t *testing.T) {
	_, proc, add := runtimeFixture(t)
	add(100, 1, "nginx: master process nginx", 10)
	add(101, 100, "nginx: worker process", 10)
	worker := filepath.Join(proc, "101", "exe")
	if err := os.Remove(worker); err != nil {
		t.Fatal(err)
	}
	children := filepath.Join(proc, "100", "task", "100", "children")
	if err := os.MkdirAll(filepath.Dir(children), 0755); err != nil {
		t.Fatal(err)
	}
	writeRestartFixture(t, children, "101")
	module := moduleRuntime{nginxRuntime: nginxRuntime{procRoot: proc}}
	selection := runtimeSelection{master: &nginxRuntimeProcess{pid: 100, start: 10000}}
	workers, err := module.activeWorkers(selection)
	if err != nil || !workers[nginxProcess{pid: "101", start: "10000"}] {
		t.Fatalf("worker scan failed without access to executable: %v %v", workers, err)
	}
}

func TestModuleActionReadiness(t *testing.T) {
	server := &Server{cfg: &app.Config{}}
	for _, scenario := range []struct {
		name, reason string
	}{
		{"not running", "未找到面板管理"},
		{"no worker", "未发现活跃 worker"},
		{"maintenance", "主配置仍包含面板维护模式"},
		{"same mount namespace", ""},
		{"verified systemd service", ""},
		{"different root user", "root 身份"},
		{"different PID namespace", "运行环境"},
		{"missing mount namespace", "无法核验 Nginx 主进程的挂载命名空间"},
		{"different mount with no systemd", "不属于可核验的 systemd 服务"},
		{"different systemd PID", "无法核验其 systemd 服务归属"},
		{"different systemd arguments", "无法核验其 systemd 服务归属"},
		{"missing config in runtime view", "挂载路径不一致"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			runtime, _, _, _ := restartFixture(t)
			runtime.systemd = scenario.name != "different mount with no systemd"
			mainPID := "100"
			serviceConfig := runtime.conf
			masterDir := filepath.Join(runtime.procRoot, mainPID)
			for _, path := range []string{masterDir, filepath.Join(runtime.procRoot, "self")} {
				if err := os.Remove(filepath.Join(path, "root")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/", filepath.Join(path, "root")); err != nil {
					t.Fatal(err)
				}
			}
			if scenario.name != "same mount namespace" {
				namespaces := filepath.Join(runtime.prefix, "master-namespaces")
				if err := os.Remove(filepath.Join(masterDir, "ns")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(namespaces, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(namespaces, filepath.Join(masterDir, "ns")); err != nil {
					t.Fatal(err)
				}
				for _, namespace := range []string{"pid", "user", "net"} {
					if scenario.name == "different PID namespace" && namespace == "pid" {
						writeRestartFixture(t, filepath.Join(namespaces, namespace), "other namespace")
					} else if err := os.Symlink(filepath.Join(runtime.prefix, namespace), filepath.Join(namespaces, namespace)); err != nil {
						t.Fatal(err)
					}
				}
				if scenario.name != "missing mount namespace" {
					writeRestartFixture(t, filepath.Join(namespaces, "mnt"), "other mount namespace")
				}
			}
			runtime.run = func(_ context.Context, bin string, args ...string) (CmdResult, error) {
				if bin != "systemctl" || args[0] != "show" {
					t.Fatalf("unexpected command: %s %v", bin, args)
				}
				if args[1] != "openresty.service" {
					return CmdResult{Stdout: "0\n"}, nil
				}
				if args[2] == "--property=MainPID" {
					return CmdResult{Stdout: mainPID}, nil
				}
				return CmdResult{Stdout: fmt.Sprintf("Id=openresty.service\nExecStart={ path=%s ; argv[]=%s -p %s -c %s ; }\n", runtime.bin, runtime.bin, runtime.prefix, serviceConfig)}, nil
			}
			selection, err := runtime.inspect(t.Context())
			if err != nil || selection.master == nil {
				t.Fatalf("could not select fixture master: %v", err)
			}
			instance := moduleRuntime{nginxRuntime: runtime, selection: selection, confPath: runtime.conf}
			switch scenario.name {
			case "not running":
				instance.selection.master = nil
			case "maintenance":
				instance.conf = []byte(nginxpatch.MaintenanceStart + "site")
			case "different root user":
				writeRestartFixture(t, filepath.Join(masterDir, "status"), "Uid:\t33\t33\t33\t33\n")
			case "different systemd PID":
				mainPID = "101"
			case "different systemd arguments":
				serviceConfig += ".other"
			case "missing config in runtime view":
				if err := os.Remove(runtime.conf); err != nil {
					t.Fatal(err)
				}
			}
			err = server.moduleActionReadiness(t.Context(), instance, scenario.name != "no worker", "fixture-http", "")
			if scenario.reason == "" && err != nil || scenario.reason != "" && (err == nil || !strings.Contains(err.Error(), scenario.reason)) {
				t.Fatalf("readiness = %v, want %q", err, scenario.reason)
			}
			if scenario.name == "verified systemd service" && runtime.localRootMaster(100) {
				t.Fatal("module exception also relaxed the manual restart check")
			}
		})
	}
}

func TestModulePathSharing(t *testing.T) {
	hostDir, peerDir := t.TempDir(), t.TempDir()
	for _, dir := range []string{hostDir, peerDir} {
		if err := os.Mkdir(filepath.Join(dir, "config"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	hostConf, peerConf := filepath.Join(hostDir, "config", "nginx.conf"), filepath.Join(peerDir, "config", "nginx.conf")
	writeRestartFixture(t, hostConf, "events {}")
	if err := os.Link(hostConf, peerConf); err != nil {
		t.Fatal(err)
	}
	host, err := os.OpenRoot(hostDir)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	peer, err := os.OpenRoot(peerDir)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if !modulePathShared(host, host, "/config/nginx.conf", false, true) || !modulePathShared(host, host, "/config/new/asset.so", true, true) {
		t.Fatal("shared config or future asset path rejected")
	}
	if modulePathShared(host, peer, "/config/nginx.conf", false, true) || modulePathShared(host, peer, "/config/new/asset.so", true, true) {
		t.Fatal("same file inode or missing paths accepted under different parent directories")
	}
	if err := os.Symlink("missing", filepath.Join(hostDir, "config", "new")); err != nil {
		t.Fatal(err)
	}
	if modulePathShared(host, host, "/config/new/asset.so", true, true) {
		t.Fatal("dangling symlink treated as an unallocated module path")
	}
}
