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
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
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
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(fmt.Sprintf("%d (nginx worker) %s", pid, strings.Join(fields, " "))), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return NewNginxExecutorWithDefaults(bin, "/etc/nginx/nginx.conf"), proc, add
}

func TestNginxRuntimeStatus(t *testing.T) {
	e, proc, add := runtimeFixture(t)
	status, err := e.runtimeStatus(proc)
	if err != nil || status.Running || status.MemoryRSS != 0 {
		t.Fatalf("stopped: %+v %v", status, err)
	}
	add(100, 1, "nginx: master process nginx -c /etc/nginx/nginx.conf", 10)
	add(101, 100, "nginx: worker process", 20)
	add(102, 100, "nginx: cache manager process", 3)
	add(103, 1, "nginx: worker process", 99)
	status, err = e.runtimeStatus(proc)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Running || status.MasterPID != 100 || status.Workers != 1 || status.Processes != 3 || status.MemoryRSS != uint64(33*os.Getpagesize()) || status.UptimeSeconds != 100 {
		t.Fatalf("unexpected status: %+v", status)
	}
	add(104, 1, "nginx: master process nginx", 1)
	if _, err = e.runtimeStatus(proc); err == nil {
		t.Fatal("multiple masters accepted")
	}
}

func TestNginxRuntimeRejectsDifferentConfig(t *testing.T) {
	for _, args := range []string{"-c /other/nginx.conf", "-c/other/nginx.conf", "-p/other -cnginx.conf"} {
		t.Run(args, func(t *testing.T) {
			e, proc, add := runtimeFixture(t)
			add(100, 1, "nginx: master process nginx "+args, 10)
			if _, err := e.runtimeStatus(proc); err == nil {
				t.Fatal("different config accepted")
			}
		})
	}
}

func TestNginxRuntimeMalformedStat(t *testing.T) {
	for _, input := range []string{"", "1 (nginx) S 1", "1 nginx S 1"} {
		if _, _, _, err := parseNginxProcStat(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestNginxControlRejectsInvalidActionAndBadConfig(t *testing.T) {
	e, _, _ := runtimeFixture(t)
	if _, err := e.control(context.Background(), "reload; stop"); err == nil {
		t.Fatal("invalid action accepted")
	}
	for _, action := range []string{"start", "restart"} {
		if _, err := e.control(context.Background(), action); err == nil || !strings.Contains(err.Error(), "配置检查未通过") {
			t.Fatalf("%s did not fail before control: %v", action, err)
		}
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
	add(100, 1, "nginx: master process nginx", 100)
	add(101, 100, "nginx: worker process", 100)
	for _, pid := range []string{"100", "101"} {
		if err := os.WriteFile(filepath.Join(proc, pid, "smaps_rollup"), []byte("Rss: 400 kB\nPss: 150 kB\nPss_Anon: 140 kB\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	status, err := e.runtimeStatus(proc)
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
	status, err = e.runtimeStatus(proc)
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
