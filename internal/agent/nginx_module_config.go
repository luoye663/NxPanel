package agent

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
)

const moduleIncludeMark = "# NXPANEL-MANAGED-MODULES\n"
const moduleFragmentMark = "# NXPANEL-DYNAMIC-MODULE "

type managedModule struct {
	path, symbol, binaryHash string
	fragment                 []byte
}

func (source moduleSource) activeDir() string { return filepath.Join(source.stateDir(), "active") }

func (source moduleSource) includeBlock() string {
	return moduleIncludeMark + "include " + filepath.Join(source.activeDir(), "*.conf") + ";\n"
}

func isModuleInclude(path string) bool {
	return path == filepath.Join((moduleSource{}).activeDir(), "*.conf") || path == filepath.Join((moduleSource{test: true}).activeDir(), "*.conf")
}

func (source moduleSource) fragmentPath(id string) string {
	return filepath.Join(source.activeDir(), id+".conf")
}

func (source moduleSource) assetPath(id, digest string) string {
	return filepath.Join(source.stateDir(), digest, id+".so")
}

func (source moduleSource) parseFragment(id string, data []byte) (managedModule, bool) {
	header, directive, ok := strings.Cut(string(data), "\n")
	header, marked := strings.CutPrefix(header, moduleFragmentMark)
	hash, symbol, separated := strings.Cut(header, " ")
	path, directiveOK := strings.CutPrefix(directive, "load_module \"")
	path, quoted := strings.CutSuffix(path, "\";\n")
	entry := managedModule{path: path, symbol: symbol, binaryHash: hash, fragment: data}
	if !ok || !marked || !separated || !directiveOK || !quoted || !nginxmodule.ValidID(id) || !nginxmodule.ValidDigest(hash) || !nginxmodule.ValidSymbol(symbol) ||
		(id == nginxmodule.GeoIP2) != (symbol == nginxmodule.Symbol) {
		return entry, false
	}
	digest := filepath.Base(filepath.Dir(path))
	return entry, nginxmodule.ValidDigest(digest) && path == source.assetPath(id, digest)
}

func (source moduleSource) managedModules(instance moduleRuntime) (map[string]managedModule, error) {
	result := make(map[string]managedModule)
	if bytes.Contains(instance.conf, []byte(moduleIncludeMark)) {
		markerCount := bytes.Count(instance.conf, []byte(moduleIncludeMark))
		if markerCount != 1 || !bytes.HasPrefix(instance.conf, []byte(source.includeBlock())) {
			other := moduleSource{test: !source.test}
			if markerCount == 1 && bytes.HasPrefix(instance.conf, []byte(other.includeBlock())) {
				configured, current := "本地测试源", "正式源"
				if source.test {
					configured, current = current, configured
				}
				return nil, fmt.Errorf("Nginx 主配置引用%s的模块加载目录，当前 Agent 使用%s；请先在原环境卸载模块再切换，未执行模块操作", configured, current)
			}
			return nil, fmt.Errorf("模块加载入口已被修改，未执行模块操作")
		}
		if err := modulePrivateDir(source.activeDir(), false); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		dir, err := os.Open(source.activeDir())
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil {
			names, err := dir.Readdirnames(129)
			_ = dir.Close()
			if err != nil && err != io.EOF || len(names) > 128 {
				return nil, fmt.Errorf("模块加载目录无法读取或条目过多")
			}
			for _, name := range names {
				id, ok := strings.CutSuffix(name, ".conf")
				if !ok || strings.HasPrefix(name, ".") {
					continue
				}
				data, err := readModuleFragment(filepath.Join(source.activeDir(), name))
				entry, valid := source.parseFragment(id, data)
				if err != nil || !valid || !instance.modulePaths[entry.path] {
					return nil, fmt.Errorf("模块加载片段无法核验: %s", name)
				}
				result[id] = entry
			}
		}
	}
	return result, nil
}

func readModuleFragment(path string) ([]byte, error) {
	file, err := openRuntimeRegular(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readModuleBytes(file, 1024)
}
