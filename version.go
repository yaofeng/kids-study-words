// Package studywords exposes build metadata for the application.
package studywords

import (
	_ "embed"
	"strings"
)

//go:embed all:.version
var versionFile string

// Version is the release number, read from .version at build time. That file
// is the only place the number is written; everything else reads this value
// or the file directly.
var Version = strings.TrimSpace(versionFile)
