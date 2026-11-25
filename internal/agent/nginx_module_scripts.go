package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
)

type moduleReceipt struct {
	Tag       string                   `json:"tag"`
	Manifest  []byte                   `json:"manifest"`
	Signature []byte                   `json:"signature"`
	Uninstall []byte                   `json:"uninstall"`
	Build     *nginxmodule.SignedBuild `json:"build,omitempty"`
}

type moduleScriptTask struct {
	Module    string `json:"module"`
	Version   string `json:"version"`
	Action    string `json:"action"`
	SHA256    string `json:"sha256"`
	Directory string `json:"directory"`
}

func (source moduleSource) receiptDir() string { return filepath.Join(source.stateDir(), "receipts") }
func (source moduleSource) receiptPath(id string) string {
	return filepath.Join(source.receiptDir(), id+".json")
}
func (source moduleSource) scriptJournal() string {
	return filepath.Join(source.stateDir(), "script-task.json")
}

func (source moduleSource) checkScriptJournal() error {
	if _, err := os.Lstat(source.scriptJournal()); !os.IsNotExist(err) {
		return fmt.Errorf("上次签名脚本操作需人工检查：%s", source.scriptJournal())
	}
	return nil
}

func removeModuleState(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func sha256Text(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func moduleScriptMatches(body []byte, script nginxmodule.Script) bool {
	return int64(len(body)) == script.Size && sha256Text(body) == script.SHA256 && !bytes.ContainsRune(body, 0)
}

func readInstalledModuleAsset(path string, asset nginxmodule.Asset) ([]byte, bool) {
	if _, err := trustedRuntimeDir(filepath.Dir(path)); err != nil {
		return nil, false
	}
	file, err := openRuntimeRegular(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	body, err := readModuleBytes(file, asset.Size)
	return body, err == nil && moduleAssetMatches(body, asset)
}

func installedModuleAsset(runtime, symbol string, asset nginxmodule.Asset) ([]byte, bool) {
	var directories []string
	switch runtime {
	case "nginx":
		directories = []string{"/usr/lib/nginx/modules", "/usr/lib64/nginx/modules"}
	case "openresty":
		directories = []string{"/usr/local/openresty/nginx/modules"}
	}
	for _, directory := range directories {
		if body, ok := readInstalledModuleAsset(filepath.Join(directory, symbol+".so"), asset); ok {
			return body, true
		}
	}
	return nil, false
}

func (source moduleSource) fetchInstallAsset(ctx context.Context, offer moduleOffer) ([]byte, error) {
	if !source.test {
		if offer.receipt.Build == nil || offer.buildRequest == nil {
			return nil, fmt.Errorf("缺少构建服务签发的模块产物")
		}
		return source.fetch(ctx, "builds/"+offer.buildRequest.Key()+"/asset", offer.asset.Size)
	}
	if installed, ok := installedModuleAsset(offer.target.Runtime, offer.module.Symbol, offer.asset); ok {
		return installed, nil
	}
	return source.fetch(ctx, offer.asset.Name, offer.asset.Size, offer.module.Release)
}

func (s *Server) uninstallOffer(ctx context.Context, offer *moduleOffer, target nginxmodule.Target) error {
	source := s.moduleSource
	var receipt moduleReceipt
	err := modulePrivateDir(source.receiptDir(), false)
	if err == nil {
		err = readModuleState(source.receiptPath(offer.module.ID), 2*(3*nginxmodule.MaxEnvelope+nginxmodule.MaxScript), &receipt)
	}
	if err != nil {
		return fmt.Errorf("已安装模块的卸载凭据无法读取")
	}
	feed, err := source.savedIndex()
	if err != nil {
		return fmt.Errorf("无法读取最近已验证的模块索引：%w", err)
	}
	entry, registered := feed.entry(offer.module.ID)
	if !registered || !nginxmodule.ValidTag(receipt.Tag) {
		return fmt.Errorf("已安装模块的发布授权已移除或凭据无效")
	}
	manifest, err := nginxmodule.VerifyManifest(receipt.Manifest, receipt.Signature, entry, receipt.Tag, source.test)
	if err != nil {
		return err
	}
	module := manifest.Module
	if module.Symbol != offer.managed.symbol {
		return fmt.Errorf("没有与已安装模块匹配的签名卸载脚本")
	}
	digest, err := fingerprintModuleBinary(ctx, offer.managed.path)
	if err != nil || !moduleScriptMatches(receipt.Uninstall, module.Uninstall) {
		return fmt.Errorf("已安装模块或卸载脚本的 SHA-256 无法核验")
	}
	assets := manifest.Assets
	if receipt.Build != nil {
		result, err := receipt.Build.Verify(manifest, receipt.Manifest, feed.policy.Build)
		if err != nil || result.Request.Target != target {
			return fmt.Errorf("无法核验当前运行时的构建卸载凭据")
		}
		assets = []nginxmodule.Asset{result.Asset}
	}
	for _, asset := range assets {
		if asset.SHA256 == digest && (receipt.Build != nil || asset.Matches(target)) {
			offer.module, offer.asset, offer.target, offer.receipt, offer.path = module, asset, target, receipt, offer.managed.path
			offer.identity = "uninstall " + module.ID + " " + sha256Text(receipt.Manifest) + " " + module.Uninstall.SHA256 + " " + offer.managed.identity()
			return nil
		}
	}
	return fmt.Errorf("已安装模块的摘要或当前环境与签名卸载凭据不匹配")
}

func (s *Server) runModuleAction(ctx context.Context, offer moduleOffer, uninstall bool) nginxmodule.Result {
	result := nginxmodule.Result{State: "failed"}
	failure := func(message string) nginxmodule.Result { result.Message = message; return result }
	source := s.moduleSource
	action, script := "install", offer.module.Install
	if os.Geteuid() != 0 {
		return failure("签名模块脚本需要以 root 运行的 Agent")
	}
	if uninstall {
		action, script = "uninstall", offer.module.Uninstall
	}
	fetchScript := func(spec nginxmodule.Script) ([]byte, error) {
		body, err := source.fetch(ctx, spec.Name, spec.Size, offer.module.Release)
		if err != nil {
			return nil, err
		}
		if !moduleScriptMatches(body, spec) {
			return nil, fmt.Errorf("脚本大小或 SHA-256 与签名目录不符")
		}
		return body, nil
	}
	var scriptBody, assetBody []byte
	var err error
	if uninstall {
		scriptBody = offer.receipt.Uninstall
	} else if scriptBody, err = fetchScript(script); err != nil {
		return failure(err.Error())
	}
	if !uninstall {
		assetBody, err = source.fetchInstallAsset(ctx, offer)
		if err != nil {
			return failure(err.Error())
		}
		if !moduleAssetMatches(assetBody, offer.asset) {
			return failure("模块大小或 SHA-256 与签名目录不符")
		}
		if offer.receipt.Uninstall, err = fetchScript(offer.module.Uninstall); err != nil {
			return failure(err.Error())
		}
	}
	if !s.executor.lifecycleMu.TryLock() {
		return failure(errModuleBusy.Error())
	}
	defer s.executor.lifecycleMu.Unlock()
	if !s.transactionMu.TryLock() {
		return failure(errModuleBusy.Error())
	}
	defer s.transactionMu.Unlock()
	instance, version, err := s.inspectModuleRuntime(ctx)
	if err != nil {
		return failure("无法核验 Nginx 实例：" + err.Error())
	}
	id := offer.module.ID
	previous, err := instance.activeWorkers(instance.selection)
	if err != nil {
		return failure("无法核验 Nginx worker 状态：" + err.Error())
	}
	if err := s.moduleActionReadiness(ctx, instance, len(previous) != 0, id, offer.path); err != nil {
		return failure(err.Error())
	}
	currentTarget, err := moduleTarget(instance, version)
	if err != nil || currentTarget != offer.target {
		return failure("Nginx 运行时已变化，请重新确认")
	}
	managed, err := source.managedModules(instance)
	if err != nil {
		return failure(err.Error())
	}
	entry := managed[id]
	paths, static, loaded, err := instance.moduleCurrent(offer.module, entry, len(previous) != 0)
	precondition := err == nil && !static
	if uninstall {
		digest, err := fingerprintModuleBinary(ctx, entry.path)
		precondition = precondition && err == nil && digest == offer.asset.SHA256 && entry.path == offer.path && len(paths) == 1 && instance.moduleRefs[entry.path] == 1 && entry.identity() == offer.managed.identity() && moduleConfirmation(instance, currentTarget, offer.identity) == offer.status.UninstallConfirmation
	} else {
		precondition = precondition && !loaded && len(paths) == 0 && entry.path == "" && moduleConfirmation(instance, currentTarget, offer.identity) == offer.confirmation
	}
	if !precondition || ctx.Err() != nil || !moduleScriptMatches(scriptBody, script) {
		return failure("模块、配置或运行时状态已变化，未启动脚本")
	}
	if result, err := instance.testRestart(ctx, instance.selection.options); err != nil {
		return failure("现有 Nginx 配置未通过检查：" + moduleDiagnostic(result, err))
	}
	for _, path := range []string{source.stateDir(), source.receiptDir()} {
		if err := modulePrivateDir(path, true); err != nil {
			return failure(err.Error())
		}
	}
	dir, err := os.MkdirTemp(source.stateDir(), "script-")
	if err != nil {
		return failure("无法创建私有模块任务目录")
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	scriptFile := filepath.Join(dir, "action.sh")
	if err := os.WriteFile(scriptFile, scriptBody, 0400); err != nil {
		return failure("无法保存已验证脚本")
	}
	assetFile := offer.path
	if !uninstall {
		assetFile = filepath.Join(dir, "asset.so")
		if err := os.WriteFile(assetFile, assetBody, 0400); err != nil {
			return failure("无法保存已验证产物")
		}
	}
	master := moduleProcessRef(*instance.selection.master)
	argv := instance.selection.options.args()
	if ctx.Err() != nil {
		return failure("模块任务已取消，未启动脚本")
	}
	if !uninstall && !source.test && !time.Now().Before(offer.trustExpiry) {
		return failure("授权清单已过期")
	}
	journal := source.scriptJournal()
	if err := writeModuleState(journal, moduleScriptTask{Module: id, Version: offer.asset.Version, Action: action, SHA256: script.SHA256, Directory: dir}); err != nil {
		return failure("无法建立脚本任务记录，未执行")
	}
	keep = true
	result.Version, result.ScriptSHA256 = offer.asset.Version, script.SHA256
	env := []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C", "LANG=C", "HOME=" + dir, "TMPDIR=" + dir,
		"NXMODULE_PROTOCOL=1", "NXMODULE_ACTION=" + action, "NXMODULE_ID=" + id, "NXMODULE_SYMBOL=" + offer.module.Symbol,
		"NXMODULE_OS=" + nginxmodule.LegacyOS(currentTarget.OS), "NXMODULE_ARCH=" + currentTarget.Arch, "NXMODULE_RUNTIME=" + currentTarget.Runtime,
		"NXMODULE_ASSET=" + assetFile, "NXMODULE_ASSET_SHA256=" + offer.asset.SHA256, "NXMODULE_DEST=" + offer.path,
		"NXMODULE_WORK=" + dir, "NXMODULE_STATE=" + source.stateDir(), "NXMODULE_FRAGMENT=" + source.fragmentPath(id),
		"NXMODULE_NGINX=" + canonicalRuntimePath(instance.bin), "NXMODULE_CONF=" + instance.confPath,
		"NXMODULE_CONF_ALIAS=" + instance.selection.options.conf, "NXMODULE_CONF_SHA256=" + sha256Text(instance.conf),
		"NXMODULE_BIN_SHA256=" + instance.binaryHash, "NXMODULE_PANEL_DIR=" + s.cfg.Nginx.PanelDir,
		"NXMODULE_MASTER_PID=" + master.pid, "NXMODULE_MASTER_START=" + master.start,
	}
	output, exit, err := executeModuleScript(ctx, scriptFile, env, argv, nginxmodule.ScriptTimeout)
	result.ExitCode = &exit
	slog.Info("Nginx 模块脚本结束", "module", id, "action", action, "version", result.Version, "script_sha256", script.SHA256, "exit_code", exit)
	uncertain := func(message string) nginxmodule.Result {
		result.State = "needs_attention"
		return failure(message + "；任务记录和脚本备份：" + dir)
	}
	if err != nil && exit != 10 {
		return uncertain("模块脚本未完成：" + moduleScriptDiagnostic(output, err))
	}
	current, _, inspectErr := s.inspectModuleRuntime(ctx)
	if inspectErr != nil || current.selection.master == nil || moduleProcessRef(*current.selection.master) != master || current.binaryHash != instance.binaryHash || current.confPath != instance.confPath || !slices.Equal(current.selection.options.args(), argv) {
		return uncertain("脚本结束后无法核验原 Nginx 实例")
	}
	if output, err := current.testRestart(ctx, current.selection.options); err != nil {
		return uncertain("脚本结束后 Nginx 配置检查失败：" + moduleDiagnostic(output, err))
	}
	currentManaged, inspectErr := source.managedModules(current)
	if inspectErr != nil {
		return uncertain("脚本结束后无法核验托管配置")
	}
	currentEntry := currentManaged[id]
	workers, inspectErr := current.activeWorkers(current.selection)
	if inspectErr != nil {
		return uncertain("无法核验模块操作后的 Nginx worker 状态")
	}
	currentPaths, currentStatic, currentLoaded, inspectErr := current.moduleCurrent(offer.module, currentEntry, len(workers) != 0)
	if inspectErr != nil || currentStatic {
		return uncertain("无法核验模块最终状态")
	}
	if exit == 10 {
		if !bytes.Equal(instance.conf, current.conf) || entry.identity() != currentEntry.identity() || currentLoaded != loaded {
			return uncertain("脚本报告失败，但原配置未确认恢复")
		}
		if uninstall {
			digest, err := fingerprintModuleBinary(ctx, entry.path)
			if err != nil || digest != offer.asset.SHA256 {
				return uncertain("脚本报告失败，但原模块产物未确认保留")
			}
		}
		if err := removeModuleState(journal); err != nil {
			return uncertain("无法清理模块任务记录")
		}
		keep = false
		return failure(moduleScriptDiagnostic(output, err))
	}
	if len(workers) == 0 {
		return uncertain("未确认 Nginx 新一代 worker 接管")
	}
	for worker := range workers {
		if previous[worker] {
			return uncertain("未确认 Nginx 旧 worker 退出接管")
		}
	}
	if uninstall {
		if currentLoaded || len(currentPaths) != 0 || currentEntry.path != "" {
			return uncertain("未确认脚本已移除模块加载项")
		}
		if err := removeModuleState(source.receiptPath(id)); err != nil {
			return uncertain("无法清除卸载凭据")
		}
	} else {
		digest, err := fingerprintModuleBinary(ctx, offer.path)
		if !currentLoaded || len(currentPaths) != 1 || current.moduleRefs[offer.path] != 1 || currentEntry.path != offer.path || currentEntry.symbol != offer.module.Symbol || err != nil || digest != offer.asset.SHA256 {
			return uncertain("未确认脚本已启用匹配的模块产物")
		}
		if err := writeModuleState(source.receiptPath(id), offer.receipt); err != nil {
			return uncertain("无法保存离线卸载凭据")
		}
	}
	if err := removeModuleState(journal); err != nil {
		return uncertain("无法清理模块任务记录")
	}
	keep = false
	result.State = "success"
	return result
}

func moduleScriptDiagnostic(output CmdResult, err error) string {
	return strings.Map(func(char rune) rune {
		if unicode.IsControl(char) && char != '\n' && char != '\t' {
			return -1
		}
		return char
	}, moduleDiagnostic(output, err))
}

func moduleDiagnostic(result CmdResult, err error) string {
	diagnostic := strings.TrimSpace(result.Stderr)
	if diagnostic == "" && err != nil {
		diagnostic = err.Error()
	}
	return strings.ToValidUTF8(diagnostic[:min(len(diagnostic), 2048)], "�")
}

func executeModuleScript(ctx context.Context, script string, env, args []string, timeout time.Duration) (CmdResult, int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", append([]string{script}, args...)...)
	cmd.Dir, cmd.Env = filepath.Dir(script), env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	output := newCommandOutputState(2<<20, 4096, cancel)
	cmd.Stdout = commandOutputWriter{state: output}
	cmd.Stderr = commandOutputWriter{state: output, stderr: true}
	err := cmd.Run()
	result, overflow := output.result()
	if overflow != nil {
		return result, -1, overflow
	}
	if ctx.Err() != nil {
		return result, -1, ctx.Err()
	}
	if cmd.ProcessState == nil {
		return result, -1, err
	}
	exit := cmd.ProcessState.ExitCode()
	if err != nil && exit != 10 {
		err = fmt.Errorf("退出状态 %s：%w", strconv.Itoa(exit), err)
	}
	return result, exit, err
}
