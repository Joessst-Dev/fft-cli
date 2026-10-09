package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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

	// Falling back to the home directory here would install the skill somewhere the
	// user did not ask for and was not told about.
	It("refuses an empty --dir rather than choosing for the user", func() {
		Expect(c.run("skill", "install", "--dir", "")).To(Equal(exitcode.Usage))

		home, err := os.UserHomeDir()
		Expect(err).NotTo(HaveOccurred())
		Expect(filepath.Join(home, ".claude")).NotTo(BeADirectory())
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

// installedVersion is the version stamped into the skill installed under root.
func installedVersion(root string) string {
	GinkgoHelper()

	meta, ok, err := skill.Installed(root)
	Expect(err).NotTo(HaveOccurred())
	Expect(ok).To(BeTrue(), "no skill is installed under %s", root)
	return meta.Version()
}

var _ = Describe("the skill's version", func() {
	var c *cli
	var root string

	BeforeEach(func() {
		c = newCLI()
		root = GinkgoT().TempDir()
	})

	It("is written into the installed SKILL.md", func() {
		c.asVersion("1.4.0")

		Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))

		Expect(installedVersion(root)).To(Equal("1.4.0"))
	})

	It("is part of what fft skill show -o json reports", func() {
		c.asVersion("1.4.0")

		Expect(c.run("skill", "show", "-o", "json")).To(Equal(exitcode.OK))

		var view skillDocView
		Expect(json.Unmarshal([]byte(c.out()), &view)).To(Succeed())
		Expect(view.Version).To(Equal("1.4.0"))
		Expect(view.Content).To(ContainSubstring(`version: "1.4.0"`))
	})

	// The notice tells people to run `fft skill install`. If that then demanded
	// --force for a file whose only difference is the stamp fft itself wrote, the
	// advice would lead straight to a refusal — and an agent, which has no terminal,
	// would hit exactly that refusal every time.
	When("an older fft installed the skill", func() {
		BeforeEach(func() {
			c.asVersion("1.3.0")
			Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))
			c.asVersion("1.4.0")
		})

		It("updates it without a terminal and without --force", func() {
			Expect(c.run("skill", "install", "--dir", root, "-o", "json")).To(Equal(exitcode.OK))

			var view skillView
			Expect(json.Unmarshal([]byte(c.out()), &view)).To(Succeed())
			Expect(view.Files).To(ContainElement(skill.Change{File: skill.Doc, Status: skill.StatusUpdated}))

			Expect(installedVersion(root)).To(Equal("1.4.0"))
		})

		// What really happens on upgrade: the release changed the skill's text, and
		// the install still holds the old text, untouched. The record of what fft wrote
		// is what tells that apart from an edit.
		When("its text differs from this one's, untouched", func() {
			BeforeEach(func() {
				olderSkillText(root, "references/commands.md", "old commands\n")
			})

			It("is reported OUTDATED, and updated without a terminal or --force", func() {
				Expect(c.run("skill", "status", "--dir", root, "-o", "json")).To(Equal(exitcode.OK))
				Expect(c.out()).To(ContainSubstring(`"status": "OUTDATED"`))

				Expect(c.run("skill", "install", "--dir", root, "-o", "json")).To(Equal(exitcode.OK))

				var view skillView
				Expect(json.Unmarshal([]byte(c.out()), &view)).To(Succeed())
				Expect(view.Files).To(ContainElements(
					skill.Change{File: skill.Doc, Status: skill.StatusUpdated},
					skill.Change{File: "references/commands.md", Status: skill.StatusUpdated},
				))
			})

			It("still refuses to overwrite it once the user has edited it", func() {
				path := filepath.Join(installed(root), "references", "commands.md")
				Expect(os.WriteFile(path, []byte("mine\n"), 0o644)).To(Succeed())

				Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.Usage))

				Expect(c.errOut()).To(ContainSubstring("CONFLICT: references/commands.md"))
				Expect(readFile(path)).To(Equal("mine\n"))
			})
		})

		It("still refuses to overwrite an edit made since", func() {
			doc := filepath.Join(installed(root), skill.Doc)
			Expect(os.WriteFile(doc, []byte(readFile(doc)+"\nmine\n"), 0o644)).To(Succeed())

			Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.Usage))

			Expect(c.errOut()).To(ContainSubstring("--force"))
			Expect(readFile(doc)).To(HaveSuffix("\nmine\n"))
		})
	})
})

