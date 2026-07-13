package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// versionFlag is --if-version: the escape hatch that lets a caller who already
// knows an entity's version skip the read fft would otherwise do to learn it.
//
// It has to tell "not given" from "given as 0", and 0 is a real version — the
// one a freshly created entity has. So the flag's value is a *int, and its
// absence is read off pflag's Changed rather than guessed from a sentinel.
//
// It is --if-version and never --version: cobra installs --version on the root
// command, and a subcommand-local --version int would read as "print the
// version" to every user and every script that ever saw another CLI.
type versionFlag struct {
	n  int
	fs *pflag.FlagSet
}

func (v *versionFlag) register(fs *pflag.FlagSet) {
	v.fs = fs
	fs.IntVar(&v.n, "if-version", 0,
		"Send this version instead of reading the current one (fails with 409 if it is stale)")
}

// value is the version the user named, or nil if they named none.
func (v *versionFlag) value() *int {
	if v.fs == nil || !v.fs.Changed("if-version") {
		return nil
	}
	return &v.n
}

// check refuses a negative version before it becomes an opaque 400.
func (v *versionFlag) check() error {
	if v.value() != nil && v.n < 0 {
		return exitcode.UsageError{Err: fmt.Errorf("--if-version cannot be negative, and %d is", v.n)}
	}
	return nil
}

// requireFlag turns a missing mandatory flag into a usage error (exit 2) that
// names it, rather than into a request the API rejects for reasons of its own.
func requireFlag(cmd *cobra.Command, name string) error {
	if !cmd.Flags().Changed(name) {
		return exitcode.UsageError{Err: fmt.Errorf("--%s is required", name)}
	}
	return nil
}
