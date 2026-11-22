package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func runtimeFixture(t *testing.T) (*NginxExecutor, string, func(int, int, string, int)) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "nginx")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nif [ \"$1\" = -V ]; then\n echo 'configure arguments: --prefix=/usr/share/nginx --conf-path=/etc/nginx/nginx.conf' >&2\n exit 0\nfi\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	proc := filepath.Join(root, "proc")
	if err := os.Mkdir(proc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "uptime"), []byte("200.50 20.00"), 0644); err != nil {
		t.Fatal(err)
	}
	add := func(pid, parent int, title string, rss int) {
		t.Helper()
		dir := filepath.Join(proc, strconv.Itoa(pid))
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(bin, filepath.Join(dir, "exe")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(title+"\x00"), 0644); err != nil {
			t.Fatal(err)
		}
		fields := make([]string, 22)
		for i := range fields {
			fields[i] = "0"
		}
		fields[0], fields[1], fields[19], fields[21] = "S", strconv.Itoa(parent), "10000", strconv.Itoa(rss)
		fields[3] = strconv.Itoa(pid)
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(fmt.Sprintf("%d (nginx worker) %s", pid, strings.Join(fields, " "))), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return NewNginxExecutorWithDefaults(bin, "/etc/nginx/nginx.conf"), proc, add
}

func loadRuntimeFixture(t *testing.T, executor *NginxExecutor, proc string) nginxRuntime {
	t.Helper()
	runtime, _, err := executor.loadRuntime(t.Context(), proc)
	if err != nil {
		t.Fatal(err)
	}
	runtime.s6, runtime.systemd = false, false
	return runtime
}

func TestNginxRuntimeStatus(t *testing.T) {
	e, proc, add := runtimeFixture(t)
	runtime := loadRuntimeFixture(t, e, proc)
	status, err := runtime.status(t.Context())
	if err != nil || status.Running || status.MemoryRSS != 0 {
		t.Fatalf("stopped: %+v %v", status, err)
	}
	add(100, 1, "nginx: master process nginx -c /etc/nginx/nginx.conf", 10)
	add(101, 100, "nginx: worker process", 20)
	add(102, 100, "nginx: cache manager process", 3)
	add(103, 1, "nginx: worker process", 99)
	status, err = runtime.status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Running || status.MasterPID != 100 || status.Workers != 1 || status.Processes != 3 || status.MemoryRSS != uint64(33*os.Getpagesize()) || status.UptimeSeconds != 100 {
		t.Fatalf("unexpected status: %+v", status)
	}
	add(104, 1, "nginx: master process nginx -p/other -cnginx.conf", 1)
	add(105, 104, "nginx: worker process", 99)
	status, err = runtime.status(t.Context())
	if err != nil || status.MasterPID != 100 || status.Processes != 3 || len(status.Instances) != 2 || status.Instances[1].ConfPath != "/other/nginx.conf" || !status.OtherActionsBlocked || status.RestartGeneration == "" {
		t.Fatalf("unrelated master affected managed instance: %+v %v", status, err)
	}
	add(106, 1, "nginx: master process nginx", 1)
	status, err = runtime.status(t.Context())
	if err != nil || status.BlockedReason == "" || status.Running || status.RestartGeneration != "" || len(status.Instances) != 3 {
		t.Fatalf("ambiguous masters: %+v %v", status, err)
	}
	runtime.run = func(_ context.Context, bin string, args ...string) (CmdResult, error) {
		t.Fatalf("must not issue a command with ambiguous masters: %s %v", bin, args)
		return CmdResult{}, nil
	}
	if _, err := runtime.restart(t.Context(), ""); err == nil || !strings.Contains(err.Error(), "多个") {
		t.Fatalf("ambiguous restart: %v", err)
	}
}

func TestNginxRuntimeRejectsDifferentConfig(t *testing.T) {
	for _, args := range []string{"-c /other/nginx.conf", "-c/other/nginx.conf", "-p/other -cnginx.conf"} {
		t.Run(args, func(t *testing.T) {
			e, proc, add := runtimeFixture(t)
			add(100, 1, "nginx: master process nginx "+args, 10)
			status, err := loadRuntimeFixture(t, e, proc).status(t.Context())
			if err != nil || status.Running || !status.OtherActionsBlocked || status.Instances[0].Match != "other" {
				t.Fatalf("different config: %+v %v", status, err)
			}
		})
	}
}

