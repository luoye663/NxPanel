package agent

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
)

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
