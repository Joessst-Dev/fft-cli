package main

import (
	"path/filepath"
	"testing"

	"github.com/Joessst-Dev/fft-cli/internal/component"
)

// BenchmarkNewRootCmd measures building the whole command tree, generated
// commands and all. The TUI builds one per request it sends, so this is the
// fixed cost every one of them pays before a byte goes over the wire.
func BenchmarkNewRootCmd(b *testing.B) {
	// An empty component root, so the number measures fft and not whatever the
	// machine running it happens to have installed.
	components := component.Open(filepath.Join(b.TempDir(), "components"))

	b.ReportAllocs()
	for b.Loop() {
		newRootCmd(&Deps{Components: components})
	}
}
