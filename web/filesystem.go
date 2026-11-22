//go:build !embed_web

package web

import (
	"github.com/luoye663/nxpanel/internal/app"
	"io/fs"
	"os"
)

func StaticFS() (fs.FS, error) {
	dir, err := app.FindWebDir()
	if err != nil {
		return nil, err
	}
	return os.DirFS(dir), nil
}
