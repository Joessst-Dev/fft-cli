package skill_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/atomicfile"
	"github.com/Joessst-Dev/fft-cli/internal/buildinfo"
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

// asVersion is the ldflags-stamped version, for the duration of the spec. Specs
// in a Ginkgo suite run one at a time in one process, so the package variable is
// the spec's alone until DeferCleanup puts it back.
func asVersion(v string) {
	previous := buildinfo.Version
	buildinfo.Version = v
	DeferCleanup(func() { buildinfo.Version = previous })
}

// shipped is what fft writes for one file: SKILL.md as the stamped document, the
// rest straight from the embed.
func shipped(name string) []byte {
	GinkgoHelper()

	if name == skill.Doc {
		return []byte(skill.Document())
	}
	data, err := fs.ReadFile(skill.FS(), name)
	Expect(err).NotTo(HaveOccurred())
	return data
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

	It("is stamped with the fft that built it", func() {
		asVersion("1.4.0")

		meta, _, err := skill.Parse(skill.Document())
		Expect(err).NotTo(HaveOccurred())
		Expect(meta.Version()).To(Equal("1.4.0"))
	})

	It("is stamped dev by a build that did not come from a release tag", func() {
		asVersion("dev")

		meta, _, err := skill.Parse(skill.Document())
		Expect(err).NotTo(HaveOccurred())
		Expect(meta.Version()).To(Equal("dev"))
	})

	// The stamp is inserted as text, below whatever metadata the file already has.
	// One of its own would leave the frontmatter with two metadata keys — which YAML
	// refuses outright — so the shipped file must not grow one.
	It("carries no metadata of its own in the repository", func() {
		raw, err := fs.ReadFile(skill.FS(), skill.Doc)
		Expect(err).NotTo(HaveOccurred())

		meta, _, err := skill.Parse(string(raw))
		Expect(err).NotTo(HaveOccurred())
		Expect(meta.Metadata).To(BeNil())
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

			got, err := os.ReadFile(filepath.Join(done.Dir, filepath.FromSlash(c.File)))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(shipped(c.File)))
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

	// The upgrade path. Every release changes SKILL.md's bytes, if only in the stamp,
	// so without this the first reinstall after `brew upgrade` would call fft's own
	// file the user's edit and refuse to touch it without --force.
	When("an older fft installed it", func() {
		BeforeEach(func() {
			asVersion("1.3.0")
			install(root)
			asVersion("1.4.0")
		})

		It("plans to restamp SKILL.md without asking", func() {
			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())

			Expect(statuses(plan)).To(HaveKeyWithValue(skill.Doc, skill.StatusOutdated))
			Expect(plan.Pending()).To(BeEmpty())
		})

		It("updates it, and says so", func() {
			done := install(root)

			Expect(statuses(done)).To(HaveKeyWithValue(skill.Doc, skill.StatusUpdated))
			Expect(os.ReadFile(doc(root))).To(BeEquivalentTo(skill.Document()))

			meta, ok, err := skill.Installed(root)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			Expect(meta.Version()).To(Equal("1.4.0"))
		})

		It("is not current until it has been reinstalled", func() {
			meta, _, err := skill.Installed(root)
			Expect(err).NotTo(HaveOccurred())
			Expect(skill.Current(meta)).To(BeFalse())

			install(root)

			meta, _, err = skill.Installed(root)
			Expect(err).NotTo(HaveOccurred())
			Expect(skill.Current(meta)).To(BeTrue())
		})

		// The stamp is left out of the comparison, and nothing else is: an edit made
		// under an old version is still the user's edit.
		It("still calls an edit a conflict", func() {
			edited, err := os.ReadFile(doc(root))
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(doc(root), append(edited, "\nmine\n"...), 0o644)).To(Succeed())

			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())
			Expect(plan.Pending()).To(Equal([]skill.Change{{File: skill.Doc, Status: skill.StatusConflict}}))
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

	// A file of the skill that is a link to the user's dotfiles. Writing over it is
	// an atomic rename, which replaces the *link* with a regular file and leaves the
	// dotfiles copy behind with the old text — so whatever its target holds, a link
	// that differs from what fft ships is the user's to decide about, never a
	// change made without asking.
	When("a file of the skill is a symlink", func() {
		var target string

		// linked moves name out of the skill into a directory of the user's and links
		// it back, the way a dotfiles manager does.
		linked := func(name string) string {
			GinkgoHelper()

			target = filepath.Join(GinkgoT().TempDir(), filepath.Base(name))
			path := filepath.Join(installed(root), filepath.FromSlash(name))
			Expect(os.Rename(path, target)).To(Succeed())
			Expect(os.Symlink(target, path)).To(Succeed())
			return path
		}

		expectLinkConflict := func(name string) {
			GinkgoHelper()

			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())
			Expect(plan.Pending()).To(ConsistOf(skill.Change{File: name, Status: skill.StatusConflict}))
		}

		BeforeEach(func() { install(root) })

		It("leaves a link to exactly what fft ships alone", func() {
			path := linked("references/commands.md")

			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())
			Expect(statuses(plan)).To(HaveKeyWithValue("references/commands.md", skill.StatusUnchanged))

			install(root)
			link, err := os.Lstat(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(link.Mode() & os.ModeSymlink).NotTo(BeZero())
		})

		It("asks about a link to text the manifest says fft wrote", func() {
			linked("references/commands.md")
			olderText(root, "references/commands.md", "old commands\n")

			expectLinkConflict("references/commands.md")
		})

		It("asks about a link to text a release before manifests shipped", func() {
			linked("references/discovery.md")
			old, err := os.ReadFile(filepath.Join("testdata", "v0.8.0", "references", "discovery.md"))
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(target, old, 0o644)).To(Succeed())

			expectLinkConflict("references/discovery.md")
		})

		It("asks about a linked SKILL.md that differs only in its stamp", func() {
			asVersion("1.3.0")
			install(root)
			linked(skill.Doc)
			Expect(os.Remove(filepath.Join(installed(root), skill.ManifestName))).To(Succeed())
			asVersion("1.4.0")

			expectLinkConflict(skill.Doc)
		})
	})

	// The blast radius of a prune. A stray that is a symlink is removed — it is not a
	// file fft ships — but what it *points at* is none of fft's business, and may be
	// anywhere at all. This is what the os.Remove in Apply buys, and what an os.RemoveAll
	// or a resolve-then-delete would quietly take away.
	When("a stray is a symlink to a file outside the skill", func() {
		var link, outside string

		BeforeEach(func() {
			install(root)

			outside = filepath.Join(GinkgoT().TempDir(), "secret.txt")
			Expect(os.WriteFile(outside, []byte("SECRET\n"), 0o600)).To(Succeed())

			link = filepath.Join(installed(root), "notes.md")
			Expect(os.Symlink(outside, link)).To(Succeed())
		})

		It("removes the link and not the file it points at", func() {
			done := install(root)

			Expect(statuses(done)).To(HaveKeyWithValue("notes.md", skill.StatusRemoved))
			Expect(link).NotTo(BeAnExistingFile())

			Expect(os.ReadFile(outside)).To(BeEquivalentTo("SECRET\n"))
		})

		It("asks first: it is a file of theirs, wherever it really lives", func() {
			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())

			Expect(plan.Pending()).To(ConsistOf(skill.Change{File: "notes.md", Status: skill.StatusStale}))
		})
	})

	// It points at nothing, so there is nothing it could be shadowing and nothing to
	// lose by taking it away.
	It("prunes a dangling symlink", func() {
		install(root)

		link := filepath.Join(installed(root), "gone.md")
		Expect(os.Symlink(filepath.Join(root, "nowhere"), link)).To(Succeed())

		done := install(root)

		Expect(statuses(done)).To(HaveKeyWithValue("gone.md", skill.StatusRemoved))
		Expect(doc(root)).To(BeAnExistingFile())
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

var _ = Describe("the version stamp", func() {
	const doc = "---\nname: fft\ndescription: Drive fft.\n---\n\n# fft\n"

	It("goes into the frontmatter, and reads back", func() {
		stamped := string(skill.Stamp([]byte(doc), "1.10.0"))

		Expect(stamped).To(Equal("---\nname: fft\ndescription: Drive fft.\nmetadata:\n  version: \"1.10.0\"\n---\n\n# fft\n"))

		meta, body, err := skill.Parse(stamped)
		Expect(err).NotTo(HaveOccurred())
		Expect(meta.Version()).To(Equal("1.10.0"), "a version YAML read as a number")
		Expect(meta.Description).To(Equal("Drive fft."))
		Expect(body).To(Equal("# fft\n"))
	})

	It("comes out again, exactly", func() {
		Expect(skill.Unstamp(skill.Stamp([]byte(doc), "1.4.0"))).To(BeEquivalentTo(doc))
	})

	It("leaves a document without a frontmatter alone", func() {
		Expect(skill.Stamp([]byte("# fft\n"), "1.4.0")).To(BeEquivalentTo("# fft\n"))
	})

	It("reads as no version in a skill from before stamping", func() {
		meta, _, err := skill.Parse(doc)
		Expect(err).NotTo(HaveOccurred())
		Expect(meta.Version()).To(BeEmpty())
	})

	// unstamp decides what may be overwritten without asking, so anything it is
	// not certain fft wrote must come through untouched.
	DescribeTable("removes nothing it did not write",
		func(document string) {
			Expect(skill.Unstamp([]byte(document))).To(BeEquivalentTo(document))
		},
		Entry("a metadata block with more than the version",
			"---\nname: fft\nmetadata:\n  version: \"1.4.0\"\n  owner: me\n---\n"),
		Entry("an unquoted version",
			"---\nname: fft\nmetadata:\n  version: 1.4.0\n---\n"),
		Entry("a version that is not last in the frontmatter",
			"---\nmetadata:\n  version: \"1.4.0\"\nname: fft\n---\n"),
		Entry("a version line in the body",
			"---\nname: fft\n---\nmetadata:\n  version: \"1.4.0\"\n---\n"),
		// An editor that rewrote the line endings has edited the file, and fft did not
		// write a single \r.
		Entry("a document with CRLF line endings",
			"---\r\nname: fft\r\nmetadata:\r\n  version: \"1.4.0\"\r\n---\r\n\r\n# fft\r\n"),
		Entry("a document whose closing fence has no newline after it",
			"---\nname: fft\nmetadata:\n  version: \"1.4.0\"\n---"),
	)

	DescribeTable("adds nothing to a frontmatter it cannot find",
		func(document string) {
			Expect(skill.Stamp([]byte(document), "1.4.0")).To(BeEquivalentTo(document))
		},
		Entry("CRLF line endings", "---\r\nname: fft\r\n---\r\n\r\n# fft\r\n"),
		Entry("no newline after the closing fence", "---\nname: fft\n---"),
	)
})

// olderText puts data at name in the skill under root, and records in the
// install's manifest that fft wrote it there — which is exactly what a release
// whose text differed from this one's would have left behind.
func olderText(root, name, data string) {
	GinkgoHelper()

	path := filepath.Join(installed(root), skill.ManifestName)
	raw, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())

	var m struct {
		Version string            `json:"version"`
		Files   map[string]string `json:"files"`
	}
	Expect(json.Unmarshal(raw, &m)).To(Succeed())

	sum := sha256.Sum256([]byte(data))
	m.Version = "1.3.0"
	m.Files[name] = "sha256:" + hex.EncodeToString(sum[:])

	raw, err = json.Marshal(m)
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(path, raw, 0o644)).To(Succeed())

	target := filepath.Join(installed(root), filepath.FromSlash(name))
	Expect(os.MkdirAll(filepath.Dir(target), 0o755)).To(Succeed())
	Expect(os.WriteFile(target, []byte(data), 0o644)).To(Succeed())
}