var _ = Describe("fft skill status", func() {
	var c *cli
	var root string

	BeforeEach(func() {
		c = newCLI()
		root = GinkgoT().TempDir()

		// The project location is the working directory's, and the package directory
		// is not a place a spec should be reading .claude/ out of.
		GinkgoT().Chdir(GinkgoT().TempDir())
	})

	// status is the one row a spec cares about, read through -o json so that the
	// assertion is on the state and not on the table's padding.
	status := func(args ...string) skillStatusView {
		GinkgoHelper()

		Expect(c.run(append([]string{"skill", "status", "-o", "json"}, args...)...)).To(Equal(exitcode.OK))

		var view skillStatusView
		Expect(json.Unmarshal([]byte(c.out()), &view)).To(Succeed())
		return view
	}

	It("reports nothing installed as MISSING, and says how to install it", func() {
		Expect(status("--dir", root).Installs).To(ConsistOf(skillInstall{
			Scope: "dir", Dir: installed(root), Status: skillMissing,
		}))

		Expect(c.run("skill", "status", "--dir", root)).To(Equal(exitcode.OK))
		Expect(c.errOut()).To(ContainSubstring("run fft skill install --dir " + pastePath(root)))
	})

	It("reports a skill this fft installed as CURRENT", func() {
		c.asVersion("1.4.0")
		Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))

		view := status("--dir", root)

		Expect(view.FFT).To(Equal("1.4.0"))
		Expect(view.Installs).To(ConsistOf(skillInstall{
			Scope: "dir", Dir: installed(root), Version: "1.4.0", Status: skillCurrent,
		}))
	})

	When("another fft installed the skill", func() {
		BeforeEach(func() {
			c.asVersion("1.3.0")
			Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))
			c.asVersion("1.4.0")
		})

		It("reports it as OUTDATED, and still exits 0", func() {
			Expect(status("--dir", root).Installs).To(ConsistOf(
				HaveField("Status", skillOutdated)))
			Expect(status("--dir", root).Installs[0].Version).To(Equal("1.3.0"))
		})

		It("names the command that brings it up to date, on stderr", func() {
			Expect(c.run("skill", "status", "--dir", root)).To(Equal(exitcode.OK))

			Expect(c.out()).To(MatchRegexp(`(?m)^dir\s+\S+\s+1\.3\.0\s+OUTDATED\s*$`))
			Expect(c.out()).NotTo(ContainSubstring("run fft skill install"))
			Expect(c.errOut()).To(ContainSubstring("is from fft 1.3.0 (you have 1.4.0) — run fft skill install --dir " + pastePath(root)))
		})

		It("reports it CURRENT once reinstalled", func() {
			Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))

			Expect(status("--dir", root).Installs).To(ConsistOf(HaveField("Status", skillCurrent)))
		})

		// A build that is not a release has nothing to hold the skill to — the same
		// rule as the update check.
		It("does not compare it on a dev build", func() {
			c.asVersion("dev")

			Expect(status("--dir", root).Installs).To(ConsistOf(HaveField("Status", skillCurrent)))
		})
	})

	// Installs made before fft stamped its skills: the one case that most needs the
	// hint, because nothing about them says how old they are.
	It("reports a skill with no version as OUTDATED, from an older fft", func() {
		c.asVersion("1.4.0")
		Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))

		unstamped, err := fs.ReadFile(skill.FS(), skill.Doc)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(installed(root), skill.Doc), unstamped, 0o644)).To(Succeed())

		Expect(status("--dir", root).Installs).To(ConsistOf(skillInstall{
			Scope: "dir", Dir: installed(root), Status: skillOutdated,
		}))

		Expect(c.run("skill", "status", "--dir", root)).To(Equal(exitcode.OK))
		Expect(c.errOut()).To(ContainSubstring("is from an older fft"))
	})

	It("reports a skill edited since this fft installed it as MODIFIED", func() {
		c.asVersion("1.4.0")
		Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))
		Expect(os.WriteFile(filepath.Join(installed(root), "references", "mine.md"), []byte("mine\n"), 0o644)).To(Succeed())

		Expect(status("--dir", root).Installs).To(ConsistOf(skillInstall{
			Scope: "dir", Dir: installed(root), Version: "1.4.0", Status: skillModified,
		}))

		Expect(c.run("skill", "status", "--dir", root)).To(Equal(exitcode.OK))
		Expect(c.errOut()).To(ContainSubstring("--force"))
	})

	It("reports a directory that is not fft's as NOT_SKILL", func() {
		Expect(os.MkdirAll(installed(root), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(installed(root), "notes.md"), []byte("mine\n"), 0o644)).To(Succeed())

		Expect(status("--dir", root).Installs).To(ConsistOf(skillInstall{
			Scope: "dir", Dir: installed(root), Status: skillNotSkill,
		}))
	})

	It("looks in both the personal and the project location by default", func() {
		Expect(c.run("skill", "install")).To(Equal(exitcode.OK))

		Expect(c.run("skill", "status")).To(Equal(exitcode.OK))

		Expect(c.out()).To(MatchRegexp(`(?m)^SCOPE\s+DIR\s+VERSION\s+STATUS\s*$`))
		Expect(c.out()).To(MatchRegexp(`(?m)^user\s+\S+\s+dev\s+CURRENT\s*$`))
		Expect(c.out()).To(MatchRegexp(`(?m)^project\s+\S+\s+-\s+MISSING\s*$`))

		// One skill is installed, which is the usual arrangement — not something to
		// be told off about.
		Expect(c.errOut()).To(BeEmpty())
	})

	It("looks only in the project location with --local", func() {
		Expect(c.run("skill", "install", "--local")).To(Equal(exitcode.OK))

		Expect(status("--local").Installs).To(ConsistOf(And(
			HaveField("Scope", "project"),
			HaveField("Status", skillCurrent),
		)))
	})

	// In the home directory the project location *is* the personal one.
	It("reports the home directory's skill once, not twice", func() {
		home, err := os.UserHomeDir()
		Expect(err).NotTo(HaveOccurred())
		GinkgoT().Chdir(home)

		Expect(status().Installs).To(ConsistOf(HaveField("Scope", "user")))
	})

	It("reports a project skill that links to the personal one once", func() {
		Expect(c.run("skill", "install")).To(Equal(exitcode.OK))
		home, err := os.UserHomeDir()
		Expect(err).NotTo(HaveOccurred())
		linkToPersonalSkill(home)

		Expect(status().Installs).To(ConsistOf(HaveField("Scope", "user")))
	})

	// The hint is a command line to paste, and a path with a space in it pasted bare
	// is two arguments.
	It("quotes a --dir that a shell would split", func() {
		spaced := filepath.Join(root, "my skills")

		Expect(c.run("skill", "status", "--dir", spaced)).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("run fft skill install --dir " + pastePath(spaced)))
		Expect(pastePath(spaced)).NotTo(Equal(spaced))
	})

	// cmd.exe has no single quotes, and PowerShell reads a backslash inside double
	// quotes as itself, so a Windows path is double-quoted and nothing in it escaped.
	DescribeTable("spells a --dir for the shell the path came from",
		func(path string, windows bool, want string) {
			Expect(pastePathFor(path, windows)).To(Equal(want))
		},
		Entry("a plain POSIX path, bare", "/home/me/skills", false, "/home/me/skills"),
		Entry("a POSIX path with a space, single-quoted", "/home/me/my skills", false, "'/home/me/my skills'"),
		Entry("a plain Windows path, bare", `C:\Users\me\skills`, true, `C:\Users\me\skills`),
		Entry("a Windows path with a space, double-quoted and unescaped",
			`C:\Users\me\my skills`, true, `"C:\Users\me\my skills"`),
	)

	// A report about two places must not lose one to the other's trouble, and must
	// not fail: an unreadable skill is a finding.
	It("reports a location it cannot read as UNREADABLE, and still exits 0", func() {
		if runtime.GOOS == "windows" {
			Skip("permission bits do not make a directory unreadable on Windows")
		}
		Expect(c.run("skill", "install", "--dir", root)).To(Equal(exitcode.OK))
		Expect(os.Chmod(installed(root), 0o000)).To(Succeed())
		DeferCleanup(os.Chmod, installed(root), os.FileMode(0o755))
		if _, err := os.ReadDir(installed(root)); err == nil {
			Skip("running as a user that permission bits do not stop")
		}

		Expect(status("--dir", root).Installs).To(ConsistOf(And(
			HaveField("Status", skillUnreadable),
			HaveField("Error", ContainSubstring("permission denied")),
		)))

		Expect(c.run("skill", "status", "--dir", root)).To(Equal(exitcode.OK))
		Expect(c.out()).To(MatchRegexp(`(?m)^dir\s+\S+\s+-\s+UNREADABLE\s*$`))
		Expect(strings.Count(c.errOut(), installed(root))).To(Equal(1), "the path is named twice: %s", c.errOut())
	})

	It("refuses --local and --dir together", func() {
		Expect(c.run("skill", "status", "--local", "--dir", root)).To(Equal(exitcode.Usage))
	})

	It("needs no project, and creates no config file", func() {
		Expect(c.run("skill", "status")).To(Equal(exitcode.OK))

		_, err := os.Stat(c.configPath)
		Expect(err).To(MatchError(os.ErrNotExist))
	})
})

