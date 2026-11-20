package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/nginx"
)

const (
	nginxMasterTitle = "nginx: master process "
	nginxS6Service   = "/run/service/nginx-service"
)

type nginxRuntime struct {
	procRoot, bin, conf, prefix, defaultConf, defaultPID string
	testTimeout                                          time.Duration
	s6, systemd                                          bool
	run                                                  func(context.Context, string, ...string) (CmdResult, error)
	openProcess                                          func(int) (restartProcess, error)
}

func (e *NginxExecutor) loadRuntime(ctx context.Context, procRoot string) (nginxRuntime, CmdResult, error) {
	e.mu.Lock()
	runtime := nginxRuntime{procRoot: procRoot, bin: e.bin, conf: e.confPath, testTimeout: e.timeouts.Test, run: e.run, openProcess: openRestartProcess}
	e.mu.Unlock()
	if runtime.bin == "" {
		return runtime, CmdResult{}, fmt.Errorf("请先检测 Nginx 二进制路径")
	}
	versionCtx, cancel := context.WithTimeout(ctx, e.timeouts.Detect)
	defer cancel()
	result, err := e.run(versionCtx, runtime.bin, "-V")
	if err != nil {
		return runtime, result, fmt.Errorf("无法确认 Nginx 默认配置: %w", err)
	}
	output := result.Stdout + "\n" + result.Stderr
	runtime.prefix, runtime.defaultConf = compiledRuntimePaths(output)
	runtime.defaultPID = "logs/nginx.pid"
	for _, field := range strings.Fields(output) {
		if value, ok := strings.CutPrefix(strings.Trim(field, "'\""), "--pid-path="); ok {
			runtime.defaultPID = value
		}
	}
	if runtime.conf == "" {
		runtime.conf = absoluteRuntimePath(runtime.prefix, runtime.defaultConf)
	}
	if !filepath.IsAbs(runtime.conf) {
		return runtime, result, fmt.Errorf("无法确认面板配置文件的绝对路径")
	}
	_, s6Err := os.Stat(nginxS6Service + "/supervise")
	_, systemdErr := os.Stat("/run/systemd/system")
	if s6Err != nil && !os.IsNotExist(s6Err) || systemdErr != nil && !os.IsNotExist(systemdErr) {
		return runtime, result, fmt.Errorf("无法确认 Nginx 服务管理器")
	}
	runtime.s6, runtime.systemd = s6Err == nil, systemdErr == nil
	return runtime, result, nil
}

type runtimeOptions struct {
	conf, prefix string
	global       string
}

