package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/skill"
)

// The drift guard.
//
// The skill's whole value is that an agent believes it. A flag renamed in cmd/fft
// and not in the skill does not produce a wrong sentence in a document nobody
// reads — it produces an agent confidently running a command that does not exist,
// against somebody's warehouse. So every fft invocation in the skill is resolved
// against the real command tree here, and a skill that has drifted fails the build.
//
// This is the same bargain as the operation census in internal/api/access_test.go
// and the tree-walk in readonly_test.go: a fact that must stay true is asserted
// where it can fail, rather than remembered.

// languages are the fences the skill may use. Anything else fails the build, and
// that is the point: a snippet in a ```shell or a ```console fence would be quietly
// skipped by the verifier below, which is exactly the hole this spec exists to
// close. To add a language, add it here and say what it means.
var languages = map[string]bool{
	"sh":   true, // an fft command line; every one of them is verified
	"json": true, // a body or a response
	"text": true, // output, or a shape that is not a command
	"go":   true, // a Go code sample (the testcontainers module), never an fft command line
	"java": true, // a Java code sample (the testcontainers module), never an fft command line
}

// snippet is one command line the skill tells an agent to run.
type snippet struct {
	file string
	line int
	text string
	args []string // "fft" and everything after it
}

func (s snippet) where() string {
	return fmt.Sprintf("%s:%d: %s", s.file, s.line, s.text)
}

// skillSnippets are every fft invocation in every fenced sh block of the skill.
func skillSnippets() []snippet {
	GinkgoHelper()

	var out []snippet

	err := fs.WalkDir(skill.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(name) != ".md" {
			return err
		}

		data, err := fs.ReadFile(skill.FS(), name)
		Expect(err).NotTo(HaveOccurred())

		var (
			lang   string
			inside bool
		)

		for i, line := range strings.Split(string(data), "\n") {
			if info, ok := strings.CutPrefix(strings.TrimSpace(line), "```"); ok {
				if inside {
					inside, lang = false, ""
					continue
				}
				inside, lang = true, info

				Expect(languages).To(HaveKey(info),
					"%s:%d: fenced block tagged %q. Only %v are verified, so any other tag "+
						"is a command line nobody is checking", name, i+1, info, keys(languages))
				continue
			}

			if !inside || lang != "sh" || strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}

			Expect(readable(line)).To(Succeed(), "%s:%d: %s", name, i+1, strings.TrimSpace(line))

			found := invocations(line)

			// The guard that makes the rest of them redundant, and that no future shape
			// can slip past: a line that says fft, and out of which the tokenizer found no
			// fft command, is a line nobody is checking. `sudo fft ...`, `for id in ...;
			// do fft ...; done`, `(fft ...)` — none of them is in the skill today, and
			// none of them can be added without this failing. The count guard below cannot
			// do this job: it counts what was found, and these are lines that are never
			// found.
			if len(found) == 0 {
				tokens, _ := fields(line)
				// The path spellings too: `./fft` and `/usr/local/bin/fft` name the binary,
				// and a line that invokes it that way would otherwise be skipped in silence.
				// (`$FFT` is not caught here and does not need to be — [readable] refuses a
				// `$` outright.) A `fft.json` or a `--data '{"a":"fft"}'` tokenizes to
				// something that is neither, so no honest line trips this.
				Expect(tokens).NotTo(ContainElement(Or(Equal("fft"), HaveSuffix("/fft"))),
					"%s:%d: %s\n  mentions fft, and the tokenizer found no fft command in it — "+
						"so nothing here is verified. Rewrite it as a plain command line",
					name, i+1, strings.TrimSpace(line))
			}

			for _, args := range found {
				out = append(out, snippet{file: name, line: i + 1, text: strings.TrimSpace(line), args: args})
			}
		}

		Expect(inside).To(BeFalse(), "%s: a fenced block is never closed", name)
		return nil
	})
	Expect(err).NotTo(HaveOccurred())

	return out
}

// readable reports the shapes of shell this tokenizer cannot read, so that the
// skill may not contain them.
//
// This is the other half of "a line it cannot read is a line nobody is checking".
// Without it that claim is simply false: `ID=$(fft facility list)` tokenizes into
// something whose first word is not `fft`, and so is skipped — silently, and
// exactly as though it had been checked and found good. A snippet nobody verifies
// is the one thing this file exists to prevent, so the unreadable shapes fail the
// build instead of vanishing from it.
//
// The count guard below would not save us either: it counts what was found, and
// this is a line that is never found.
func readable(line string) error {
	switch trimmed := strings.TrimSpace(line); {
	case strings.Contains(line, "`"):
		return errors.New("command substitution: the tokenizer cannot see the fft call inside it")

	// Every use of $: a substitution `$(fft ...)` hides the call inside it, and a
	// `$FFT ...` or `$EDITOR ...` hides what the word even is. Neither is a shape the
	// tokenizer can reason about, and a snippet it cannot read is one nobody checks —
	// so the skill does without them. It loses nothing: these are command lines for an
	// agent to run, and it has no shell variables set.
	case strings.Contains(line, "$"):
		return errors.New("a shell variable or substitution: write the command out in full")
	case strings.HasPrefix(trimmed, "$ "):
		return errors.New("a `$ ` prompt prefix: the snippets are command lines to run, not a transcript")
	case strings.HasSuffix(trimmed, "\\"):
		return errors.New("a line continuation: keep an fft command line on one line")
	default:
		return nil
	}
}

