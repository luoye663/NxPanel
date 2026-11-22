package georuntime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

func TestNativeGeoIP2(t *testing.T) {
	binary, module, databasePath := os.Getenv("NXPANEL_GEOIP2_TEST_NGINX"), os.Getenv("NXPANEL_GEOIP2_TEST_MODULE"), os.Getenv("NXPANEL_GEOIP2_TEST_DB")
	if binary == "" || databasePath == "" {
		t.Skip("set NXPANEL_GEOIP2_TEST_NGINX and NXPANEL_GEOIP2_TEST_DB, plus NXPANEL_GEOIP2_TEST_MODULE for a dynamic module")
	}
	var load string
	if module != "" {
		quoted, err := QuotePath(module)
		if err != nil {
			t.Fatal(err)
		}
		load = "load_module " + quoted + ";\n"
	}
	shared, err := Config(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	database, err := maxminddb.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	root := t.TempDir()
	config := load + fmt.Sprintf(`worker_processes 1;
pid %s/nginx.pid;
error_log %s/error.log;
events {}
http {
    access_log off;
    %s
    server {
        listen unix:%s/trusted.sock;
        set_real_ip_from unix:;
        real_ip_header X-Forwarded-For;
        real_ip_recursive on;
        location / { return 200 %s; }
    }
    server {
        listen unix:%s/direct.sock;
        location / { return 200 %s; }
    }
}
`, root, root, shared, root, CountryVariable, root, CountryVariable)
	startTestNginx(t, binary, root, config)
	for _, test := range []struct{ address, socket string }{
		{"8.8.8.8", "trusted.sock"}, {"2001:4860:4860::8888", "trusted.sock"}, {"192.0.2.1", "trusted.sock"}, {"8.8.8.8", "direct.sock"},
	} {
		expected := "ZZ"
		if test.socket == "trusted.sock" {
			expected = nativeTestCountry(t, database, test.address)
		}
		if got := requestCountry(t, filepath.Join(root, test.socket), test.address); got != expected {
			t.Errorf("lookup of %s via %s = %q, want %q", test.address, test.socket, got, expected)
		}
	}
}

func nativeTestCountry(t *testing.T, database *maxminddb.Reader, address string) string {
	t.Helper()
	type country struct {
		ISOCode string `maxminddb:"iso_code"`
	}
	var record struct {
		Country    country `maxminddb:"country"`
		Registered country `maxminddb:"registered_country"`
	}
	if err := database.Lookup(net.ParseIP(address), &record); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{record.Country.ISOCode, record.Registered.ISOCode} {
		if len(code) == 2 {
			return code
		}
	}
	return "ZZ"
}

func startTestNginx(t *testing.T, binary, root, config string) *os.Process {
	t.Helper()
	if err := os.Mkdir(filepath.Join(root, "logs"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "nginx.conf")
	if err := os.WriteFile(path, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(binary, "-t", "-p", root, "-c", path).CombinedOutput(); err != nil {
		t.Fatalf("nginx -t: %v: %s", err, output)
	}
	command := exec.Command(binary, "-p", root, "-c", path, "-g", "daemon off;")
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	exit := make(chan error, 1)
	go func() { exit <- command.Wait() }()
	t.Cleanup(func() {
		_ = command.Process.Signal(syscall.SIGQUIT)
		select {
		case <-exit:
		case <-time.After(3 * time.Second):
			_ = command.Process.Kill()
			<-exit
		}
	})
	return command.Process
}

func requestCountry(t *testing.T, socket, ip string) string {
	t.Helper()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	request, err := http.NewRequest(http.MethodGet, "http://localhost/", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Forwarded-For", ip)
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Do(request)
		if err == nil {
			defer response.Body.Close()
			body, err := io.ReadAll(io.LimitReader(response.Body, 1024))
			if err != nil || response.StatusCode != http.StatusOK {
				t.Fatalf("request returned %d: %s, %v", response.StatusCode, body, err)
			}
			return string(body)
		}
		time.Sleep(50 * time.Millisecond)
	}
	log, _ := os.ReadFile(filepath.Join(filepath.Dir(socket), "error.log"))
	t.Fatalf("nginx did not accept test traffic on %s: %s", socket, log)
	return ""
}

func TestNativeGeoIP2Memory(t *testing.T) {
	binary, module, databasePath := os.Getenv("NXPANEL_GEOIP2_TEST_NGINX"), os.Getenv("NXPANEL_GEOIP2_TEST_MODULE"), os.Getenv("NXPANEL_GEOIP2_TEST_DB")
	cachePath := os.Getenv("NXPANEL_GEOIP2_TEST_COUNTRY_CACHE")
	if binary == "" || databasePath == "" || cachePath == "" {
		t.Skip("set NXPANEL_GEOIP2_TEST_COUNTRY_CACHE to compare PSS against the old CIDR compiler")
	}
	cache, err := os.Open(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	var oldData struct{ Networks map[string][]string }
	if err := json.NewDecoder(io.LimitReader(cache, 128<<20)).Decode(&oldData); err != nil {
		t.Fatal(err)
	}
	entriesPath := filepath.Join(t.TempDir(), "networks.conf")
	entries, err := os.Create(entriesPath)
	if err != nil {
		t.Fatal(err)
	}
	defer entries.Close()
	writer := bufio.NewWriter(entries)
	for code, networks := range oldData.Networks {
		if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
			t.Fatalf("invalid country code %q", code)
		}
		for _, network := range networks {
			prefix, err := netip.ParsePrefix(network)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fmt.Fprintf(writer, "%s %s;\n", prefix, code); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	shared, err := Config(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, sites := range []int{1, 10} {
		var oldPSS, newPSS int
		for _, modern := range []bool{false, true} {
			t.Run(fmt.Sprintf("sites-%d/mmdb-%t", sites, modern), func(t *testing.T) {
				root := t.TempDir()
				var config strings.Builder
				if modern && module != "" {
					quoted, err := QuotePath(module)
					if err != nil {
						t.Fatal(err)
					}
					fmt.Fprintf(&config, "load_module %s;\n", quoted)
				}
				if os.Geteuid() == 0 {
					config.WriteString("user root;\n")
				}
				fmt.Fprintf(&config, "worker_processes 1; pid %s/nginx.pid; error_log %s/error.log; events {}\nhttp { access_log off;\n", root, root)
				if modern {
					config.WriteString(shared)
				}
				for index := range sites {
					source := CountryVariable
					if !modern {
						source = fmt.Sprintf("$previous_site_%d", index)
						fmt.Fprintf(&config, "geo %s { default ZZ; include %s; }\n", source, entriesPath)
					}
					fmt.Fprintf(&config, "map %s $site_%d { default pass; CN cn; }\n", source, index)
					fmt.Fprintf(&config, "server { listen unix:%s/geo.sock; server_name site%d; set_real_ip_from unix:; real_ip_header X-Forwarded-For; location / { return 200 $site_%d; }}\n", root, index, index)
				}
				config.WriteByte('}')
				master := startTestNginx(t, binary, root, config.String())
				for _, address := range []string{"1.0.1.0", "8.8.8.8", "2001:4860:4860::8888"} {
					requestCountry(t, filepath.Join(root, "geo.sock"), address)
				}
				pss := testTreePSS(t, master.Pid)
				if modern {
					newPSS = pss
				} else {
					oldPSS = pss
				}
				t.Logf("master + worker PSS: %d KiB", pss)
			})
		}
		if newPSS <= 0 || newPSS >= oldPSS {
			t.Errorf("%d sites: MMDB PSS %d KiB, old CIDR PSS %d KiB", sites, newPSS, oldPSS)
		}
	}
}

func testTreePSS(t *testing.T, master int) int {
	t.Helper()
	children, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", master, master))
	if err != nil || len(children) == 0 {
		t.Fatalf("expected an nginx worker: %v", err)
	}
	var total int
	for _, pid := range append([]string{strconv.Itoa(master)}, strings.Fields(string(children))...) {
		mapping, err := os.ReadFile(filepath.Join("/proc", pid, "smaps_rollup"))
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.SplitSeq(string(mapping), "\n") {
			if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "Pss:" {
				pss, err := strconv.Atoi(fields[1])
				if err != nil {
					t.Fatal(err)
				}
				total += pss
			}
		}
	}
	return total
}
