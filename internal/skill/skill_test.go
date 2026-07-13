package skill_test

import (
	"io/fs"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
	"github.com/Joessst-Dev/fft-cli/internal/skill"
	"github.com/Joessst-Dev/fft-cli/internal/testsupport"
)

// installed is the skill's own directory under a root.
func installed(root string) string { return filepath.Join(root, skill.Name) }

func doc(root string) string { return filepath.Join(installed(root), skill.Doc) }

func statuses(plan skill.Plan) map[string]skill.Status {
	out := make(map[string]skill.Status, len(plan.Files))
	for _, c := range plan.Files {
		out[c.File] = c.Status
	}
	return out
}

func evaluated(path string) string {
	GinkgoHelper()

	out, err := filepath.EvalSymlinks(path)
	Expect(err).NotTo(HaveOccurred())
	return out
}

func install(root string) skill.Plan {
	GinkgoHelper()

	plan, err := skill.NewPlan(root)
	Expect(err).NotTo(HaveOccurred())

	done, err := plan.Apply()
	Expect(err).NotTo(HaveOccurred())
	return done
}

var _ = Describe("the embedded skill", func() {
	It("has a SKILL.md and at least one reference", func() {
		var files []string
		Expect(fs.WalkDir(skill.FS(), ".", func(name string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			files = append(files, name)
			return nil
		})).To(Succeed())

		Expect(files).To(ContainElement(skill.Doc))
		Expect(files).To(ContainElement(HavePrefix("references/")))
	})

	It("parses into a frontmatter and a body", func() {
		meta, body, err := skill.Parse(skill.Document())
		Expect(err).NotTo(HaveOccurred())

		Expect(meta.Name).To(Equal(skill.Name))
		Expect(meta.Description).NotTo(BeEmpty())
		Expect(body).To(ContainSubstring("fft"))
	})

	DescribeTable("refuses a document that is not one",
		func(document string) {
			_, _, err := skill.Parse(document)
			Expect(err).To(HaveOccurred())
		},
		Entry("no frontmatter at all", "# fft\n"),
		Entry("an unclosed frontmatter", "---\nname: fft\n"),
		Entry("a frontmatter that is not YAML", "---\nname: [fft\n---\n\n# fft\n"),
	)
})

