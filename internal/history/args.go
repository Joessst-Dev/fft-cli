package history

import (
	"slices"
	"strings"
)

// separator ends a command line's flags: whatever follows it is an argument, even
// when it starts with a dash.
const separator = "--"

// Args returns the arguments an entry records for a command line, after [Redact]:
// its positional arguments, then its flags, each written as --name=value or, for a
// flag given without one, a bare --name.
//
// An argument that starts with a dash would read back as a flag there, so when one
// does, the flags come first and the arguments after a "--" separator: the order a
// shell needs them in too, which keeps `fft history list` a command line that can
// be run again. Read the result back with [SplitArgs].
func Args(positional, flags []string) []string {
	args := make([]string, 0, len(positional)+len(flags)+1)
	if slices.ContainsFunc(positional, looksLikeFlag) {
		args = append(args, flags...)
		args = append(args, separator)
		args = append(args, positional...)
	} else {
		args = append(args, positional...)
		args = append(args, flags...)
	}
	return Redact(args)
}

// SplitArgs returns the positional arguments and the flags of args as [Args]
// recorded them. An entry recorded before the separator was has its arguments
// first, and its flags from the first one that starts with "--"; neither layout
// can be mistaken for the other, since only the separator is ever a bare "--".
//
// Both results are copies.
func SplitArgs(args []string) (positional, flags []string) {
	if i := slices.Index(args, separator); i >= 0 {
		return slices.Clone(args[i+1:]), slices.Clone(args[:i])
	}
	i := slices.IndexFunc(args, func(arg string) bool { return strings.HasPrefix(arg, separator) })
	if i < 0 {
		i = len(args)
	}
	return slices.Clone(args[:i]), slices.Clone(args[i:])
}

// looksLikeFlag reports whether a shell would take arg for a flag. A lone "-" is
// the conventional name of stdin, and an argument like any other.
func looksLikeFlag(arg string) bool {
	return strings.HasPrefix(arg, "-") && arg != "-"
}