var _ = Describe("the stale-skill notice", func() {
	const notice = "The fft skill in "

	var c *cli

	BeforeEach(func() {
		c = newCLI()

		// The project location is the working directory's: without this the notice
		// would read cmd/fft/.claude out of the source tree.
		GinkgoT().Chdir(GinkgoT().TempDir())

		c.asVersion("1.3.0")
		Expect(c.run("skill", "install")).To(Equal(exitcode.OK))

		// `fft facility delete` is the vehicle for the same reason it is for the update
		// notice: it writes nothing to stdout, so stdout can be asserted byte-empty.
		c.fakeAPI(deletedOK)

		// A release build with a terminal on stderr — the update notice's own
		// conditions — and a fresh cache that already knows this release is the
		// latest, so that no request leaves and the update notice keeps quiet.
		c.fakeGitHub(latestRelease)
		c.cachedRelease(testVersion, time.Hour)

		c.asVersion("1.4.0")
	})

	It("names the stale skill and the fix on stderr, and leaves stdout byte-empty", func() {
		Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

		home, err := os.UserHomeDir()
		Expect(err).NotTo(HaveOccurred())
		Expect(c.errOut()).To(ContainSubstring(
			notice + filepath.Join(home, ".claude", "skills", skill.Name) +
				" is from fft 1.3.0 (you have 1.4.0) — run fft skill install"))

		Expect(c.out()).To(BeEmpty())
	})

	It("tells a project skill to reinstall with --local", func() {
		project := GinkgoT().TempDir()
		GinkgoT().Chdir(project)
		c.asVersion("1.3.0")
		Expect(c.run("skill", "install", "--local")).To(Equal(exitcode.OK))
		c.asVersion("1.4.0")

		Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("run fft skill install --local"))
	})

	// A project whose .claude/skills/fft is a link to the personal skill has one
	// skill, and hears about it once.
	It("names a skill reached through a symlink once", func() {
		home, err := os.UserHomeDir()
		Expect(err).NotTo(HaveOccurred())
		linkToPersonalSkill(home)

		Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

		Expect(strings.Count(c.errOut(), notice)).To(Equal(1), c.errOut())
	})

	It("says nothing once the skill has been reinstalled", func() {
		Expect(c.run("skill", "install")).To(Equal(exitcode.OK))

		Expect(c.run("facility", "delete", "BER-01", "--yes")).To(Equal(exitcode.OK))

		Expect(c.errOut()).NotTo(ContainSubstring(notice))
	})

	DescribeTable("says nothing",
		func(setup func(), args ...string) {
			setup()

			Expect(c.run(args...)).To(Equal(exitcode.OK))

			Expect(c.errOut()).NotTo(ContainSubstring(notice))
		},
		Entry("on a dev build",
			func() { c.asVersion("dev") },
			"facility", "delete", "BER-01", "--yes"),
		Entry("under -o json",
			func() {},
			"facility", "delete", "BER-01", "--yes", "-o", "json"),
		Entry("with FFT_NO_UPDATE_CHECK set",
			func() { c.setenv(envNoUpdateCheck, "1") },
			"facility", "delete", "BER-01", "--yes"),
		Entry("when stderr is not a terminal",
			func() { c.deps.Terminal = ptr(false) },
			"facility", "delete", "BER-01", "--yes"),
		Entry("under fft skill show, which is about the skill already",
			func() {},
			"skill", "show"),
	)

	// status words its hint exactly as the notice is worded, so the notice coming
	// on top of it would be the same line twice.
	It("leaves fft skill status to say it, once", func() {
		Expect(c.run("skill", "status")).To(Equal(exitcode.OK))

		Expect(strings.Count(c.errOut(), notice)).To(Equal(1), c.errOut())
	})
})

