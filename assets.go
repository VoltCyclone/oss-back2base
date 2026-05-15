package main

import (
	"embed"
	"io/fs"
)

// all: ensures files with leading "." or "_" under the tree are embedded too.
//go:embed all:back2base-container
var embeddedAssets embed.FS

// shipFS returns the embedded FS rooted inside back2base-container/ so callers
// see the same paths they saw pre-reorg (e.g. "Dockerfile",
// "defaults/env.example"). The back2base-container/ prefix is an internal
// layout detail that stops at this function.
func shipFS() fs.FS {
	sub, err := fs.Sub(embeddedAssets, "back2base-container")
	if err != nil {
		panic(err)
	}
	return sub
}
