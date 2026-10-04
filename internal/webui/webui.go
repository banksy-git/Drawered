// Package webui embeds the built Svelte frontend.
package webui

import (
    "embed"
    "io/fs"
)

// dist is populated by `npm run build` in web/, which writes here.
//
//go:embed all:dist
var dist embed.FS

// FS returns the built frontend rooted at its index.html.
func FS() fs.FS {
    sub, err := fs.Sub(dist, "dist")
    if err != nil {
        panic(err)
    }
    return sub
}