var _ = Describe("installing the skill", func() {
	var root string

	BeforeEach(func() { root = GinkgoT().TempDir() })

	It("writes every file, byte for byte", func() {
		done := install(root)

		Expect(done.Dir).To(Equal(installed(root)))
		Expect(done.Files).NotTo(BeEmpty())

		for _, c := range done.Files {
			Expect(c.Status).To(Equal(skill.StatusWritten))

			want, err := fs.ReadFile(skill.FS(), c.File)
			Expect(err).NotTo(HaveOccurred())

			got, err := os.ReadFile(filepath.Join(done.Dir, filepath.FromSlash(c.File)))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		}
	})

	// The skill is documentation, and 0600 is a credential's mode. A file the user's
	// editor cannot open is not much of a document.
	It("writes it readable, not private", func() {
		install(root)

		// Not through the Plan's own Dir: on Windows these assertions skip the spec,
		// which staticcheck reads as a call that does not return — and a value computed
		// above it as one that is never used.
		testsupport.ExpectReadableFile(doc(root))
		testsupport.ExpectReadableDir(installed(root))
		testsupport.ExpectReadableDir(filepath.Join(installed(root), "references"))
	})

	It("takes an absolute path from a relative root", func() {
		GinkgoT().Chdir(root)

		plan, err := skill.NewPlan(".")
		Expect(err).NotTo(HaveOccurred())

		Expect(filepath.IsAbs(plan.Dir)).To(BeTrue())
	})

	It("installs into a directory somebody has already created", func() {
		Expect(os.MkdirAll(installed(root), 0o755)).To(Succeed())

		Expect(doc(install(root).Dir)).NotTo(BeEmpty())
		Expect(doc(root)).To(BeAnExistingFile())
	})

	When("it is already installed", func() {
		BeforeEach(func() { install(root) })

		It("has nothing to do", func() {
			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())

			Expect(plan.Pending()).To(BeEmpty())
			for _, c := range plan.Files {
				Expect(c.Status).To(Equal(skill.StatusUnchanged))
			}
		})

		It("rewrites nothing", func() {
			before, err := os.Stat(doc(root))
			Expect(err).NotTo(HaveOccurred())

			install(root)

			after, err := os.Stat(doc(root))
			Expect(err).NotTo(HaveOccurred())
			Expect(after.ModTime()).To(Equal(before.ModTime()))
		})
	})

	When("a file has been edited", func() {
		BeforeEach(func() {
			install(root)
			Expect(os.WriteFile(doc(root), []byte("mine\n"), 0o644)).To(Succeed())
		})

		It("reports it as a conflict, and only it", func() {
			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())

			Expect(plan.Pending()).To(Equal([]skill.Change{{File: skill.Doc, Status: skill.StatusConflict}}))
		})

		It("replaces it, and says so", func() {
			done := install(root)

			Expect(statuses(done)[skill.Doc]).To(Equal(skill.StatusReplaced))
			Expect(os.ReadFile(doc(root))).To(BeEquivalentTo(skill.Document()))
		})
	})

	// A reference file dropped in a later release would otherwise sit in the user's
	// home telling their agent about a command that no longer exists — and the agent
	// has no way to know which of the two documents is the stale one.
	When("a file fft no longer ships is there", func() {
		var stale string

		BeforeEach(func() {
			install(root)

			stale = filepath.Join(installed(root), "references", "gone.md")
			Expect(os.WriteFile(stale, []byte("old\n"), 0o644)).To(Succeed())
		})

		It("plans to remove it", func() {
			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())

			Expect(plan.Pending()).To(ContainElement(skill.Change{
				File:   "references/gone.md",
				Status: skill.StatusStale,
			}))
		})

		It("removes it", func() {
			done := install(root)

			Expect(statuses(done)["references/gone.md"]).To(Equal(skill.StatusRemoved))
			Expect(stale).NotTo(BeAnExistingFile())

			// And takes nothing of fft's with it.
			Expect(doc(root)).To(BeAnExistingFile())
		})
	})

	// The invariant: Apply removes exactly what the plan named. An install that
	// reports no changes has made none — so nothing may be swept up on the side,
	// however confident fft is that the user did not want it.
	When("the user keeps files of their own in the skill directory", func() {
		var notes, empty string

		BeforeEach(func() {
			install(root)

			notes = filepath.Join(installed(root), "notes", "mine.md")
			Expect(os.MkdirAll(filepath.Dir(notes), 0o755)).To(Succeed())
			Expect(os.WriteFile(notes, []byte("mine\n"), 0o644)).To(Succeed())

			// An empty directory of the user's. Nothing fft ships would put one here, and
			// that is exactly why fft must not be the one to decide it is rubbish.
			empty = filepath.Join(installed(root), "scratch")
			Expect(os.Mkdir(empty, 0o755)).To(Succeed())
		})

		It("names the file in the plan rather than quietly removing it", func() {
			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())

			Expect(plan.Pending()).To(ConsistOf(skill.Change{
				File:   "notes/mine.md",
				Status: skill.StatusStale,
			}))
		})

		It("leaves their empty directory alone", func() {
			install(root)

			Expect(empty).To(BeADirectory())
		})

		It("removes the directory it emptied itself, and no other", func() {
			install(root)

			Expect(notes).NotTo(BeAnExistingFile())
			Expect(filepath.Dir(notes)).NotTo(BeADirectory(), "the directory pruning emptied was left behind")
			Expect(empty).To(BeADirectory())
			Expect(doc(root)).To(BeAnExistingFile())
		})
	})

	// The dotfiles arrangement: ~/.claude/skills/fft is a link to a directory kept
	// somewhere else. fft installs *through* the link. What it must never do is
	// mistake the link for a file it does not ship and prune it — writing the skill
	// correctly and then deleting the only thing that pointed at it.
	When("the skill directory is a symlink", func() {
		var target string

		BeforeEach(func() {
			target = filepath.Join(GinkgoT().TempDir(), "elsewhere")
			Expect(os.MkdirAll(target, 0o755)).To(Succeed())
			Expect(os.Symlink(target, installed(root))).To(Succeed())
		})

		It("installs through it, and does not delete it", func() {
			done := install(root)

			Expect(filepath.Join(target, skill.Doc)).To(BeAnExistingFile())

			// Compared resolved, because a temp directory is itself behind a symlink on
			// macOS (/var -> /private/var) and the point here is the directory, not the
			// spelling of it.
			Expect(done.Dir).To(Equal(evaluated(target)))

			// The link itself, which pruning came within one bug of removing.
			link, err := os.Lstat(installed(root))
			Expect(err).NotTo(HaveOccurred())
			Expect(link.Mode() & os.ModeSymlink).NotTo(BeZero())
		})

		It("is idempotent through it", func() {
			install(root)

			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())
			Expect(plan.Pending()).To(BeEmpty())
		})
	})

	// The same bug as the symlinked skill root, one level down — and worse, because it
	// reported success: the reference files were written *through* the link, and then
	// the link was pruned as a file fft does not ship. What was left was a SKILL.md
	// every one of whose links was dead.
	When("references/ is a symlink", func() {
		var target string

		BeforeEach(func() {
			install(root)

			target = filepath.Join(GinkgoT().TempDir(), "refs")
			Expect(os.MkdirAll(target, 0o755)).To(Succeed())
			Expect(os.RemoveAll(filepath.Join(installed(root), "references"))).To(Succeed())
			Expect(os.Symlink(target, filepath.Join(installed(root), "references"))).To(Succeed())
		})

		It("writes through it, and does not prune it", func() {
			install(root)

			link, err := os.Lstat(filepath.Join(installed(root), "references"))
			Expect(err).NotTo(HaveOccurred())
			Expect(link.Mode()&os.ModeSymlink).NotTo(BeZero(), "the symlinked references/ was pruned")

			Expect(filepath.Join(target, "recipes.md")).To(BeAnExistingFile())

			// The whole point: every link in SKILL.md still resolves.
			Expect(filepath.Join(installed(root), "references", "recipes.md")).To(BeAnExistingFile())
		})

		It("plans no removal at all", func() {
			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())

			for _, c := range plan.Files {
				Expect(c.Status).NotTo(Equal(skill.StatusStale))
			}
		})
	})

	// A first install killed between the temporary file and the rename leaves a
	// .tmp-* and no SKILL.md. fft made that file, so fft does not get to call it
	// evidence of a stranger and refuse the directory for ever — which is what it did,
	// with no way out but a manual rm of a file the user had never heard of.
	When("an interrupted install left its temporary file behind", func() {
		var tmp string

		BeforeEach(func() {
			Expect(os.MkdirAll(installed(root), 0o755)).To(Succeed())

			tmp = filepath.Join(installed(root), atomicfile.TempPrefix+"123456")
			Expect(os.WriteFile(tmp, []byte("half\n"), 0o600)).To(Succeed())
		})

		It("installs over it rather than refusing the directory", func() {
			done := install(root)

			Expect(doc(root)).To(BeAnExistingFile())
			Expect(tmp).NotTo(BeAnExistingFile())
			Expect(statuses(done)).To(HaveKeyWithValue(filepath.Base(tmp), skill.StatusRemoved))
		})

		// Its removal is reported, but it is not a question: asking the user to consent
		// to deleting a file they never wrote and cannot identify teaches them only to
		// say yes without reading.
		It("does not ask about it", func() {
			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())

			Expect(plan.Pending()).To(BeEmpty())
		})
	})

	// --force means "replace my copy of the skill", not "delete whatever is at this
	// path", and the path can come from a shell where a typo is one keystroke.
	When("the directory is not a skill", func() {
		It("refuses one holding somebody else's files", func() {
			Expect(os.MkdirAll(installed(root), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(installed(root), "notes.md"), []byte("mine\n"), 0o644)).To(Succeed())

			_, err := skill.NewPlan(root)
			Expect(err).To(MatchError(skill.ErrNotSkill))
		})

		It("refuses a file where the directory should be", func() {
			Expect(os.WriteFile(installed(root), []byte("mine\n"), 0o644)).To(Succeed())

			_, err := skill.NewPlan(root)
			Expect(err).To(MatchError(skill.ErrNotSkill))
		})
	})

	// atomicfile's contract, through the skill: a write that cannot complete leaves
	// the file that was there intact. A SKILL.md truncated half way is a skill that
	// lies to an agent, which is worse than one that was never replaced.
	It("fails, without destroying what was there, when the directory cannot be written", func() {
		install(root)
		Expect(os.WriteFile(doc(root), []byte("mine\n"), 0o644)).To(Succeed())

		// The skill's own directory, not the root above it: creating a subdirectory in
		// an unwritable directory still succeeds on Windows, so a root made unwritable
		// would not stop the write — it would only move it.
		testsupport.MakeUnwritableDir(installed(root))

		plan, err := skill.NewPlan(root)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Pending()).NotTo(BeEmpty())

		_, err = plan.Apply()
		Expect(err).To(HaveOccurred())

		Expect(os.ReadFile(doc(root))).To(BeEquivalentTo("mine\n"))
	})
})
