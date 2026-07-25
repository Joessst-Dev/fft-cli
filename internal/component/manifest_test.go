package component_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/component"
)

var _ = Describe("the manifest", func() {
	const valid = `
apiVersion: 1
name: weather
version: 1.0.0
description: forecasts
kind: command
exec: bin/fft-weather
commands:
  - name: weather
    short: Show the forecast
    session: none
`

	It("reads a well-formed manifest", func() {
		m, err := component.ParseManifest([]byte(valid), "component.yaml")
		Expect(err).NotTo(HaveOccurred())

		Expect(m.Name).To(Equal("weather"))
		Expect(m.Kind).To(Equal(component.KindCommand))
		Expect(m.Commands).To(HaveLen(1))
		Expect(m.Commands[0].Session).To(Equal(component.SessionNone))
	})

	DescribeTable("refuses a manifest fft cannot honour",
		func(yaml, because string) {
			_, err := component.ParseManifest([]byte(yaml), "component.yaml")
			Expect(err).To(MatchError(ContainSubstring(because)))
		},

		// A contract fft does not speak is refused rather than read leniently: the
		// fields it would silently ignore are the ones a later version added to change
		// what a component does.
		Entry("a newer contract", `
apiVersion: 2
name: weather
kind: command
exec: bin/x
commands: [{name: weather, short: s, session: none}]
`, "apiVersion 2 is not the contract"),

		// Same reasoning one level down: a field fft has never heard of is a manifest
		// written against something else.
		Entry("a field fft does not know", `
apiVersion: 1
name: weather
kind: command
exec: bin/x
sandbox: true
commands: [{name: weather, short: s, session: none}]
`, "field sandbox not found"),

		Entry("a name that is not a name", `
apiVersion: 1
name: ../../etc
kind: command
exec: bin/x
commands: [{name: weather, short: s, session: none}]
`, "must be lowercase letters"),

		// The two that matter most: exec is what gets executed, so a manifest may not
		// name a file outside the directory the archive was unpacked into.
		Entry("an absolute exec", `
apiVersion: 1
name: weather
kind: command
exec: /usr/bin/env
commands: [{name: weather, short: s, session: none}]
`, "must be a relative"),

		Entry("an exec that escapes", `
apiVersion: 1
name: weather
kind: command
exec: ../../../bin/sh
commands: [{name: weather, short: s, session: none}]
`, "leaves the component's directory"),

		Entry("an unknown session", `
apiVersion: 1
name: weather
kind: command
exec: bin/x
commands: [{name: weather, short: s, session: root}]
`, `unknown session "root"`),

		// A command that writes needs a session that can, and fft will not pick one of
		// the two for it.
		Entry("mutates without a write session", `
apiVersion: 1
name: weather
kind: command
exec: bin/x
commands: [{name: weather, short: s, session: read, mutates: true}]
`, "a write needs session"),

		// The inverse, and the more dangerous of the two: the read-only gate reads
		// mutates, so a command holding a writable session while declaring it writes
		// nothing would sail straight through it.
		Entry("a write session without mutates", `
apiVersion: 1
name: weather
kind: command
exec: bin/x
commands: [{name: weather, short: s, session: write}]
`, "the read-only gate reads mutates"),

		// The FFT_ namespace is fft's to hand over. A manifest that could ask for
		// FFT_PASSWORD by name would make the session level negotiable by the component
		// being decided about.
		Entry("an FFT_ variable in env", `
apiVersion: 1
name: weather
kind: command
exec: bin/x
env: [FFT_PASSWORD]
commands: [{name: weather, short: s, session: none}]
`, "cannot ask for an FFT_ variable"),

		Entry("a command component with no commands", `
apiVersion: 1
name: weather
kind: command
exec: bin/x
`, "declares no commands"),

		Entry("a transport with no targets", `
apiVersion: 1
name: weather
kind: transport
exec: bin/x
`, "declares no targets"),

		Entry("an unknown kind", `
apiVersion: 1
name: weather
kind: sidecar
exec: bin/x
`, `unknown kind "sidecar"`),
	)
})

var _ = Describe("ParseSource", func() {
	DescribeTable("reads an install spec",
		func(spec, repo, version, name string) {
			src, err := component.ParseSource(spec)
			Expect(err).NotTo(HaveOccurred())

			Expect(src.Repo).To(Equal(repo))
			Expect(src.Version).To(Equal(version))
			Expect(src.Name).To(Equal(name))
		},

		// A bare name is a component fft ships, so it resolves to fft's own releases.
		Entry("a first-party name", "emulator", component.DefaultRepo, "", "emulator"),
		Entry("a repository", "acme/fft-thing", "acme/fft-thing", "", ""),
		Entry("a pinned repository", "acme/fft-thing@v1.2.3", "acme/fft-thing", "v1.2.3", ""),
	)

	DescribeTable("refuses what it cannot resolve",
		func(spec, because string) {
			_, err := component.ParseSource(spec)
			Expect(err).To(MatchError(ContainSubstring(because)))
		},

		Entry("nothing", "", "no component named"),
		// Pasting the release page's URL is a reasonable thing to do, and telling
		// someone the form that works beats guessing which path segment is the repo.
		Entry("a URL", "https://github.com/acme/thing", "name the repository as owner/repo"),
		Entry("too many segments", "github.com/acme/thing", "is not an owner/repo"),
		Entry("a name that is not one", "Weather", "neither a component name nor an owner/repo"),
	)
})