var _ = Describe("the install's manifest", func() {
	var root string

	BeforeEach(func() { root = GinkgoT().TempDir() })

	It("records the digest of every file it wrote, as written", func() {
		install(root)

		raw, err := os.ReadFile(filepath.Join(installed(root), skill.ManifestName))
		Expect(err).NotTo(HaveOccurred())

		var m struct {
			Files map[string]string `json:"files"`
		}
		Expect(json.Unmarshal(raw, &m)).To(Succeed())

		Expect(fs.WalkDir(skill.FS(), ".", func(name string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			sum := sha256.Sum256(shipped(name))
			Expect(m.Files).To(HaveKeyWithValue(name, "sha256:"+hex.EncodeToString(sum[:])))
			return nil
		})).To(Succeed())
	})

	// It is fft's bookkeeping, not a file of the skill and not a stray: the plan
	// neither offers to write it nor asks to remove it.
	It("is not part of the plan", func() {
		install(root)

		plan, err := skill.NewPlan(root)
		Expect(err).NotTo(HaveOccurred())
		Expect(statuses(plan)).NotTo(HaveKey(skill.ManifestName))
		Expect(plan.Pending()).To(BeEmpty())
	})

	It("is not rewritten by an install that changed nothing", func() {
		install(root)
		path := filepath.Join(installed(root), skill.ManifestName)
		before, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())

		install(root)

		after, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(after.ModTime()).To(Equal(before.ModTime()))
	})

	It("does not make a directory holding only it somebody else's", func() {
		install(root)
		for _, name := range []string{skill.Doc, "references"} {
			Expect(os.RemoveAll(filepath.Join(installed(root), name))).To(Succeed())
		}

		_, err := skill.NewPlan(root)
		Expect(err).NotTo(HaveOccurred())
	})

	// The record is what lets fft overwrite without asking, so a record fft cannot
	// read must vouch for nothing.
	It("vouches for nothing when it cannot be read", func() {
		install(root)
		olderText(root, "references/commands.md", "old commands\n")
		Expect(os.WriteFile(filepath.Join(installed(root), skill.ManifestName), []byte("{"), 0o644)).To(Succeed())

		plan, err := skill.NewPlan(root)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Pending()).To(ConsistOf(skill.Change{File: "references/commands.md", Status: skill.StatusConflict}))
	})
})

