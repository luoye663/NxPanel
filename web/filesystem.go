package web

import (
	"github.com/luoye663/nxpanel/internal/app"
)

func StaticFS() (string, error) {
	return app.FindWebDir()
}