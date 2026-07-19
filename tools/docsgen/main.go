// Command docsgen renders the documentation-site guide pages from their real
// sources, so the site is never a second copy of the docs that can drift from the
// first. Two sources feed it:
//
//   - the agent skill compiled into the binary (internal/skill/assets) — the
//     prose an AI reads before driving fft, and the best usage documentation the
//     repo has. Its files map one-to-one onto guide pages.
//   - the README — the home of install, setup, authentication and CI, which the
//     skill does not cover. docsgen slices out the sections it needs by heading.
//
// Everything it writes is committed, and CI re-runs it and fails on a diff — the
// same no-drift contract make generate has. Edit a skill file or a README section
// and the guide page follows on the next `make docs`; rename a README heading out
// from under it and docsgen fails loudly rather than emitting a half-empty page.
//
//	go run ./tools/docsgen -skill internal/skill/assets -readme README.md -out docs/guide
package main

import (
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

// skillPage is a file under the skill assets copied verbatim (bar its front-matter
// and links) into a guide page.
type skillPage struct{ src, out, title string }

// readmePage is a guide page sliced out of one README section, named by its H2
// heading.
type readmePage struct{ heading, out, title string }

var skillPages = []skillPage{
	{"SKILL.md", "overview.md", "Overview"},
	{"references/commands.md", "commands.md", "Commands"},
	{"references/discovery.md", "discovery.md", "Discovery"},
	{"references/recipes.md", "recipes.md", "Recipes"},
	{"references/troubleshooting.md", "troubleshooting.md", "Troubleshooting"},
	{"references/emulator.md", "emulator.md", "Emulator"},
}

var readmePages = []readmePage{
	{"Install", "install.md", "Install"},
	{"Before you begin", "prerequisites.md", "Before you begin"},
	{"Getting started", "getting-started.md", "Getting started"},
	{"Setting up a project", "configuration.md", "Setting up a project"},
	{"Authentication, honestly", "auth.md", "Authentication"},
	{"CI and headless use", "ci.md", "CI & headless use"},
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
}

func run(args []string) error {
	fs := flag.NewFlagSet("docsgen", flag.ContinueOnError)
	var (
		skill  = fs.String("skill", "internal/skill/assets", "path to the skill assets directory")
		readme = fs.String("readme", "README.md", "path to the README")
		out    = fs.String("out", "docs/guide", "directory to write the guide pages to")
		repo   = fs.String("repo", "https://github.com/Joessst-Dev/fft-cli", "repository URL, for links to README sections that are not pages")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	readmeSrc, err := os.ReadFile(*readme)
	if err != nil {
		return fmt.Errorf("read README: %w", err)
	}

	// The anchor map is what lets a README cross-reference survive being split
	// across pages: an anchor to a heading that became (part of) a page points at
	// that page; anything else falls back to the README on GitHub.
	anchors, sections, err := indexReadme(string(readmeSrc))
	if err != nil {
		return err
	}
	rewrite := linkRewriter(anchors, *repo)

	// The guide directory holds only generated pages, so wiping it drops the page
	// of any source that was renamed or removed instead of leaving an orphan the
	// drift gate cannot see.
	if err := os.RemoveAll(*out); err != nil {
		return fmt.Errorf("clear %s: %w", *out, err)
	}
	if err := os.MkdirAll(*out, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", *out, err)
	}

	for _, p := range skillPages {
		src, err := os.ReadFile(filepath.Join(*skill, p.src))
		if err != nil {
			return fmt.Errorf("read skill %s: %w", p.src, err)
		}
		body := rewrite(stripFrontMatter(string(src)))
		if err := writePage(*out, p.out, p.title, body); err != nil {
			return err
		}
	}

	for _, p := range readmePages {
		body, ok := sections[p.heading]
		if !ok {
			return fmt.Errorf("README has no %q section — docsgen's page map is stale", p.heading)
		}
		if err := writePage(*out, p.out, p.title, rewrite(shiftHeadings(body))); err != nil {
			return err
		}
	}

	return nil
}

// writePage prepends VitePress front-matter and writes one guide page.
func writePage(dir, name, title, body string) error {
	content := fmt.Sprintf("---\ntitle: %s\n---\n\n%s", title, docsmd.EscapeAngles(strings.TrimLeft(body, "\n")))
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
}

var headingRE = regexp.MustCompile(`^(#{1,6}) `)

// indexReadme walks the README once, splitting it into H2 sections and recording,
// for every heading, which page (if any) will contain it. anchorTarget carries the
// destination the rewriter needs: the guide slug, and whether the heading is a
// page's own top heading (a link to which needs no fragment) or one within it.
func indexReadme(src string) (map[string]anchorTarget, map[string]string, error) {
	pageOf := map[string]string{} // H2 heading text -> guide slug (without .md)
	for _, p := range readmePages {
		pageOf[p.heading] = strings.TrimSuffix(p.out, ".md")
	}

	anchors := map[string]anchorTarget{}
	sections := map[string]string{}

	var (
		curHeading string
		curSlug    string
		buf        []string
		inFence    bool
	)
	flush := func() {
		if curHeading != "" {
			sections[curHeading] = strings.Join(buf, "\n")
		}
	}

	for line := range strings.SplitSeq(src, "\n") {
		if docsmd.IsFenceDelimiter(line) {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			flush()
			curHeading = strings.TrimPrefix(line, "## ")
			curSlug = pageOf[curHeading]
			buf = []string{line}
			if curSlug != "" {
				anchors[slug(curHeading)] = anchorTarget{page: curSlug, top: true}
			}
			continue
		}
		if curHeading != "" {
			buf = append(buf, line)
			// A sub-heading inside a page's section is reachable at that page under
			// its own anchor. The key is the GitHub slug (what the README links with);
			// the value carries the VitePress slug (what the rendered page anchors it
			// as) — the two differ when a heading has runs of punctuation, which
			// VitePress collapses and GitHub does not.
			if !inFence && curSlug != "" {
				if m := headingRE.FindString(line); m != "" {
					text := strings.TrimPrefix(line, m)
					anchors[slug(text)] = anchorTarget{page: curSlug, frag: vitepressSlug(text)}
				}
			}
		}
	}
	flush()

	return anchors, sections, nil
}

// anchorTarget is where a README anchor points on the site: the guide page, the
// fragment on it (a heading's VitePress anchor), and whether the anchor is the
// page's own top heading — which is reached by the page URL alone, no fragment.
type anchorTarget struct {
	page string
	frag string
	top  bool
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

// shiftHeadings promotes every heading in an extracted README section by one
// level, so the section's H2 becomes the page's H1 and the outline stays clean.
// Lines inside fenced code blocks are left alone — a leading '#' there is a shell
// comment, not a heading.
func shiftHeadings(s string) string {
	var out []string
	inFence := false
	for line := range strings.SplitSeq(s, "\n") {
		if docsmd.IsFenceDelimiter(line) {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "##") {
			line = line[1:]
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

var linkRE = regexp.MustCompile(`\]\(([^)]+)\)`)

// linkRewriter returns a function that rewrites every Markdown link target in a
// body so it resolves on the site: intra-skill file links to their guide pages,
// README anchors to the page that holds the heading (or to the README on GitHub
// when no page does), and external links untouched.
func linkRewriter(anchors map[string]anchorTarget, repo string) func(string) string {
	readmeURL := strings.TrimRight(repo, "/") + "/blob/main/README.md"

	resolveAnchor := func(frag string) string {
		if t, ok := anchors[frag]; ok {
			if t.top {
				return "./" + t.page + ".md"
			}
			return "./" + t.page + ".md#" + t.frag
		}
		return readmeURL + "#" + frag
	}

	resolve := func(target string) string {
		switch {
		case strings.HasPrefix(target, "http://"), strings.HasPrefix(target, "https://"),
			strings.HasPrefix(target, "mailto:"):
			return target
		case strings.HasPrefix(target, "#"):
			return resolveAnchor(strings.TrimPrefix(target, "#"))
		}

		path, frag, _ := strings.Cut(target, "#")
		base := filepath.Base(path)
		if slugName, ok := skillLinks[base]; ok {
			if frag != "" {
				return "./" + slugName + ".md#" + frag
			}
			return "./" + slugName + ".md"
		}
		if strings.EqualFold(base, "README.md") {
			if frag != "" {
				return resolveAnchor(frag)
			}
			return readmeURL
		}
		return target
	}

	rewriteLine := func(line string) string {
		return linkRE.ReplaceAllStringFunc(line, func(m string) string {
			target := linkRE.FindStringSubmatch(m)[1]
			return "](" + resolve(target) + ")"
		})
	}

	return func(body string) string {
		lines := strings.Split(body, "\n")
		inFence := false
		for i, line := range lines {
			if docsmd.IsFenceDelimiter(line) {
				inFence = !inFence
				continue
			}
			if !inFence {
				lines[i] = rewriteLine(line)
			}
		}
		return strings.Join(lines, "\n")
	}
}

// slug renders a heading the way GitHub anchors it — which is how the README links
// to its own sections: lower-cased, punctuation dropped, spaces to hyphens, and
// runs of hyphens (the `--flag` in a heading leaves some) kept as-is. This is the
// key an incoming README anchor is looked up by.
func slug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

var hyphenRun = regexp.MustCompile(`-+`)

// vitepressSlug renders a heading the way VitePress anchors it on the rendered
// page — the same as GitHub, except it collapses runs of spaces and hyphens to a
// single hyphen and trims the ends. This is the fragment a site link must use, and
// it diverges from slug only on a heading like the Windows `--no-keyring` caveat.
func vitepressSlug(heading string) string {
	return strings.Trim(hyphenRun.ReplaceAllString(slug(heading), "-"), "-")
}
