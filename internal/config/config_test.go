package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/testsupport"
)

var _ = Describe("Store", func() {
	var (
		dir   string
		path  string
		store *config.Store
	)

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		path = filepath.Join(dir, "fft", "config.yaml")
		store = config.NewStore(path)
	})

	sampleConfig := func() *config.Config {
		cfg := config.New()
		cfg.ActiveProject = "staging"
		cfg.Upsert(config.Project{
			Name:           "staging",
			BaseURL:        "https://acme.api.fulfillmenttools.com",
			FirebaseAPIKey: "AIzaSyExample",
			Email:          "bot@ocff-acme-staging.com",
			Username:       "bot",
			ProjectID:      "acme",
			Environment:    "staging",
		})
		return cfg
	}

	Describe("loading a config that does not exist yet", func() {
		It("returns the defaults rather than an error, because a first run has no config", func() {
			cfg, err := store.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Version).To(Equal(config.Version))
			Expect(cfg.Projects).To(BeEmpty())
			Expect(cfg.Settings.Output).To(Equal(config.OutputTable))
			Expect(cfg.Settings.UpdateCheck).To(BeTrue())
		})
	})

	Describe("saving", func() {
		BeforeEach(func() {
			Expect(store.Save(sampleConfig())).To(Succeed())
		})

		It("writes the file with mode 0600, so no other user can read the project's email", func() {
			testsupport.ExpectOwnerOnlyFile(path)
		})

		It("creates the parent directory with mode 0700", func() {
			testsupport.ExpectOwnerOnlyDir(filepath.Dir(path))
		})

		It("leaves no temporary file behind, having renamed it into place", func() {
			entries, err := os.ReadDir(filepath.Dir(path))

			Expect(err).NotTo(HaveOccurred())
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			Expect(names).To(ConsistOf("config.yaml"))
		})

		It("replaces the previous file wholesale rather than truncating it in place", func() {
			// A shorter config must not leave a tail of the longer one behind.
			smaller := config.New()
			Expect(store.Save(smaller)).To(Succeed())

			data, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).NotTo(ContainSubstring("staging"))

			var raw map[string]any
			Expect(yaml.Unmarshal(data, &raw)).To(Succeed())
		})

		It("keeps the previous config intact when the write cannot complete", func() {
			// Nothing may be created in the directory, so atomicfile's temporary file
			// cannot be either. The rename never happens, and the old file survives
			// untouched. Both halves are asserted: that the save failed, and that it
			// failed at the temporary file rather than somewhere that would have left
			// the target already truncated.
			testsupport.MakeUnwritableDir(filepath.Dir(path))

			err := store.Save(config.New())
			Expect(err).To(MatchError(fs.ErrPermission))
			Expect(err).To(MatchError(ContainSubstring("create a temporary file")))

			reloaded, err := config.NewStore(path).Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(reloaded.ActiveProject).To(Equal("staging"))
		})
	})

	Describe("the YAML round trip", func() {
		// This is the viper trap. Viper lower-cases every key it touches, so a
		// config persisted through it comes back as `activeproject` and `baseurl`
		// and a load-then-save cycle silently rewrites the user's file into keys
		// nothing reads. These specs prove fft persists with yaml.v3 instead.
		BeforeEach(func() {
			Expect(store.Save(sampleConfig())).To(Succeed())
		})

		It("writes camelCase keys, not the lower-cased ones viper would produce", func() {
			data, err := os.ReadFile(path)

			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("activeProject:"))
			Expect(string(data)).To(ContainSubstring("baseUrl:"))
			Expect(string(data)).To(ContainSubstring("firebaseApiKey:"))
			Expect(string(data)).To(ContainSubstring("updateCheck:"))

			Expect(string(data)).NotTo(ContainSubstring("activeproject:"))
			Expect(string(data)).NotTo(ContainSubstring("baseurl:"))
			Expect(string(data)).NotTo(ContainSubstring("firebaseapikey:"))
		})

		It("reads back every field it wrote", func() {
			cfg, err := store.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.ActiveProject).To(Equal("staging"))
			Expect(cfg.Projects).To(HaveLen(1))

			Expect(cfg.Projects[0]).To(Equal(config.Project{
				Name:           "staging",
				BaseURL:        "https://acme.api.fulfillmenttools.com",
				FirebaseAPIKey: "AIzaSyExample",
				Email:          "bot@ocff-acme-staging.com",
				Username:       "bot",
				ProjectID:      "acme",
				Environment:    "staging",
			}))
		})

		It("survives a load-and-save cycle without dropping the active project", func() {
			cfg, err := store.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(store.Save(cfg)).To(Succeed())

			reloaded, err := store.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(reloaded.ActiveProject).To(Equal("staging"))
		})

		It("never writes a secret to the config file", func() {
			data, err := os.ReadFile(path)

			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).NotTo(ContainSubstring("password"))
			Expect(string(data)).NotTo(ContainSubstring("Token"))
		})

		// The read-only flag is a safety property, so it has to survive the trip to
		// disk and back — and it has to cost nothing when it is off, or every user's
		// config file would grow a `readOnly: false` line on their next `project use`.
		It("writes no readOnly key for a writable project", func() {
			data, err := os.ReadFile(path)

			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).NotTo(ContainSubstring("readOnly"))
		})

		It("round-trips a read-only project", func() {
			cfg := sampleConfig()
			cfg.Projects[0].ReadOnly = true
			Expect(store.Save(cfg)).To(Succeed())

			data, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("readOnly: true"))

			reloaded, err := store.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(reloaded.Projects[0].ReadOnly).To(BeTrue())
		})

		It("loads a config written before the feature existed as writable", func() {
			cfg, err := store.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Projects[0].ReadOnly).To(BeFalse())
		})
	})

	Describe("loading a corrupt config", func() {
		It("fails with exit code 3 and a hint about how to recover", func() {
			Expect(os.MkdirAll(filepath.Dir(path), 0o700)).To(Succeed())
			Expect(os.WriteFile(path, []byte("projects: [oops"), 0o600)).To(Succeed())

			_, err := store.Load()

			Expect(err).To(HaveOccurred())
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Config))

			var cfgErr *config.Error
			Expect(errors.As(err, &cfgErr)).To(BeTrue())
			Expect(cfgErr.Hint()).To(ContainSubstring("fft project add"))
		})
	})

	Describe("loading a config from a newer fft", func() {
		It("refuses rather than misreading it", func() {
			Expect(os.MkdirAll(filepath.Dir(path), 0o700)).To(Succeed())
			Expect(os.WriteFile(path, []byte("version: 99\n"), 0o600)).To(Succeed())

			_, err := store.Load()

			Expect(err).To(MatchError(ContainSubstring("newer fft")))
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Config))
		})
	})
})

