package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/mod/semver"
	"golang.org/x/sys/unix"
)

type Updater struct {
	Home, Supervisor string
	UID, GID         int
	keys             map[string]ed25519.PublicKey
	mu               sync.Mutex
	work             sync.Mutex
	state            State
	ready            chan error
}

type peerKey struct{}

func (u *Updater) Serve() error {
	if err := checkInstallHome(u.Home); err != nil {
		return err
	}
	if err := checkInstallHome(filepath.Join(u.Home, "bin")); err != nil {
		return err
	}
	for _, directory := range []struct {
		name string
		mode os.FileMode
	}{{"versions", 0755}, {"update-control", 0755}, {"update-control/private", 0700}} {
		path := filepath.Join(u.Home, directory.name)
		if err := os.Mkdir(path, directory.mode); err != nil && !os.IsExist(err) {
			return err
		}
		if err := checkInstallHome(path); err != nil {
			return err
		}
		if err := os.Chmod(path, directory.mode); err != nil {
			return err
		}
	}
	lock, err := os.OpenFile(filepath.Join(u.private(), "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return err
	}
	if u.state, err = ReadState(u.Home); err != nil {
		return err
	}
	entries, err := os.ReadDir(u.private())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "archive-") || strings.HasPrefix(entry.Name(), ".update-") {
			_ = os.Remove(filepath.Join(u.private(), entry.Name()))
		}
	}
	if u.keys, err = Keys(ActiveKeys()); err != nil {
		slog.Error("invalid update trust configuration", "error", err)
	}
	socket := filepath.Join(controlDir(u.Home), "updater.sock")
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chown(socket, 0, u.GID); err != nil {
		return err
	}
	if err := os.Chmod(socket, 0660); err != nil {
		return err
	}
	if u.state.Busy() && u.state.Phase != Blocked {
		state := u.state
		u.work.Lock()
		go func() {
			defer u.work.Unlock()
			u.rollback(state, errors.New("activation interrupted"))
		}()
	} else if !u.state.Busy() {
		u.cleanupSlots()
	}
	server := &http.Server{Handler: http.HandlerFunc(u.handle), ConnContext: peerContext, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute}
	return server.Serve(listener)
}

func peerContext(ctx context.Context, conn net.Conn) context.Context {
	if socket, ok := conn.(*net.UnixConn); ok {
		if raw, err := socket.SyscallConn(); err == nil {
			var peer *unix.Ucred
			if err := raw.Control(func(fd uintptr) { peer, _ = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err == nil && peer != nil {
				return context.WithValue(ctx, peerKey{}, peer)
			}
		}
	}
	return ctx
}

func checkInstallHome(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("update path must be absolute and canonical")
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode().Perm()&0022 != 0 || info.Sys().(*syscall.Stat_t).Uid != 0 {
			return fmt.Errorf("update path is not root-controlled: %s", current)
		}
		if current == "/" {
			return nil
		}
	}
}

func (u *Updater) private() string { return filepath.Join(controlDir(u.Home), "private") }

func (u *Updater) status() State {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state
}

func (u *Updater) store(state State) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	dir := controlDir(u.Home)
	if err := replaceFile(dir, "state.json", 0644, func(file *os.File) error {
		_, err := file.Write(data)
		return err
	}); err != nil {
		return err
	}
	u.state = state
	return nil
}

func replaceFile(dir, name string, mode os.FileMode, write func(*os.File) error) error {
	file, err := os.CreateTemp(dir, ".update-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := errors.Join(write(file), file.Chmod(mode), file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), filepath.Join(dir, name)); err != nil {
		return err
	}
	return syncDir(dir)
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (u *Updater) handle(w http.ResponseWriter, r *http.Request) {
	peer, ok := r.Context().Value(peerKey{}).(*unix.Ucred)
	if !ok || peer.Uid != uint32(u.UID) && !(peer.Uid == 0 && r.Method == http.MethodGet && r.URL.Path == "/status") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	status, err := u.dispatch(w, r, peer)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status.Error = err.Error()
		w.WriteHeader(http.StatusConflict)
	}
	_ = json.NewEncoder(w).Encode(status)
}

func (u *Updater) dispatch(w http.ResponseWriter, r *http.Request, peer *unix.Ucred) (State, error) {
	state := u.status()
	if r.Method == http.MethodGet && r.URL.Path == "/status" {
		return state, nil
	}
	if r.Method != http.MethodPost {
		return state, errors.New("unsupported updater request")
	}
	var action Action
	if r.URL.Path != "/stage" {
		if err := Decode(http.MaxBytesReader(w, r.Body, 8<<10), &action); err != nil {
			return state, errors.New("invalid updater request")
		}
	}
	if r.URL.Path == "/ready" || r.URL.Path == "/reject" {
		u.mu.Lock()
		defer u.mu.Unlock()
		state = u.state
		if state.TrialVersion() == "" || action.ID != state.ID || action.Version != state.TrialVersion() || action.DBPath != state.DBPath || !u.isCurrentAPI(peer.Pid) || u.ready == nil {
			return state, errors.New("unexpected update readiness report")
		}
		var result error
		if r.URL.Path == "/reject" {
			result = errors.New("panel rejected its startup trial")
		}
		select {
		case u.ready <- result:
		default:
		}
		return state, nil
	}
	if !u.work.TryLock() {
		return state, errors.New("another update operation is in progress")
	}
	state = u.status()
	if r.URL.Path == "/activate" && state.Phase == Prepared && state.ID == action.ID && u.isCurrentAPI(peer.Pid) && validDatabasePath(action.DBPath) {
		current, err := os.Readlink(filepath.Join(u.Home, "current"))
		if err != nil || current != state.OldSlot {
			u.work.Unlock()
			return state, errors.New("installed slot changed after staging")
		}
		state.Phase, state.DBPath = Applying, action.DBPath
		if err := u.store(state); err != nil {
			u.work.Unlock()
			return u.status(), err
		}
		go func() {
			defer u.work.Unlock()
			u.activate(state)
		}()
		return state, nil
	}
	defer u.work.Unlock()
	switch r.URL.Path {
	case "/stage":
		if state.Busy() || state.Phase == Prepared {
			return state, errors.New("cancel or finish the current update first")
		}
		return u.stage(r.Context(), http.MaxBytesReader(w, r.Body, PackageMax+2*MetadataMax+8))
	case "/cancel":
		if state.Phase == Prepared && state.ID == action.ID {
			if err := u.store(State{Phase: Idle}); err != nil {
				return state, err
			}
			u.cleanupSlots()
			return u.status(), nil
		}
	}
	return state, errors.New("update task changed or is not ready")
}

