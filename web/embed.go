// Package web embeds the HTML templates and static assets so both binaries
// are fully self-contained. Nothing is loaded from a CDN or the network.
package web

import "embed"

// FS contains the templates and static files.
//
//go:embed templates/*.html static
var FS embed.FS
