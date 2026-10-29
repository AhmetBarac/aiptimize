// Package webui embeds the built Vue single-page application.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the embedded SPA files rooted at dist/.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		// fs.Sub fails only for an invalid path, which is a programming error.
		panic(err)
	}
	return sub
}
