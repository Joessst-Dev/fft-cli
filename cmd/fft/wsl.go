package main

import (
	"os"
	"runtime"
	"strings"
)

// osReleasePath is what the kernel calls itself. Under WSL it names Microsoft;
// under any other Linux it does not.
const osReleasePath = "/proc/sys/kernel/osrelease"

// runningOnWSL reports whether fft is running inside a Windows Subsystem for
// Linux distribution.
//
// It is consulted only when composing the "no keychain here" advice, because that
// is the one place the answer changes what a user should do — WSL ships no Secret
// Service, so the Linux default is wrong there and says so. It never runs on the
// path of a command that works.
func runningOnWSL() bool { return onWSL(runtime.GOOS, os.LookupEnv, os.ReadFile) }

// onWSL is [runningOnWSL] with its three sources of truth passed in, so that the
// detection can be exercised from any of the three platforms CI runs on.
func onWSL(goos string, lookupEnv func(string) (string, bool), readFile func(string) ([]byte, error)) bool {
	// WSL is Linux. Leaving early also means no /proc syscall on macOS or Windows,
	// and that a WSL_DISTRO_NAME left in a developer's environment cannot make a
	// Mac claim to be WSL.
	if goos != "linux" {
		return false
	}

	for _, name := range []string{"WSL_DISTRO_NAME", "WSL_INTEROP"} {
		if v, ok := lookupEnv(name); ok && v != "" {
			return true
		}
	}

	// sudo and `env -i` strip the WSL_* variables; the kernel's own name survives
	// both. WSL1 reports "…-Microsoft" and WSL2 "…-microsoft-standard-WSL2" —
	// neither has a Secret Service and both want the same advice, so they are not
	// told apart here.
	//
	// "wsl" as well as "microsoft" because a hand-built WSL2 kernel carries
	// whatever CONFIG_LOCALVERSION it was given, and the Microsoft branding is the
	// first thing to go. Being wrong either way costs one sentence of advice, so
	// the broader match is the cheaper mistake.
	data, err := readFile(osReleasePath)
	if err != nil {
		return false
	}
	release := strings.ToLower(string(data))
	return strings.Contains(release, "microsoft") || strings.Contains(release, "wsl")
}
