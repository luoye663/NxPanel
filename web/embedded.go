//go:build embed_web

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

func StaticFS() (fs.FS, error) { return fs.Sub(files, "dist") }
