// Package webui embeds the compiled admin dashboard so the gateway ships as a
// single self-contained binary. The dist directory is produced by the frontend
// build (see web/) and copied here by `make web`.
package webui

import "embed"

// Assets holds the built single-page application.
//
//go:embed all:dist
var Assets embed.FS
