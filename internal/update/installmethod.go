package update

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// envInstallMethod lets a packager — or a user fft has guessed wrong about —
// name the install method outright, bypassing detection. The container image
// sets it (see the Dockerfile), which is the only way a binary at /usr/bin/fft
// could ever be told from a hand-unpacked tarball.
const envInstallMethod = "FFT_INSTALL_METHOD"

// Method is how this copy of fft got onto the machine, as far as where it sits
// can tell.
//
// It exists for exactly one reason: to name the right upgrade command. fft never
// replaces its own binary — every upgrade is a package manager's — so naming the
// wrong manager is the whole failure. A Scoop user told to run `brew upgrade
// fft` gets "command not found" and learns nothing from it, which is worse than
// being told nothing at all.
type Method int

const (
	// MethodUnknown is the honest answer for a tarball in /usr/local/bin, a
	// binary a colleague copied over, or anything with no package manager behind
	// it. It is the zero value on purpose: every branch that cannot decide lands
	// here, and the hint it carries points at the install guide rather than
	// guessing.
	MethodUnknown Method = iota
	MethodHomebrew
	MethodScoop
	MethodWinGet
	MethodGoInstall
	MethodDocker
)

// UpgradeHint is the command that replaces this fft with a newer one.
//
// Every string here is a compile-time constant. Nothing a path or an environment
// variable contains ever reaches it, which is what keeps the update banner
// injection-safe by construction: a hostile SCOOP=... selects a branch, it can
// never contribute text.
func (m Method) UpgradeHint() string {
	switch m {
	case MethodHomebrew:
		return "brew upgrade fft"
	case MethodScoop:
		return "scoop update fft"
	case MethodWinGet:
		return "winget upgrade Joessst-Dev.fft"
	case MethodGoInstall:
		return "go install github.com/Joessst-Dev/fft-cli/cmd/fft@latest"
	case MethodDocker:
		return "docker pull ghcr.io/joessst-dev/fft:latest"
	default:
		return "see https://joessst-dev.github.io/fft-cli/guide/install"
	}
}

// executable is os.Executable, indirected so a spec can place fft anywhere on
// any platform without building one.
var executable = os.Executable

// InstallMethod reports how the running fft was installed, judging only by where
// its executable sits and what the environment says.
//
// Deliberately cheap: os.Executable, at most one EvalSymlinks, some string
// matching, a handful of environment reads. No subprocess — in particular not
// `go env GOPATH`, which costs tens of milliseconds and fails outright on a
// machine with no toolchain. The banner is a courtesy; it may not cost the user
// anything, and it may not fail.
func InstallMethod() Method {
	exe, err := executable()
	if err != nil {
		return MethodUnknown
	}
	if m := detectMethod(exe, runtime.GOOS, os.Getenv); m != MethodUnknown {
		return m
	}
	// Only now, and only for the ambiguous case. A cask is exec'd through
	// /opt/homebrew/bin/fft — on an Intel Mac, /usr/local/bin/fft — a symlink into
	// the Caskroom, and until it is resolved that is indistinguishable from the
	// tarball recipe in the install guide. A few lstats on a local path cost
	// microseconds; doing them only after the cheap pass gave up keeps a slow
	// network home directory out of the common path entirely.
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return MethodUnknown
	}
	return detectMethod(resolved, runtime.GOOS, os.Getenv)
}

// detectMethod classifies an executable path.
//
// Everything it consults is an argument: the path, the GOOS whose rules apply,
// and a lookup for the environment. That is the whole point — one table can then
// exercise the Windows rules from a Mac, and the Windows branches are precisely
// the ones nobody who edits this file will ever run.
func detectMethod(exe, goos string, getenv func(string) string) Method {
	if m, ok := namedMethod(getenv(envInstallMethod)); ok {
		return m
	}
	if exe == "" {
		return MethodUnknown
	}

	windows := goos == "windows"
	segs := segments(exe, windows)
	dir := pathDir(exe, windows)

	switch {
	case scoopPath(segs, dir, windows, getenv):
		return MethodScoop
	case wingetPath(segs, dir, windows, getenv):
		return MethodWinGet
	case homebrewPath(segs, dir, windows, getenv):
		return MethodHomebrew
	case goInstallPath(dir, windows, getenv):
		return MethodGoInstall
	default:
		return MethodUnknown
	}
}

// namedMethod parses an FFT_INSTALL_METHOD value. An unrecognised value is
// ignored rather than treated as an error: the variable is a hint, and a typo in
// it must degrade to detection, never break a command.
func namedMethod(v string) (Method, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "homebrew", "brew":
		return MethodHomebrew, true
	case "scoop":
		return MethodScoop, true
	case "winget":
		return MethodWinGet, true
	case "go":
		return MethodGoInstall, true
	case "docker":
		return MethodDocker, true
	default:
		return MethodUnknown, false
	}
}

