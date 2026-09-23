package main

import (
	"github.com/Joessst-Dev/fft-cli/internal/history"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

// cliHistory is the TUI's [tui.History]: the file the runs record to, and the
// reason they record nothing, decided exactly as a run decides it.
type cliHistory struct {
	runner *cliRunner
}

var _ tui.History = cliHistory{}

// History is the request history the runner's runs record to.
func (r *cliRunner) History() tui.History {
	return cliHistory{runner: r}
}

// Read implements [tui.History].
//
// It takes none of the runner's locks. The file is appended to one whole line at a
// time and rewritten by rename, so a read never sees half of a change; and a
// history clear waiting on its question holds the config lock, which a read that
// waited on it would hold up the screen for.
func (h cliHistory) Read() ([]history.Entry, string, error) {
	// A Deps of its own, as every run has: deciding whether history is off reads
	// the config file, and keeps what it read on the Deps it was asked on.
	//
	// Cut from the session as it is *now*, not as it was when the runner was built.
	// A session pointed at the emulator records nothing — its runs act on the
	// environment's project — and the screen has to say that rather than report the
	// configured project's setting.
	d := h.runner.deps.forRun(nil, h.runner.sessionRun())
	off := d.historyOff()
	log, err := d.historyLog()
	if err != nil {
		return nil, off, err
	}
	entries, err := log.Read()
	return entries, off, err
}
