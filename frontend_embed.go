//go:build embed_frontend

package main

import (
	"embed"
	"io/fs"
)

//go:embed web/dist
var embeddedBuildFS embed.FS

//go:embed web/dist/index.html
var indexPage []byte

var buildFS fs.FS = embeddedBuildFS