// The upgrade that actually happens: a release whose text differs from this one.
// Without a record of what fft wrote, every file the release changed looked like
// the user's edit, and `fft skill install` — the fix the notice recommends —
// refused to proceed without --force.
var _ = Describe("upgrading a skill an older fft installed", func() {
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		asVersion("1.3.0")
		install(root)
		asVersion("1.4.0")
	})

	When("the older text is untouched", func() {
		BeforeEach(func() { olderText(root, "references/commands.md", "old commands\n") })

		It("plans to replace it without asking", func() {
			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())

			Expect(statuses(plan)).To(HaveKeyWithValue("references/commands.md", skill.StatusOutdated))
			Expect(plan.Pending()).To(BeEmpty())
		})

		It("replaces it, and says it updated it", func() {
			done := install(root)

			Expect(statuses(done)).To(HaveKeyWithValue("references/commands.md", skill.StatusUpdated))
			Expect(os.ReadFile(filepath.Join(installed(root), "references", "commands.md"))).
				To(Equal(shipped("references/commands.md")))
		})
	})

	It("still asks about the older text once the user has edited it", func() {
		olderText(root, "references/commands.md", "old commands\n")
		Expect(os.WriteFile(filepath.Join(installed(root), "references", "commands.md"), []byte("mine\n"), 0o644)).To(Succeed())

		plan, err := skill.NewPlan(root)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Pending()).To(ConsistOf(skill.Change{File: "references/commands.md", Status: skill.StatusConflict}))
	})

	When("it shipped a file this fft no longer does", func() {
		var gone string

		BeforeEach(func() {
			olderText(root, "references/gone.md", "old\n")
			gone = filepath.Join(installed(root), "references", "gone.md")
		})

		It("plans to remove it without asking, and still names it", func() {
			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())

			Expect(statuses(plan)).To(HaveKeyWithValue("references/gone.md", skill.StatusObsolete))
			Expect(plan.Pending()).To(BeEmpty())
		})

		It("removes it, and reports that it did", func() {
			done := install(root)

			Expect(statuses(done)).To(HaveKeyWithValue("references/gone.md", skill.StatusRemoved))
			Expect(gone).NotTo(BeAnExistingFile())
		})

		It("asks first once the user has edited it", func() {
			Expect(os.WriteFile(gone, []byte("mine\n"), 0o644)).To(Succeed())

			plan, err := skill.NewPlan(root)
			Expect(err).NotTo(HaveOccurred())
			Expect(plan.Pending()).To(ConsistOf(skill.Change{File: "references/gone.md", Status: skill.StatusStale}))
		})
	})

	// Losing the record — a copy that skipped dotfiles — falls back on the stamp,
	// which is still enough to know SKILL.md is fft's.
	It("updates a SKILL.md whose manifest is gone, by its stamp", func() {
		Expect(os.Remove(filepath.Join(installed(root), skill.ManifestName))).To(Succeed())

		plan, err := skill.NewPlan(root)
		Expect(err).NotTo(HaveOccurred())
		Expect(statuses(plan)).To(HaveKeyWithValue(skill.Doc, skill.StatusOutdated))
		Expect(plan.Pending()).To(BeEmpty())
	})
})