// assignment is a leading NAME=value: an environment variable, not a command. It
// has to be a real identifier, or `ID=$(fft ...)` would be stripped as though it
// were one — see [readable], which refuses that line before it gets here.
var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// invocations pulls every fft command line out of one line of shell.
//
// It is a very small shell, and deliberately so: it understands only what the
// skill is allowed to contain — and [readable] is what keeps the skill inside
// that, rather than letting a line the tokenizer misreads pass for one it checked.
func invocations(line string) [][]string {
	GinkgoHelper()

	tokens, ok := fields(line)
	Expect(ok).To(BeTrue(), "unbalanced quotes: %s", line)

	var out [][]string
	var current []string

	flush := func() {
		// So that the recipe showing FFT_READ_ONLY=1 in front of an fft call is still
		// checked, rather than read as a command named FFT_READ_ONLY=1.
		for len(current) > 0 && assignment.MatchString(current[0]) {
			current = current[1:]
		}
		if len(current) > 0 && current[0] == "fft" {
			out = append(out, current)
		}
		current = nil
	}

	for i := 0; i < len(tokens); i++ {
		switch tok := tokens[i]; {
		// A separator ends one command and begins another, so that both halves of
		// `fft ... | jq ...` are considered — the fft half checked, the jq half ignored.
		case tok == "|" || tok == "&&" || tok == "||" || tok == ";":
			flush()

		// A redirection and its target are not arguments. `fft ... --example > x.json`
		// would otherwise be checked with "x.json" as a positional.
		case tok == ">" || tok == ">>" || tok == "2>":
			i++

		default:
			current = append(current, tok)
		}
	}
	flush()

	return out
}

// fields splits a line into tokens, keeping a quoted argument whole: the skill
// contains --data '{"status":"ACTIVE"}' and --name "Berlin Warehouse", and
// strings.Fields would shred both. It reports false on an unbalanced quote.
func fields(line string) ([]string, bool) {
	var (
		out   []string
		token strings.Builder
		quote rune
		open  bool
	)

	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			token.WriteRune(r)

		case r == '\'' || r == '"':
			quote = r
			open = true

		case r == ' ' || r == '\t':
			if token.Len() > 0 || open {
				out = append(out, token.String())
				token.Reset()
				open = false
			}

		default:
			token.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, false
	}
	if token.Len() > 0 || open {
		out = append(out, token.String())
	}

	return out, true
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

var _ = Describe("the skill's fft invocations", func() {
	var snippets []snippet

	BeforeEach(func() {
		snippets = skillSnippets()
	})

	// A drift guard that scanned nothing passes every build and protects nothing. If
	// a refactor breaks the extractor — a fence tag changes, a file moves — this is
	// what says so, rather than the suite going quietly green.
	It("finds them", func() {
		Expect(len(snippets)).To(BeNumerically(">=", 30))
	})

	It("resolves every one against the real command tree", func() {
		for _, s := range snippets {
			// A fresh tree per snippet: ParseFlags mutates the flag set — Changed, and the
			// accumulated values of a StringArray — so a shared one would leak --param from
			// one snippet into the next and let a wrong one pass.
			root := newRootCmd(&Deps{})

			cmd, rest, err := root.Find(s.args[1:])
			Expect(err).NotTo(HaveOccurred(), s.where())
			Expect(cmd).NotTo(Equal(root), "%s: resolves to no command", s.where())

			// What cobra itself does before it runs a command. Doing the same thing means
			// this spec has no opinion of its own to keep in step with the real one:
			// --help exists only once InitDefaultHelpFlag has put it there, and ParseFlags
			// merges every parent's persistent flags, so -o and --read-only resolve on a
			// leaf exactly as they do in anger.
			cmd.InitDefaultHelpFlag()
			Expect(cmd.ParseFlags(rest)).To(Succeed(), s.where())

			args := cmd.Flags().Args()

			// ValidateArgs is what catches a typo in a *subcommand*: `fft facility lst`
			// resolves to `fft facility` with a leftover positional, and only NoArgs
			// notices. Without it, a misspelled command would sail through the flag check.
			Expect(cmd.ValidateArgs(args)).To(Succeed(), s.where())
			Expect(cmd.ValidateRequiredFlags()).To(Succeed(), s.where())
			Expect(cmd.ValidateFlagGroups()).To(Succeed(), s.where())

			// `fft api <operationId>` names its operation as an argument, so the tree
			// cannot check it and this must: a skill that tells an agent to run an
			// operation the spec does not have is a skill that has drifted from the API
			// rather than from the CLI, which is no better.
			if cmd.CommandPath() == "fft api" && len(args) > 0 {
				_, found := api.LookupOperation(args[0])
				Expect(found).To(BeTrue(), "%s: no such operation %q", s.where(), args[0])
			}
		}
	})

	// A `<facilityId>` in a shell fence is a redirection, not a placeholder: pasted
	// into a terminal it truncates a file. Placeholders belong in prose; a fence gets
	// a value that runs.
	It("uses no angle-bracket placeholders a shell would misread", func() {
		for _, s := range snippets {
			Expect(s.args).NotTo(ContainElement(HavePrefix("<")), s.where())
		}
	})
})

