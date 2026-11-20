package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/nginx"
)

type nginxRuntimeProcess struct {
	pid, parent               int
	rss, start                uint64
	title                     string
	sameBinary, deletedBinary bool
}

var (
	errRuntimeConflict         = errors.New("无法安全控制 Nginx")
	errNginxRestartUnconfirmed = errors.New("重启命令已执行，但运行状态未确认，请刷新状态")
)

func runtimeConflict(message string) error {
	return fmt.Errorf("%w：%s", errRuntimeConflict, message)
}

func parseNginxProcStat(data string) (parent int, start, rss uint64, state string, err error) {
	end := strings.LastIndex(data, ")")
	if end < 0 {
		return 0, 0, 0, "", fmt.Errorf("invalid proc stat")
	}
	fields := strings.Fields(data[end+1:])
	if len(fields) < 22 {
		return 0, 0, 0, "", fmt.Errorf("short proc stat")
	}
	state = fields[0]
	parent, err = strconv.Atoi(fields[1])
	if err != nil {
		return
	}
	start, err = strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return
	}
	rss, err = strconv.ParseUint(fields[21], 10, 64)
	return
}

func scanNginxRuntime(ctx context.Context, procRoot, bin string) ([]nginxRuntimeProcess, error) {
	binary, err := os.Stat(bin)
	if err != nil {
		return nil, fmt.Errorf("无法读取 Nginx 二进制: %w", err)
	}
	resolvedBin, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}
	if len(entries) > 32768 {
		return nil, fmt.Errorf("process count exceeds inspection budget")
	}
	var processes []nginxRuntimeProcess
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		dir := filepath.Join(procRoot, entry.Name())
		exe, err := os.Stat(filepath.Join(dir, "exe"))
		sameBinary := err == nil && os.SameFile(binary, exe)
		var deletedBinary bool
		if !sameBinary {
			path, linkErr := os.Readlink(filepath.Join(dir, "exe"))
			deletedBinary = linkErr == nil && path == resolvedBin+" (deleted)"
			sameBinary = deletedBinary
		}
		if !sameBinary && !hasNginxMasterTitle(filepath.Join(dir, "cmdline")) {
			continue
		}
		command, err := procText(filepath.Join(dir, "cmdline"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		title := strings.TrimSpace(strings.ReplaceAll(string(command), "\x00", " "))
		stat, err := procText(filepath.Join(dir, "stat"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		parent, start, rss, state, err := parseNginxProcStat(stat)
		if err != nil {
			return nil, err
		}
		if state == "Z" || state == "X" {
			continue
		}
		processes = append(processes, nginxRuntimeProcess{pid: pid, parent: parent, start: start, rss: rss, title: title, sameBinary: sameBinary, deletedBinary: deletedBinary})
	}
	return processes, nil
}

func hasNginxMasterTitle(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	var prefix [len(nginxMasterTitle)]byte
	_, err = io.ReadFull(file, prefix[:])
	return err == nil && string(prefix[:]) == nginxMasterTitle
}

func (runtime nginxRuntime) status(ctx context.Context) (*nginx.RuntimeStatus, error) {
	selection, err := runtime.inspect(ctx)
	if err != nil {
		return nil, err
	}
	result, master := selection.status, selection.master
	if master == nil {
		return &result, nil
	}
	result.Running, result.MasterPID = true, master.pid
	var totalPSS uint64
	pssAvailable := true
	for _, p := range selection.processes {
		if p.pid != master.pid && (!p.sameBinary || p.parent != master.pid || strings.HasPrefix(p.title, nginxMasterTitle)) {
			continue
		}
		result.Processes++
		result.MemoryRSS += p.rss * uint64(os.Getpagesize())
		if pss, err := readNginxPSS(filepath.Join(runtime.procRoot, strconv.Itoa(p.pid), "smaps_rollup")); err == nil {
			totalPSS += pss
		} else {
			// Never present a partial process-tree sum as total memory usage.
			pssAvailable = false
		}
		if strings.HasPrefix(p.title, "nginx: worker process") {
			result.Workers++
		}
	}
	if pssAvailable {
		result.MemoryPSS = &totalPSS
	}
	uptime, err := os.ReadFile(filepath.Join(runtime.procRoot, "uptime"))
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(uptime))
	if len(fields) == 0 {
		return nil, fmt.Errorf("invalid proc uptime")
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil, err
	}
	// Linux exports /proc stat times in USER_HZ (100), independent of kernel HZ.
	if seconds > float64(master.start)/100 {
		result.UptimeSeconds = uint64(seconds - float64(master.start)/100)
	}
	return &result, nil
}

func readNginxPSS(path string) (uint64, error) {
	text, err := procText(path)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "Pss:" || fields[2] != "kB" {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || kb > ^uint64(0)/1024 {
			return 0, fmt.Errorf("invalid Pss value")
		}
		return kb * 1024, nil
	}
	return 0, fmt.Errorf("Pss not available")
}

func (s *Server) handleNginxStatus(w http.ResponseWriter, r *http.Request) {
	runtime, _, err := s.executor.loadRuntime(r.Context(), "/proc")
	if err != nil {
		writeAgentError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	status, err := runtime.status(r.Context())
	if err != nil {
		writeAgentError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeAgentOK(w, status)
}

func (s *Server) handleNginxControl(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.URL.Path, "/internal/v1/nginx/")
	if action != "start" && action != "stop" && action != "restart" {
		writeAgentError(w, http.StatusBadRequest, "无效操作")
		return
	}
	if !s.executor.lifecycleMu.TryLock() {
		writeAgentError(w, http.StatusConflict, "Nginx 控制操作正在执行，请稍后重试")
		return
	}
	defer s.executor.lifecycleMu.Unlock()
	var req nginx.RuntimeControlRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&req)
	if (decodeErr != io.EOF && (decodeErr != nil || decoder.Decode(new(any)) != io.EOF)) || !req.ValidFor(action) {
		writeAgentError(w, http.StatusBadRequest, "无效的 Nginx 控制请求")
		return
	}
	if action == "restart" {
		if !s.transactionMu.TryLock() {
			writeAgentError(w, http.StatusConflict, "配置写入正在执行，请稍后重试")
			return
		}
		defer s.transactionMu.Unlock()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.executor.control(ctx, action, req.RestartGeneration)
	if err != nil {
		switch {
		case errors.Is(err, errNginxRestartUnconfirmed):
			writeAgentError(w, http.StatusServiceUnavailable, err.Error())
			return
		case errors.Is(err, errRuntimeConflict):
			writeAgentError(w, http.StatusConflict, err.Error())
			return
		}
		message := err.Error()
		if stderr := strings.TrimSpace(result.Stderr); stderr != "" {
			message += "\n" + stderr
		}
		writeAgentError(w, http.StatusInternalServerError, message)
		return
	}
	writeAgentOK(w, map[string]any{"ok": true})
}

// Never fall back after a supervisor command fails: that can create an unmanaged
// second master or allow the supervisor to undo a requested stop.
func (e *NginxExecutor) control(ctx context.Context, action, generation string) (CmdResult, error) {
	if action != "start" && action != "stop" && action != "restart" {
		return CmdResult{}, fmt.Errorf("invalid action")
	}
	runtime, result, err := e.loadRuntime(ctx, "/proc")
	if err != nil {
		return result, err
	}
	if action == "restart" {
		return runtime.restart(ctx, generation)
	}
	selection, err := runtime.inspect(ctx)
	if err != nil {
		return CmdResult{}, err
	}
	if selection.status.OtherActionsBlocked {
		return CmdResult{}, runtimeConflict("存在其他或无法确认归属的 Nginx 主进程，请先检查运行状态")
	}
	masterPID := 0
	if selection.master != nil {
		masterPID = selection.master.pid
	}
	if action != "stop" {
		result, err := e.Test(ctx)
		if err != nil {
			return result, fmt.Errorf("配置检查未通过，未执行 %s: %w", action, err)
		}
	}
	if runtime.s6 {
		if masterPID != 0 {
			pid, err := runtime.supervisorPID(ctx, nginxS6Service)
			if err != nil || pid != masterPID {
				return CmdResult{}, runtimeConflict("运行中实例不属于容器内的 Nginx 服务")
			}
		}
		// nginx has no s6 readiness notification; wait for up, not ready.
		args := []string{"-wu", "-T", "20000", "-u", "/run/service/nginx-service"}
		if action == "stop" {
			args[0], args[3] = "-wd", "-d"
		}
		result, err := e.run(ctx, "/command/s6-svc", args...)
		if err != nil {
			return result, err
		}
		return result, runtime.wait(ctx, action != "stop")
	}
	if runtime.systemd {
		service, err := e.runtimeSystemdUnit(ctx, masterPID, runtime.defaultConf, runtime.prefix)
		if err != nil {
			return CmdResult{}, err
		}
		result, err := e.run(ctx, "systemctl", action, service)
		if err != nil {
			return result, err
		}
		return result, runtime.wait(ctx, action != "stop")
	}
	if (action == "start") == (masterPID != 0) {
		return CmdResult{}, nil
	}
	args := []string{}
	if conf := e.GetConfPath(); conf != "" {
		args = append(args, "-c", conf)
	}
	if action == "stop" {
		result, err := e.run(ctx, e.GetBin(), append(args, "-s", "quit")...)
		if err != nil {
			return result, err
		}
		return result, runtime.wait(ctx, false)
	}
	result, err = e.run(ctx, e.GetBin(), args...)
	if err != nil {
		return result, err
	}
	return result, runtime.wait(ctx, true)
}

func (e *NginxExecutor) matchesSystemdExecStart(output, defaultConf, prefix string) bool {
	options, ok := systemdRuntimeOptions(output, e.GetBin(), defaultConf, prefix)
	expected := e.GetConfPath()
	if expected == "" {
		expected = absoluteRuntimePath(prefix, defaultConf)
	}
	return ok && canonicalRuntimePath(options.conf) == canonicalRuntimePath(expected)
}

func systemdRuntimeOptions(output, bin, defaultConf, prefix string) (runtimeOptions, bool) {
	_, path, ok := strings.Cut(output, "path=")
	if !ok {
		return runtimeOptions{}, false
	}
	path, _, ok = strings.Cut(path, " ;")
	if !ok || canonicalRuntimePath(strings.TrimSpace(path)) != canonicalRuntimePath(bin) {
		return runtimeOptions{}, false
	}
	_, args, ok := strings.Cut(output, "argv[]=")
	if !ok {
		return runtimeOptions{}, false
	}
	args, _, ok = strings.Cut(args, " ;")
	if !ok || strings.Count(output, "argv[]=") != 1 {
		return runtimeOptions{}, false
	}
	return parseRuntimeOptions(args, defaultConf, prefix)
}

func (e *NginxExecutor) runtimeSystemdUnit(ctx context.Context, masterPID int, defaultConf, prefix string) (string, error) {
	for _, service := range []string{"nginx.service", "openresty.service"} {
		unit, err := e.run(ctx, "systemctl", "show", service, "--property=ExecStart", "--value")
		if err != nil || !e.matchesSystemdExecStart(unit.Stdout, defaultConf, prefix) {
			continue
		}
		if masterPID != 0 {
			pid, err := e.run(ctx, "systemctl", "show", service, "--property=MainPID", "--value")
			if err != nil || strings.TrimSpace(pid.Stdout) != strconv.Itoa(masterPID) {
				continue
			}
		}
		return service, nil
	}
	return "", fmt.Errorf("未找到与当前二进制、主配置及主进程匹配的 nginx/openresty systemd 服务，拒绝操作")
}

func (e *NginxExecutor) waitRuntime(ctx context.Context, running bool) error {
	runtime, _, err := e.loadRuntime(ctx, "/proc")
	if err != nil {
		return err
	}
	return runtime.wait(ctx, running)
}

func (runtime nginxRuntime) wait(ctx context.Context, running bool) error {
	ticker := time.NewTicker(runtimePollInterval)
	defer ticker.Stop()
	for {
		selection, err := runtime.inspect(ctx)
		if err != nil {
			return err
		}
		if selection.status.BlockedReason != "" {
			return runtimeConflict(selection.status.BlockedReason)
		}
		if (selection.master != nil) == running {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待 Nginx 状态变更超时，请刷新状态: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
