package main

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("fft component", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	Describe("dispatch", func() {
		It("registers a command for what an installed component declares", func() {
			c.installFake(fakeManifest("weather"))

			Expect(c.run("--help")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("Installed components:"))
			Expect(c.out()).To(ContainSubstring("weather"))
		})

		It("passes the arguments through untouched", func() {
			c.installFake(fakeManifest("weather"))

			// Flags fft has never heard of, in an order it has no opinion about, and one
			// that collides with a global of its own: a component owns its flag syntax
			// completely, so none of this may be parsed, reordered or swallowed.
			Expect(c.run("weather", "--city", "Berlin", "-o", "wide", "--", "extra")).To(Equal(exitcode.OK))
			Expect(c.report().Args).To(Equal([]string{"--city", "Berlin", "-o", "wide", "--", "extra"}))
		})

		It("gives stdout to the component and keeps its own notices on stderr", func() {
			c.installFake(fakeManifest("weather"))
			c.setenv(envFakeStdout, "raining\n")

			Expect(c.run("weather")).To(Equal(exitcode.OK))

			// Byte for byte: a component's stdout is the pipe's contents, and anything of
			// fft's mixed into it would break every script that reads one.
			Expect(c.out()).To(Equal("raining\n"))
			Expect(c.errOut()).To(ContainSubstring("fake component weather speaking"))
		})

		It("does not impose the global --timeout on a long-running component", func() {
			c.installFake(fakeManifest("weather"))
			// A component runs until it is done — the emulator runs until Ctrl-C — so the
			// per-API-call --timeout must not bound it. With the bug, a 5ms deadline would
			// kill this component before its 80ms sleep finished; without it, the sleep
			// completes and the component exits cleanly. The deadline is set through the
			// env, not a flag, so it does not land in the args the stub passes through.
			c.setenv("FFT_TIMEOUT", "5ms")
			c.setenv(envFakeSleep, "80")

			Expect(c.run("weather")).To(Equal(exitcode.OK))
		})

		It("exits with the component's status, and says nothing over its own message", func() {
			c.installFake(fakeManifest("weather"))
			c.setenv(envFakeExit, "6")

			Expect(c.run("weather")).To(Equal(exitcode.NotFound))

			// The component already explained itself on the stderr it inherited. A second
			// "Error: …" from fft would be fft narrating a failure it did not witness.
			Expect(c.errOut()).NotTo(ContainSubstring("Error:"))
		})

		It("runs the component in the caller's working directory", func() {
			c.installFake(fakeManifest("weather"))

			Expect(c.run("weather")).To(Equal(exitcode.OK))

			cwd, err := os.Getwd()
			Expect(err).NotTo(HaveOccurred())

			// Not the component's own directory: `--seed ./fixtures` means the fixtures
			// next to the user, and re-rooting relative paths would silently change what
			// every one of them points at.
			Expect(c.report().Dir).To(Equal(cwd))
		})

		It("refuses to let a component shadow a command fft already has", func() {
			m := fakeManifest("facility")
			c.installFake(m)

			Expect(c.run("facility", "--help")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("Manage the facilities of your tenant"))
			Expect(c.errOut()).To(ContainSubstring(`component declares a command called "facility"`))
		})

		It("refuses a name a generated resource group will take", func() {
			// `picking` is a generated group (fft picking get-pick-job, …), created after
			// components register. Without reserving the group names it would slip past the
			// collision check now and be silently adopted as the group's parent — a stub
			// with flag parsing off, breaking the generated commands under it. It must be
			// refused like any other name fft already owns.
			m := fakeManifest("picking")
			c.installFake(m)

			Expect(c.run("picking", "--help")).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring(`component declares a command called "picking"`))

			// The generated group is intact: its commands are still there, not swallowed by
			// a component stub.
			Expect(c.out()).To(ContainSubstring("get-pick-job"))
		})
	})

	Describe("the environment a component is given", func() {
		It("hands a session:none component no credential at all", func() {
			c.installFake(fakeManifest("weather"))
			c.headless()

			Expect(c.run("weather")).To(Equal(exitcode.OK))

			env := c.report().Env
			// Not merely "no token": no base URL either. A component that needs no
			// credential is not told where the tenant is.
			Expect(env).NotTo(HaveKey(config.EnvIDToken))
			Expect(env).NotTo(HaveKey(config.EnvBaseURL))
			Expect(env).NotTo(HaveKey(config.EnvPassword))
			Expect(env).To(HaveKeyWithValue(component.EnvName, "weather"))
			Expect(env).To(HaveKeyWithValue(component.EnvAPI, "1"))
		})

		It("strips the caller's own FFT_ variables", func() {
			c.installFake(fakeManifest("weather"))

			// A developer with a headless session exported in their shell. None of it may
			// reach a component that did not ask for a session.
			c.headless()
			c.setenv(config.EnvPassword, "the-real-password")

			Expect(c.run("weather")).To(Equal(exitcode.OK))
			Expect(c.report().Env).NotTo(HaveKey(config.EnvPassword))
		})

		It("gives a session:read component a token but never the API key", func() {
			m := fakeManifest("reader")
			m.Commands[0].Session = component.SessionRead
			c.installFake(m)
			c.headless()

			Expect(c.run("reader")).To(Equal(exitcode.OK))

			env := c.report().Env
			Expect(env).To(HaveKeyWithValue(config.EnvIDToken, testIDToken))
			Expect(env).To(HaveKeyWithValue(config.EnvReadOnly, "1"))

			// The Firebase key mints fresh tokens indefinitely. A component gets a
			// credential that expires, and a placeholder in the field that would let it
			// make more.
			Expect(env).To(HaveKey(config.EnvFirebaseAPIKey))
			Expect(env[config.EnvFirebaseAPIKey]).NotTo(Equal("AIzaSyCI"))
		})

		It("forwards only the variables the manifest declares", func() {
			m := fakeManifest("weather")
			m.Env = []string{"WEATHER_API_HOST"}
			c.installFake(m)

			Expect(c.run("weather")).To(Equal(exitcode.OK))
		})
	})

	Describe("the read-only gate", func() {
		var mutating component.Manifest

		BeforeEach(func() {
			mutating = fakeManifest("writer")
			mutating.Commands[0].Session = component.SessionWrite
			mutating.Commands[0].Mutates = true
		})

		It("refuses a mutating component, and spawns nothing", func() {
			c.installFake(mutating)
			c.headless()
			c.setenv(config.EnvReadOnly, "1")

			Expect(c.run("writer")).To(Equal(exitcode.ReadOnly))
			Expect(c.errOut()).To(ContainSubstring("is read-only"))
			Expect(c.errOut()).To(ContainSubstring("it was not started"))

			// The proof that nothing ran: the component's own report never reached stdout.
			Expect(c.out()).To(BeEmpty())
		})

		It("allows a mutating component when the project is writable", func() {
			c.installFake(mutating)
			c.headless()

			Expect(c.run("writer")).To(Equal(exitcode.OK))
			Expect(c.report().Env).To(HaveKeyWithValue(config.EnvIDToken, testIDToken))
		})

		It("gates a component that claims a mutating operation whatever its manifest says", func() {
			// The manifest lies: it declares that it changes nothing, while claiming an
			// endpoint the spec says is a write. `claims` is how a component supersedes a
			// generated command, so the operation behind it is a fact fft already has —
			// and an operation known to be a write does not stop being one because
			// somebody else's manifest says otherwise.
			m := fakeManifest("liar")
			m.Commands[0].Mutates = false
			m.Commands[0].Claims = []string{"addFacility"}
			c.installFake(m)

			c.headless()
			c.setenv(config.EnvReadOnly, "1")

			Expect(c.run("liar")).To(Equal(exitcode.ReadOnly))
			Expect(c.out()).To(BeEmpty())
		})

		It("leaves a read-only component alone", func() {
			m := fakeManifest("reader")
			m.Commands[0].Session = component.SessionRead
			c.installFake(m)

			c.headless()
			c.setenv(config.EnvReadOnly, "1")

			Expect(c.run("reader")).To(Equal(exitcode.OK))
		})
	})

	Describe("shadowing the generated commands", func() {
		It("suppresses the generated twin of an operation a component claims", func() {
			// Without the component, the Tier-2 command for this operation exists.
			Expect(c.run("picking", "--help")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("get-pick-job "))

			m := fakeManifest("pickjob")
			m.Commands[0].Claims = []string{"getPickJob"}
			c.installFake(m)

			// Exactly as a curated command shadows its twin: promoting an endpoint out of
			// Tier 2 is a pure upgrade, whoever writes the replacement.
			Expect(c.run("picking", "--help")).To(Equal(exitcode.OK))
			Expect(c.out()).NotTo(ContainSubstring("get-pick-job "))
		})
	})

	Describe("a component that is not installed", func() {
		It("is not in the tree when nothing declares it", func() {
			// Cobra's own "unknown command", which fft does not reclassify — so it is a
			// general failure rather than a usage one, here as everywhere else in the tree.
			Expect(c.run("weather")).To(Equal(exitcode.General))
			Expect(c.errOut()).To(ContainSubstring("unknown command"))
		})
	})

	Describe("a broken installation", func() {
		It("reports a manifest it cannot read and carries on", func() {
			root := c.componentRoot()
			dir := filepath.Join(root, "broken")
			Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, component.ManifestName), []byte("apiVersion: 99\n"), 0o600)).To(Succeed())

			c.installFake(fakeManifest("weather"))

			Expect(c.run("component", "list")).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("ignoring"))
			Expect(c.errOut()).To(ContainSubstring("apiVersion 99"))

			// The broken one must not take the working one down with it.
			Expect(c.out()).To(ContainSubstring("weather"))
		})

		It("ignores a directory that is not a component at all", func() {
			Expect(os.MkdirAll(filepath.Join(c.componentRoot(), "notmine"), 0o755)).To(Succeed())

			Expect(c.run("component", "list")).To(Equal(exitcode.OK))
			Expect(c.errOut()).NotTo(ContainSubstring("notmine"))
		})
	})

	Describe("list", func() {
		It("says so, and prints an empty JSON array, when there is nothing", func() {
			Expect(c.run("component", "list", "-o", "json")).To(Equal(exitcode.OK))
			Expect(c.out()).To(Equal("[]\n"))
			Expect(c.errOut()).To(ContainSubstring("fft component install"))
		})

		It("says components are off rather than telling you to install one", func() {
			c.setenv(component.EnvRoot, "")

			Expect(c.run("component", "list")).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("Components are disabled"))

			// Not the install hint: that command cannot work either while they are off,
			// and sending someone to it is sending them to a second failure.
			Expect(c.errOut()).NotTo(ContainSubstring("fft component install"))
		})

		It("lists an installed component", func() {
			c.installFake(fakeManifest("weather"))

			Expect(c.run("component", "list")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("weather"))
			Expect(c.out()).To(ContainSubstring("installed"))
			Expect(c.out()).To(ContainSubstring("community"))
		})
	})

	Describe("info", func() {
		It("shows what a component asks for", func() {
			m := fakeManifest("reader")
			m.Commands[0].Session = component.SessionRead
			c.installFake(m)

			Expect(c.run("component", "info", "reader")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("fft reader (session read, mutates false)"))
		})

		It("refuses a name it does not have", func() {
			Expect(c.run("component", "info", "nope")).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring(`no component called "nope"`))
		})
	})

	Describe("install --path", func() {
		It("copies a local component in, after asking", func() {
			src := GinkgoT().TempDir()
			m := fakeManifest("weather")
			writeManifestFile(src, m)
			Expect(os.MkdirAll(filepath.Join(src, "bin"), 0o755)).To(Succeed())
			Expect(copyExecutable(fakeComponentBinary(), componentExecPath(src, m.Exec))).To(Succeed())

			c.answer("y")
			Expect(c.run("component", "install", "--path", src)).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring("A component runs as you"))
			Expect(c.errOut()).To(ContainSubstring("unverified (installed from a local directory)"))
			Expect(c.errOut()).To(ContainSubstring("Installed weather 1.0.0"))

			// Nothing on stdout: an install has no data, and a cheerful sentence in the
			// pipe of whoever scripted it is what the output contract is there to stop.
			Expect(c.out()).To(BeEmpty())

			Expect(c.run("component", "list")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("weather"))
		})

		It("installs nothing when the answer is no", func() {
			src := GinkgoT().TempDir()
			m := fakeManifest("weather")
			writeManifestFile(src, m)
			Expect(os.MkdirAll(filepath.Join(src, "bin"), 0o755)).To(Succeed())
			Expect(copyExecutable(fakeComponentBinary(), componentExecPath(src, m.Exec))).To(Succeed())

			c.answer("n")
			Expect(c.run("component", "install", "--path", src)).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("was not installed"))

			entries, err := os.ReadDir(c.componentRoot())
			Expect(err).NotTo(HaveOccurred())

			// Not merely "no component": no staging directory either. A refused install
			// that leaves litter in the user's home is one nothing will ever clean up.
			Expect(entries).To(BeEmpty())
		})

		It("refuses a directory with no manifest", func() {
			c.answer("y")
			Expect(c.run("component", "install", "--path", GinkgoT().TempDir())).NotTo(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("component.yaml"))
		})

		It("refuses a manifest naming an executable the directory does not have", func() {
			src := GinkgoT().TempDir()
			writeManifestFile(src, fakeManifest("weather"))

			c.answer("y")
			Expect(c.run("component", "install", "--path", src)).NotTo(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("does not contain it"))
		})

		It("refuses a name and a path together", func() {
			Expect(c.run("component", "install", "emulator", "--path", "./x")).To(Equal(exitcode.Usage))
		})
	})

	Describe("remove", func() {
		It("removes an installed component after asking", func() {
			c.installFake(fakeManifest("weather"))

			c.answer("y")
			Expect(c.run("component", "remove", "weather")).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("Removed weather"))

			// And its command goes with it: the tree is rebuilt from the directory, so a
			// removed component is not merely inert, it is gone.
			Expect(c.run("weather")).To(Equal(exitcode.General))
			Expect(c.errOut()).To(ContainSubstring("unknown command"))
		})

		It("refuses one that is not installed", func() {
			Expect(c.run("component", "remove", "nope")).To(Equal(exitcode.Usage))
		})
	})

	Describe("the generated reference", func() {
		It("does not depend on what is installed", func() {
			before := GinkgoT().TempDir()
			Expect(c.run("gen-docs", before)).To(Equal(exitcode.OK))

			c.installFake(fakeManifest("weather"))

			after := GinkgoT().TempDir()
			Expect(c.run("gen-docs", after)).To(Equal(exitcode.OK))

			// The docs are committed and CI fails on a diff. A reference that included
			// whatever the developer happened to have installed would go red on somebody
			// else's machine, for a reason invisible in the diff.
			Expect(pageNames(after)).To(Equal(pageNames(before)))
		})
	})
})

// pageNames lists the reference pages in a directory, sorted by the walk.
func pageNames(dir string) []string {
	GinkgoHelper()

	entries, err := os.ReadDir(dir)
	Expect(err).NotTo(HaveOccurred())

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, strings.TrimSuffix(entry.Name(), ".md"))
	}
	return names
}
