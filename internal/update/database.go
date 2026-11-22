package update

import (
	"database/sql"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func validDatabasePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && filepath.Base(path) == "panel.db"
}

func DatabaseAction(action, path string, input io.Reader, output io.Writer) error {
	if os.Geteuid() == 0 || !validDatabasePath(path) {
		return errors.New("database worker requires an unprivileged user and an absolute panel database path")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("panel database is not a regular file")
	}
	switch action {
	case "snapshot":
		return snapshotDatabase(path, output)
	case "restore":
		dir := filepath.Dir(path)
		return replaceFile(dir, filepath.Base(path), info.Mode().Perm()&0600, func(file *os.File) error {
			if _, err := io.Copy(file, input); err != nil {
				return err
			}
			for _, suffix := range []string{"-wal", "-shm", "-journal"} {
				if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
			return nil
		})
	default:
		return errors.New("unknown database action")
	}
}

func snapshotDatabase(path string, output io.Writer) error {
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw&_pragma=busy_timeout(5000)"}
	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return err
	}
	var busy, pages, checkpointed int
	err = database.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &pages, &checkpointed)
	if err := errors.Join(err, database.Close()); err != nil {
		return err
	}
	if busy != 0 {
		return errors.New("SQLite checkpoint did not complete")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(output, file)
	return err
}