func TestNginxRuntimeDoesNotAssumeDefaultsOfDeletedBinary(t *testing.T) {
	executor, proc, add := runtimeFixture(t)
	add(100, 1, nginxMasterTitle+"nginx", 1)
	path := filepath.Join(proc, "100", "exe")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executor.GetBin()+" (deleted)", path); err != nil {
		t.Fatal(err)
	}
	status, err := loadRuntimeFixture(t, executor, proc).status(t.Context())
	if err != nil || status.Running || status.BlockedReason == "" || status.Instances[0].Match != "unknown" {
		t.Fatalf("deleted binary identified using a new binary's compiled defaults: %+v %v", status, err)
	}
}

func TestNginxRuntimeMalformedStat(t *testing.T) {
	for _, input := range []string{"", "1 (nginx) S 1", "1 nginx S 1"} {
		if _, _, _, _, err := parseNginxProcStat(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestNginxControlRejectsInvalidActionAndBadConfig(t *testing.T) {
	e, _, _ := runtimeFixture(t)
	if _, err := e.control(context.Background(), "reload; stop", ""); err == nil {
		t.Fatal("invalid action accepted")
	}
	if _, err := e.control(context.Background(), "start", ""); err == nil || !strings.Contains(err.Error(), "配置检查未通过") {
		t.Fatalf("start did not fail before control: %v", err)
	}
}

func TestNginxControlBusy(t *testing.T) {
	e, _, _ := runtimeFixture(t)
	s := &Server{executor: e}
	e.lifecycleMu.Lock()
	defer e.lifecycleMu.Unlock()
	for _, action := range []string{"start", "stop", "restart", "reload"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/nginx/"+action, strings.NewReader("{}"))
		if action == "reload" {
			s.handleNginxReload(rec, req)
		} else {
			s.handleNginxControl(rec, req)
		}
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s: status %d", action, rec.Code)
		}
	}
}

func TestNginxSystemdExecStartIdentity(t *testing.T) {
	e, _, _ := runtimeFixture(t)
	for _, tc := range []struct {
		path, args string
		want       bool
	}{
		{e.GetBin(), e.GetBin(), true},
		{e.GetBin(), e.GetBin() + " -c /etc/nginx/nginx.conf", true},
		{e.GetBin(), e.GetBin() + " -c/other/nginx.conf", false},
		{"/other/nginx", e.GetBin(), false},
		{e.GetBin(), e.GetBin() + " -p /etc/nginx -c nginx.conf", true},
	} {
		output := fmt.Sprintf("{ path=%s ; argv[]=%s ; ignore_errors=no ; }", tc.path, tc.args)
		if got := e.matchesSystemdExecStart(output, "/etc/nginx/nginx.conf", "/usr/share/nginx"); got != tc.want {
			t.Fatalf("%s: got %v want %v", output, got, tc.want)
		}
	}
	if e.matchesSystemdExecStart("", "/etc/nginx/nginx.conf", "/usr/share/nginx") {
		t.Fatal("accepted missing unit")
	}
}

func TestNginxRuntimePSS(t *testing.T) {
	e, proc, add := runtimeFixture(t)
	runtime := loadRuntimeFixture(t, e, proc)
	add(100, 1, "nginx: master process nginx", 100)
	add(101, 100, "nginx: worker process", 100)
	for _, pid := range []string{"100", "101"} {
		if err := os.WriteFile(filepath.Join(proc, pid, "smaps_rollup"), []byte("Rss: 400 kB\nPss: 150 kB\nPss_Anon: 140 kB\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	status, err := runtime.status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status.MemoryPSS == nil || *status.MemoryPSS != 300*1024 {
		t.Fatalf("unexpected PSS: %+v", status.MemoryPSS)
	}
	// A process without PSS must invalidate the sum, not silently undercount it.
	if err := os.WriteFile(filepath.Join(proc, "101", "smaps_rollup"), []byte("Rss: 400 kB\n"), 0644); err != nil {
		t.Fatal(err)
	}
	status, err = runtime.status(t.Context())
	if err != nil || status.MemoryPSS != nil || status.MemoryRSS != uint64(200*os.Getpagesize()) {
		t.Fatalf("fallback: %+v %v", status, err)
	}
}

func TestReadNginxPSS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smaps_rollup")
	for _, text := range []string{"", "Pss_Anon: 10 kB\n", "Pss: -1 kB\n", "Pss: 18446744073709551615 kB\n", "Pss: 20 MB\n"} {
		if err := os.WriteFile(path, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := readNginxPSS(path); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}