// olderSkillText puts data at name in the skill installed under root, and records
// in the install's manifest that fft wrote it — what a release whose text differed
// from this one's leaves behind.
func olderSkillText(root, name, data string) {
	GinkgoHelper()

	path := filepath.Join(installed(root), skill.ManifestName)
	var m struct {
		Version string            `json:"version"`
		Files   map[string]string `json:"files"`
	}
	Expect(json.Unmarshal([]byte(readFile(path)), &m)).To(Succeed())

	sum := sha256.Sum256([]byte(data))
	m.Files[name] = "sha256:" + hex.EncodeToString(sum[:])

	raw, err := json.Marshal(m)
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(path, raw, 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(installed(root), filepath.FromSlash(name)), []byte(data), 0o644)).To(Succeed())
}

// linkToPersonalSkill makes the working directory's .claude/skills/fft a symlink
// to the one in home — the dotfiles arrangement that puts one skill in both
// places Claude Code looks.
func linkToPersonalSkill(home string) {
	GinkgoHelper()

	wd, err := os.Getwd()
	Expect(err).NotTo(HaveOccurred())

	project := filepath.Join(wd, ".claude", "skills")
	Expect(os.MkdirAll(project, 0o755)).To(Succeed())
	Expect(os.Symlink(installed(filepath.Join(home, ".claude", "skills")), installed(project))).To(Succeed())
}
