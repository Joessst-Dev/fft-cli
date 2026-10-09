// Package skill ships fft's agent skill: the document an AI coding assistant
// reads before it drives fft on somebody's behalf.
//
// The skill is markdown, compiled into the binary, and `fft skill install`
// copies it onto disk. Compiled in rather than fetched, because the skill a user
// installs must be the one that describes the fft they are running — and the
// drift spec in cmd/fft/skill_drift_test.go resolves every fft invocation in it
// against the real command tree, so a renamed flag fails the build instead of
// quietly making the skill wrong. A skill that lies is worse than no skill: an
// agent without one asks, and an agent with a wrong one acts.
//
// It is documentation, not configuration and not a secret. It is written 0644
// under a 0755 directory — unlike everything else fft writes — because the user,
// their editor and their agent all read it, and 0600 is a credential's mode.
package skill

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Joessst-Dev/fft-cli/internal/buildinfo"
)

// Name is the skill's name, its directory's name, and the name in SKILL.md's
// frontmatter. All three have to agree, and a spec asserts that they do.
const Name = "fft"

// Doc is the entry point: the file an agent always loads, and the file whose
// presence in a directory is what identifies that directory as this skill's.
const Doc = "SKILL.md"

//go:embed assets
var assets embed.FS

// tree is the skill's files, rooted at the skill directory itself: SKILL.md at
// the top and references/ under it. That is the shape it has once installed, so
// installing is a copy and nothing more.
var tree = func() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		// Only reachable by moving the embedded directory, which is a build defect —
		// and one better found at startup than by a user whose `fft skill install`
		// writes an empty directory.
		panic(fmt.Sprintf("embedded skill: %v", err))
	}
	return sub
}()

// FS is the skill's tree as it is written in the repository: SKILL.md without the
// version stamp that installing adds. The drift spec and the docs generator read
// it, and neither has any use for which fft built the binary they run in — the
// generated guide pages would otherwise change on every release. What is written
// to disk, and what `fft skill show` prints, is [Document].
func FS() fs.FS { return tree }

// Meta is SKILL.md's YAML frontmatter: the two fields the skill format requires,
// and the only two an agent reads before deciding whether to open the file at
// all. A description that does not say when to use the skill is a skill that is
// never used.
//
// Metadata is the format's own free-form map, and the one place fft writes into
// the frontmatter: the version of fft that installed the skill. See [Meta.Version].
type Meta struct {
	Name        string            `json:"name" yaml:"name"`
	Description string            `json:"description" yaml:"description"`
	Metadata    map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// Version is the fft that wrote this skill, or "" for one written before fft
// stamped its skills at all.
func (m Meta) Version() string { return m.Metadata["version"] }

// Parse splits SKILL.md into its frontmatter and the body below it.
func Parse(doc string) (Meta, string, error) {
	const fence = "---"

	rest, ok := strings.CutPrefix(doc, openFence)
	if !ok {
		return Meta{}, "", fmt.Errorf("%s does not open with a %q frontmatter fence", Doc, fence)
	}

	front, body, ok := strings.Cut(rest, closeFence)
	if !ok {
		return Meta{}, "", fmt.Errorf("%s has no closing %q frontmatter fence", Doc, fence)
	}

	var meta Meta
	if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
		return Meta{}, "", fmt.Errorf("%s frontmatter: %w", Doc, err)
	}
	return meta, strings.TrimPrefix(body, "\n"), nil
}

// Document is the whole of SKILL.md, frontmatter and version stamp included: what
// `fft skill show` prints, what `fft skill install` writes, and what an agent
// that is not Claude Code wants appended to its own context file.
func Document() string {
	data, err := content(Doc)
	if err != nil {
		// The embed either has SKILL.md or the build is broken.
		panic(fmt.Sprintf("embedded skill: %v", err))
	}
	return string(data)
}

// content is the bytes fft ships for one file of the skill: SKILL.md stamped with
// this binary's version, everything else exactly as embedded.
//
// Only SKILL.md carries the stamp because it is the one file every agent loads,
// and the frontmatter is the one part of it that is meant for machines. The
// reference files have no frontmatter to put it in, and a skill is stale or not
// as a whole.
func content(name string) ([]byte, error) {
	data, err := fs.ReadFile(tree, name)
	if err != nil {
		return nil, err
	}
	if name == Doc {
		data = stamp(data, buildinfo.Version)
	}
	return data, nil
}

