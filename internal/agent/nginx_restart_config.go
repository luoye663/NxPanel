package agent

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type runtimeConfigBudget struct{ bytes, files, entries int }

type runtimeConfig struct {
	base, pid string
	noDaemon  bool
	budget    *runtimeConfigBudget
	active    map[string]bool
}

var errRuntimeConfig = errors.New("配置不是可核验的静态 Nginx 配置，或超出读取上限")

func (runtime nginxRuntime) pidPath(options runtimeOptions, budget *runtimeConfigBudget) (string, bool, error) {
	config := runtimeConfig{base: filepath.Dir(options.conf), budget: budget, active: make(map[string]bool)}
	if err := config.read(options.conf); err != nil {
		return "", false, err
	}
	pid := config.pid
	if pid == "" {
		pid = runtime.defaultPID
	}
	if !plainRuntimePath(pid, false) {
		return "", false, errRuntimeConfig
	}
	path := absoluteRuntimePath(options.prefix, pid)
	if path == "" || path == "/" {
		return "", false, errRuntimeConfig
	}
	if _, err := trustedRuntimeDir(options.prefix); err != nil {
		return "", false, err
	}
	dir, err := trustedRuntimeDir(filepath.Dir(path))
	if err != nil {
		return "", false, err
	}
	return filepath.Join(dir, filepath.Base(path)), !config.noDaemon, nil
}

func trustedRuntimeDir(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	for _, dir := range []string{path, real} {
		for current := dir; ; current = filepath.Dir(current) {
			info, err := os.Stat(current)
			if err != nil || !info.IsDir() || !runtimeOwned(info) && (current == dir || info.Mode()&os.ModeSticky == 0 || !runtimeOwner(info)) {
				return "", errRuntimeConfig
			}
			if current == "/" {
				break
			}
		}
	}
	return real, nil
}

func (config *runtimeConfig) read(path string) error {
	if !filepath.IsAbs(path) || config.budget.files <= 0 || len(config.active) >= 8 {
		return errRuntimeConfig
	}
	config.budget.files--
	real, err := filepath.EvalSymlinks(path)
	if err != nil || config.active[real] {
		return errRuntimeConfig
	}
	for _, dir := range []string{filepath.Dir(path), filepath.Dir(real)} {
		if _, err := trustedRuntimeDir(dir); err != nil {
			return err
		}
	}
	file, err := openRuntimeRegular(real)
	if err != nil {
		return err
	}
	contents, err := io.ReadAll(io.LimitReader(file, int64(config.budget.bytes)+1))
	_ = file.Close()
	if err != nil || len(contents) > config.budget.bytes {
		return errRuntimeConfig
	}
	config.budget.bytes -= len(contents)
	config.active[real] = true
	defer delete(config.active, real)
	return config.scan(string(contents))
}

func (config *runtimeConfig) scan(text string) error {
	var depth, count int
	var name, value string
	for len(text) > 0 {
		token, delimiter, rest, err := nextRuntimeConfigToken(text)
		if err != nil {
			return err
		}
		text = rest
		switch delimiter {
		case '{':
			if depth == 0 && (name == "pid" || name == "include" || name == "daemon" || name == "master_process") {
				return errRuntimeConfig
			}
			depth++
		case '}':
			depth--
			if depth < 0 {
				return errRuntimeConfig
			}
		case ';':
			if depth == 0 {
				if err := config.directive(name, value, count); err != nil {
					return err
				}
			}
		default:
			if depth == 0 && (token != "" || rest != "") {
				if count == 0 {
					name = token
				} else if count == 1 {
					value = token
				}
				count++
			}
			continue
		}
		name, value, count = "", "", 0
	}
	if depth != 0 || count != 0 {
		return errRuntimeConfig
	}
	return nil
}

func (config *runtimeConfig) directive(name, value string, count int) error {
	switch name {
	case "pid":
		if count != 2 || config.pid != "" || !plainRuntimePath(value, false) {
			return errRuntimeConfig
		}
		config.pid = value
	case "daemon", "master_process":
		if count != 2 || (value != "on" && value != "off") {
			return errRuntimeConfig
		}
		config.noDaemon = config.noDaemon || value == "off"
	case "include":
		if count != 2 || !plainRuntimePath(value, true) {
			return errRuntimeConfig
		}
		return config.include(absoluteRuntimePath(config.base, value))
	}
	return nil
}

func plainRuntimePath(value string, glob bool) bool {
	return value != "" && !strings.ContainsAny(value, " \t\r\n\x00\\$\"'{};") && (glob || !strings.ContainsAny(value, "*?[]"))
}

func (config *runtimeConfig) include(path string) error {
	if strings.ContainsAny(path, "[]") {
		return errRuntimeConfig
	}
	if !strings.ContainsAny(path, "*?") {
		return config.read(path)
	}
	dir, pattern := filepath.Dir(path), filepath.Base(path)
	if strings.ContainsAny(dir, "*?") {
		return errRuntimeConfig
	}
	if _, err := trustedRuntimeDir(dir); err != nil {
		return err
	}
	if _, err := filepath.Match(pattern, ""); err != nil || config.budget.entries <= 0 {
		return errRuntimeConfig
	}
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	names, err := file.Readdirnames(config.budget.entries + 1)
	_ = file.Close()
	if err != nil && err != io.EOF || len(names) > config.budget.entries {
		return errRuntimeConfig
	}
	config.budget.entries -= len(names)
	sort.Strings(names)
	for _, name := range names {
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(pattern, ".") {
			continue
		}
		if matches, _ := filepath.Match(pattern, name); matches {
			if err := config.read(filepath.Join(dir, name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func nextRuntimeConfigToken(text string) (string, byte, string, error) {
	for len(text) > 0 {
		text = strings.TrimLeft(text, " \t\r\n")
		if !strings.HasPrefix(text, "#") {
			break
		}
		_, text, _ = strings.Cut(text, "\n")
	}
	if text == "" {
		return "", 0, "", nil
	}
	if strings.ContainsRune("{};", rune(text[0])) {
		return "", text[0], text[1:], nil
	}
	var quote byte
	start := 0
	if text[0] == '\'' || text[0] == '"' {
		quote, start = text[0], 1
	}
	for index := start; index < len(text); index++ {
		char := text[index]
		if char == '\\' {
			index++
			if index >= len(text) {
				return "", 0, "", errRuntimeConfig
			}
			continue
		}
		if quote != 0 {
			if char == quote {
				if index+1 < len(text) && !strings.ContainsRune(" \t\r\n;{", rune(text[index+1])) {
					return "", 0, "", errRuntimeConfig
				}
				return text[start:index], 0, text[index+1:], nil
			}
			continue
		}
		if char == '{' && index > 0 && text[index-1] == '$' {
			end := strings.IndexByte(text[index:], '}')
			if end < 0 {
				return "", 0, "", errRuntimeConfig
			}
			index += end
			continue
		}
		if strings.ContainsRune(" \t\r\n;{", rune(char)) {
			return text[:index], 0, text[index:], nil
		}
	}
	if quote != 0 {
		return "", 0, "", errRuntimeConfig
	}
	return text, 0, "", nil
}
