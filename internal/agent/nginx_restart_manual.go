package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type restartProcess interface {
	quit() error
	exited() (bool, error)
	close()
}

type restartPIDFD int

func openRestartProcess(pid int) (restartProcess, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	return restartPIDFD(fd), err
}

func (fd restartPIDFD) quit() error {
	return unix.PidfdSendSignal(int(fd), unix.SIGQUIT, nil, 0)
}

func (fd restartPIDFD) exited() (bool, error) {
	items := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	_, err := unix.Poll(items, 0)
	if err == unix.EINTR {
		return false, nil
	}
	return items[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0, err
}

func (fd restartPIDFD) close() { _ = unix.Close(int(fd)) }

func (runtime nginxRuntime) restartManual(ctx context.Context, selection runtimeSelection) (CmdResult, error) {
	pid := selection.master.pid
	if strings.Contains(selection.options.global, "daemon off;") {
		return CmdResult{}, runtimeConflict("无法直接重启前台运行的 Nginx 实例")
	}
	if err := runtime.checkManualMaster(*selection.master); err != nil {
		return CmdResult{}, err
	}
	pidPath, err := runtime.checkPIDFiles(selection, selection.options, pid, true)
	if err != nil {
		return CmdResult{}, err
	}
	process, err := runtime.openProcess(pid)
	if err != nil {
		return CmdResult{}, runtimeConflict("无法锁定手动启动的 Nginx 主进程身份（需要内核支持 pidfd）")
	}
	defer process.close()
	result, err := runtime.testRestart(ctx, selection.options)
	if err != nil {
		return result, err
	}
	current, err := runtime.recheck(selection)
	if err != nil {
		return CmdResult{}, err
	}
	if err := runtime.checkManualMaster(*current.master); err != nil {
		return CmdResult{}, err
	}
	if path, err := runtime.checkPIDFiles(current, selection.options, pid, true); err != nil || path != pidPath {
		return CmdResult{}, runtimeConflict("PID 文件核验未通过或已变化，未执行重启")
	}
	if exited, err := process.exited(); err != nil || exited || ctx.Err() != nil {
		return CmdResult{}, runtimeConflict("重启目标已退出或状态无法确认，未执行重启")
	}
	if err := process.quit(); err != nil {
		return CmdResult{}, runtimeConflict("无法向已核验的主进程发送退出信号，请刷新状态")
	}
	if err := waitManualExit(ctx, process); err != nil {
		return CmdResult{}, fmt.Errorf("已请求停止旧主进程，但未确认退出，未启动新实例: %w", err)
	}
	current, err = runtime.inspect()
	if err != nil || current.master != nil || current.status.BlockedReason != "" || ctx.Err() != nil {
		return CmdResult{}, runtimeConflict("旧主进程已退出，但当前实例状态已变化，未启动新实例")
	}
	if path, err := runtime.checkPIDFiles(current, selection.options, 0, true); err != nil || path != pidPath {
		return CmdResult{}, runtimeConflict("旧主进程已退出，但 PID 文件未释放或配置已变化，未启动新实例")
	}
	result, err = runtime.run(ctx, runtime.bin, selection.options.args()...)
	if err != nil {
		return result, fmt.Errorf("旧主进程已停止，启动新实例失败: %w", err)
	}
	if err := runtime.wait(ctx, true, *selection.master, ""); err != nil {
		return result, fmt.Errorf("旧主进程已停止，无法确认新实例已启动: %w", err)
	}
	current, err = runtime.inspect()
	if err != nil || current.master == nil || !sameRuntimeOptions(selection.options, current.options) || !pidFileMatches(pidPath, current.master.pid) {
		return result, runtimeConflict("旧主进程已停止，但无法核验新实例的 PID 文件，请刷新状态")
	}
	return result, nil
}

func waitManualExit(ctx context.Context, process restartProcess) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if exited, err := process.exited(); exited || err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (runtime nginxRuntime) checkManualMaster(master nginxRuntimeProcess) error {
	dir := filepath.Join(runtime.procRoot, strconv.Itoa(master.pid))
	stat, err := procText(filepath.Join(dir, "stat"))
	end := strings.LastIndex(stat, ")")
	if err != nil || end < 0 {
		return runtimeConflict("无法确认手动启动的主进程状态")
	}
	fields := strings.Fields(stat[end+1:])
	if master.parent != 1 || len(fields) < 4 || fields[3] != strconv.Itoa(master.pid) || !runtime.localRootMaster(master.pid) {
		return runtimeConflict("只能直接重启同命名空间中由 root 手动启动的 Nginx 守护进程")
	}
	groups, err := procText(filepath.Join(dir, "cgroup"))
	selfGroups, _ := procText(filepath.Join(runtime.procRoot, "self", "cgroup"))
	if err != nil || !manualCgroups(groups, selfGroups) {
		return runtimeConflict("该实例可能由其他服务管理，拒绝直接重启")
	}
	return nil
}

func manualCgroups(groups, selfGroups string) bool {
	lines := strings.Fields(groups)
	if len(lines) == 0 {
		return false
	}
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || !filepath.IsAbs(parts[2]) {
			return false
		}
		if parts[2] == "/" || (parts[2] == "/system.slice/nxpanel-agent.service" && strings.TrimSpace(groups) == strings.TrimSpace(selfGroups)) {
			continue
		}
		if !strings.HasPrefix(parts[2], "/user.slice/") || strings.Contains(parts[2], ".service") {
			return false
		}
		id, ok := strings.CutPrefix(filepath.Base(parts[2]), "session-")
		id, suffix := strings.CutSuffix(id, ".scope")
		if !ok || !suffix || id == "" {
			return false
		}
	}
	return true
}

