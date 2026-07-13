package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/skill"
)

// installed is the skill's directory under root, and what is in it.
func installed(root string) string { return filepath.Join(root, skill.Name) }

func readFile(path string) string {
	GinkgoHelper()

	data, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	return string(data)
}

var _ = Describe("fft skill show", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	// `fft skill show >> AGENTS.md` is the whole point of the command, so what lands
	// on stdout must be the document and nothing else — not a table of it, not a
	// summary of it, and not a byte of commentary.
	It("prints the skill's markdown, and only that", func() {
		Expect(c.run("skill", "show")).To(Equal(exitcode.OK))

		Expect(c.out()).To(Equal(skill.Document()))
		Expect(c.errOut()).To(BeEmpty())
	})

	It("wraps it with its name and description under -o json", func() {
		Expect(c.run("skill", "show", "-o", "json")).To(Equal(exitcode.OK))

		var view skillDocView
		Expect(json.Unmarshal([]byte(c.out()), &view)).To(Succeed())

		Expect(view.Name).To(Equal(skill.Name))
		Expect(view.Description).To(ContainSubstring("fulfillmenttools"))
		Expect(view.Content).To(Equal(skill.Document()))
	})

	It("needs no project", func() {
		Expect(c.run("skill", "show")).To(Equal(exitcode.OK))

		_, err := os.Stat(c.configPath)
		Expect(err).To(MatchError(os.ErrNotExist), "printing the skill wrote a config file")
	})
})

