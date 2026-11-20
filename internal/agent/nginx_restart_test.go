package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type fakeRestartProcess struct {
	done bool
	stop func()
}

func (process *fakeRestartProcess) quit() error {
	process.stop()
	process.done = true
	return nil
}

func (process *fakeRestartProcess) exited() (bool, error) { return process.done, nil }
func (process *fakeRestartProcess) close()                {}

func restartFixture(t *testing.T) (nginxRuntime, string, string, func(int, string)) {
	t.Helper()
	executor, proc, add := runtimeFixture(t)
	runtime := loadRuntimeFixture(t, executor, proc)
	root := filepath.Dir(proc)
	runtime.conf, runtime.prefix = filepath.Join(root, "panel.conf"), root
	pidPath, peerPath := filepath.Join(root, "panel.pid"), filepath.Join(root, "peer.pid")
	writeRestartFixture(t, runtime.conf, "include modules/*.conf; events {} http { include conf.d/*.conf; log_format test '$request;#{}'; }")
	if err := os.Mkdir(filepath.Join(root, "modules"), 0755); err != nil {
		t.Fatal(err)
	}
	writeRestartFixture(t, filepath.Join(root, "modules", "pid.conf"), "# pid ignored.pid;\npid\n 'panel.pid';")
	peerConf := filepath.Join(root, "peer.conf")
	writeRestartFixture(t, peerConf, "pid peer.pid; events {}")
	writeRestartFixture(t, pidPath, "100\n")
	writeRestartFixture(t, peerPath, "200\n")
	addMaster := func(pid int, conf string) {
		t.Helper()
		add(pid, 1, "nginx: master process nginx -p "+root+" -c "+conf, 1)
		dir := filepath.Join(proc, strconv.Itoa(pid))
		if err := os.Symlink(root, filepath.Join(dir, "root")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(root, filepath.Join(dir, "ns")); err != nil {
			t.Fatal(err)
		}
		writeRestartFixture(t, filepath.Join(dir, "status"), "Uid:\t0\t0\t0\t0\n")
		writeRestartFixture(t, filepath.Join(dir, "cgroup"), "0::/user.slice/user-0.slice/session-7.scope\n")
	}
	if err := os.Mkdir(filepath.Join(proc, "self"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"root", "ns"} {
		if err := os.Symlink(root, filepath.Join(proc, "self", path)); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"pid", "mnt", "user", "net"} {
		writeRestartFixture(t, filepath.Join(root, path), "namespace")
	}
	addMaster(100, runtime.conf)
	addMaster(200, peerConf)
	return runtime, pidPath, peerPath, addMaster
}

func writeRestartFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func removeRestartMaster(t *testing.T, runtime nginxRuntime, pid int, pidPath string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(runtime.procRoot, strconv.Itoa(pid))); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(pidPath); err != nil {
		t.Fatal(err)
	}
}

func TestNginxManualRestartTargetsOnlyManagedInstance(t *testing.T) {
	runtime, pidPath, peerPath, addMaster := restartFixture(t)
	runtime.systemd = true
	var calls []string
	runtime.openProcess = func(pid int) (restartProcess, error) {
		if pid != 100 {
			t.Fatalf("unexpected target PID: %d", pid)
		}
		return &fakeRestartProcess{stop: func() {
			calls = append(calls, "quit")
			removeRestartMaster(t, runtime, pid, pidPath)
		}}, nil
	}
	runtime.run = func(_ context.Context, bin string, args ...string) (CmdResult, error) {
		if bin == "systemctl" && args[0] == "show" {
			return CmdResult{Stdout: "0\n"}, nil
		}
		if bin != runtime.bin || !strings.HasSuffix(strings.Join(args, " "), "-p "+runtime.prefix+" -c "+runtime.conf) {
			t.Fatalf("unexpected command: %s %v", bin, args)
		}
		if args[0] == "-t" {
			calls = append(calls, "test")
		} else {
			calls = append(calls, "start")
			addMaster(101, runtime.conf)
			writeRestartFixture(t, pidPath, "101\n")
		}
		return CmdResult{}, nil
	}
	status, err := runtime.status()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.restart(t.Context(), status.RestartGeneration); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls, ","); got != "test,quit,start" || !pidFileMatches(peerPath, 200) {
		t.Fatalf("unexpected restart: %s", got)
	}
}