func absoluteRuntimePath(prefix, path string) string {
	if strings.Contains("/"+path+"/", "/../") || strings.Contains("/"+prefix+"/", "/../") {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if path == "" || !filepath.IsAbs(prefix) {
		return ""
	}
	return filepath.Join(prefix, path)
}

func parseRuntimeOptions(command, defaultConf, prefix string) (runtimeOptions, bool) {
	fields := strings.Fields(command)
	conf := defaultConf
	var seenConf, seenPrefix bool
	var global string
	if len(fields) == 0 {
		return runtimeOptions{}, false
	}
	for index := 1; index < len(fields); index++ {
		field := fields[index]
		if len(field) < 2 || field[0] != '-' {
			return runtimeOptions{}, false
		}
		option, value := field[1], field[2:]
		if value == "" {
			index++
			if index >= len(fields) {
				return runtimeOptions{}, false
			}
			value = fields[index]
		}
		switch option {
		case 'c', 'p':
			if strings.HasPrefix(value, "-") || strings.ContainsAny(value, "\"'\\;$\x00") {
				return runtimeOptions{}, false
			}
			if option == 'c' && !seenConf {
				conf, seenConf = value, true
			} else if option == 'p' && !seenPrefix {
				prefix, seenPrefix = value, true
			} else {
				return runtimeOptions{}, false
			}
		case 'g':
			if global != "" {
				return runtimeOptions{}, false
			}
			remaining := append([]string{value}, fields[index+1:]...)
			for _, pair := range []string{
				"daemon on;", "daemon off;", "master_process on;",
				"daemon on; master_process on;", "daemon off; master_process on;",
				"master_process on; daemon on;", "master_process on; daemon off;",
			} {
				words := strings.Fields(pair)
				if len(words) > len(remaining) || strings.Join(remaining[:len(words)], " ") != pair {
					continue
				}
				if len(words) < len(remaining) && !strings.HasPrefix(remaining[len(words)], "-") {
					continue
				}
				global = pair
				index += len(words) - 1
				value = ""
				break
			}
			if value != "" {
				return runtimeOptions{}, false
			}
		default:
			return runtimeOptions{}, false
		}
	}
	conf = absoluteRuntimePath(prefix, conf)
	return runtimeOptions{conf: conf, prefix: filepath.Clean(prefix), global: global}, conf != "" && filepath.IsAbs(prefix) && len(conf) <= 4096
}

func (options runtimeOptions) args() []string {
	args := []string{"-p", options.prefix, "-c", options.conf}
	if options.global != "" {
		args = append(args, "-g", options.global)
	}
	return args
}

type runtimeSelection struct {
	status    nginx.RuntimeStatus
	processes []nginxRuntimeProcess
	master    *nginxRuntimeProcess
	options   runtimeOptions
}

func (runtime nginxRuntime) inspect() (runtimeSelection, error) {
	processes, err := scanNginxRuntime(runtime.procRoot, runtime.bin)
	if err != nil {
		return runtimeSelection{}, err
	}
	selection := runtimeSelection{processes: processes}
	selection.status.Instances = []nginx.RuntimeInstance{}
	expected := canonicalRuntimePath(runtime.conf)
	var matching, sameBinary int
	var unknown bool
	for index := range processes {
		process := &processes[index]
		command, master := strings.CutPrefix(process.title, nginxMasterTitle)
		if !master {
			continue
		}
		if len(selection.status.Instances) >= 128 {
			return runtimeSelection{}, fmt.Errorf("Nginx 主进程数量超出检查上限")
		}
		instance := nginx.RuntimeInstance{PID: process.pid, Match: "other"}
		if process.sameBinary {
			sameBinary++
			defaultConf := runtime.defaultConf
			if process.deletedBinary {
				defaultConf = ""
			}
			options, ok := parseRuntimeOptions(command, defaultConf, runtime.prefix)
			switch {
			case !ok:
				instance.Match = "unknown"
				unknown = true
			case canonicalRuntimePath(options.conf) == expected:
				instance.Match, instance.ConfPath = "matching", options.conf
				matching++
				selection.master, selection.options = process, options
			default:
				instance.ConfPath = options.conf
			}
		} else if options, ok := parseRuntimeOptions(command, "", "/"); ok {
			instance.ConfPath = options.conf
			if canonicalRuntimePath(options.conf) == expected {
				unknown = true
				instance.Match = "unknown"
			}
		}
		selection.status.Instances = append(selection.status.Instances, instance)
	}
	switch {
	case matching > 1:
		selection.status.BlockedReason = "有多个 Nginx 主进程使用面板配置，无法确定重启目标。请检查下方 PID 与配置文件。"
	case unknown:
		selection.status.BlockedReason = "存在无法确认归属的 Nginx 主进程。请检查下方 PID 与配置文件。"
	}
	selection.status.OtherActionsBlocked = selection.status.BlockedReason != "" || sameBinary > matching || len(selection.status.Instances) > 1
	if selection.status.BlockedReason != "" {
		selection.master = nil
	}
	if selection.master != nil {
		identity := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%s", runtime.bin, runtime.conf, expected, selection.options.prefix, selection.master.pid, selection.master.start, selection.master.title)
		selection.status.RestartGeneration = fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
	}
	return selection, nil
}

func (runtime nginxRuntime) supervisorPID(ctx context.Context, service string) (int, error) {
	var result CmdResult
	var err error
	if service == nginxS6Service {
		result, err = runtime.run(ctx, "/command/s6-svstat", "-o", "pid", service)
	} else {
		result, err = runtime.run(ctx, "systemctl", "show", service, "--property=MainPID", "--value")
	}
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(result.Stdout))
}