// segments splits a path into its components, normalising separators first so
// that a Windows path written either way compares the same. On Windows the
// comparison is case-insensitive, because its filesystems are.
//
// Splitting into segments is what keeps the matching honest: a directory
// literally named "scoopy" contains the substring "scoop" and must not match.
func segments(p string, windows bool) []string {
	if windows {
		p = strings.ReplaceAll(p, `\`, "/")
		p = strings.ToLower(p)
	}
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// pathDir is filepath.Dir for the target platform rather than the running one,
// normalised the same way segments normalises, so that a directory read out of
// the environment can be compared against it.
func pathDir(p string, windows bool) string {
	if windows {
		p = strings.ReplaceAll(p, `\`, "/")
		p = strings.ToLower(p)
	}
	return path.Dir(p)
}

// hasPair reports whether segs contains want immediately followed by any of
// next. "scoop" then "apps" or "shims" is Scoop; "scoop" alone is somebody's
// directory name.
func hasPair(segs []string, want string, next ...string) bool {
	for i, s := range segs {
		if s != want || i+1 >= len(segs) {
			continue
		}
		for _, n := range next {
			if segs[i+1] == n {
				return true
			}
		}
	}
	return false
}

// hasSegment reports whether segs contains any of want.
func hasSegment(segs []string, want ...string) bool {
	for _, s := range segs {
		for _, w := range want {
			if s == w {
				return true
			}
		}
	}
	return false
}

// under reports whether the normalised path dir sits inside root, comparing
// whole segments so that /opt/homebrewery is not "under" /opt/homebrew.
func under(dir, root string, windows bool) bool {
	if root == "" {
		return false
	}
	if windows {
		root = strings.ReplaceAll(root, `\`, "/")
		root = strings.ToLower(root)
	}
	root = strings.TrimSuffix(root, "/")
	if root == "" {
		return false
	}
	return dir == root || strings.HasPrefix(dir, root+"/")
}

// scoopPath matches both shapes Scoop can present. Its shim
// (~\scoop\shims\fft.exe) is a separate executable that launches the real
// binary, so os.Executable inside fft normally reports
// ...\scoop\apps\fft\current\fft.exe — but a spec, or a future Scoop, may see
// either, and both carry the scoop+apps/shims pair.
func scoopPath(segs []string, dir string, windows bool, getenv func(string) string) bool {
	if hasPair(segs, "scoop", "apps", "shims") {
		return true
	}
	for _, env := range []string{"SCOOP", "SCOOP_GLOBAL"} {
		if under(dir, getenv(env), windows) {
			return true
		}
	}
	return false
}

// wingetPath matches WinGet's portable layout: the package itself lands in
// %LOCALAPPDATA%\Microsoft\WinGet\Packages\<id>_<hash>\ and its alias in
// ...\WinGet\Links\, and which of the two os.Executable reports does not matter
// because both are under the same root.
func wingetPath(segs []string, dir string, windows bool, getenv func(string) string) bool {
	if local := getenv("LOCALAPPDATA"); local != "" {
		if under(dir, local+"/Microsoft/WinGet", windows) {
			return true
		}
	}
	return hasPair(segs, "microsoft", "winget") && hasSegment(segs, "packages", "links")
}

// homebrewPath deliberately does NOT match a bare /usr/local/bin.
//
// That is where this project's own tarball recipe puts fft (`sudo mv fft
// /usr/local/bin/`), and on an Intel Mac it is also where the cask's symlink
// lives. Only a resolved Cellar or Caskroom segment tells the two apart — which
// is why InstallMethod resolves symlinks and asks again. Matching the bare
// directory would print `brew upgrade fft` at every tarball user on Linux.
func homebrewPath(segs []string, dir string, windows bool, getenv func(string) string) bool {
	if hasSegment(segs, cellar(windows), caskroom(windows)) {
		return true
	}
	if prefix := getenv("HOMEBREW_PREFIX"); prefix != "" && under(dir, prefix+"/bin", windows) {
		return true
	}
	return under(dir, "/opt/homebrew", windows) || under(dir, "/home/linuxbrew/.linuxbrew", windows)
}

func cellar(windows bool) string {
	if windows {
		return "cellar"
	}
	return "Cellar"
}

func caskroom(windows bool) string {
	if windows {
		return "caskroom"
	}
	return "Caskroom"
}

// goInstallPath matches where `go install` drops a binary: $GOBIN, or <entry>/bin
// for any entry of $GOPATH, or the default $HOME/go/bin.
//
// GOPATH is read from the environment only. Shelling out to `go env GOPATH` is
// the obvious implementation and is forbidden here: it costs tens of
// milliseconds and fails outright on a machine with no toolchain, and this
// function runs while the user is waiting for their command.
func goInstallPath(dir string, windows bool, getenv func(string) string) bool {
	if gobin := getenv("GOBIN"); gobin != "" {
		return dir == normalise(gobin, windows)
	}
	if gopath := getenv("GOPATH"); gopath != "" {
		for _, entry := range strings.Split(gopath, string(os.PathListSeparator)) {
			if entry == "" {
				continue
			}
			if dir == normalise(entry+"/bin", windows) {
				return true
			}
		}
		return false
	}
	home := getenv("HOME")
	if windows {
		home = getenv("USERPROFILE")
	}
	if home == "" {
		return false
	}
	return dir == normalise(home+"/go/bin", windows)
}

// normalise puts an environment-supplied directory into the same shape
// segments and pathDir produce, so the two can be compared.
func normalise(p string, windows bool) string {
	if windows {
		p = strings.ReplaceAll(p, `\`, "/")
		p = strings.ToLower(p)
	}
	return strings.TrimSuffix(path.Clean(p), "/")
}