// The frontmatter fences. SKILL.md opens with one and closes the frontmatter with
// the other, each on a line of its own.
const (
	openFence  = "---\n"
	closeFence = "\n---\n"
)

// stampBlock is what [stamp] adds to the frontmatter. Quoted, because "1.10" is a
// version and an unquoted YAML 1.10 is a float that reads back as 1.1.
func stampBlock(version string) string {
	return "metadata:\n  version: " + strconv.Quote(version) + "\n"
}

// stamp records version in doc's frontmatter, just above the closing fence.
//
// It inserts text rather than decoding and re-encoding the YAML: the description
// is hand-wrapped prose an agent reads before anything else, and a marshaller
// would re-flow and re-quote it on every install. A document without a
// frontmatter is returned as it is — the embedded one always has one, and a spec
// parses it on every build.
func stamp(doc []byte, version string) []byte {
	at := closing(doc)
	if at < 0 {
		return doc
	}
	return slices.Concat(doc[:at], []byte(stampBlock(version)), doc[at:])
}

// unstamp takes out what [stamp] put in, and nothing else.
//
// "Nothing else" is the point. It decides whether a SKILL.md on disk differs from
// what fft ships only in the version that wrote it, and a looser match — any
// metadata block, any version line — would let a user's own edit to the
// frontmatter pass for fft's, and be overwritten without asking.
func unstamp(doc []byte) []byte {
	at := closing(doc)
	if at < 0 {
		return doc
	}

	front := doc[:at]
	start := bytes.LastIndex(front, []byte("\nmetadata:\n  version: "))
	if start < 0 {
		return doc
	}
	start++ // keep the newline that ends the line above

	quoted, ok := bytes.CutSuffix(front[start+len("metadata:\n  version: "):], []byte("\n"))
	if !ok {
		return doc
	}
	version, err := strconv.Unquote(string(quoted))
	if err != nil || stampBlock(version) != string(front[start:]) {
		return doc
	}
	return slices.Concat(doc[:start], doc[at:])
}

// closing is the offset of the frontmatter's closing fence line, or -1 when doc
// has no frontmatter. It finds the same fence [Parse] does.
func closing(doc []byte) int {
	if !bytes.HasPrefix(doc, []byte(openFence)) {
		return -1
	}
	i := bytes.Index(doc[len(openFence):], []byte(closeFence))
	if i < 0 {
		return -1
	}
	// Past the newline that ends the frontmatter's last line, so that the fence
	// itself is what follows.
	return len(openFence) + i + 1
}

// ErrMalformed reports an installed SKILL.md whose frontmatter does not parse.
var ErrMalformed = errors.New("malformed skill document")

// Installed reads the frontmatter of the skill installed under root — the
// SKILL.md in root/fft — and reports false when there is none there.
//
// A SKILL.md whose frontmatter does not parse is [ErrMalformed] rather than an
// unversioned skill: fft never wrote one like that, so it is somebody's edit, and
// calling it "from an older fft" would send them to an install that refuses to
// touch it. Any other error is the file being unreadable, and is returned as the
// filesystem gave it, path and all.
func Installed(root string) (Meta, bool, error) {
	path := filepath.Join(root, Name, Doc)

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Meta{}, false, nil
	case err != nil:
		return Meta{}, false, err
	}

	meta, _, err := Parse(string(data))
	if err != nil {
		return Meta{}, true, fmt.Errorf("%s: %w: %w", path, ErrMalformed, err)
	}
	return meta, true, nil
}

// Current reports whether an installed skill describes the fft that is running.
//
// A build that did not come from a release tag has no version to hold the skill
// to, and counts every skill as current — the same rule, for the same reason, as
// the update check: a developer running what they are working on does not need
// telling to reinstall it. A skill with no stamp at all predates stamping, and is
// not current: that is the install that most needs the hint.
func Current(m Meta) bool {
	if !buildinfo.IsRelease() {
		return true
	}
	return m.Version() == buildinfo.Version
}

// UserDir is ~/.claude/skills: where Claude Code looks for a personal skill.
//
// XDG_CONFIG_HOME is deliberately not consulted, unlike [config.DefaultPath].
// That path is fft's to choose; this one is Claude Code's, and honouring XDG here
// would put the skill somewhere nothing reads it.
func UserDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate your home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "skills"), nil
}

// ProjectDir is ./.claude/skills: the skill for one project, which Claude Code
// prefers over the personal one when it is working in that directory.
func ProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("locate the current directory: %w", err)
	}
	return filepath.Join(wd, ".claude", "skills"), nil
}
