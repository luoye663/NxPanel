package upgrade

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/luoye663/nxpanel/internal/update"
)

func expectStages(t *testing.T, progress *Progress, want ...ProgressStage) {
	t.Helper()
	if progress == nil {
		t.Fatal("missing update progress")
	}
	stages := make([]ProgressStage, 0, len(progress.Events))
	for index, event := range progress.Events {
		stages = append(stages, event.Stage)
		if event.At.IsZero() || index > 0 && event.At.Before(progress.Events[index-1].At) {
			t.Fatalf("invalid event time: %+v", progress.Events)
		}
	}
	if !slices.Equal(stages, want) || progress.Stage != want[len(want)-1] {
		t.Fatalf("stages %v (%s), want %v", stages, progress.Stage, want)
	}
}

func TestProgressJournalSurvivesRestartAndRollback(t *testing.T) {
	dir := t.TempDir()
	journal := NewProgressJournal(dir)
	state := update.State{ID: "new-job", Version: "v1.2.3", Phase: update.Prepared}
	journal.Begin(state.Version, "previous-job")
	journal.DownloadStage(state.Version, Validating)
	journal.Status(state)

	journal = NewProgressJournal(dir)
	journal.Recover(state)
	expectStages(t, journal.Status(state), Downloading, Validating, AwaitingRestart)
	state.Phase = update.Applying
	journal.Status(state)

	journal = NewProgressJournal(dir)
	state.Phase = update.Trial
	journal.Recover(state)
	journal = NewProgressJournal(dir)
	state.Phase = update.VerifyOld
	journal.Recover(state)
	state.Phase = update.RolledBack
	journal.Status(state)

	journal = NewProgressJournal(dir)
	state.Phase = update.Prepared
	progress := journal.Status(state)
	expectStages(t, progress, Downloading, Validating, AwaitingRestart, Applying, CheckingNew, CheckingPrevious, RolledBack)
	if progress.TaskID != state.ID || progress.Version != state.Version {
		t.Fatalf("unexpected task: %+v", progress)
	}
	progress.Events[0].Stage = Completed
	if journal.Status(state).Events[0].Stage != Downloading {
		t.Fatal("caller could mutate journal events")
	}
}

func TestProgressJournalRetriesAndCancels(t *testing.T) {
	dir := t.TempDir()
	journal := NewProgressJournal(dir)
	old := update.State{ID: "old", Version: "v1.2.3", Phase: update.RolledBack}
	journal.Begin(old.Version, old.ID)
	journal.DownloadStage(old.Version, DownloadFailed)
	expectStages(t, NewProgressJournal(dir).Status(old), Downloading, DownloadFailed)

	journal.Begin(old.Version, old.ID)
	expectStages(t, journal.Status(old), Downloading)
	journal = NewProgressJournal(dir)
	journal.Recover(old)
	expectStages(t, journal.Status(old), Downloading, Interrupted)
	if journal.Status(old).TaskID != "" {
		t.Fatal("retry bound to the previous update")
	}

	journal.Begin(old.Version, old.ID)
	journal.DownloadStage(old.Version, Validating)
	prepared := update.State{ID: "new", Version: old.Version, Phase: update.Prepared}
	journal = NewProgressJournal(dir)
	journal.Recover(prepared)
	journal.Cancel(prepared.ID)
	journal = NewProgressJournal(dir)
	expectStages(t, journal.Status(prepared), Downloading, Validating, AwaitingRestart, Cancelled)
	journal.Begin(prepared.Version, "")
	expectStages(t, journal.Status(prepared), Downloading)
}

func TestProgressJournalDoesNotInventLegacyHistory(t *testing.T) {
	dir := t.TempDir()
	state := update.State{ID: "legacy", Version: "v1.2.3", Phase: update.Committed}
	journal := NewProgressJournal(dir)
	journal.Recover(state)
	if journal.Status(state) != nil {
		t.Fatal("legacy task acquired invented progress")
	}
	if err := os.WriteFile(filepath.Join(dir, "panel-update-progress.json"), make([]byte, progressMaxBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if NewProgressJournal(dir).Status(state) != nil {
		t.Fatal("accepted oversized progress history")
	}
}