func TestNginxManualRestartRefusesUnsafeTargets(t *testing.T) {
	for _, scenario := range []string{"stale confirmation", "PID collision", "PID reused", "config check", "unknown options", "custom supervisor", "stale PID file", "launch failure"} {
		t.Run(scenario, func(t *testing.T) {
			runtime, pidPath, _, _ := restartFixture(t)
			status, err := runtime.status()
			if err != nil {
				t.Fatal(err)
			}
			generation := status.RestartGeneration
			var quit bool
			runtime.openProcess = func(pid int) (restartProcess, error) {
				if pid != 100 {
					t.Fatalf("unexpected PID: %d", pid)
				}
				return &fakeRestartProcess{stop: func() {
					quit = true
					if scenario == "launch failure" {
						removeRestartMaster(t, runtime, pid, pidPath)
					} else if scenario == "stale PID file" {
						if err := os.RemoveAll(filepath.Join(runtime.procRoot, "100")); err != nil {
							t.Fatal(err)
						}
					}
				}}, nil
			}
			procDir := filepath.Join(runtime.procRoot, "100")
			switch scenario {
			case "stale confirmation":
				generation = strings.Repeat("f", 64)
			case "PID collision":
				writeRestartFixture(t, filepath.Join(runtime.prefix, "peer.conf"), "pid panel.pid; events {}")
			case "unknown options":
				writeRestartFixture(t, filepath.Join(procDir, "cmdline"), nginxMasterTitle+"nginx -c "+runtime.conf+" -g pid other.pid;\x00")
			case "custom supervisor":
				writeRestartFixture(t, filepath.Join(procDir, "cgroup"), "0::/system.slice/custom-nginx.service\n")
			}
			runtime.run = func(_ context.Context, bin string, args ...string) (CmdResult, error) {
				if bin == runtime.bin && args[0] == "-t" {
					switch scenario {
					case "config check":
						return CmdResult{Stderr: "invalid configuration"}, errors.New("failed")
					case "PID reused":
						path := filepath.Join(procDir, "stat")
						stat, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						writeRestartFixture(t, path, strings.Replace(string(stat), "10000", "10100", 1))
					}
					return CmdResult{}, nil
				}
				if scenario == "launch failure" && quit && bin == runtime.bin && args[0] == "-p" {
					return CmdResult{}, errors.New("bind failed")
				}
				t.Fatalf("unexpected command: %s %v", bin, args)
				return CmdResult{}, nil
			}
			result, err := runtime.restart(t.Context(), generation)
			if scenario == "config check" {
				if err == nil || result.Stderr != "invalid configuration" || !strings.Contains(err.Error(), "配置检查未通过") {
					t.Fatalf("bad config handling: %+v %v", result, err)
				}
			} else if scenario == "launch failure" {
				if err == nil || !strings.Contains(err.Error(), "旧主进程已停止") {
					t.Fatalf("launch failure: %v", err)
				}
			} else if !errors.Is(err, errRuntimeConflict) {
				t.Fatalf("expected conflict: %v", err)
			}
			if quit != (scenario == "stale PID file" || scenario == "launch failure") {
				t.Fatalf("unexpected quit=%v", quit)
			}
		})
	}
}

func TestNginxSystemdRestartDoesNotFallBack(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(strconv.FormatBool(fails), func(t *testing.T) {
			runtime, pidPath, _, addMaster := restartFixture(t)
			runtime.systemd = true
			masterPID := 100
			var commands []string
			runtime.openProcess = func(int) (restartProcess, error) {
				t.Fatal("supervised process must not be signaled directly")
				return nil, nil
			}
			runtime.run = func(_ context.Context, bin string, args ...string) (CmdResult, error) {
				switch {
				case bin == "systemctl" && args[0] == "show":
					if args[1] != "nginx.service" {
						return CmdResult{Stdout: "0\n"}, nil
					}
					if args[2] == "--property=MainPID" {
						return CmdResult{Stdout: strconv.Itoa(masterPID)}, nil
					}
					return CmdResult{Stdout: fmt.Sprintf("Id=nginx.service\nExecStart={ path=%s ; argv[]=%s -p %s -c %s ; }\n", runtime.bin, runtime.bin, runtime.prefix, runtime.conf)}, nil
				case bin == runtime.bin && args[0] == "-t":
					commands = append(commands, "test")
					return CmdResult{}, nil
				case bin == "systemctl" && strings.Join(args, " ") == "restart nginx.service":
					commands = append(commands, "restart")
					if fails {
						return CmdResult{}, errors.New("systemctl failed")
					}
					removeRestartMaster(t, runtime, masterPID, pidPath)
					masterPID = 101
					addMaster(masterPID, runtime.conf)
					return CmdResult{}, nil
				default:
					t.Fatalf("unexpected command: %s %v", bin, args)
					return CmdResult{}, nil
				}
			}
			_, err := runtime.restart(t.Context(), "")
			if (err != nil) != fails || strings.Join(commands, ",") != "test,restart" {
				t.Fatalf("restart commands %v: %v", commands, err)
			}
		})
	}
}

func TestNginxS6RestartChecksSupervisorPID(t *testing.T) {
	for _, matches := range []bool{false, true} {
		t.Run(strconv.FormatBool(matches), func(t *testing.T) {
			runtime, pidPath, _, addMaster := restartFixture(t)
			runtime.s6 = true
			masterPID := 200
			if matches {
				masterPID = 100
			}
			var commands []string
			runtime.run = func(_ context.Context, bin string, args ...string) (CmdResult, error) {
				switch {
				case bin == "/command/s6-svstat" && strings.Join(args, " ") == "-o pid "+nginxS6Service:
					return CmdResult{Stdout: strconv.Itoa(masterPID)}, nil
				case matches && bin == runtime.bin && args[0] == "-t":
					commands = append(commands, "test")
					return CmdResult{}, nil
				case matches && bin == "/command/s6-svc" && strings.Join(args, " ") == "-r "+nginxS6Service:
					commands = append(commands, "restart")
					removeRestartMaster(t, runtime, masterPID, pidPath)
					masterPID = 101
					addMaster(masterPID, runtime.conf)
					return CmdResult{}, nil
				default:
					t.Fatalf("unexpected command: %s %v", bin, args)
					return CmdResult{}, nil
				}
			}
			_, err := runtime.restart(t.Context(), "")
			if matches && (err != nil || strings.Join(commands, ",") != "test,restart") || !matches && !errors.Is(err, errRuntimeConflict) {
				t.Fatalf("restart commands %v: %v", commands, err)
			}
		})
	}
}
