package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const trialTimeout = 90 * time.Second

func (u *Updater) activate(state State) {
	err := u.apply(&state)
	if err != nil {
		u.rollback(state, err)
		return
	}
	_ = os.Remove(u.snapshotPath())
	u.cleanupSlots()
	slog.Info("update committed", "version", state.Version, "id", state.ID)
}

func (u *Updater) apply(state *State) error {
	if err := u.stopPanel(); err != nil {
		return err
	}
	if err := u.snapshot(state.DBPath); err != nil {
		return fmt.Errorf("database snapshot failed: %w", err)
	}
	state.Snapshot = true
	if err := u.store(*state); err != nil {
		return err
	}
	if err := u.switchSlot("versions/" + state.ID); err != nil {
		return err
	}
	state.Phase = Trial
	if err := u.startTrial(*state); err != nil {
		return err
	}
	state.Phase, state.Snapshot = Committed, false
	if err := u.store(*state); err != nil {
		state.Snapshot = true
		return err
	}
	return nil
}

func (u *Updater) rollback(state State, cause error) {
	slog.Error("update activation failed", "version", state.Version, "id", state.ID, "error", cause)
	state.Phase, state.Error = Restoring, "更新未通过检查，正在恢复原版本"
	err := u.store(state)
	if err == nil {
		err = u.restore(&state)
	}
	if err != nil {
		slog.Error("update rollback failed", "id", state.ID, "error", err)
		state.Phase, state.Error = Blocked, "自动回滚失败，请查看更新服务日志；已保留数据快照"
		if err := u.store(state); err != nil {
			slog.Error("persist update recovery state failed", "error", err)
		}
		_ = u.stopPanel()
		return
	}
	_ = os.Remove(u.snapshotPath())
	u.cleanupSlots()
	slog.Warn("update rolled back", "running_version", state.Previous, "id", state.ID)
}

func (u *Updater) restore(state *State) error {
	if err := u.stopPanel(); err != nil {
		return err
	}
	if state.Snapshot {
		if err := u.restoreSnapshot(state.DBPath); err != nil {
			return err
		}
	}
	if err := u.switchSlot(state.OldSlot); err != nil {
		return err
	}
	state.Phase = VerifyOld
	if err := u.startTrial(*state); err != nil {
		return err
	}
	state.Phase, state.Snapshot, state.Error = RolledBack, false, "新版本未能启动，已恢复原版本"
	return u.store(*state)
}

func (u *Updater) startTrial(state State) error {
	ready := make(chan error, 1)
	u.mu.Lock()
	u.ready = ready
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.ready = nil
		u.mu.Unlock()
	}()
	if err := u.store(state); err != nil {
		return err
	}
	if err := u.service("start", "nxpanel-agent"); err != nil {
		return err
	}
	if err := u.service("start", "nxpanel-api"); err != nil {
		return err
	}
	timer := time.NewTimer(trialTimeout)
	defer timer.Stop()
	select {
	case err := <-ready:
		return err
	case <-timer.C:
		return errors.New("panel readiness timed out")
	}
}

func (u *Updater) stopPanel() error {
	if err := u.service("stop", "nxpanel-api"); err != nil {
		return err
	}
	return u.service("stop", "nxpanel-agent")
}

func (u *Updater) service(action, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	var command *exec.Cmd
	switch u.Supervisor {
	case "systemd":
		command = exec.CommandContext(ctx, "/usr/bin/systemctl", action, name+".service")
	case "s6":
		switch action {
		case "stop":
			command = exec.CommandContext(ctx, "/command/s6-svc", "-wd", "-T", "45000", "-d", "/run/service/"+name)
		case "start":
			command = exec.CommandContext(ctx, "/command/s6-svc", "-wu", "-T", "45000", "-u", "/run/service/"+name)
		}
	}
	if command == nil {
		return errors.New("unsupported panel supervisor action")
	}
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", action, name, err, output)
	}
	return nil
}

func (u *Updater) snapshotPath() string { return filepath.Join(u.private(), "panel.snapshot") }

func (u *Updater) snapshot(path string) error {
	return replaceFile(u.private(), "panel.snapshot", 0600, func(file *os.File) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		cmd := u.userCommand(ctx, filepath.Join(u.Home, "bin", "nxpanel-updater"), "-db-action", "snapshot", "-db-path", path)
		cmd.Stdout, cmd.Stderr = file, os.Stderr
		return cmd.Run()
	})
}

func (u *Updater) restoreSnapshot(path string) error {
	file, err := os.Open(u.snapshotPath())
	if err != nil {
		return err
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := u.userCommand(ctx, filepath.Join(u.Home, "bin", "nxpanel-updater"), "-db-action", "restore", "-db-path", path)
	cmd.Stdin, cmd.Stderr = file, os.Stderr
	return cmd.Run()
}