func (runtime nginxRuntime) restartService(ctx context.Context, selection runtimeSelection) (string, error) {
	if runtime.s6 {
		pid, err := runtime.supervisorPID(ctx, nginxS6Service)
		if err != nil || pid != selection.master.pid {
			return "", runtimeConflict("容器内 Nginx 服务的主进程与面板实例不一致")
		}
		return nginxS6Service, nil
	}
	if !runtime.systemd {
		return "", nil
	}
	var match string
	for _, service := range []string{"nginx.service", "openresty.service"} {
		pid, err := runtime.supervisorPID(ctx, service)
		if err != nil || pid != selection.master.pid {
			continue
		}
		unit, err := runtime.run(ctx, "systemctl", "show", service, "--property=Id", "--property=ExecStart")
		id, execStart := "", ""
		for _, line := range strings.Split(unit.Stdout, "\n") {
			key, value, _ := strings.Cut(line, "=")
			if key == "Id" {
				id = value
			} else if key == "ExecStart" {
				execStart = value
			}
		}
		options, ok := systemdRuntimeOptions(execStart, runtime.bin, runtime.defaultConf, runtime.prefix)
		if err != nil || !ok || !sameRuntimeOptions(options, selection.options) || (id != "nginx.service" && id != "openresty.service") || (match != "" && match != id) {
			return "", runtimeConflict("无法核验 systemd 服务与运行中实例的启动配置一致")
		}
		match = id
	}
	return match, nil
}

func sameRuntimeOptions(left, right runtimeOptions) bool {
	return canonicalRuntimePath(left.conf) == canonicalRuntimePath(right.conf) && left.prefix == right.prefix && left.global == right.global
}

func (runtime nginxRuntime) restart(ctx context.Context, generation string) (CmdResult, error) {
	selection, err := runtime.inspect()
	if err != nil {
		return CmdResult{}, err
	}
	if selection.status.BlockedReason != "" {
		return CmdResult{}, runtimeConflict(selection.status.BlockedReason)
	}
	if selection.master == nil || generation != "" && selection.status.RestartGeneration != generation {
		return CmdResult{}, runtimeConflict("重启目标已变化，请刷新状态后重试")
	}
	service, err := runtime.restartService(ctx, selection)
	if err != nil {
		return CmdResult{}, err
	}
	if service == "" {
		return runtime.restartManual(ctx, selection)
	}
	if len(selection.status.Instances) > 1 {
		if _, err := runtime.checkPIDFiles(selection, selection.options, selection.master.pid, false); err != nil {
			return CmdResult{}, err
		}
	}
	result, err := runtime.testRestart(ctx, selection.options)
	if err != nil {
		return result, err
	}
	current, err := runtime.recheck(selection)
	if err != nil {
		return CmdResult{}, err
	}
	if len(current.status.Instances) > 1 {
		if _, err := runtime.checkPIDFiles(current, current.options, current.master.pid, false); err != nil {
			return CmdResult{}, err
		}
	}
	if currentService, err := runtime.restartService(ctx, current); err != nil || currentService != service {
		return CmdResult{}, runtimeConflict("重启目标所属服务已变化，请刷新状态后重试")
	}
	if service == nginxS6Service {
		result, err = runtime.run(ctx, "/command/s6-svc", "-r", service)
	} else {
		result, err = runtime.run(ctx, "systemctl", "restart", service)
	}
	if err != nil {
		return result, err
	}
	return result, runtime.wait(ctx, true, *selection.master, service)
}

func (runtime nginxRuntime) recheck(previous runtimeSelection) (runtimeSelection, error) {
	current, err := runtime.inspect()
	if err != nil {
		return current, err
	}
	if current.master == nil || current.status.RestartGeneration != previous.status.RestartGeneration {
		return current, runtimeConflict("重启目标已变化，请刷新状态后重试")
	}
	return current, nil
}

func (runtime nginxRuntime) testRestart(ctx context.Context, options runtimeOptions) (CmdResult, error) {
	testCtx, cancel := context.WithTimeout(ctx, runtime.testTimeout)
	defer cancel()
	result, err := runtime.run(testCtx, runtime.bin, append([]string{"-t"}, options.args()...)...)
	if err != nil {
		return result, fmt.Errorf("配置检查未通过，未执行重启: %w", err)
	}
	return result, nil
}
