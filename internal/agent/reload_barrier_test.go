package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeProcess(t *testing.T, root, exe, pid, start, cmd, children string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(filepath.Join(dir, "task", pid), 0755); err != nil {
		t.Fatal(err)
	}
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "S"
	fields[19] = start
	for name, content := range map[string]string{"stat": fmt.Sprintf("%s (nginx) %s", pid, strings.Join(fields, " ")), "cmdline": cmd, "task/" + pid + "/children": children} {
		if err := writeFileAtomic(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(exe, filepath.Join(dir, "exe")); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
}
func TestReloadBarrierWaitsForOldWorkersToStopAccepting(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "nginx")
	if err := os.WriteFile(exe, []byte("test executable"), 0755); err != nil {
		t.Fatal(err)
	}
	fakeProcess(t, root, exe, "10", "100", "nginx: master process nginx -c /etc/nginx/nginx.conf", "11")
	fakeProcess(t, root, exe, "11", "101", "nginx: worker process", "")
	wait, err := newGenerationProbe(root, exe, "/etc/nginx/nginx.conf", "/etc/nginx/nginx.conf")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- wait(ctx) }()
	fakeProcess(t, root, exe, "12", "102", "nginx: worker process", "")
	if err := writeFileAtomic(filepath.Join(root, "10/task/10/children"), []byte("11 12"), 0644); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		t.Fatalf("reported readiness with an old accepting worker: %v", err)
	case <-time.After(60 * time.Millisecond):
	}
	if err := writeFileAtomic(filepath.Join(root, "11/cmdline"), []byte("nginx: worker process is shutting down"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestReloadBarrierNoOldWorkerAndPIDReuse(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "nginx")
	if err := os.WriteFile(exe, nil, 0755); err != nil {
		t.Fatal(err)
	}
	wait, err := newGenerationProbe(root, exe, "/etc/nginx/nginx.conf", "/etc/nginx/nginx.conf")
	if err != nil {
		t.Fatal(err)
	}
	fakeProcess(t, root, exe, "10", "100", "nginx: master process nginx", "11")
	fakeProcess(t, root, exe, "11", "101", "nginx: worker process", "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := wait(ctx); err != nil {
		t.Fatal(err)
	}
	wait, err = newGenerationProbe(root, exe, "/etc/nginx/nginx.conf", "/etc/nginx/nginx.conf")
	if err != nil {
		t.Fatal(err)
	}
	fakeProcess(t, root, exe, "11", "202", "nginx: worker process", "")
	if err := wait(ctx); err != nil {
		t.Fatal("PID reuse was confused with the old worker:", err)
	}
}

func TestRuntimeConfigPaths(t *testing.T) {
	for _, tc := range []struct{ cmd, conf, prefix, want string }{
		{"nginx -p /opt/nginx/ -c conf/nginx.conf", "/etc/nginx/nginx.conf", "/usr/local/nginx", "/opt/nginx/conf/nginx.conf"},
		{"nginx -cconf/nginx.conf -p/opt/nginx/", "/etc/nginx/nginx.conf", "/usr/local/nginx", "/opt/nginx/conf/nginx.conf"},
		{"nginx -c conf/custom.conf", "/etc/nginx/nginx.conf", "/opt/nginx", "/opt/nginx/conf/custom.conf"},
		{"nginx -p /opt/nginx", "conf/nginx.conf", "/compiled", "/opt/nginx/conf/nginx.conf"},
		{"nginx -p /opt/nginx", "/etc/nginx/nginx.conf", "/compiled", "/etc/nginx/nginx.conf"},
		{"nginx -c /etc/custom.conf -p /opt/nginx", "conf/nginx.conf", "/compiled", "/etc/custom.conf"},
		{"nginx -p relative -c conf/nginx.conf", "conf/nginx.conf", "/compiled", ""},
		{"nginx -c", "conf/nginx.conf", "/compiled", ""},
	} {
		if got := runtimeConfPath(tc.cmd, tc.conf, tc.prefix); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.cmd, got, tc.want)
		}
	}
	prefix, conf := compiledRuntimePaths("configure arguments: --prefix=/opt/nginx --conf-path=conf/custom.conf")
	if prefix != "/opt/nginx" || conf != "conf/custom.conf" {
		t.Fatalf("compiled paths expanded prematurely: %s %s", prefix, conf)
	}
}