var _ = Describe("the skill's shell tokenizer", func() {
	DescribeTable("finds the fft command in a line",
		func(line string, want ...[]string) {
			got := invocations(line)

			if len(want) == 0 {
				Expect(got).To(BeEmpty())
				return
			}
			Expect(got).To(Equal(want))
		},
		Entry("a plain command", "fft facility list", []string{"fft", "facility", "list"}),
		Entry("a pipe, whose right-hand side is not fft",
			"fft facility list -o json | jq -r '.[].id'",
			[]string{"fft", "facility", "list", "-o", "json"}),
		Entry("a redirection, whose target is not an argument",
			"fft stock create --example > stock.json",
			[]string{"fft", "stock", "create", "--example"}),
		Entry("a quoted value with a space",
			`fft facility patch f1 --name "Berlin Warehouse"`,
			[]string{"fft", "facility", "patch", "f1", "--name", "Berlin Warehouse"}),
		Entry("a quoted body containing a pipe and a hash",
			`fft api addPickJob --data '{"note":"a|b #c"}'`,
			[]string{"fft", "api", "addPickJob", "--data", `{"note":"a|b #c"}`}),
		Entry("an environment prefix",
			"FFT_READ_ONLY=1 fft facility list",
			[]string{"fft", "facility", "list"}),
		Entry("a line that is not fft at all", "jq -r '.[].id' facilities.json"),
	)

	// The shapes the tokenizer would misread, and therefore skip. Each of them looks
	// exactly like a verified snippet from the outside — a green build, and nobody
	// checking the command in it — so each of them fails the build instead.
	DescribeTable("refuses a line it cannot read",
		func(line string) {
			Expect(readable(line)).NotTo(Succeed())
		},
		Entry("command substitution", `ID=$(fft facility list -o json | jq -r '.[0].id')`),
		Entry("a backquoted command", "ID=`fft facility list`"),
		Entry("a prompt prefix", "$ fft facility list"),
		Entry("a line continuation", `fft facility list \`),
		Entry("the binary behind a variable", "$FFT facility list"),
		Entry("an argument behind a variable", "fft facility get $ID"),
	)

	It("reads the lines it does allow", func() {
		Expect(readable("fft facility list -o json | jq -r '.[].id'")).To(Succeed())
		Expect(readable("FFT_READ_ONLY=1 fft facility list")).To(Succeed())
	})
})

var _ = Describe("the skill's frontmatter", func() {
	It("is the one Claude Code will load", func() {
		meta, body, err := skill.Parse(skill.Document())
		Expect(err).NotTo(HaveOccurred())

		Expect(meta.Name).To(Equal(skill.Name))
		Expect(body).NotTo(BeEmpty())

		// The description is the only thing an agent reads before it decides whether to
		// open the skill at all. Empty, or over the limit, and the skill is one nobody
		// ever loads — a failure that is completely silent at the point of use.
		Expect(meta.Description).NotTo(BeEmpty())
		Expect(len(meta.Description)).To(BeNumerically("<", 1024))
		Expect(meta.Description).To(ContainSubstring("fulfillmenttools"))
		Expect(meta.Description).NotTo(ContainSubstring("\n"))
	})
})

var _ = Describe("the skill's progressive disclosure", func() {
	// Both directions. A reference nothing links to is a reference no agent opens, and
	// a link to a file that is not there is a dead end an agent cannot recover from —
	// it has no way of knowing whether it missed something that mattered.
	It("links every reference from SKILL.md, and ships every reference it links", func() {
		doc := skill.Document()

		var shipped []string
		Expect(fs.WalkDir(skill.FS(), ".", func(name string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || name == skill.Doc {
				return err
			}
			shipped = append(shipped, name)
			return nil
		})).To(Succeed())

		Expect(shipped).NotTo(BeEmpty())

		for _, name := range shipped {
			Expect(doc).To(ContainSubstring(name), "%s is shipped and SKILL.md links to nothing that reaches it", name)
		}

		for _, link := range links(doc) {
			_, err := fs.Stat(skill.FS(), link)
			Expect(err).NotTo(HaveOccurred(), "SKILL.md links to %s, which the skill does not ship", link)
		}
	})
})

// links are the relative markdown links in a document: the (references/x.md) of
// [text](references/x.md).
func links(doc string) []string {
	var out []string

	for rest := doc; ; {
		_, after, ok := strings.Cut(rest, "](")
		if !ok {
			return out
		}
		target, remainder, ok := strings.Cut(after, ")")
		rest = remainder
		if !ok {
			return out
		}

		if !strings.Contains(target, "://") && strings.HasSuffix(target, ".md") {
			out = append(out, target)
		}
	}
}
