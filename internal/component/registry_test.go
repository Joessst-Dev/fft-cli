package component_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/Joessst-Dev/fft-cli/internal/component"
)

var _ = Describe("the registry", func() {
	var root string

	BeforeEach(func() { root = GinkgoT().TempDir() })

	// install writes a component directory: the manifest, and an empty file where the
	// executable goes unless the spec asks for it to be missing.
	install := func(m component.Manifest, withBinary bool) {
		GinkgoHelper()

		dir := filepath.Join(root, m.Name)
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())

		data, err := yaml.Marshal(m)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(dir, component.ManifestName), data, 0o600)).To(Succeed())

		if !withBinary {
			return
		}
		exec := filepath.Join(dir, filepath.FromSlash(m.Exec))
		Expect(os.MkdirAll(filepath.Dir(exec), 0o755)).To(Succeed())
		Expect(os.WriteFile(exec, []byte("binary"), 0o755)).To(Succeed()) //nolint:gosec // a fake executable in a temp dir
	}

	manifest := func(name string) component.Manifest {
		return component.Manifest{
			APIVersion: component.APIVersion,
			Name:       name,
			Version:    "1.0.0",
			Kind:       component.KindCommand,
			Exec:       "bin/" + name,
			Commands:   []component.Command{{Name: name, Short: "s", Session: component.SessionNone}},
		}
	}

	Describe("discovery", func() {
		It("finds an installed component", func() {
			install(manifest("weather"), true)

			reg := component.Open(root)
			c, ok := reg.Lookup("weather")

			Expect(ok).To(BeTrue())
			Expect(c.Installed).To(BeTrue())
			Expect(c.FirstParty).To(BeFalse())
			Expect(c.ExecPath()).To(BeAnExistingFile())
		})

		It("reports a component whose binary is missing rather than pretending it works", func() {
			install(manifest("weather"), false)

			c, ok := component.Open(root).Lookup("weather")
			Expect(ok).To(BeTrue())
			Expect(c.Installed).To(BeFalse())
		})

		// A machine with no components is not an error condition, and creating the
		// directory to find it empty would put something in the user's home for a
		// feature they have not used.
		It("is empty, and fine, when the root does not exist", func() {
			reg := component.Open(filepath.Join(root, "nope"))

			Expect(reg.All()).To(BeEmpty())
			Expect(reg.Problems()).To(BeEmpty())
		})

		It("holds nothing when components are disabled", func() {
			install(manifest("weather"), true)

			reg := component.Open("")
			Expect(reg.All()).To(BeEmpty())
			Expect(reg.Root()).To(BeEmpty())
		})

		It("ignores a directory that is not a component", func() {
			Expect(os.MkdirAll(filepath.Join(root, "somebody-elses"), 0o755)).To(Succeed())

			reg := component.Open(root)
			Expect(reg.All()).To(BeEmpty())

			// Not a problem, either: a directory with no manifest is not a broken
			// component, it is not a component.
			Expect(reg.Problems()).To(BeEmpty())
		})

		It("records a manifest it cannot read, and carries on", func() {
			dir := filepath.Join(root, "broken")
			Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, component.ManifestName), []byte("kind: ???\n"), 0o600)).To(Succeed())

			install(manifest("weather"), true)

			reg := component.Open(root)
			Expect(reg.Problems()).To(HaveLen(1))

			// The broken one must not take the working one down with it.
			_, ok := reg.Lookup("weather")
			Expect(ok).To(BeTrue())
		})

		// The directory name is how a component is addressed for removal and upgrade,
		// so one that disagrees with its own manifest is ambiguous rather than merely
		// untidy.
		It("refuses a component installed under a name that is not its own", func() {
			m := manifest("weather")
			m.Name = "forecast"

			dir := filepath.Join(root, "weather")
			Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
			data, err := yaml.Marshal(m)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(dir, component.ManifestName), data, 0o600)).To(Succeed())

			reg := component.Open(root)
			Expect(reg.Problems()).To(HaveLen(1))
			Expect(reg.Problems()[0].Err).To(MatchError(ContainSubstring("installed as")))
		})

		It("lists only installed transports", func() {
			transport := component.Manifest{
				APIVersion: component.APIVersion,
				Name:       "pubsub",
				Kind:       component.KindTransport,
				Exec:       "bin/pubsub",
				Targets:    []string{"GOOGLE_CLOUD_PUB_SUB"},
			}
			install(transport, true)
			install(manifest("weather"), true)

			reg := component.Open(root)
			Expect(reg.Transports()).To(HaveLen(1))
			Expect(reg.Transports()[0].Delivers("GOOGLE_CLOUD_PUB_SUB")).To(BeTrue())
		})
	})

	Describe("a first-party component", func() {
		table := []component.Manifest{{
			APIVersion:  component.APIVersion,
			Name:        "emulator",
			Kind:        component.KindCommand,
			Exec:        "bin/fft-emulator",
			Description: "the compiled-in description",
			Commands: []component.Command{{
				Name: "emulator", Short: "Run a local emulator", Session: component.SessionNone,
			}},
		}}

		It("is registered whether or not it is installed", func() {
			reg := component.Open(root, component.WithFirstParty(table))

			c, ok := reg.Lookup("emulator")
			Expect(ok).To(BeTrue())
			Expect(c.FirstParty).To(BeTrue())
			Expect(c.Installed).To(BeFalse())

			// This is what lets `fft emulator` explain how to get itself rather than not
			// existing, and what keeps `fft --help` and the generated reference the same
			// on every machine.
			Expect(c.Commands).To(HaveLen(1))
		})

		It("takes its version and binary from the installed copy", func() {
			installed := manifest("emulator")
			installed.Version = "9.9.9"
			install(installed, true)

			c, ok := component.Open(root, component.WithFirstParty(table)).Lookup("emulator")
			Expect(ok).To(BeTrue())
			Expect(c.Installed).To(BeTrue())
			Expect(c.Version).To(Equal("9.9.9"))
		})

		It("keeps the compiled-in command tree whatever the installed manifest says", func() {
			// An old, or a tampered-with, installed manifest: it declares a different
			// command, asking for a session the compiled-in table never granted.
			installed := manifest("emulator")
			installed.Description = "something else"
			installed.Commands = []component.Command{{
				Name: "emulator", Short: "s", Session: component.SessionWrite, Mutates: true,
			}}
			install(installed, true)

			c, ok := component.Open(root, component.WithFirstParty(table)).Lookup("emulator")
			Expect(ok).To(BeTrue())

			// fft ships this component, so what it may do is fft's answer and not the
			// disk's. An installed copy cannot widen its own session by shipping a
			// manifest that asks for more.
			Expect(c.Commands[0].Session).To(Equal(component.SessionNone))
			Expect(c.Commands[0].Mutates).To(BeFalse())
			Expect(c.Description).To(Equal("the compiled-in description"))
		})
	})
})

var _ = Describe("Root", func() {
	It("prefers FFT_COMPONENT_DIR", func() {
		dir, enabled, err := component.Root(lookup(map[string]string{component.EnvRoot: "/tmp/mine"}))

		Expect(err).NotTo(HaveOccurred())
		Expect(enabled).To(BeTrue())
		Expect(dir).To(Equal("/tmp/mine"))
	})

	// Set-but-empty is a different answer from unset, and it is the one `fft gen-docs`
	// and the specs rely on: components off, deterministically.
	It("reports components disabled when it is set to nothing", func() {
		_, enabled, err := component.Root(lookup(map[string]string{component.EnvRoot: ""}))

		Expect(err).NotTo(HaveOccurred())
		Expect(enabled).To(BeFalse())
	})

	It("falls back to XDG_DATA_HOME", func() {
		dir, enabled, err := component.Root(lookup(map[string]string{"XDG_DATA_HOME": "/data"}))

		Expect(err).NotTo(HaveOccurred())
		Expect(enabled).To(BeTrue())
		Expect(dir).To(Equal(filepath.Join("/data", "fft", "components")))
	})
})

// lookup builds an os.LookupEnv stand-in from a map.
func lookup(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
}
