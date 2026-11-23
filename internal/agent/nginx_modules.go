package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
	"github.com/luoye663/nxpanel/internal/update"
)

type moduleOffer struct {
	status       nginxmodule.Status
	module       nginxmodule.Module
	asset        nginxmodule.Asset
	target       nginxmodule.Target
	managed      managedModule
	path         string
	identity     string
	confirmation string
	receipt      moduleReceipt
	buildRequest *nginxmodule.BuildRequest
	buildStatus  *nginxmodule.BuildStatus
}

func (s *Server) handleNginxModules(w http.ResponseWriter, r *http.Request) {
	local := strings.HasSuffix(r.URL.Path, "/local")
	listing := nginxmodule.Listing{Modules: []nginxmodule.Status{}, FeedState: "busy", TestFeed: s.moduleSource.test}
	respond := func() {
		if local {
			if len(listing.Modules) == 0 {
				listing.Modules = append(listing.Modules, nginxmodule.Status{ID: nginxmodule.GeoIP2, State: "busy", Reason: "模块操作或检查正在进行"})
			}
			writeAgentOK(w, listing.Modules)
			return
		}
		writeAgentOK(w, listing)
	}
	if !s.moduleMu.TryLock() {
		listing.Busy = true
		respond()
		return
	}
	defer s.moduleMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 23*time.Second)
	defer cancel()
	var feed moduleFeed
	if !local {
		var err error
		feed, err = s.loadModuleFeed(ctx, r.URL.Query().Get("refresh") == "1")
		listing.FeedState = "online"
		if err != nil {
			listing.FeedState, listing.FeedMessage = "error", err.Error()
			feed = s.cachedModuleFeed()
			if feed.floor.Sequence != 0 {
				listing.FeedState = "stale"
			}
		}
	}
	instance, version, managed, runtimeErr := s.moduleSnapshot(ctx)
	listing.Busy = runtimeErr == errModuleBusy
	recoveryErr := s.moduleSource.checkScriptJournal()
	modules := make([]nginxmodule.Module, 0, len(feed.entries))
	if local {
		modules = []nginxmodule.Module{nginxmodule.GeoIP2Module()}
	} else {
		seen := make(map[string]bool, len(feed.entries))
		for _, entry := range feed.entries {
			modules = append(modules, entry.DisplayModule())
			seen[entry.ID] = true
		}
		if !seen[nginxmodule.GeoIP2] && (instance.static || len(instance.loadedPaths) != 0 || managed[nginxmodule.GeoIP2].path != "") {
			modules = append(modules, nginxmodule.GeoIP2Module())
		}
		var ids []string
		for id := range managed {
			if id != nginxmodule.GeoIP2 && !seen[id] {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			modules = append(modules, nginxmodule.Module{ID: id, Name: id, Kind: nginxmodule.DynamicKind, Symbol: managed[id].symbol})
		}
	}
	for _, module := range modules {
		_, registered := feed.entry(module.ID)
		offer := s.describeModule(ctx, instance, version, module, managed[module.ID], !local, registered && listing.FeedState == "online", runtimeErr)
		if recoveryErr != nil {
			offer.status.State, offer.status.Reason = "needs_attention", recoveryErr.Error()
			offer.status.UninstallConfirmation, offer.status.Action = "", ""
		}
		listing.Modules = append(listing.Modules, offer.status)
	}
	respond()
}

var errModuleBusy = fmt.Errorf("Nginx 控制操作或配置事务正在执行")

func (s *Server) moduleSnapshot(ctx context.Context) (moduleRuntime, CmdResult, map[string]managedModule, error) {
	if !s.executor.lifecycleMu.TryLock() {
		return moduleRuntime{}, CmdResult{}, nil, errModuleBusy
	}
	defer s.executor.lifecycleMu.Unlock()
	if !s.transactionMu.TryLock() {
		return moduleRuntime{}, CmdResult{}, nil, errModuleBusy
	}
	defer s.transactionMu.Unlock()
	instance, version, err := s.inspectModuleRuntime(ctx)
	if err != nil {
		return instance, version, nil, err
	}
	managed, err := s.moduleSource.managedModules(instance)
	return instance, version, managed, err
}

func (s *Server) handleNginxModuleInstall(w http.ResponseWriter, r *http.Request) {
	s.handleNginxModuleAction(w, r, false)
}

func (s *Server) handleNginxModuleUninstall(w http.ResponseWriter, r *http.Request) {
	s.handleNginxModuleAction(w, r, true)
}

