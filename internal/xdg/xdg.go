// Package xdg resolves the files fft keeps on disk to the XDG base directory
// they belong in: $XDG_<KIND>_HOME/fft/<name>, else the conventional directory
// under the user's home.
//
// Which base a file belongs to is a decision about the file, not about this
// package: the config is something the user edits, the update cache is
// something fft can rebuild by asking again, credentials and history are state,
// and templates and components are data.
package xdg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Base is one XDG base directory: the variable that overrides it and the
// directory under the home it falls back to.
//
// The bases fft uses are handed out by functions rather than kept in exported
// variables, so that no importer can reassign one and move, say, the credentials
// file for the whole process.
type Base struct {
	env  string
	home []string
}

// Config is $XDG_CONFIG_HOME, else ~/.config.
func Config() Base { return Base{env: "XDG_CONFIG_HOME", home: []string{".config"}} }

// State is $XDG_STATE_HOME, else ~/.local/state.
func State() Base { return Base{env: "XDG_STATE_HOME", home: []string{".local", "state"}} }

// Data is $XDG_DATA_HOME, else ~/.local/share.
func Data() Base { return Base{env: "XDG_DATA_HOME", home: []string{".local", "share"}} }

// Cache is $XDG_CACHE_HOME, else ~/.cache.
func Cache() Base { return Base{env: "XDG_CACHE_HOME", home: []string{".cache"}} }

// Path is name inside fft's directory under b, reading b's variable from the
// environment. A variable set to the empty string counts as unset.
func (b Base) Path(name string) (string, error) {
	if dir := os.Getenv(b.env); dir != "" {
		return filepath.Join(dir, "fft", name), nil
	}
	return b.homePath(name)
}

// LookupPath is [Base.Path] reading b's variable through lookup, os.LookupEnv
// when nil, and trimming it first: a variable holding only whitespace counts
// as unset.
func (b Base) LookupPath(lookup func(string) (string, bool), name string) (string, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	dir, _ := lookup(b.env)
	if dir = strings.TrimSpace(dir); dir != "" {
		return filepath.Join(dir, "fft", name), nil
	}
	return b.homePath(name)
}

func (b Base) homePath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate the home directory: %w", err)
	}
	return filepath.Join(home, filepath.Join(b.home...), "fft", name), nil
}
