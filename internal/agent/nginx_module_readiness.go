package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/luoye663/nxpanel/internal/georuntime"
	"github.com/luoye663/nxpanel/internal/nginxmodule"
	"github.com/luoye663/nxpanel/internal/nginxpatch"
)

func (s *Server) moduleActionReadiness(ctx context.Context, instance moduleRuntime, hasWorkers bool, id, assetPath string) error {
	master := instance.selection.master
	if master == nil {
		return fmt.Errorf("未找到面板管理的 Nginx 主进程，请在状态管理中检查实例")
	}
	if !hasWorkers {
		return fmt.Errorf("面板管理的 Nginx 已启动，但未发现活跃 worker，请检查 Nginx 日志")
	}
	if nginxpatch.HasMaintenance(string(instance.conf)) {
		return fmt.Errorf("Nginx 主配置仍包含面板维护模式，请退出维护模式后重试")
	}
	if !instance.rootMasterInScope(master.pid, "ns/pid", "ns/user", "ns/net") {
		return fmt.Errorf("无法核验 Nginx 主进程的 root 身份或运行环境")
	}
	mount, sameMount, err := instance.masterPathIdentity(master.pid, "ns/mnt")
	if err != nil {
		return fmt.Errorf("无法核验 Nginx 主进程的挂载命名空间")
	}
	if sameMount {
		return nil
	}
	if !instance.systemd || instance.s6 {
		return fmt.Errorf("Nginx 主进程的挂载命名空间不同，且不属于可核验的 systemd 服务")
	}
	service, err := instance.restartService(ctx, instance.selection)
	if err != nil || service == "" {
		return fmt.Errorf("Nginx 主进程的挂载命名空间不同，且无法核验其 systemd 服务归属")
	}
	proc := filepath.Join(instance.procRoot, strconv.Itoa(master.pid))
	if err := s.checkModuleSharedPaths(proc, instance, id, assetPath); err != nil {
		return err
	}
	probe := nginxGeneration{proc: instance.procRoot}
	current, err := probe.identity(strconv.Itoa(master.pid))
	currentMount, _, mountErr := instance.masterPathIdentity(master.pid, "ns/mnt")
	if err != nil || current != moduleProcessRef(*master) || mountErr != nil || !os.SameFile(mount, currentMount) {
		return fmt.Errorf("核验挂载路径时 Nginx 主进程已变化，请刷新后重试")
	}
	return nil
}

func (s *Server) checkModuleSharedPaths(proc string, instance moduleRuntime, id, assetPath string) error {
	host, err := os.OpenRoot("/")
	if err != nil {
		return fmt.Errorf("无法核验 Agent 的文件系统视图")
	}
	defer host.Close()
	peer, err := os.OpenRoot(filepath.Join(proc, "root"))
	if err != nil {
		return fmt.Errorf("无法核验 Nginx 的文件系统视图")
	}
	defer peer.Close()
	paths := []string{canonicalRuntimePath(instance.bin), instance.selection.options.prefix, instance.selection.options.conf}
	for _, path := range paths {
		if !modulePathShared(host, peer, path, false, false) {
			return fmt.Errorf("Nginx 与 Agent 的挂载路径不一致：%s", path)
		}
	}
	if !modulePathShared(host, peer, instance.confPath, false, true) {
		return fmt.Errorf("Nginx 与 Agent 未共享可替换的主配置：%s", instance.confPath)
	}
	paths = []string{s.moduleSource.stateDir(), s.moduleSource.activeDir(), s.moduleSource.fragmentPath(id), assetPath, s.cfg.Nginx.PanelDir}
	if id == nginxmodule.GeoIP2 {
		paths = append(paths, georuntime.ConfigPath(s.cfg.Nginx.PanelDir))
	}
	for _, path := range paths {
		if path != "" && !modulePathShared(host, peer, path, true, true) {
			return fmt.Errorf("Nginx 与 Agent 未共享模块路径：%s", path)
		}
	}
	return nil
}

func modulePathShared(host, peer *os.Root, path string, optional, replace bool) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	parentOnly := false
	for {
		name := strings.TrimPrefix(path, "/")
		if name == "" {
			name = "."
		}
		local, localErr := host.Stat(name)
		remote, remoteErr := peer.Stat(name)
		if localErr == nil && remoteErr == nil {
			if !os.SameFile(local, remote) {
				return false
			}
			if parentOnly {
				return local.IsDir()
			}
			return !replace || local.IsDir() || local.Mode().IsRegular() &&
				modulePathShared(host, peer, filepath.Dir(path), false, false) &&
				moduleFileFollowsParent(host, name) && moduleFileFollowsParent(peer, name)
		}
		if !optional || !os.IsNotExist(localErr) || !os.IsNotExist(remoteErr) || path == "/" {
			return false
		}
		if _, err := host.Lstat(name); !os.IsNotExist(err) {
			return false
		}
		if _, err := peer.Lstat(name); !os.IsNotExist(err) {
			return false
		}
		path, parentOnly = filepath.Dir(path), true
	}
}

func moduleFileFollowsParent(root *os.Root, name string) bool {
	file, err := root.OpenFile(name, unix.O_PATH|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false
	}
	defer file.Close()
	parent, err := root.OpenFile(filepath.Dir(name), unix.O_PATH|unix.O_DIRECTORY, 0)
	if err != nil {
		return false
	}
	defer parent.Close()
	var fileStat, parentStat unix.Statx_t
	return unix.Statx(int(file.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID|unix.STATX_TYPE, &fileStat) == nil &&
		fileStat.Mask&unix.STATX_MNT_ID != 0 && fileStat.Mode&unix.S_IFMT == unix.S_IFREG &&
		unix.Statx(int(parent.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &parentStat) == nil &&
		parentStat.Mask&unix.STATX_MNT_ID != 0 && fileStat.Mnt_id == parentStat.Mnt_id
}
