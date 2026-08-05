package main

import (
	"io/fs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("detecting WSL", func() {
	// env and proc stand in for the two things onWSL reads, so that every case
	// below runs identically on all three platforms CI builds for.
	env := func(pairs ...string) func(string) (string, bool) {
		values := map[string]string{}
		for i := 0; i+1 < len(pairs); i += 2 {
			values[pairs[i]] = pairs[i+1]
		}
		return func(name string) (string, bool) {
			v, ok := values[name]
			return v, ok
		}
	}
	proc := func(release string) func(string) ([]byte, error) {
		return func(path string) ([]byte, error) {
			if path != osReleasePath || release == "" {
				return nil, fs.ErrNotExist
			}
			return []byte(release), nil
		}
	}

	DescribeTable("this is WSL",
		func(lookupEnv func(string) (string, bool), readFile func(string) ([]byte, error)) {
			Expect(onWSL("linux", lookupEnv, readFile)).To(BeTrue())
		},
		Entry("the distribution names itself",
			env("WSL_DISTRO_NAME", "Ubuntu-24.04"), proc("")),
		Entry("only the interop socket is left",
			env("WSL_INTEROP", "/run/WSL/8_interop"), proc("")),
		// What survives `sudo`, which strips the WSL_* variables.
		Entry("nothing but the kernel's own name, WSL2",
			env(), proc("5.15.167.4-microsoft-standard-WSL2\n")),
		Entry("nothing but the kernel's own name, WSL1",
			env(), proc("4.4.0-19041-Microsoft\n")),
		// A hand-built WSL2 kernel keeps whatever CONFIG_LOCALVERSION it was given,
		// and the Microsoft branding is the first thing to go.
		Entry("a custom kernel that kept only the wsl in its name",
			env(), proc("6.6.36-wsl2-custom\n")),
	)

	DescribeTable("this is not WSL",
		func(goos string, lookupEnv func(string) (string, bool), readFile func(string) ([]byte, error)) {
			Expect(onWSL(goos, lookupEnv, readFile)).To(BeFalse())
		},
		Entry("an ordinary Linux box", "linux", env(), proc("6.8.0-51-generic\n")),
		Entry("no /proc at all", "linux", env(), proc("")),
		Entry("an empty variable is not an answer", "linux", env("WSL_DISTRO_NAME", ""), proc("")),
		// A developer's shell can carry WSL_DISTRO_NAME anywhere, including into an
		// ssh session from WSL to a Mac. WSL is Linux; nothing else can be it.
		Entry("a Mac that inherited the variable", "darwin", env("WSL_DISTRO_NAME", "Ubuntu"), proc("")),
		Entry("Windows itself", "windows", env("WSL_DISTRO_NAME", "Ubuntu"), proc("")),
	)
})