func TestReloadBarrierRelativeConfig(t *testing.T) {
	for _, cmd := range []string{
		"nginx: master process nginx -p /opt/nginx/ -c conf/nginx.conf",
		"nginx: master process nginx -cconf/nginx.conf -p/opt/nginx/",
		"nginx: master process nginx -c conf/nginx.conf",
	} {
		t.Run(cmd, func(t *testing.T) {
			root := t.TempDir()
			exe := filepath.Join(root, "nginx")
			if err := os.WriteFile(exe, nil, 0755); err != nil {
				t.Fatal(err)
			}
			fakeProcess(t, root, exe, "10", "100", cmd, "11")
			fakeProcess(t, root, exe, "11", "101", "nginx: worker process", "")
			wait, err := newGenerationProbe(root, exe, "/opt/nginx/conf/nginx.conf", "/etc/nginx/nginx.conf", "/opt/nginx")
			if err != nil {
				t.Fatal(err)
			}
			fakeProcess(t, root, exe, "11", "101", "nginx: worker process is shutting down", "")
			fakeProcess(t, root, exe, "12", "102", "nginx: worker process", "")
			fakeProcess(t, root, exe, "10", "100", cmd, "11 12")
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := wait(ctx); err != nil {
				t.Fatalf("relative-config worker transition not detected: %v", err)
			}
			other, err := newGenerationProbe(root, exe, "/other/conf/nginx.conf", "/etc/nginx/nginx.conf", "/opt/nginx")
			if err != nil {
				t.Fatal(err)
			}
			short, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer stop()
			if err := other(short); err == nil {
				t.Fatal("accepted a master using a different configuration")
			}
		})
	}
}
func TestReloadBarrierAmbiguityAndCancellation(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "nginx")
	if err := os.WriteFile(exe, nil, 0755); err != nil {
		t.Fatal(err)
	}
	fakeProcess(t, root, exe, "10", "100", "nginx: master process nginx -c /etc/nginx/nginx.conf", "")
	fakeProcess(t, root, exe, "20", "200", "nginx: master process nginx -c /etc/nginx/nginx.conf", "")
	if _, err := newGenerationProbe(root, exe, "/etc/nginx/nginx.conf", "/etc/nginx/nginx.conf"); err == nil {
		t.Fatal("accepted ambiguous master processes")
	}
	if err := os.RemoveAll(filepath.Join(root, "20")); err != nil {
		t.Fatal(err)
	}
	wait, err := newGenerationProbe(root, exe, "/etc/nginx/nginx.conf", "/etc/nginx/nginx.conf")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := wait(ctx); err == nil {
		t.Fatal("canceled observation reported readiness")
	}
	if got := compiledConfPath(`configure arguments: --prefix=/opt/resty --conf-path=conf/custom.conf`); got != "/opt/resty/conf/custom.conf" {
		t.Fatal(got)
	}
}

// Executed inside each real runtime container by the external integration test.
func TestNativeReloadGenerationProbe(t *testing.T) {
	bin, conf := os.Getenv("NXPANEL_KERNEL_PROBE_BIN"), os.Getenv("NXPANEL_KERNEL_PROBE_CONF")
	if bin == "" || conf == "" {
		t.Skip("native Nginx namespace probe is opt-in")
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{executor: NewNginxExecutor(path, conf, DefaultNginxTimeouts())}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wait, err := s.captureReloadGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.executor.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if err := wait(ctx); err != nil {
		t.Fatal("native /proc observation failed:", err)
	}
}
