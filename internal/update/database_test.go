package update

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotIncludesWAL(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "panel.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec("PRAGMA journal_mode=WAL; CREATE TABLE sample (value TEXT); INSERT INTO sample VALUES ('in-wal');"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + "-wal"); err != nil {
		t.Fatalf("expected a WAL file before checkpoint: %v", err)
	}
	var saved bytes.Buffer
	if err := snapshotDatabase(path, &saved); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(directory, "copy.db")
	if err := os.WriteFile(copyPath, saved.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := sql.Open("sqlite", copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var value string
	if err := restored.QueryRow("SELECT value FROM sample").Scan(&value); err != nil || value != "in-wal" {
		t.Fatalf("committed row missing from standalone snapshot: %q, %v", value, err)
	}
}