func (u *Updater) isCurrentAPI(pid int32) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var command *exec.Cmd
	if u.Supervisor == "systemd" {
		command = exec.CommandContext(ctx, "/usr/bin/systemctl", "show", "--property=MainPID", "nxpanel-api.service")
	} else {
		command = exec.CommandContext(ctx, "/command/s6-svstat", "-o", "pid", "/run/service/nxpanel-api")
	}
	output, err := command.Output()
	actual, parseErr := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(string(output)), "MainPID="), 10, 32)
	return err == nil && parseErr == nil && pid > 0 && actual == int64(pid)
}

func (u *Updater) stage(ctx context.Context, reader io.Reader) (State, error) {
	oldSlot, err := os.Readlink(filepath.Join(u.Home, "current"))
	if err != nil || !validSlot(oldSlot) {
		return u.status(), errors.New("unsupported installation layout")
	}
	previous, err := u.version(ctx, filepath.Join(u.Home, oldSlot, "nxpanel-api"))
	if err != nil || !ValidVersion(previous) {
		return u.status(), errors.New("installed version cannot be upgraded")
	}
	raw, signature, err := decodeStage(reader)
	if err != nil {
		return u.status(), err
	}
	manifest, asset, err := Verify(raw, signature, u.keys)
	if err != nil {
		return u.status(), err
	}
	if semver.Compare(manifest.Version, previous) <= 0 {
		return u.status(), errors.New("update is not newer than installed version")
	}
	if err := availableSpace(u.Home, asset.Size+2*BinaryMax); err != nil {
		return u.status(), err
	}
	id := hex.EncodeToString(randBytes())
	archive, err := os.CreateTemp(u.private(), "archive-*")
	if err != nil {
		return u.status(), err
	}
	defer archive.Close()
	defer os.Remove(archive.Name())
	if err := CopyPackage(archive, reader, asset); err != nil {
		return u.status(), err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return u.status(), err
	}
	dir := filepath.Join(u.Home, "versions", id)
	if err := os.Mkdir(dir, 0755); err != nil {
		return u.status(), err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	if err := unpack(archive, dir); err != nil {
		return u.status(), err
	}
	for _, name := range []string{"nxpanel-api", "nxpanel-agent"} {
		version, err := u.version(ctx, filepath.Join(dir, name))
		if err != nil || version != manifest.Version {
			return u.status(), errors.New("update binary version or architecture mismatch")
		}
	}
	if err := errors.Join(syncDir(dir), syncDir(filepath.Dir(dir))); err != nil {
		return u.status(), err
	}
	state := State{ID: id, Phase: Prepared, Version: manifest.Version, Previous: previous, OldSlot: oldSlot}
	if err := u.store(state); err != nil {
		return u.status(), err
	}
	keep = true
	u.cleanupSlots()
	return state, nil
}

func randBytes() []byte {
	data := make([]byte, 16)
	_, _ = rand.Read(data)
	return data
}

func availableSpace(path string, bytes int64) error {
	var stats unix.Statfs_t
	if err := unix.Statfs(path, &stats); err != nil {
		return err
	}
	if stats.Bsize <= 0 || stats.Bavail < uint64((bytes+stats.Bsize-1)/stats.Bsize) {
		return errors.New("not enough disk space for update")
	}
	return nil
}

func validSlot(slot string) bool {
	if slot == "bin" {
		return true
	}
	id, ok := strings.CutPrefix(slot, "versions/")
	_, err := hex.DecodeString(id)
	return ok && len(id) == 32 && err == nil
}

func (u *Updater) switchSlot(slot string) error {
	if !validSlot(slot) {
		return errors.New("invalid program slot")
	}
	link := filepath.Join(u.Home, ".current-"+hex.EncodeToString(randBytes()))
	if err := os.Symlink(slot, link); err != nil {
		return err
	}
	defer os.Remove(link)
	if err := os.Rename(link, filepath.Join(u.Home, "current")); err != nil {
		return err
	}
	return syncDir(u.Home)
}

func (u *Updater) cleanupSlots() {
	state := u.status()
	current, err := os.Readlink(filepath.Join(u.Home, "current"))
	if err != nil || !validSlot(current) {
		return
	}
	dir := filepath.Join(u.Home, "versions")
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		slot := "versions/" + entry.Name()
		if validSlot(slot) && slot != current && slot != state.OldSlot && entry.Name() != state.ID {
			_ = os.RemoveAll(filepath.Join(dir, entry.Name()))
		}
	}
}

func (u *Updater) userCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(u.UID), Gid: uint32(u.GID)}, Pdeathsig: syscall.SIGKILL}
	cmd.Env = os.Environ()
	return cmd
}

func (u *Updater) version(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := u.userCommand(ctx, path, "--version").Output()
	return strings.TrimSpace(string(out)), err
}