var _ = Describe("fft skill install", func() {
	var c *cli
	var root string

	BeforeEach(func() {
		c = newCLI()
		root = GinkgoT().TempDir()
	})

	It("writes the skill, and says where", func() {
		Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))

		dir := installed(root)
		Expect(readFile(filepath.Join(dir, skill.Doc))).To(Equal(skill.Document()))
		Expect(filepath.Join(dir, "references", "recipes.md")).To(BeAnExistingFile())

		// The path is on stdout, because where the skill landed is the answer to the
		// command; the sentence about it is on stderr, because it is not.
		Expect(c.errOut()).To(ContainSubstring(dir))
		Expect(c.out()).To(ContainSubstring(skill.Doc))
	})

	It("reports what it did under -o json", func() {
		Expect(c.run("skill", "install", "--dir", root, "-o", "json")).To(Equal(exitcode.OK))

		var view skillView
		Expect(json.Unmarshal([]byte(c.out()), &view)).To(Succeed())

		Expect(view.Dir).To(Equal(installed(root)))
		Expect(view.Files).NotTo(BeEmpty())
		for _, f := range view.Files {
			Expect(f.Status).To(Equal(skill.StatusWritten))
		}
	})

	// hermeticEnv points HOME (and USERPROFILE) at a temp directory, which is what
	// makes this safe to assert at all: without it, the spec would install the skill
	// into the developer's own home.
	It("installs into ~/.claude/skills by default", func() {
		Expect(c.run("skill", "install")).To(Equal(exitcode.OK))

		home, err := os.UserHomeDir()
		Expect(err).NotTo(HaveOccurred())
		Expect(filepath.Join(home, ".claude", "skills", skill.Name, skill.Doc)).To(BeAnExistingFile())
	})

	It("installs into ./.claude/skills with --local", func() {
		GinkgoT().Chdir(root)

		Expect(c.run("skill", "install", "--local")).To(Equal(exitcode.OK))

		Expect(filepath.Join(root, ".claude", "skills", skill.Name, skill.Doc)).To(BeAnExistingFile())
	})

	It("refuses --local and --dir together", func() {
		Expect(c.run("skill", "install", "--local", "--dir", root)).To(Equal(exitcode.Usage))
	})

	// A machine with no project is exactly the machine a user most wants to run this
	// on: the skill is how their agent learns to configure the rest.
	It("needs no project, and creates no config file", func() {
		Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))

		_, err := os.Stat(c.configPath)
		Expect(err).To(MatchError(os.ErrNotExist))
	})

	// The read-only gate protects the tenant, not the user's home directory. A
	// read-only project that could not install a skill would be a project whose user
	// cannot ask an agent for help.
	It("is not gated by a read-only project", func() {
		t := c.readOnlyProject(true)

		Expect(c.run("skill", "install", "--dir", root, "--read-only")).To(Equal(exitcode.OK))

		Expect(t.calls).To(BeEmpty())
		Expect(filepath.Join(installed(root), skill.Doc)).To(BeAnExistingFile())
	})

	When("the skill is already installed", func() {
		BeforeEach(func() {
			Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))
			c = newCLI() // a fresh run, with fresh streams
		})

		// Installing again must be silent and must ask nothing: a provisioning script,
		// or an agent, will do it on every run.
		It("changes nothing, and asks nothing", func() {
			Expect(c.run("skill", "install", "--dir", root, "-o", "json")).To(Equal(exitcode.OK))

			var view skillView
			Expect(json.Unmarshal([]byte(c.out()), &view)).To(Succeed())
			for _, f := range view.Files {
				Expect(f.Status).To(Equal(skill.StatusUnchanged))
			}
		})

		It("does not rewrite a file it does not have to", func() {
			doc := filepath.Join(installed(root), skill.Doc)
			before, err := os.Stat(doc)
			Expect(err).NotTo(HaveOccurred())

			Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))

			after, err := os.Stat(doc)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.ModTime()).To(Equal(before.ModTime()), "an unchanged file was rewritten")
		})
	})

	When("the user has edited the installed skill", func() {
		var doc string

		BeforeEach(func() {
			Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))

			doc = filepath.Join(installed(root), skill.Doc)
			Expect(os.WriteFile(doc, []byte("mine\n"), 0o644)).To(Succeed())

			c = newCLI()
		})

		// An agent's shell is not a terminal, so this is the path an agent takes — and
		// an agent must not silently overwrite what a human wrote.
		It("refuses to replace it when there is no terminal to ask on", func() {
			code := c.run("skill", "install", "--dir", root)

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("--force"))
			Expect(readFile(doc)).To(Equal("mine\n"), "the edited file was overwritten anyway")
		})

		It("replaces it with --force", func() {
			Expect(c.run("skill", "install", "--dir", root, "--force", "-o", "json")).To(Equal(exitcode.OK))

			Expect(readFile(doc)).To(Equal(skill.Document()))

			var view skillView
			Expect(json.Unmarshal([]byte(c.out()), &view)).To(Succeed())
			Expect(view.Files).To(ContainElement(skill.Change{File: skill.Doc, Status: skill.StatusReplaced}))
		})

		It("asks, and replaces it on a yes", func() {
			c.answer("y")

			Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring("Replace"))
			Expect(readFile(doc)).To(Equal(skill.Document()))
		})

		It("leaves it alone on a no", func() {
			c.answer("n")

			Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring("Aborted"))
			Expect(readFile(doc)).To(Equal("mine\n"))
		})
	})

	// A reference file dropped in a later release must not survive in the user's
	// home, telling their agent about a command that no longer exists.
	When("a file fft no longer ships is there", func() {
		var stale string

		BeforeEach(func() {
			Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))

			stale = filepath.Join(installed(root), "references", "gone.md")
			Expect(os.WriteFile(stale, []byte("old\n"), 0o644)).To(Succeed())

			c = newCLI()
		})

		It("does not remove it without being told to", func() {
			Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.Usage))

			Expect(stale).To(BeAnExistingFile())
		})

		It("removes it with --force", func() {
			Expect(c.run("skill", "install", "--dir", root, "--force")).To(Equal(exitcode.OK))

			Expect(stale).NotTo(BeAnExistingFile())
		})
	})

	// --force means "replace my copy of the skill", not "delete whatever is at this
	// path" — and --dir comes from a shell, where a typo is one keystroke.
	It("refuses a directory that is not a skill, --force or not", func() {
		theirs := filepath.Join(installed(root), "notes.md")
		Expect(os.MkdirAll(filepath.Dir(theirs), 0o755)).To(Succeed())
		Expect(os.WriteFile(theirs, []byte("mine\n"), 0o644)).To(Succeed())

		Expect(c.run("skill", "install", "--dir", root, "--force")).To(Equal(exitcode.Usage))

		Expect(readFile(theirs)).To(Equal("mine\n"))
	})
})
