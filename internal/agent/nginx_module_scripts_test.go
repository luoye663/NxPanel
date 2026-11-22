package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
)

func TestModuleScriptExecution(t *testing.T) {
	t.Setenv("NXPANEL_AGENT_TOKEN", "not-for-scripts")
	dir := t.TempDir()
	script := filepath.Join(dir, "action.sh")
	env := []string{"PATH=/usr/bin:/bin", "NXMODULE_ACTION=uninstall"}
	run := func(body string, timeout time.Duration, args ...string) (CmdResult, int, error) {
		t.Helper()
		if err := os.WriteFile(script, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		return executeModuleScript(context.Background(), script, env, args, timeout)
	}
	result, exit, err := run(`test -z "${NXPANEL_AGENT_TOKEN:-}" || exit 99; printf '%s %s' "$NXMODULE_ACTION" "$1" >&2; exit 10`, 2*time.Second, "-g daemon off;")
	if err == nil || exit != 10 || result.Stderr != "uninstall -g daemon off;" {
		t.Fatalf("execution did not isolate the environment or preserve the arguments: %+v, %d, %v", result, exit, err)
	}
	_, exit, err = run(`sleep 20 & wait`, 100*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || exit != -1 {
		t.Fatalf("script process group did not time out: %d, %v", exit, err)
	}
	result, exit, err = run(`yes >&2`, 2*time.Second)
	if err == nil || exit != -1 || len(result.Stderr) > 4096 {
		t.Fatalf("script output was not capped: %d bytes, %d, %v", len(result.Stderr), exit, err)
	}
}

func TestModuleScriptBytesMustMatch(t *testing.T) {
	body := []byte("#!/bin/sh\nexit 0\n")
	digest := sha256Text(body)
	spec := nginxmodule.Script{Name: nginxmodule.ScriptName("test", "install", digest), Size: int64(len(body)), SHA256: digest}
	if !moduleScriptMatches(body, spec) {
		t.Fatal("rejected matching script")
	}
	changed := []byte(strings.Replace(string(body), "exit 0", "exit 1", 1))
	if moduleScriptMatches(changed, spec) {
		t.Fatal("accepted same-size tampering")
	}
}
