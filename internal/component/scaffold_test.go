package component_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/component"
)

// fileOf returns one emitted file by its relative name.
func fileOf(files []component.File, name string) (component.File, bool) {
	for _, f := range files {
		if f.Name == name {
			return f, true
		}
	}
	return component.File{}, false
}

// buildEntries is one table row per (kind, lang, session) combination the scaffolder
// can produce — a transport has no session, a command is exercised at all three.
func buildEntries() []TableEntry {
	langs := []string{component.LangShell, component.LangGo, component.LangPython, component.LangNode}
	sessions := []component.Session{component.SessionNone, component.SessionRead, component.SessionWrite}

	var entries []TableEntry
	for _, lang := range langs {
		for _, session := range sessions {
			name := "command/" + lang + "/" + string(session)
			entries = append(entries, Entry(name, component.KindCommand, lang, session))
		}
		entries = append(entries, Entry("transport/"+lang, component.KindTransport, lang, component.Session("")))
	}
	return entries
}

var _ = Describe("Scaffold", func() {
	Describe("Build", func() {
		// The guarantee the whole feature rests on: every combination it can produce
		// emits a manifest the real parser accepts, so a scaffold that would not install
		// cannot be made.
		DescribeTable("emits a component.yaml that ParseManifest accepts",
			func(kind component.Kind, lang string, session component.Session) {
				files, err := component.Scaffold{Name: "widget", Kind: kind, Lang: lang, Session: session}.Build()
				Expect(err).NotTo(HaveOccurred())

				manifest, ok := fileOf(files, component.ManifestName)
				Expect(ok).To(BeTrue(), "a scaffold must carry a manifest")

				m, err := component.ParseManifest(manifest.Data, component.ManifestName)
				Expect(err).NotTo(HaveOccurred())
				Expect(m.Name).To(Equal("widget"))
				Expect(m.Kind).To(Equal(kind))
				Expect(m.Exec).To(Equal("bin/fft-widget"))
			},
			buildEntries(),
		)

		It("derives mutates from the session so a write component still validates", func() {
			for _, tc := range []struct {
				session component.Session
				mutates bool
			}{
				{component.SessionNone, false},
				{component.SessionRead, false},
				{component.SessionWrite, true},
			} {
				files, err := component.Scaffold{
					Name: "widget", Kind: component.KindCommand, Lang: component.LangShell, Session: tc.session,
				}.Build()
				Expect(err).NotTo(HaveOccurred())

				manifest, _ := fileOf(files, component.ManifestName)
				m, err := component.ParseManifest(manifest.Data, component.ManifestName)
				Expect(err).NotTo(HaveOccurred())
				Expect(m.Commands[0].Session).To(Equal(tc.session))
				Expect(m.Commands[0].Mutates).To(Equal(tc.mutates), "session %s", tc.session)
			}
		})

		It("makes an interpreter script executable and gives it a shebang", func() {
			for _, lang := range []string{component.LangShell, component.LangPython, component.LangNode} {
				files, err := component.Scaffold{Name: "widget", Kind: component.KindCommand, Lang: lang}.Build()
				Expect(err).NotTo(HaveOccurred())

				bin, ok := fileOf(files, "bin/fft-widget")
				Expect(ok).To(BeTrue(), "lang %s", lang)
				Expect(bin.Mode.Perm()&0o100).NotTo(BeZero(), "lang %s: not executable", lang)
				Expect(string(bin.Data)).To(HavePrefix("#!"), "lang %s", lang)
			}
		})

		It("redacts the tenant token in every command skeleton", func() {
			// A read or write session hands the command a live FFT_ID_TOKEN, and the skeleton
			// echoes the FFT_ environment to stdout — the stream the output contract keeps safe
			// to pipe. Every language's skeleton must mask it, not just the default shell one.
			for _, lang := range []string{component.LangShell, component.LangGo, component.LangPython, component.LangNode} {
				files, err := component.Scaffold{Name: "widget", Kind: component.KindCommand, Lang: lang}.Build()
				Expect(err).NotTo(HaveOccurred())

				body := ""
				for _, f := range files {
					if f.Name == "bin/fft-widget" || f.Name == "main.go" {
						body = string(f.Data)
					}
				}
				Expect(body).To(ContainSubstring("FFT_ID_TOKEN"), "lang %s", lang)
				Expect(body).To(ContainSubstring("<redacted>"), "lang %s", lang)
			}
		})

		It("emits a Go module rather than a bin/ script for --lang go", func() {
			files, err := component.Scaffold{Name: "widget", Kind: component.KindCommand, Lang: component.LangGo}.Build()
			Expect(err).NotTo(HaveOccurred())

			_, hasMain := fileOf(files, "main.go")
			gomod, hasMod := fileOf(files, "go.mod")
			_, hasScript := fileOf(files, "bin/fft-widget")
			Expect(hasMain).To(BeTrue())
			Expect(hasMod).To(BeTrue())
			Expect(hasScript).To(BeFalse(), "a Go component is built into bin/, not shipped as a script")
			Expect(string(gomod.Data)).To(ContainSubstring("module widget"))
		})

		It("pins fft in a Go transport's go.mod only when a version is given", func() {
			base := component.Scaffold{Name: "widget", Kind: component.KindTransport, Lang: component.LangGo}

			unpinned, err := base.Build()
			Expect(err).NotTo(HaveOccurred())
			gomod, _ := fileOf(unpinned, "go.mod")
			Expect(string(gomod.Data)).NotTo(ContainSubstring("require"))

			pinned := base
			pinned.FFTRequire = "v1.2.3"
			built, err := pinned.Build()
			Expect(err).NotTo(HaveOccurred())
			gomod, _ = fileOf(built, "go.mod")
			Expect(string(gomod.Data)).To(ContainSubstring("require github.com/Joessst-Dev/fft-cli v1.2.3"))
		})

		It("gives a transport skeleton a target and a Serve loop", func() {
			files, err := component.Scaffold{Name: "widget", Kind: component.KindTransport, Lang: component.LangGo}.Build()
			Expect(err).NotTo(HaveOccurred())

			manifest, _ := fileOf(files, component.ManifestName)
			m, err := component.ParseManifest(manifest.Data, component.ManifestName)
			Expect(err).NotTo(HaveOccurred())
			Expect(m.Targets).NotTo(BeEmpty())

			main, _ := fileOf(files, "main.go")
			Expect(string(main.Data)).To(ContainSubstring("transportproto.Serve"))
		})

		It("refuses a name the manifest rules reject, writing nothing", func() {
			_, err := component.Scaffold{Name: "Bad_Name", Kind: component.KindCommand, Lang: component.LangShell}.Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Bad_Name"))
		})

		It("reports an unknown language", func() {
			_, err := component.Scaffold{Name: "widget", Kind: component.KindCommand, Lang: "rust"}.Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("rust"))
		})
	})

	Describe("DefaultLang", func() {
		It("is shell for a command and go for a transport", func() {
			Expect(component.DefaultLang(component.KindCommand)).To(Equal(component.LangShell))
			Expect(component.DefaultLang(component.KindTransport)).To(Equal(component.LangGo))
		})
	})
})
