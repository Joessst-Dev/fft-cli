package template_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/template"
)

var _ = Describe("the template store", func() {
	var (
		store   *template.Store
		dataDir string
		workDir string
	)

	BeforeEach(func() {
		dataDir = GinkgoT().TempDir()
		GinkgoT().Chdir(GinkgoT().TempDir())

		// The working directory is read back rather than remembered: on macOS a
		// temp directory is reached through a symlink, and os.Getwd resolves it.
		var err error
		workDir, err = os.Getwd()
		Expect(err).NotTo(HaveOccurred())

		store, err = template.NewStore(func(key string) (string, bool) {
			if key == "XDG_DATA_HOME" {
				return dataDir, true
			}
			return "", false
		})
		Expect(err).NotTo(HaveOccurred())
	})

	// sample is a template with a body, for the specs that only care about where
	// it lands rather than what is in it.
	sample := func(project string) *template.Template {
		return &template.Template{
			SchemaVersion: template.Version,
			OperationID:   "createOrder",
			Project:       project,
			Body:          map[string]any{"order": map[string]any{"tenantOrderId": "A-1"}},
		}
	}

	Describe("names", func() {
		It("accepts the ordinary ones", func() {
			for _, name := range []string{"rush-order", "stock_bulk", "a", "v1.2"} {
				Expect(template.ValidateName(name)).To(Succeed(), "expected %q to be accepted", name)
			}
		})

		It("refuses anything that could escape the directory", func() {
			for _, name := range []string{"", "..", ".", ".hidden", "a/b", `a\b`, "a b", "a:b"} {
				err := template.ValidateName(name)
				Expect(err).To(HaveOccurred(), "expected %q to be refused", name)
				Expect(exitcode.FromError(err)).To(Equal(exitcode.Usage))
			}
		})

		It("never joins a refused name onto a path", func() {
			_, err := store.Path("../../escape", template.ScopeUser)
			Expect(err).To(HaveOccurred())

			_, err = store.Remove("../../escape", template.ScopeUser)
			Expect(err).To(HaveOccurred())
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Usage))
		})
	})

	Describe("writing and reading", func() {
		It("round-trips a template", func() {
			path, err := store.Write("rush", template.ScopeUser, sample("staging"))
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal(filepath.Join(dataDir, "fft", "templates", "rush.json")))

			saved, err := store.Resolve("rush")
			Expect(err).NotTo(HaveOccurred())
			Expect(saved.Name).To(Equal("rush"))
			Expect(saved.Scope).To(Equal(template.ScopeUser))
			Expect(saved.OperationID).To(Equal("createOrder"))
			Expect(saved.Project).To(Equal("staging"))
		})

		It("writes the user scope owner-only", func() {
			if runtime.GOOS == "windows" {
				Skip("Windows does not carry Unix file modes")
			}
			path, err := store.Write("rush", template.ScopeUser, sample(""))
			Expect(err).NotTo(HaveOccurred())

			info, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))

			dir, err := os.Stat(filepath.Dir(path))
			Expect(err).NotTo(HaveOccurred())
			Expect(dir.Mode().Perm()).To(Equal(os.FileMode(0o700)))
		})

		It("gives the project directory a mode a working tree can traverse", func() {
			if runtime.GOOS == "windows" {
				Skip("Windows does not carry Unix file modes")
			}
			path, err := store.Write("rush", template.ScopeProject, sample(""))
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal(filepath.Join(workDir, ".fft", "templates", "rush.json")))

			dir, err := os.Stat(filepath.Dir(path))
			Expect(err).NotTo(HaveOccurred())
			Expect(dir.Mode().Perm()).To(Equal(os.FileMode(0o755)))
		})

		It("keeps a 19-digit id exact across the round trip", func() {
			body := decode(`{"order":{"id":9007199254740993}}`)
			_, err := store.Write("big", template.ScopeUser, &template.Template{
				SchemaVersion: template.Version,
				Body:          body,
			})
			Expect(err).NotTo(HaveOccurred())

			saved, err := store.Resolve("big")
			Expect(err).NotTo(HaveOccurred())

			out, err := template.Render(saved.Template, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("9007199254740993"))
		})
	})

	Describe("resolving", func() {
		It("prefers the project scope and reports the user one as shadowed", func() {
			_, err := store.Write("rush", template.ScopeUser, sample("personal"))
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Write("rush", template.ScopeProject, sample("team"))
			Expect(err).NotTo(HaveOccurred())

			saved, err := store.Resolve("rush")
			Expect(err).NotTo(HaveOccurred())
			Expect(saved.Scope).To(Equal(template.ScopeProject))
			Expect(saved.Project).To(Equal("team"))

			listing, err := store.List()
			Expect(err).NotTo(HaveOccurred())
			Expect(listing.Found).To(HaveLen(1))
			Expect(listing.Found[0].Scope).To(Equal(template.ScopeProject))
			Expect(listing.Shadowed).To(HaveLen(1))
			Expect(listing.Shadowed[0].Scope).To(Equal(template.ScopeUser))
		})

		It("exits 6 for a name nobody saved, naming the ones that exist", func() {
			_, err := store.Write("rush", template.ScopeUser, sample(""))
			Expect(err).NotTo(HaveOccurred())

			_, err = store.Resolve("rsuh")
			Expect(err).To(HaveOccurred())
			Expect(exitcode.FromError(err)).To(Equal(exitcode.NotFound))

			notFound := &template.NotFoundError{}
			Expect(errors.As(err, &notFound)).To(BeTrue())
			Expect(notFound.Hint()).To(ContainSubstring("rush"))
		})
	})

	Describe("listing", func() {
		It("is empty, not an error, when neither directory exists", func() {
			listing, err := store.List()
			Expect(err).NotTo(HaveOccurred())
			Expect(listing.Found).To(BeEmpty())
		})

		It("reports an unreadable file as a problem and lists the rest", func() {
			_, err := store.Write("good", template.ScopeUser, sample(""))
			Expect(err).NotTo(HaveOccurred())

			bad := filepath.Join(dataDir, "fft", "templates", "bad.json")
			Expect(os.WriteFile(bad, []byte("{not json"), 0o600)).To(Succeed())

			listing, err := store.List()
			Expect(err).NotTo(HaveOccurred())
			Expect(listing.Found).To(HaveLen(1))
			Expect(listing.Found[0].Name).To(Equal("good"))
			Expect(listing.Problems).To(HaveLen(1))
			Expect(listing.Problems[0].Path).To(Equal(bad))
		})

		It("ignores files that are not templates", func() {
			_, err := store.Write("good", template.ScopeUser, sample(""))
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(dataDir, "fft", "templates", "notes.md"),
				[]byte("hello"), 0o600)).To(Succeed())

			listing, err := store.List()
			Expect(err).NotTo(HaveOccurred())
			Expect(listing.Found).To(HaveLen(1))
		})
	})

	Describe("removing", func() {
		It("deletes the file and reports which one", func() {
			path, err := store.Write("rush", template.ScopeUser, sample(""))
			Expect(err).NotTo(HaveOccurred())

			removed, err := store.Remove("rush", template.ScopeUser)
			Expect(err).NotTo(HaveOccurred())
			Expect(removed).To(Equal(path))
			Expect(path).NotTo(BeAnExistingFile())
		})

		It("exits 6 for a template that is not in that scope", func() {
			_, err := store.Write("rush", template.ScopeUser, sample(""))
			Expect(err).NotTo(HaveOccurred())

			_, err = store.Remove("rush", template.ScopeProject)
			Expect(exitcode.FromError(err)).To(Equal(exitcode.NotFound))
		})
	})

	Describe("the schema version", func() {
		It("refuses a file from a newer fft rather than misreading it", func() {
			path := filepath.Join(dataDir, "fft", "templates", "future.json")
			Expect(os.MkdirAll(filepath.Dir(path), 0o700)).To(Succeed())

			data, err := json.Marshal(map[string]any{
				"schemaVersion": template.Version + 1,
				"body":          map[string]any{},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(path, data, 0o600)).To(Succeed())

			_, err = store.Resolve("future")
			Expect(err).To(MatchError(ContainSubstring("upgrade fft")))
		})
	})
})
