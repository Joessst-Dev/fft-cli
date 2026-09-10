package update

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These specs drive detectMethod rather than InstallMethod, which is the whole
// reason detectMethod takes its GOOS and its environment as arguments: the
// Windows rules are the ones nobody who edits this file will ever run, and here
// they are proved on every platform CI builds on, not only on the Windows leg.
var _ = Describe("detectMethod", func() {
	// env turns a spec's map into the lookup detectMethod wants. Passing it in
	// rather than calling os.Setenv keeps the table parallel-safe and free of
	// cleanup.
	env := func(kv map[string]string) func(string) string {
		return func(k string) string { return kv[k] }
	}

	DescribeTable("what the executable's location says about the install",
		func(exe, goos string, kv map[string]string, expected Method) {
			Expect(detectMethod(exe, goos, env(kv))).To(Equal(expected))
		},

		// Scoop. The shim and the real binary are different executables in
		// different directories, and os.Executable can report either.
		Entry("scoop, the app directory os.Executable normally reports",
			`C:\Users\jo\scoop\apps\fft\current\fft.exe`, "windows", nil, MethodScoop),
		Entry("scoop, the shim that launched it",
			`C:\Users\jo\scoop\shims\fft.exe`, "windows", nil, MethodScoop),
		Entry("scoop, installed globally",
			`C:\ProgramData\scoop\apps\fft\current\fft.exe`, "windows", nil, MethodScoop),
		Entry("scoop, relocated with SCOOP",
			`D:\tools\scoop-root\apps\fft\current\fft.exe`, "windows",
			map[string]string{"SCOOP": `D:\tools\scoop-root`}, MethodScoop),
		Entry("scoop, relocated with SCOOP_GLOBAL",
			`D:\shared\sg\apps\fft\current\fft.exe`, "windows",
			map[string]string{"SCOOP_GLOBAL": `D:\shared\sg`}, MethodScoop),
		Entry("windows paths are matched case-insensitively, as the filesystem is",
			`C:\Users\JO\Scoop\Apps\fft\current\fft.exe`, "windows", nil, MethodScoop),
		Entry("forward slashes on windows are the same path",
			`C:/Users/jo/scoop/apps/fft/current/fft.exe`, "windows", nil, MethodScoop),
		// The reason detection compares whole segments rather than substrings.
		Entry("a directory that merely starts with scoop is not scoop",
			`C:\scoopy\fft.exe`, "windows", nil, MethodUnknown),
		Entry("a scoop segment not followed by apps or shims is somebody's directory",
			`C:\Users\jo\scoop\notes\fft.exe`, "windows", nil, MethodUnknown),

		// WinGet. The package and its alias live under one root, so which of the
		// two os.Executable reports does not matter.
		Entry("winget, the portable package directory",
			`C:\Users\jo\AppData\Local\Microsoft\WinGet\Packages\Joessst-Dev.fft_abc123\fft.exe`,
			"windows", map[string]string{"LOCALAPPDATA": `C:\Users\jo\AppData\Local`}, MethodWinGet),
		Entry("winget, the alias in Links",
			`C:\Users\jo\AppData\Local\Microsoft\WinGet\Links\fft.exe`, "windows",
			map[string]string{"LOCALAPPDATA": `C:\Users\jo\AppData\Local`}, MethodWinGet),
		Entry("winget, with LOCALAPPDATA unset, recognised by its path segments",
			`C:\Users\jo\AppData\Local\Microsoft\WinGet\Packages\Joessst-Dev.fft_abc123\fft.exe`,
			"windows", nil, MethodWinGet),

		// Homebrew. The bare bin directory is never enough on its own — see the
		// /usr/local/bin entry below, which is the regression this all exists for.
		Entry("homebrew, a cask on apple silicon",
			"/opt/homebrew/Caskroom/fft/0.7.0/fft", "darwin", nil, MethodHomebrew),
		Entry("homebrew, a formula's cellar",
			"/usr/local/Cellar/fft/0.7.0/bin/fft", "darwin", nil, MethodHomebrew),
		Entry("homebrew, the apple-silicon prefix",
			"/opt/homebrew/bin/fft", "darwin", nil, MethodHomebrew),
		Entry("homebrew, linuxbrew's default prefix",
			"/home/linuxbrew/.linuxbrew/bin/fft", "linux", nil, MethodHomebrew),
		Entry("homebrew, relocated with HOMEBREW_PREFIX",
			"/home/jo/.linuxbrew/bin/fft", "linux",
			map[string]string{"HOMEBREW_PREFIX": "/home/jo/.linuxbrew"}, MethodHomebrew),
		// The one that matters most. This is where the install guide's own tarball
		// recipe puts fft, and calling it Homebrew would tell every tarball user on
		// Linux to run a command they do not have.
		Entry("/usr/local/bin alone is the tarball, not homebrew",
			"/usr/local/bin/fft", "linux", nil, MethodUnknown),
		Entry("a prefix that merely starts the same is not homebrew",
			"/opt/homebrewery/bin/fft", "darwin", nil, MethodUnknown),

		// go install.
		Entry("go install, into GOBIN",
			"/home/jo/bin/fft", "linux", map[string]string{"GOBIN": "/home/jo/bin"}, MethodGoInstall),
		Entry("go install, into GOPATH/bin",
			"/home/jo/go/bin/fft", "linux", map[string]string{"GOPATH": "/home/jo/go"}, MethodGoInstall),
		Entry("go install, into the second entry of a multi-entry GOPATH",
			"/home/jo/work/bin/fft", "linux",
			map[string]string{"GOPATH": "/home/jo/go:/home/jo/work"}, MethodGoInstall),
		Entry("go install, into the default GOPATH nobody set",
			"/home/jo/go/bin/fft", "linux", map[string]string{"HOME": "/home/jo"}, MethodGoInstall),
		Entry("go install, into the windows default",
			`C:\Users\jo\go\bin\fft.exe`, "windows",
			map[string]string{"USERPROFILE": `C:\Users\jo`}, MethodGoInstall),
		// GOBIN, when set, is where go install writes — so GOPATH/bin is not.
		Entry("GOBIN set elsewhere means GOPATH/bin is not a go install",
			"/home/jo/go/bin/fft", "linux",
			map[string]string{"GOBIN": "/opt/gobin", "GOPATH": "/home/jo/go"}, MethodUnknown),
		Entry("a directory below GOPATH/bin is not where go install writes",
			"/home/jo/go/bin/nested/fft", "linux",
			map[string]string{"GOPATH": "/home/jo/go"}, MethodUnknown),

		// The override, and the degenerate cases.
		Entry("FFT_INSTALL_METHOD wins over a path that says otherwise",
			"/opt/homebrew/Caskroom/fft/0.7.0/fft", "darwin",
			map[string]string{envInstallMethod: "docker"}, MethodDocker),
		Entry("FFT_INSTALL_METHOD is case-insensitive and tolerates whitespace",
			"/usr/local/bin/fft", "linux",
			map[string]string{envInstallMethod: "  Scoop "}, MethodScoop),
		// A typo in the hint must degrade to detection, never break a command.
		Entry("an unrecognised FFT_INSTALL_METHOD falls through to the path",
			"/opt/homebrew/Caskroom/fft/0.7.0/fft", "darwin",
			map[string]string{envInstallMethod: "aptitude"}, MethodHomebrew),
		Entry("an empty FFT_INSTALL_METHOD is not a value",
			"/opt/homebrew/Caskroom/fft/0.7.0/fft", "darwin",
			map[string]string{envInstallMethod: ""}, MethodHomebrew),
		Entry("the container image names itself",
			"/usr/bin/fft", "linux",
			map[string]string{envInstallMethod: "docker"}, MethodDocker),
		Entry("no path at all is nothing we can say", "", "linux", nil, MethodUnknown),
		Entry("an ordinary place a binary was copied to",
			"/home/jo/Downloads/fft", "linux", nil, MethodUnknown),
	)
})

var _ = Describe("Method.UpgradeHint", func() {
	// Cheap insurance: a constant added to the iota block without a case in
	// UpgradeHint would otherwise produce a banner ending in a dangling em dash.
	It("gives every method something to say", func() {
		for m := MethodUnknown; m <= MethodDocker; m++ {
			Expect(m.UpgradeHint()).NotTo(BeEmpty(), "method %d", m)
		}
	})

	It("falls back to the install guide for a method it does not know", func() {
		Expect(Method(99).UpgradeHint()).To(ContainSubstring("guide/install"))
	})
})
