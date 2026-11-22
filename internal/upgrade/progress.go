package upgrade

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/luoye663/nxpanel/internal/update"
)

type ProgressStage string

const (
	Downloading      ProgressStage = "downloading"
	Validating       ProgressStage = "validating"
	DownloadFailed   ProgressStage = "download_failed"
	Interrupted      ProgressStage = "interrupted"
	AwaitingRestart  ProgressStage = "prepared"
	Applying         ProgressStage = "applying"
	CheckingNew      ProgressStage = "trial"
	Restoring        ProgressStage = "restoring"
	CheckingPrevious ProgressStage = "verifying_previous"
	Completed        ProgressStage = "committed"
	RolledBack       ProgressStage = "rolled_back"
	RollbackFailed   ProgressStage = "blocked"
	UpdateFailed     ProgressStage = "failed"
	Cancelled        ProgressStage = "cancelled"

	progressMaxEntries = 12
	progressMaxBytes   = 4096
)

var stageOrder = map[ProgressStage]int{
	Downloading: 1, Validating: 2, DownloadFailed: 3, Interrupted: 3, AwaitingRestart: 4,
	Applying: 5, CheckingNew: 6, Restoring: 7, CheckingPrevious: 8,
	Completed: 9, RolledBack: 9, RollbackFailed: 9, UpdateFailed: 9, Cancelled: 9,
}

type ProgressEvent struct {
	Stage ProgressStage `json:"stage"`
	At    time.Time     `json:"at"`
}

type Progress struct {
	Version string          `json:"version"`
	TaskID  string          `json:"task_id,omitempty"`
	Stage   ProgressStage   `json:"stage"`
	Events  []ProgressEvent `json:"events"`
}

type progressRecord struct {
	Progress
	PreviousTaskID string `json:"previous_task_id,omitempty"`
}

type ProgressJournal struct {
	mu     sync.Mutex
	path   string
	record progressRecord
}

func NewProgressJournal(dataDir string) *ProgressJournal {
	if dataDir == "" {
		return nil
	}
	journal := &ProgressJournal{path: filepath.Join(dataDir, "panel-update-progress.json")}
	if err := journal.load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("读取面板更新进度失败", "error", err)
	}
	return journal
}

func (journal *ProgressJournal) load() error {
	fd, err := unix.Open(journal.path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), journal.path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > progressMaxBytes {
		return errors.New("invalid progress journal file")
	}
	var record progressRecord
	if err := update.Decode(io.LimitReader(file, progressMaxBytes+1), &record); err != nil {
		return err
	}
	if !validProgress(record) {
		return errors.New("invalid progress journal contents")
	}
	journal.record = record
	return nil
}

func validProgress(record progressRecord) bool {
	if !update.ValidVersion(record.Version) || len(record.TaskID) > 64 || len(record.PreviousTaskID) > 64 || len(record.Events) == 0 || len(record.Events) > progressMaxEntries || record.Events[0].Stage != Downloading || record.Events[len(record.Events)-1].Stage != record.Stage {
		return false
	}
	previous := 0
	for _, event := range record.Events {
		order := stageOrder[event.Stage]
		if order <= previous || event.At.IsZero() {
			return false
		}
		previous = order
	}
	return true
}

func (journal *ProgressJournal) Begin(version, previousTaskID string) {
	if journal == nil || !update.ValidVersion(version) {
		return
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if previousTaskID == "" {
		previousTaskID = journal.record.TaskID
	}
	journal.record = progressRecord{Progress: Progress{Version: version}, PreviousTaskID: previousTaskID}
	journal.advance(Downloading)
}

func (journal *ProgressJournal) DownloadStage(version string, stage ProgressStage) {
	if journal == nil {
		return
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.record.TaskID == "" && journal.record.Version == version && (stage == Validating || stage == DownloadFailed || stage == Interrupted) {
		journal.advance(stage)
	}
}

func (journal *ProgressJournal) Cancel(taskID string) {
	if journal == nil || taskID == "" {
		return
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.record.TaskID == taskID && journal.record.Stage == AwaitingRestart {
		journal.advance(Cancelled)
	}
}

func (journal *ProgressJournal) Recover(boot update.State) {
	if journal == nil {
		return
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	journal.observe(boot)
	if journal.record.Stage == Downloading || journal.record.Stage == Validating {
		journal.advance(Interrupted)
	}
}

func (journal *ProgressJournal) Status(state update.State) *Progress {
	if journal == nil {
		return nil
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	journal.observe(state)
	if journal.record.Stage == "" {
		return nil
	}
	progress := journal.record.Progress
	progress.Events = slices.Clone(progress.Events)
	return &progress
}

func (journal *ProgressJournal) observe(state update.State) {
	if state.ID == "" || state.Version != journal.record.Version || state.ID == journal.record.PreviousTaskID {
		return
	}
	stage := ProgressStage(state.Phase)
	switch stage {
	case AwaitingRestart, Applying, CheckingNew, Restoring, CheckingPrevious, Completed, RolledBack, RollbackFailed, UpdateFailed:
	default:
		return
	}
	if journal.record.TaskID == "" {
		journal.record.TaskID = state.ID
	}
	if journal.record.TaskID == state.ID {
		journal.advance(stage)
	}
}

func (journal *ProgressJournal) advance(stage ProgressStage) {
	if stageOrder[stage] <= stageOrder[journal.record.Stage] || len(journal.record.Events) >= progressMaxEntries {
		return
	}
	at := time.Now().UTC()
	if events := journal.record.Events; len(events) > 0 && at.Before(events[len(events)-1].At) {
		at = events[len(events)-1].At
	}
	journal.record.Stage = stage
	journal.record.Events = append(journal.record.Events, ProgressEvent{Stage: stage, At: at})
	if err := journal.save(); err != nil {
		slog.Warn("保存面板更新进度失败", "error", err)
	}
}

func (journal *ProgressJournal) save() error {
	data, err := json.Marshal(journal.record)
	if err != nil {
		return err
	}
	dir := filepath.Dir(journal.path)
	file, err := os.CreateTemp(dir, ".panel-update-progress-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(data)
	if err := errors.Join(err, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), journal.path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
