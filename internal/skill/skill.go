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
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
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

// FS is the skill's tree.
func FS() fs.FS { return tree }

// Meta is SKILL.md's YAML frontmatter: the two fields the skill format requires,
// and the only two an agent reads before deciding whether to open the file at
// all. A description that does not say when to use the skill is a skill that is
// never used.
type Meta struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
}

// Parse splits SKILL.md into its frontmatter and the body below it.
func Parse(doc string) (Meta, string, error) {
	const fence = "---"

	rest, ok := strings.CutPrefix(doc, fence+"\n")
	if !ok {
		return Meta{}, "", fmt.Errorf("%s does not open with a %q frontmatter fence", Doc, fence)
	}

	front, body, ok := strings.Cut(rest, "\n"+fence+"\n")
	if !ok {
		return Meta{}, "", fmt.Errorf("%s has no closing %q frontmatter fence", Doc, fence)
	}

	var meta Meta
	if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
		return Meta{}, "", fmt.Errorf("%s frontmatter: %w", Doc, err)
	}
	return meta, strings.TrimPrefix(body, "\n"), nil
}

// Document is the whole of SKILL.md, frontmatter included: what `fft skill show`
// prints, and what an agent that is not Claude Code wants appended to its own
// context file.
func Document() string {
	data, err := fs.ReadFile(tree, Doc)
	if err != nil {
		// The embed either has SKILL.md or the build is broken.
		panic(fmt.Sprintf("embedded skill: %v", err))
	}
	return string(data)
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
