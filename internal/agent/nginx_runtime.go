package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/nginx"
)

type nginxRuntimeProcess struct {
	pid, parent int
	rss, start  uint64
	title       string
}

func parseNginxProcStat(data string) (parent int, start, rss uint64, err error) {
	end := strings.LastIndex(data, ")")
	if end < 0 {
		return 0, 0, 0, fmt.Errorf("invalid proc stat")
	}
	fields := strings.Fields(data[end+1:])
	if len(fields) < 22 {
		return 0, 0, 0, fmt.Errorf("short proc stat")
	}
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

func (e *NginxExecutor) runtimeStatus(procRoot string) (*nginx.RuntimeStatus, error) {
	bin := e.GetBin()
	if bin == "" {
		return nil, fmt.Errorf("请先检测 Nginx 二进制路径")
	}
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
	var master *nginxRuntimeProcess
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		dir := filepath.Join(procRoot, entry.Name())
		exe, err := os.Stat(filepath.Join(dir, "exe"))
		if err != nil || !os.SameFile(binary, exe) {
			// Package upgrades can unlink the running executable before restart.
			path, linkErr := os.Readlink(filepath.Join(dir, "exe"))
			if linkErr != nil || strings.TrimSuffix(path, " (deleted)") != resolvedBin {
				continue
			}
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
		parent, start, rss, err := parseNginxProcStat(string(stat))
		if err != nil {
			return nil, err
		}
		p := nginxRuntimeProcess{pid: pid, parent: parent, start: start, rss: rss, title: title}
		processes = append(processes, p)
		if strings.HasPrefix(title, "nginx: master process ") {
			if master != nil {
				return nil, fmt.Errorf("检测到多个 Nginx 主进程，无法安全确定受管实例")
			}
			master = &processes[len(processes)-1]
		}
	}
	result := &nginx.RuntimeStatus{}
	if master == nil {
		return result, nil
	}
	// Refuse a known different configuration rather than signal another instance.
	expected := e.GetConfPath()
	conf := runtimeConfPath(master.title, expected, "")
	if expected != "" && (conf == "" || canonicalRuntimePath(conf) != canonicalRuntimePath(expected)) {
		return nil, fmt.Errorf("无法确认运行中的 Nginx 使用了面板配置的主配置文件")
	}
	result.Running, result.MasterPID = true, master.pid
	var totalPSS uint64
	pssAvailable := true
	for _, p := range processes {
		if p.pid != master.pid && p.parent != master.pid {
			continue
		}
		result.Processes++
		result.MemoryRSS += p.rss * uint64(os.Getpagesize())
		if pss, err := readNginxPSS(filepath.Join(procRoot, strconv.Itoa(p.pid), "smaps_rollup")); err == nil {
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
	uptime, err := os.ReadFile(filepath.Join(procRoot, "uptime"))
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
	return result, nil
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
	status, err := s.executor.runtimeStatus("/proc")
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
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.executor.control(ctx, action)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, fmt.Sprintf("Nginx %s 失败: %v\n%s", action, err, result.Stderr))
		return
	}
	writeAgentOK(w, map[string]any{"ok": true})
}

// Never fall back after a supervisor command fails: that can create an unmanaged
// second master or allow the supervisor to undo a requested stop.
func (e *NginxExecutor) control(ctx context.Context, action string) (CmdResult, error) {
	if action != "start" && action != "stop" && action != "restart" {
		return CmdResult{}, fmt.Errorf("invalid action")
	}
	status, err := e.runtimeStatus("/proc")
	if err != nil {
		return CmdResult{}, err
	}
	if action != "stop" {
		result, err := e.Test(ctx)
		if err != nil {
			return result, fmt.Errorf("配置检查未通过，未执行 %s: %w", action, err)
		}
	}
	version, err := e.run(ctx, e.GetBin(), "-V")
	if err != nil {
		return version, fmt.Errorf("无法确认 Nginx 默认配置: %w", err)
	}
	prefix, defaultConf := compiledRuntimePaths(version.Stdout + "\n" + version.Stderr)
	if status.Running {
		command, err := procText(filepath.Join("/proc", strconv.Itoa(status.MasterPID), "cmdline"))
		if err != nil {
			return CmdResult{}, err
		}
		if !e.matchesRuntimeConfig(command, defaultConf, prefix) {
			return CmdResult{}, fmt.Errorf("运行中实例的主配置与面板配置不一致，拒绝操作")
		}
	}
	if _, err := os.Stat("/run/service/nginx-service/supervise"); err == nil {
		// nginx has no s6 readiness notification; wait for up, not ready.
		args := []string{"-wu", "-T", "20000", "-u", "/run/service/nginx-service"}
		if action == "stop" {
			args[0], args[3] = "-wd", "-d"
		}
		if action == "restart" {
			result, err := e.run(ctx, "/command/s6-svc", "-wd", "-T", "20000", "-d", "/run/service/nginx-service")
			if err != nil {
				return result, err
			}
		}
		result, err := e.run(ctx, "/command/s6-svc", args...)
		if err != nil {
			return result, err
		}
		return result, e.waitRuntime(ctx, action != "stop")
	}
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		service, err := e.runtimeSystemdUnit(ctx, status.MasterPID, defaultConf, prefix)
		if err != nil {
			return CmdResult{}, err
		}
		result, err := e.run(ctx, "systemctl", action, service)
		if err != nil {
			return result, err
		}
		return result, e.waitRuntime(ctx, action != "stop")
	}
	if action == "start" && status.Running || action == "stop" && !status.Running {
		return CmdResult{}, nil
	}
	args := []string{}
	if conf := e.GetConfPath(); conf != "" {
		args = append(args, "-c", conf)
	}
	if action != "start" && status.Running {
		result, err := e.run(ctx, e.GetBin(), append(args, "-s", "quit")...)
		if err != nil {
			return result, err
		}
		if err = e.waitRuntime(ctx, false); err != nil {
			return result, err
		}
		if action == "stop" {
			return result, nil
		}
	}
	result, err := e.run(ctx, e.GetBin(), args...)
	if err != nil {
		return result, err
	}
	return result, e.waitRuntime(ctx, true)
}

func (e *NginxExecutor) matchesRuntimeConfig(command, defaultConf, prefix string) bool {
	conf := runtimeConfPath(command, defaultConf, prefix)
	expected := e.GetConfPath()
	if expected == "" {
		expected = runtimeConfPath("", defaultConf, prefix)
	}
	return filepath.IsAbs(conf) && canonicalRuntimePath(conf) == canonicalRuntimePath(expected)
}

func (e *NginxExecutor) matchesSystemdExecStart(output, defaultConf, prefix string) bool {
	_, path, ok := strings.Cut(output, "path=")
	if !ok {
		return false
	}
	path, _, ok = strings.Cut(path, " ;")
	if !ok || canonicalRuntimePath(strings.TrimSpace(path)) != canonicalRuntimePath(e.GetBin()) {
		return false
	}
	_, args, ok := strings.Cut(output, "argv[]=")
	if !ok {
		return false
	}
	args, _, ok = strings.Cut(args, " ;")
	return ok && e.matchesRuntimeConfig(args, defaultConf, prefix)
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
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := e.runtimeStatus("/proc")
		if err != nil {
			return err
		}
		if status.Running == running {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待 Nginx 状态变更超时，请刷新状态: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
