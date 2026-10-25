package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type nginxProcess struct{ pid, start string }
type nginxGeneration struct {
	proc, exe, conf, defaultConf string
	prefix                       string
	exeInfo                      os.FileInfo
	workers                      map[nginxProcess]bool
}

func procText(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil {
		return "", err
	}
	if len(b) > 16384 {
		return "", fmt.Errorf("process metadata exceeds inspection budget")
	}
	return string(b), nil
}
func canonicalRuntimePath(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}
func (g *nginxGeneration) identity(pid string) (nginxProcess, error) {
	if _, err := strconv.ParseUint(pid, 10, 32); err != nil {
		return nginxProcess{}, fmt.Errorf("invalid process identity")
	}
	text, err := procText(filepath.Join(g.proc, pid, "stat"))
	if err != nil {
		return nginxProcess{}, err
	}
	end := strings.LastIndex(text, ")")
	if end < 0 {
		return nginxProcess{}, fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(text[end+1:])
	if len(fields) < 20 {
		return nginxProcess{}, fmt.Errorf("incomplete process stat")
	}
	return nginxProcess{pid: pid, start: fields[19]}, nil
}
func (g *nginxGeneration) activeWorker(ref nginxProcess) (bool, error) {
	current, err := g.identity(ref.pid)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if current != ref {
		return false, nil
	}
	cmd, err := procText(filepath.Join(g.proc, ref.pid, "cmdline"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.Contains(cmd, "worker process") && !strings.Contains(cmd, "shutting down"), nil
}
func (g *nginxGeneration) master() (*nginxProcess, error) {
	entries, err := os.ReadDir(g.proc)
	if err != nil {
		return nil, err
	}
	if len(entries) > 32768 {
		return nil, fmt.Errorf("process count exceeds inspection budget")
	}
	var found *nginxProcess
	for _, entry := range entries {
		pid := entry.Name()
		if _, err := strconv.ParseUint(pid, 10, 32); err != nil {
			continue
		}
		exePath := filepath.Join(g.proc, pid, "exe")
		exe, err := os.Readlink(exePath)
		if err != nil {
			continue
		}
		exe = strings.TrimSuffix(exe, " (deleted)")
		if canonicalRuntimePath(exe) != g.exe {
			continue
		}
		info, err := os.Stat(exePath)
		if err != nil {
			continue
		}
		if !os.SameFile(g.exeInfo, info) {
			continue
		}
		cmd, err := procText(filepath.Join(g.proc, pid, "cmdline"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !strings.Contains(cmd, "master process") {
			continue
		}
		conf := runtimeConfPath(cmd, g.defaultConf, g.prefix)
		if conf == "" || !filepath.IsAbs(conf) || canonicalRuntimePath(conf) != g.conf {
			continue
		}
		ref, err := g.identity(pid)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if found != nil {
			return nil, fmt.Errorf("multiple Nginx masters match configured executable and configuration")
		}
		found = &ref
	}
	return found, nil
}
func (g *nginxGeneration) currentWorkers() (map[nginxProcess]bool, error) {
	master, err := g.master()
	if err != nil {
		return nil, err
	}
	result := map[nginxProcess]bool{}
	if master == nil {
		return result, nil
	}
	text, err := procText(filepath.Join(g.proc, master.pid, "task", master.pid, "children"))
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	children := strings.Fields(text)
	if len(children) > 4096 {
		return nil, fmt.Errorf("worker count exceeds inspection budget")
	}
	for _, pid := range children {
		ref, err := g.identity(pid)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		active, err := g.activeWorker(ref)
		if err != nil {
			return nil, err
		}
		if active {
			result[ref] = true
		}
	}
	return result, nil
}
func (g *nginxGeneration) wait(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		oldActive := false
		for ref := range g.workers {
			active, err := g.activeWorker(ref)
			if err != nil {
				return err
			}
			if active {
				oldActive = true
			}
		}
		if !oldActive {
			current, err := g.currentWorkers()
			if err != nil {
				return err
			}
			for ref := range current {
				if !g.workers[ref] {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func newGenerationProbe(proc, exe, conf, defaultConf string, prefixes ...string) (func(context.Context) error, error) {
	g := &nginxGeneration{proc: proc, exe: canonicalRuntimePath(exe), conf: canonicalRuntimePath(conf), defaultConf: defaultConf}
	g.prefix = "/usr/local/nginx"
	if len(prefixes) > 0 {
		g.prefix = prefixes[0]
	}
	var err error
	g.exeInfo, err = os.Stat(exe)
	if err != nil {
		return nil, err
	}
	workers, err := g.currentWorkers()
	if err != nil {
		return nil, err
	}
	g.workers = workers
	return g.wait, nil
}

// Nginx resolves a relative -c against its runtime -p (or compiled prefix),
// not against the Agent's working directory. Keep the compiled path unexpanded
// until runtime options are applied.
func runtimeConfPath(cmd, conf, prefix string) string {
	fields := strings.Fields(strings.ReplaceAll(cmd, "\x00", " "))
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if len(field) < 2 || field[0] != '-' {
			continue
		}
		option := field[1]
		if option != 'c' && option != 'p' && option != 'g' && option != 'e' {
			continue
		}
		value := field[2:]
		if value == "" {
			i++
			if i >= len(fields) {
				return ""
			}
			value = fields[i]
		}
		switch option {
		case 'c':
			conf = value
		case 'p':
			prefix = value
		}
	}
	if conf == "" || filepath.IsAbs(conf) {
		return conf
	}
	if !filepath.IsAbs(prefix) {
		return "" // The original startup cwd cannot be inferred safely.
	}
	return filepath.Join(prefix, conf)
}

func compiledRuntimePaths(output string) (string, string) {
	prefix := "/usr/local/nginx"
	conf := ""
	for _, field := range strings.Fields(output) {
		field = strings.Trim(field, "'\"")
		if strings.HasPrefix(field, "--prefix=") {
			prefix = strings.TrimPrefix(field, "--prefix=")
		}
		if strings.HasPrefix(field, "--conf-path=") {
			conf = strings.TrimPrefix(field, "--conf-path=")
		}
	}
	if conf == "" {
		conf = "conf/nginx.conf"
	}
	return prefix, conf
}

func compiledConfPath(output string) string {
	prefix, conf := compiledRuntimePaths(output)
	if !filepath.IsAbs(conf) {
		return filepath.Join(prefix, conf)
	}
	return conf
}

func (s *Server) captureReloadGeneration(ctx context.Context) (func(context.Context) error, error) {
	if s.reloadProbeForTest != nil {
		return s.reloadProbeForTest(ctx)
	}
	s.executor.mu.Lock()
	bin, conf := s.executor.bin, s.executor.confPath
	s.executor.mu.Unlock()
	if bin == "" || conf == "" {
		return nil, fmt.Errorf("configured Nginx executable and main configuration are required")
	}
	result, err := s.executor.run(ctx, bin, "-V")
	if err != nil {
		return nil, fmt.Errorf("cannot inspect compiled Nginx configuration")
	}
	prefix, defaultConf := compiledRuntimePaths(result.Stdout + "\n" + result.Stderr)
	return newGenerationProbe("/proc", bin, conf, defaultConf, prefix)
}

// SetReloadProbeForTest adapts process-namespace observation for container-based
// integration tests where Agent and the tested Nginx intentionally run apart.
func (s *Server) SetReloadProbeForTest(probe func(context.Context) (func(context.Context) error, error)) {
	s.reloadProbeForTest = probe
}
