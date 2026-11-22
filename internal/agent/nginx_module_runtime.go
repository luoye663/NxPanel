package agent

import (
	"context"
	"crypto/sha256"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
	"github.com/luoye663/nxpanel/internal/nginxpatch"
)

type moduleRuntime struct {
	nginxRuntime
	selection     runtimeSelection
	binaryHash    string
	confPath      string
	conf          []byte
	loadedPaths   map[string]bool
	modulePaths   map[string]bool
	moduleRefs    map[string]int
	moduleSymbols map[string]map[string]bool
	staticSymbols map[string]bool
	static        bool
}

func trustedModuleBinary(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("需要已探测的 Nginx 绝对二进制路径")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	for _, dir := range []string{filepath.Dir(path), filepath.Dir(real)} {
		if _, err := trustedRuntimeDir(dir); err != nil {
			return "", err
		}
	}
	return real, nil
}

func fingerprintModuleBinary(ctx context.Context, path string) (string, error) {
	file, err := openRuntimeRegular(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	const limit = 128 << 20
	size, err := copyWithContext(ctx, hash, io.LimitReader(file, limit+1))
	if err != nil {
		return "", err
	}
	if size == 0 || size > limit {
		return "", fmt.Errorf("Nginx 二进制大小无法核验")
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func moduleConfigFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	if stat, err := file.Stat(); err != nil || !stat.Mode().IsRegular() {
		_ = file.Close()
		return nil, errRuntimeConfig
	}
	return file, nil
}

func (s *Server) readModuleMain(path string) (string, []byte, error) {
	if _, err := s.policy.Validate(path); err != nil {
		return "", nil, err
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, err
	}
	if _, err := s.policy.Validate(real); err != nil {
		return "", nil, err
	}
	file, err := moduleConfigFile(real)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	data, err := readModuleBytes(file, nginxpatch.MaxConfigBytes)
	return real, data, err
}

func exportedModuleSymbols(path string) (map[string]bool, error) {
	file, err := moduleConfigFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > 128<<20 {
		return nil, errRuntimeConfig
	}
	binary, err := elf.NewFile(file)
	if err != nil {
		return nil, err
	}
	defer binary.Close()
	if symbols := binary.SectionByType(elf.SHT_DYNSYM); symbols != nil && symbols.Size > 4<<20 {
		return nil, errRuntimeConfig
	}
	if strings := binary.Section(".dynstr"); strings != nil && strings.Size > 4<<20 {
		return nil, errRuntimeConfig
	}
	symbols, err := binary.DynamicSymbols()
	if errors.Is(err, elf.ErrNoSymbols) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	exported := make(map[string]bool)
	for _, symbol := range symbols {
		if nginxmodule.ValidSymbol(symbol.Name) && symbol.Section != elf.SHN_UNDEF {
			exported[symbol.Name] = true
		}
	}
	return exported, nil
}

func (s *Server) inspectModuleRuntime(ctx context.Context) (moduleRuntime, CmdResult, error) {
	var module moduleRuntime
	bin, err := trustedModuleBinary(s.executor.GetBin())
	if err != nil {
		return module, CmdResult{}, err
	}
	module.binaryHash, err = fingerprintModuleBinary(ctx, bin)
	if err != nil {
		return module, CmdResult{}, err
	}
	instance, version, err := s.executor.loadRuntime(ctx, "/proc")
	module.nginxRuntime = instance
	if err != nil {
		return module, version, err
	}
	module.selection, err = instance.inspect(ctx)
	if err != nil {
		return module, version, err
	}
	if module.selection.status.OtherActionsBlocked || module.selection.master != nil && module.selection.master.deletedBinary {
		return module, version, fmt.Errorf("存在其他 Nginx 实例或运行中二进制已被替换，请先核对实例并完成 Nginx 升级")
	}
	options := module.selection.options
	if module.selection.master == nil {
		options = runtimeOptions{prefix: instance.prefix, conf: instance.conf}
	}
	module.confPath, module.conf, err = s.readModuleMain(options.conf)
	if err != nil {
		return module, version, err
	}
	module.staticSymbols, err = exportedModuleSymbols(bin)
	if err != nil {
		return module, version, fmt.Errorf("无法核验 Nginx 内建模块: %w", err)
	}
	module.loadedPaths = make(map[string]bool)
	module.static = module.staticSymbols[nginxmodule.Symbol]
	module.modulePaths = make(map[string]bool)
	module.moduleRefs = make(map[string]int)
	module.moduleSymbols = make(map[string]map[string]bool)
	seen := make(map[string]bool)
	config := runtimeConfig{base: filepath.Dir(options.conf), budget: &runtimeConfigBudget{nginxpatch.MaxConfigBytes, 128, 4096}, active: make(map[string]bool),
		configDir: func(path string) error {
			if _, err := trustedRuntimeDir(path); err == nil {
				return nil
			}
			_, err := s.policy.Validate(path)
			return err
		}, open: moduleConfigFile, module: func(value string) error {
			path := absoluteRuntimePath(options.prefix, value)
			if path == "" {
				return errRuntimeConfig
			}
			realPath := canonicalRuntimePath(path)
			module.moduleRefs[realPath]++
			if seen[path] {
				return nil
			}
			if len(seen) >= 32 {
				return errRuntimeConfig
			}
			seen[path] = true
			module.modulePaths[realPath] = true
			if filepath.Base(path) == nginxmodule.Filename {
				module.loadedPaths[realPath] = true
				module.moduleSymbols[realPath] = map[string]bool{nginxmodule.Symbol: true}
				return nil
			}
			real, err := trustedModuleBinary(path)
			if err != nil {
				return err
			}
			symbols, err := exportedModuleSymbols(real)
			module.moduleSymbols[real] = symbols
			if symbols[nginxmodule.Symbol] {
				module.loadedPaths[real] = true
			}
			return err
		}}
	return module, version, config.read(options.conf)
}

func (instance moduleRuntime) moduleCurrent(module nginxmodule.Module, managed managedModule, hasWorkers bool) (map[string]bool, bool, bool, error) {
	paths := make(map[string]bool)
	symbol := module.Symbol
	if managed.path != "" {
		symbol = managed.symbol
		paths[managed.path] = true
	}
	for path, symbols := range instance.moduleSymbols {
		if symbols[symbol] || module.ID == nginxmodule.GeoIP2 && instance.loadedPaths[path] {
			paths[path] = true
		}
	}
	static := instance.staticSymbols[symbol]
	if !hasWorkers || static || len(paths) == 0 {
		return paths, static, static && hasWorkers, nil
	}
	loaded, err := instance.masterHasModule(instance.selection, func(path string) bool {
		return paths[path]
	})
	return paths, static, loaded, err
}

func moduleTarget(instance moduleRuntime, version CmdResult) (nginxmodule.Target, error) {
	target := nginxmodule.Target{Arch: runtime.GOARCH, BinarySHA256: instance.binaryHash}
	for _, line := range strings.Split(version.Stdout+"\n"+version.Stderr, "\n") {
		if value, ok := strings.CutPrefix(line, "nginx version: "); ok {
			target.Runtime, target.RuntimeVersion, _ = strings.Cut(strings.TrimSpace(value), "/")
		}
	}
	if target.Runtime != "nginx" && target.Runtime != "openresty" || target.RuntimeVersion == "" {
		return target, fmt.Errorf("无法识别 Nginx / OpenResty 运行时")
	}
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return target, err
	}
	data, err := readModuleBytes(file, 16<<10)
	_ = file.Close()
	if err != nil {
		return target, err
	}
	target.OS = moduleOS(string(data))
	return target, nil
}

func moduleOS(data string) string {
	var id, version string
	for _, line := range strings.Split(data, "\n") {
		name, value, _ := strings.Cut(line, "=")
		switch name {
		case "ID":
			id = strings.Trim(value, "\"'")
		case "VERSION_ID":
			version = strings.Trim(value, "\"'")
		}
	}
	if id == "rocky" || id == "debian" {
		version, _, _ = strings.Cut(version, ".")
	}
	if id == "alpine" {
		major, rest, _ := strings.Cut(version, ".")
		minor, _, _ := strings.Cut(rest, ".")
		version = major + "." + minor
	}
	return id + ":" + version
}

func (module moduleRuntime) activeWorkers(selection runtimeSelection) (map[nginxProcess]bool, error) {
	if selection.master == nil {
		return nil, nil
	}
	probe := nginxGeneration{proc: module.procRoot}
	return probe.workersOf(moduleProcessRef(*selection.master))
}

func moduleProcessRef(process nginxRuntimeProcess) nginxProcess {
	return nginxProcess{pid: strconv.Itoa(process.pid), start: strconv.FormatUint(process.start, 10)}
}

func processHasModule(procRoot string, process nginxProcess, matches func(string) bool) (bool, error) {
	probe := nginxGeneration{proc: procRoot}
	if current, err := probe.identity(process.pid); err != nil || current != process {
		return false, errRuntimeConfig
	}
	file, err := os.Open(filepath.Join(procRoot, process.pid, "maps"))
	if err != nil {
		return false, err
	}
	data, err := readModuleBytes(file, 4<<20)
	_ = file.Close()
	if err != nil {
		return false, err
	}
	if current, err := probe.identity(process.pid); err != nil || current != process {
		return false, errRuntimeConfig
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.SplitN(strings.Join(strings.Fields(line), " "), " ", 6)
		if len(fields) == 6 && matches(strings.TrimSuffix(fields[5], " (deleted)")) {
			return true, nil
		}
	}
	return false, nil
}

func (module moduleRuntime) masterHasModule(selection runtimeSelection, matches func(string) bool) (bool, error) {
	if selection.master == nil {
		return false, nil
	}
	return processHasModule(module.procRoot, moduleProcessRef(*selection.master), matches)
}
