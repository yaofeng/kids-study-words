// Package assets embeds the HTML templates and static files served by the
// application, so the binary runs offline without a CDN.
package assets

import "embed"

// Templates holds web/templates/*.html.
//
//go:embed templates
var Templates embed.FS

// Static holds web/static/* (CSS, JS, fonts).
//
//go:embed static
var Static embed.FS
