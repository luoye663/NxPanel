package main

import (
	"flag"
	"log/slog"
	"os"
	"os/user"
	"strconv"

	"github.com/luoye663/nxpanel/internal/update"
)

func main() {
	home := flag.String("home", "/usr/local/nxpanel", "root-controlled installation directory")
	account := flag.String("api-user", "openrest", "unprivileged panel account")
	supervisor := flag.String("supervisor", "systemd", "systemd or s6")
	dbAction := flag.String("db-action", "", "internal unprivileged SQLite worker action")
	dbPath := flag.String("db-path", "", "internal SQLite worker path")
	flag.Parse()
	if *dbAction != "" {
		if err := update.DatabaseAction(*dbAction, *dbPath, os.Stdin, os.Stdout); err != nil {
			slog.Error("update database worker failed", "error", err)
			os.Exit(1)
		}
		return
	}
	panel, err := user.Lookup(*account)
	if err != nil {
		slog.Error("panel user not found", "error", err)
		os.Exit(1)
	}
	uid, uidErr := strconv.Atoi(panel.Uid)
	gid, gidErr := strconv.Atoi(panel.Gid)
	if os.Geteuid() != 0 || uidErr != nil || gidErr != nil || uid <= 0 || gid <= 0 || (*supervisor != "systemd" && *supervisor != "s6") {
		slog.Error("invalid updater privileges or supervisor")
		os.Exit(1)
	}
	updater := update.Updater{Home: *home, UID: uid, GID: gid, Supervisor: *supervisor}
	if err := updater.Serve(); err != nil {
		slog.Error("updater stopped", "error", err)
		os.Exit(1)
	}
}
