//go:build !embed_frontend

package main

import "io/fs"

var buildFS fs.FS
var indexPage []byte