var _ = Describe("Config", func() {
	var cfg *config.Config

	BeforeEach(func() {
		cfg = config.New()
		cfg.Upsert(config.Project{Name: "staging", BaseURL: "https://a.example.com"})
		cfg.Upsert(config.Project{Name: "prod", BaseURL: "https://b.example.com"})
		cfg.ActiveProject = "staging"
	})

	Describe("Upsert", func() {
		It("replaces a project of the same name rather than adding a duplicate", func() {
			cfg.Upsert(config.Project{Name: "staging", BaseURL: "https://changed.example.com"})

			Expect(cfg.Projects).To(HaveLen(2))
			p, ok := cfg.Find("staging")
			Expect(ok).To(BeTrue())
			Expect(p.BaseURL).To(Equal("https://changed.example.com"))
		})
	})

	Describe("Remove", func() {
		It("clears the active selection when the active project is the one removed", func() {
			Expect(cfg.Remove("staging")).To(BeTrue())

			Expect(cfg.ActiveProject).To(BeEmpty())
			Expect(cfg.Projects).To(HaveLen(1))
		})

		It("leaves the active selection alone when another project is removed", func() {
			Expect(cfg.Remove("prod")).To(BeTrue())

			Expect(cfg.ActiveProject).To(Equal("staging"))
		})

		It("reports that a project it does not know about was not removed", func() {
			Expect(cfg.Remove("nope")).To(BeFalse())
		})
	})

	Describe("Resolve", func() {
		When("a project is named explicitly", func() {
			It("returns it, in preference to the active one", func() {
				p, err := cfg.Resolve("prod")

				Expect(err).NotTo(HaveOccurred())
				Expect(p.Name).To(Equal("prod"))
			})
		})

		When("no project is named", func() {
			It("falls back to the active project", func() {
				p, err := cfg.Resolve("")

				Expect(err).NotTo(HaveOccurred())
				Expect(p.Name).To(Equal("staging"))
			})
		})

		When("the named project is not configured", func() {
			It("exits 3 and lists the projects that do exist", func() {
				_, err := cfg.Resolve("nope")

				Expect(err).To(MatchError(config.ErrProjectNotFound))
				Expect(exitcode.FromError(err)).To(Equal(exitcode.Config))

				var cfgErr *config.Error
				Expect(errors.As(err, &cfgErr)).To(BeTrue())
				Expect(cfgErr.Hint()).To(ContainSubstring("staging"))
				Expect(cfgErr.Hint()).To(ContainSubstring("prod"))
			})
		})

		When("there is no active project and none was named", func() {
			It("exits 3 and tells the user to run 'fft project add'", func() {
				cfg.ActiveProject = ""

				_, err := cfg.Resolve("")

				Expect(err).To(MatchError(config.ErrNoActiveProject))
				Expect(exitcode.FromError(err)).To(Equal(exitcode.Config))

				var cfgErr *config.Error
				Expect(errors.As(err, &cfgErr)).To(BeTrue())
				Expect(cfgErr.Hint()).To(ContainSubstring("fft project add"))
			})
		})
	})
})

var _ = Describe("CandidateEmail", func() {
	DescribeTable("building the synthetic fulfillmenttools address",
		func(username, projectID, environment, want string) {
			Expect(config.CandidateEmail(username, projectID, environment)).To(Equal(want))
		},
		Entry("joins the three parts in the documented shape",
			"bot", "acme", "staging", "bot@ocff-acme-staging.com"),
		Entry("passes a value that is already an email through untouched",
			"someone@acme.com", "acme", "staging", "someone@acme.com"),
		Entry("returns nothing rather than a plausible wrong answer when the project id is missing",
			"bot", "", "staging", ""),
		Entry("returns nothing rather than a plausible wrong answer when the environment is missing",
			"bot", "acme", "", ""),
		Entry("returns nothing when there is no username",
			"", "acme", "staging", ""),
	)
})
