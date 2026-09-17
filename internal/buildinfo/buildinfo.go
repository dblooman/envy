// Package buildinfo exposes release metadata injected by the build pipeline.
package buildinfo

// Version and Commit are deliberately variables so release builds can set them
// with -ldflags. Source builds remain reproducible and identifiable.
var (
	Version = "dev"
	Commit  = "unknown"
)
