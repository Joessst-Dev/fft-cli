// Package buildinfo carries version metadata stamped into the binary at link time.
package buildinfo

import "runtime/debug"

// Values injected via -ldflags by GoReleaser. Defaults apply to `go build` and
// `go run`, which do not set them.
var (
	// Version is the released semver tag, or "dev" for an unreleased build.
	Version = "dev"
	// Commit is the git SHA the binary was built from.
	Commit = "none"
	// Date is the RFC3339 build timestamp.
	Date = "unknown"
)

// IsRelease reports whether this binary came from a tagged release. The update
// check and its notice are suppressed when it does not.
func IsRelease() bool { return Version != "dev" }

// Info is the machine-readable form of the build metadata, used by `fft version --output json`.
type Info struct {
	Version   string `json:"version" yaml:"version"`
	Commit    string `json:"commit" yaml:"commit"`
	Date      string `json:"date" yaml:"date"`
	GoVersion string `json:"goVersion" yaml:"goVersion"`
}

// Current returns the build metadata for this binary.
func Current() Info {
	info := Info{Version: Version, Commit: Commit, Date: Date, GoVersion: "unknown"}
	if bi, ok := debug.ReadBuildInfo(); ok {
		info.GoVersion = bi.GoVersion
	}
	return info
}
