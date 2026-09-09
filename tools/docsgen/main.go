// Command docsgen renders the skill-derived guide pages of the documentation
// site, so those pages are never a second copy of the docs that can drift from
// the first.
//
// Its one source is the agent skill compiled into the binary
// (internal/skill/assets) — the prose an AI reads before driving fft, and the
// best usage documentation the repo has. Its files map one-to-one onto guide
// pages, and every page docsgen writes says so in its front matter:
//
//	source: internal/skill/assets/references/recipes.md
//
// That key is the whole ownership contract. A page carrying one is generated:
// edit the skill asset, not the page. A page without one — install, auth, the
// setup guides — is hand-written and docsgen never touches it. The sweep at the
// end of a run deletes generated pages this run did not write, so renaming a
// skillPages entry drops its orphan instead of leaving one behind that the
// no-drift gate cannot see.
//
// Everything it writes is committed, and CI re-runs it and fails on a diff — the
// same no-drift contract make generate has.
//
//	go run ./tools/docsgen -skill internal/skill/assets -out docs/guide
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Joessst-Dev/fft-cli/internal/docsmd"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "docsgen: %v\n", err)
		os.Exit(1)
	}
}

// skillPage is a file under the skill assets copied verbatim (bar its front
// matter and links) into a guide page.
type skillPage struct{ src, out, title string }

var skillPages = []skillPage{
	{"SKILL.md", "overview.md", "Overview"},
	{"references/commands.md", "commands.md", "Commands"},
	{"references/discovery.md", "discovery.md", "Discovery"},
	{"references/recipes.md", "recipes.md", "Recipes"},
	{"references/troubleshooting.md", "troubleshooting.md", "Troubleshooting"},
	{"references/emulator.md", "emulator.md", "Emulator"},
	{"references/components.md", "components.md", "Components"},
	{"references/templates.md", "templates.md", "Templates"},
}

// skillLinks maps a skill file's basename to the guide slug it becomes, so an
// intra-skill link resolves to the right page on the site.
var skillLinks = map[string]string{
	"SKILL.md":           "overview",
	"commands.md":        "commands",
	"discovery.md":       "discovery",
	"recipes.md":         "recipes",
	"troubleshooting.md": "troubleshooting",
	"emulator.md":        "emulator",
	"components.md":      "components",
	"templates.md":       "templates",
}

func run(args []string) error {
	fs := flag.NewFlagSet("docsgen", flag.ContinueOnError)
	var (
		skill = fs.String("skill", "internal/skill/assets", "path to the skill assets directory")
		out   = fs.String("out", "docs/guide", "directory to write the guide pages to")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := os.MkdirAll(*out, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", *out, err)
	}

	written := make(map[string]bool, len(skillPages))
	for _, p := range skillPages {
		body, err := os.ReadFile(filepath.Join(*skill, p.src))
		if err != nil {
			return fmt.Errorf("read skill %s: %w", p.src, err)
		}
		// Forward slashes whatever the host: this is a repository path in an
		// edit URL, not a path anything opens, and a Windows run must produce
		// the same bytes as a Linux one or the no-drift gate fails on it.
		source := filepath.ToSlash(filepath.Join(*skill, p.src))
		if err := writePage(*out, p.out, p.title, source, rewriteLinks(stripFrontMatter(string(body)))); err != nil {
			return err
		}
		written[p.out] = true
	}

	return sweep(*out, written)
}

// sweep deletes the generated pages of a source that was renamed or removed. A
// page is generated if its front matter carries a source key; anything without
// one is hand-written and is left alone, which is what lets generated and
// hand-written pages share a directory.
func sweep(dir string, written map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || written[name] {
			continue
		}
		generated, err := hasSourceKey(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if !generated {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("remove orphan %s: %w", name, err)
		}
	}
	return nil
}

// hasSourceKey reports whether a page's front matter carries a source key. It
// reads only the front matter: a "source:" line in the body is prose, not
// ownership.
func hasSourceKey(file string) (bool, error) {
	f, err := os.Open(file) // #nosec G304 -- a page in the output directory docsgen owns
	if err != nil {
		return false, fmt.Errorf("open %s: %w", file, err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	if !sc.Scan() || sc.Text() != "---" {
		return false, nil
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "---" {
			return false, nil
		}
		if strings.HasPrefix(line, "source:") {
			return true, nil
		}
	}
	return false, sc.Err()
}

// writePage prepends VitePress front matter and writes one guide page.
func writePage(dir, name, title, source, body string) error {
	content := fmt.Sprintf("---\ntitle: %s\nsource: %s\n---\n\n%s",
		title, source, docsmd.EscapeAngles(strings.TrimLeft(body, "\n")))
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	// #nosec G703 -- dir is this build tool's own -out flag and name is a
	// constant from skillPages; there is no untrusted path here. G703 is the
	// taint-analysis rule gosec grew in the v2.2x line (it does not exist in
	// v2.22 and earlier, where the ids stop at G601) and it, not G304, is what
	// tracks os.WriteFile — so the id is deliberate and is not interchangeable
	// with the G304 above.
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
}

// stripFrontMatter drops a leading YAML front-matter block. The skill's SKILL.md
// carries one (name + description for the agent runtime); the reference files do
// not, and are returned untouched.
func stripFrontMatter(s string) string {
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	if end := strings.Index(s[4:], "\n---\n"); end >= 0 {
		return strings.TrimLeft(s[4+end+len("\n---\n"):], "\n")
	}
	return s
}

var linkRE = regexp.MustCompile(`\]\(([^)]+)\)`)

// rewriteLinks rewrites every Markdown link target in a skill file so it
// resolves on the site. Only one kind of target needs it: a link to another
// skill file, which becomes that file's guide page. A fragment-only link is a
// link within the page and is left alone, and so is everything external.
func rewriteLinks(body string) string {
	resolve := func(target string) string {
		if strings.HasPrefix(target, "#") {
			return target
		}
		p, frag, _ := strings.Cut(target, "#")
		slugName, ok := skillLinks[filepath.Base(p)]
		if !ok {
			return target
		}
		if frag != "" {
			return "./" + slugName + ".md#" + frag
		}
		return "./" + slugName + ".md"
	}

	lines := strings.Split(body, "\n")
	inFence := false
	for i, line := range lines {
		if docsmd.IsFenceDelimiter(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i] = linkRE.ReplaceAllStringFunc(line, func(m string) string {
			return "](" + resolve(linkRE.FindStringSubmatch(m)[1]) + ")"
		})
	}
	return strings.Join(lines, "\n")
}