func (s *Server) handleNginxModuleAction(w http.ResponseWriter, r *http.Request, uninstall bool) {
	var request nginxmodule.InstallRequest
	if update.Decode(http.MaxBytesReader(w, r.Body, 1024), &request) != nil || !request.Valid(uninstall) {
		writeAgentError(w, http.StatusBadRequest, "无效的模块操作确认")
		return
	}
	if !s.moduleMu.TryLock() {
		writeAgentOK(w, nginxmodule.Result{State: "failed", Message: "另一个模块操作或检查正在执行，请重试"})
		return
	}
	defer s.moduleMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), nginxmodule.ActionTimeout)
	defer cancel()
	if err := s.moduleSource.checkScriptJournal(); err != nil {
		writeAgentOK(w, nginxmodule.Result{State: "needs_attention", Message: err.Error()})
		return
	}
	var offer moduleOffer
	var verifyErr error
	var ticket string
	if uninstall {
		instance, version, managed, runtimeErr := s.moduleSnapshot(ctx)
		module := nginxmodule.Module{ID: request.ID, Kind: nginxmodule.DynamicKind, Symbol: managed[request.ID].symbol}
		if request.ID == nginxmodule.GeoIP2 {
			module = nginxmodule.GeoIP2Module()
		}
		offer = s.describeModule(ctx, instance, version, module, managed[request.ID], true, false, runtimeErr)
		ticket = offer.status.UninstallConfirmation
	} else {
		offer, verifyErr = s.prepareModuleInstall(ctx, request.ID, request.Tag, true)
		ticket = offer.confirmation
	}
	if verifyErr != nil || ticket != request.Confirmation {
		message := "模块授权、版本、运行时或配置已变化，请重新确认"
		if verifyErr != nil {
			message += "：" + verifyErr.Error()
		} else if offer.status.Reason != "" {
			message += "：" + offer.status.Reason
		}
		writeAgentOK(w, nginxmodule.Result{State: "failed", Message: message})
		return
	}
	result := s.runModuleAction(ctx, offer, uninstall)
	if result.State != "success" {
		slog.Warn("Nginx 模块操作未完成", "module", request.ID, "uninstall", uninstall, "state", result.State, "reason", result.Message)
	}
	writeAgentOK(w, result)
}

func (s *Server) describeModule(ctx context.Context, instance moduleRuntime, version CmdResult, module nginxmodule.Module, managed managedModule, checkUninstall, canInstall bool, runtimeErr error) moduleOffer {
	offer := moduleOffer{module: module, managed: managed, status: nginxmodule.Status{ID: module.ID, Name: module.Name, Description: module.Description, Repository: module.Release.Repository, State: "unavailable", TestFeed: s.moduleSource.test}}
	status := &offer.status
	if runtimeErr != nil {
		status.Reason = runtimeErr.Error()
		if runtimeErr == errModuleBusy {
			status.State = "busy"
		}
		return offer
	}
	if module.Kind != nginxmodule.DynamicKind && managed.path == "" {
		status.Reason = "当前 Agent 不支持此模块的安装类型"
		return offer
	}
	status.Runtime = strings.TrimSpace(strings.SplitN(version.Stderr+version.Stdout, "\n", 2)[0])
	workers, err := instance.activeWorkers(instance.selection)
	if err != nil {
		status.Reason = "无法核验 Nginx worker 状态：" + err.Error()
		return offer
	}
	paths, static, loaded, err := instance.moduleCurrent(module, managed, len(workers) != 0)
	if err != nil {
		status.Reason = "无法核验模块加载状态：" + err.Error()
		return offer
	}
	status.Configured, status.Loaded = static || len(paths) > 0, loaded
	status.Present = static || loaded
	for path := range paths {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			status.Present = true
		}
	}
	if managed.binaryHash != "" && managed.binaryHash != instance.binaryHash {
		status.State, status.Reason = "drifted", "Nginx 二进制已变化，请核对模块兼容性和主配置"
		return offer
	}
	target, targetErr := moduleTarget(instance, version)
	if checkUninstall && targetErr == nil && !static && managed.path != "" && len(paths) == 1 && paths[managed.path] && instance.moduleRefs[managed.path] == 1 {
		if err := s.moduleActionReadiness(ctx, instance, len(workers) != 0, module.ID, managed.path); err != nil {
			status.Reason = err.Error()
		} else if err := s.uninstallOffer(ctx, &offer, target); err == nil {
			status.UninstallConfirmation = moduleConfirmation(instance, offer.target, offer.identity)
			status.Version = offer.asset.Version
		} else {
			status.Reason = err.Error()
		}
	}
	if loaded {
		status.State = "enabled"
		return offer
	}
	if status.Configured {
		status.State, status.Reason = "configured", "模块已写入配置，但未确认加载；请检查 Nginx 配置和日志"
		return offer
	}
	if !canInstall {
		status.Reason = "运行中未加载模块"
		return offer
	}
	if targetErr != nil {
		status.Reason = targetErr.Error()
		return offer
	}
	if err := s.moduleActionReadiness(ctx, instance, len(workers) != 0, module.ID, ""); err != nil {
		status.Reason = err.Error()
		return offer
	}
	status.State, status.Action = "available", "install"
	return offer
}

func (entry managedModule) identity() string {
	return sha256Text(entry.fragment)
}

func moduleConfirmation(instance moduleRuntime, target nginxmodule.Target, identity string) string {
	value := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%x", instance.selection.status.RestartGeneration, instance.binaryHash, target.OS, target.Arch, target.Runtime, target.RuntimeVersion, identity, instance.confPath, sha256.Sum256(instance.conf))
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func inModuleContainer() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func (s *Server) lockNginxFileMutation(paths ...string) func() {
	var conf, panel string
	if s.executor != nil {
		conf = s.executor.GetConfPath()
	}
	if s.cfg != nil {
		panel = s.cfg.Nginx.PanelDir
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			continue
		}
		if filepath.IsAbs(conf) && (pathsOverlap(path, filepath.Dir(conf)) || pathsOverlap(path, conf)) ||
			filepath.IsAbs(panel) && pathsOverlap(path, panel) {
			s.transactionMu.Lock()
			return s.transactionMu.Unlock
		}
	}
	return func() {}
}