func (runtime nginxRuntime) localRootMaster(pid int) bool {
	dir := filepath.Join(runtime.procRoot, strconv.Itoa(pid))
	status, err := procText(filepath.Join(dir, "status"))
	if err != nil {
		return false
	}
	var root bool
	for _, line := range strings.Split(status, "\n") {
		if value, ok := strings.CutPrefix(line, "Uid:"); ok {
			root = strings.Join(strings.Fields(value), " ") == "0 0 0 0"
			break
		}
	}
	if !root {
		return false
	}
	for _, path := range []string{"root", "ns/pid", "ns/mnt", "ns/user", "ns/net"} {
		peer, err := os.Stat(filepath.Join(dir, path))
		self, selfErr := os.Stat(filepath.Join(runtime.procRoot, "self", path))
		if err != nil || selfErr != nil || !os.SameFile(peer, self) {
			return false
		}
	}
	return true
}

func (runtime nginxRuntime) checkPIDFiles(selection runtimeSelection, options runtimeOptions, targetPID int, requireDaemon bool) (string, error) {
	budget := runtimeConfigBudget{bytes: 2 << 20, files: 128, entries: 4096}
	path, daemon, err := runtime.pidPath(options, &budget)
	if err != nil || requireDaemon && !daemon {
		return "", runtimeConflict("无法核验面板实例的 PID 文件或守护进程配置")
	}
	targetInfo, statErr := os.Lstat(path)
	if targetPID == 0 && !os.IsNotExist(statErr) || targetPID != 0 && !pidFileMatches(path, targetPID) {
		return "", runtimeConflict("PID 文件未释放或没有指向面板实例")
	}
	for _, process := range selection.processes {
		command, master := strings.CutPrefix(process.title, nginxMasterTitle)
		if !master || process.pid == targetPID {
			continue
		}
		if !process.sameBinary || !runtime.localRootMaster(process.pid) {
			return "", runtimeConflict("无法核验其他 Nginx 实例的 PID 文件，拒绝直接重启")
		}
		other, ok := parseRuntimeOptions(command, runtime.defaultConf, runtime.prefix)
		if !ok {
			return "", runtimeConflict("无法解析其他 Nginx 实例的启动配置")
		}
		otherPath, _, err := runtime.pidPath(other, &budget)
		if err != nil || otherPath == path {
			return "", runtimeConflict("其他 Nginx 实例的 PID 文件可能冲突，拒绝直接重启")
		}
		otherInfo, err := os.Lstat(otherPath)
		if err != nil || targetInfo != nil && os.SameFile(targetInfo, otherInfo) || !pidFileMatches(otherPath, process.pid) {
			return "", runtimeConflict("其他 Nginx 实例的 PID 文件无法核验，拒绝直接重启")
		}
	}
	return path, nil
}

func pidFileMatches(path string, pid int) bool {
	file, err := openRuntimeRegular(path)
	if err != nil {
		return false
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, 33))
	return err == nil && len(contents) <= 32 && strings.TrimSpace(string(contents)) == strconv.Itoa(pid)
}

func openRuntimeRegular(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !runtimeOwned(info) {
		file.Close()
		return nil, fmt.Errorf("不是可信的普通文件")
	}
	return file, nil
}

func runtimeOwned(info os.FileInfo) bool {
	return runtimeOwner(info) && info.Mode().Perm()&0022 == 0
}

func runtimeOwner(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && (stat.Uid == 0 || int(stat.Uid) == os.Geteuid())
}