// Installs from before manifests recorded nothing, so fft recognises its own old
// text by every hash it ever shipped. testdata/v0.8.0 is that release's skill,
// byte for byte (git show v0.8.0:internal/skill/assets/...), which makes this
// spec a check on the frozen table as much as on the upgrade.
var _ = Describe("upgrading a skill from before installs kept a manifest", func() {
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()

		release := filepath.Join("testdata", "v0.8.0")
		Expect(filepath.WalkDir(release, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(release, p)
			Expect(err).NotTo(HaveOccurred())

			data, err := os.ReadFile(p)
			Expect(err).NotTo(HaveOccurred())

			target := filepath.Join(installed(root), rel)
			Expect(os.MkdirAll(filepath.Dir(target), 0o755)).To(Succeed())
			return os.WriteFile(target, data, 0o644)
		})).To(Succeed())
	})

	It("asks about nothing", func() {
		plan, err := skill.NewPlan(root)
		Expect(err).NotTo(HaveOccurred())

		Expect(statuses(plan)).To(HaveKeyWithValue(skill.Doc, skill.StatusOutdated))
		Expect(plan.Pending()).To(BeEmpty())
	})

	It("leaves exactly what this fft ships, and a record of it", func() {
		done := install(root)

		for _, c := range done.Files {
			Expect(os.ReadFile(filepath.Join(done.Dir, filepath.FromSlash(c.File)))).To(Equal(shipped(c.File)))
		}
		Expect(filepath.Join(installed(root), skill.ManifestName)).To(BeAnExistingFile())
	})

	It("still asks about a file the user edited", func() {
		Expect(os.WriteFile(filepath.Join(installed(root), "references", "recipes.md"), []byte("mine\n"), 0o644)).To(Succeed())

		plan, err := skill.NewPlan(root)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Pending()).To(ConsistOf(skill.Change{File: "references/recipes.md", Status: skill.StatusConflict}))
	})
})

var _ = Describe("an unreadable skill directory", func() {
	It("is an error that names the path once", func() {
		if runtime.GOOS == "windows" {
			Skip("permission bits do not make a directory unreadable on Windows")
		}

		root := GinkgoT().TempDir()
		install(root)

		Expect(os.Chmod(installed(root), 0o000)).To(Succeed())
		DeferCleanup(os.Chmod, installed(root), os.FileMode(0o755))
		if _, err := os.ReadDir(installed(root)); err == nil {
			Skip("running as a user that permission bits do not stop")
		}

		_, err := skill.NewPlan(root)
		Expect(err).To(MatchError(fs.ErrPermission))
		Expect(strings.Count(err.Error(), installed(root))).To(Equal(1), err.Error())
	})
})
