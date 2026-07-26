package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/component"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("fft component init", func() {
	var c *cli

	BeforeEach(func() { c = newCLI() })

	// parseManifestAt reads and validates the component.yaml a scaffold wrote, which is
	// the acceptance criterion: what init writes, install's own parser accepts.
	parseManifestAt := func(dir string) component.Manifest {
		GinkgoHelper()
		data, err := os.ReadFile(filepath.Join(dir, component.ManifestName))
		Expect(err).NotTo(HaveOccurred())
		m, err := component.ParseManifest(data, component.ManifestName)
		Expect(err).NotTo(HaveOccurred())
		return m
	}

	It("stamps a command skeleton whose manifest parses, and says nothing on stdout", func() {
		dir := GinkgoT().TempDir()
		Expect(c.run("component", "init", "pricing", "--dir", dir)).To(Equal(exitcode.OK))

		pricing := filepath.Join(dir, "pricing")
		Expect(filepath.Join(pricing, "component.yaml")).To(BeARegularFile())
		Expect(filepath.Join(pricing, "bin", "fft-pricing")).To(BeAnExistingFile())
		Expect(filepath.Join(pricing, "README.md")).To(BeARegularFile())

		m := parseManifestAt(pricing)
		Expect(m.Name).To(Equal("pricing"))
		Expect(m.Kind).To(Equal(component.KindCommand))

		// Init emits no data; the summary is a courtesy on stderr.
		Expect(c.out()).To(BeEmpty())
		Expect(c.errOut()).To(ContainSubstring("Scaffolded"))
	})

	It("makes the command script executable", func() {
		if runtime.GOOS == "windows" {
			Skip("a Unix executable bit does not apply on Windows")
		}
		dir := GinkgoT().TempDir()
		Expect(c.run("component", "init", "pricing", "--dir", dir)).To(Equal(exitcode.OK))

		info, err := os.Stat(filepath.Join(dir, "pricing", "bin", "fft-pricing"))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm() & 0o100).NotTo(BeZero())
	})

	It("derives mutates from a write session", func() {
		dir := GinkgoT().TempDir()
		Expect(c.run("component", "init", "billing", "--session", "write", "--dir", dir)).To(Equal(exitcode.OK))

		m := parseManifestAt(filepath.Join(dir, "billing"))
		Expect(m.Commands[0].Session).To(Equal(component.SessionWrite))
		Expect(m.Commands[0].Mutates).To(BeTrue())
	})

	It("stamps a Go transport with a module and a main.go", func() {
		dir := GinkgoT().TempDir()
		Expect(c.run("component", "init", "ship", "--kind", "transport", "--dir", dir)).To(Equal(exitcode.OK))

		ship := filepath.Join(dir, "ship")
		Expect(filepath.Join(ship, "main.go")).To(BeARegularFile())
		Expect(filepath.Join(ship, "go.mod")).To(BeARegularFile())

		m := parseManifestAt(ship)
		Expect(m.Kind).To(Equal(component.KindTransport))
		Expect(m.Targets).NotTo(BeEmpty())
	})

	It("stamps a shebanged interpreter script for --lang python", func() {
		dir := GinkgoT().TempDir()
		Expect(c.run("component", "init", "weather", "--lang", "python", "--dir", dir)).To(Equal(exitcode.OK))

		data, err := os.ReadFile(filepath.Join(dir, "weather", "bin", "fft-weather"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(HavePrefix("#!/usr/bin/env python3"))
	})

	It("refuses to write over a non-empty directory", func() {
		dir := GinkgoT().TempDir()
		Expect(c.run("component", "init", "pricing", "--dir", dir)).To(Equal(exitcode.OK))

		Expect(c.run("component", "init", "pricing", "--dir", dir)).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("already exists and is not empty"))
	})

	It("refuses --session on a transport, which has no command to give it to", func() {
		dir := GinkgoT().TempDir()
		Expect(c.run("component", "init", "ship", "--kind", "transport", "--session", "read", "--dir", dir)).
			To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("a transport declares no commands"))
	})

	It("reports an unknown language", func() {
		dir := GinkgoT().TempDir()
		Expect(c.run("component", "init", "x", "--lang", "rust", "--dir", dir)).To(Equal(exitcode.Usage))
		Expect(c.errOut()).To(ContainSubstring("unknown --lang"))
	})

	It("rejects a name the manifest rules forbid, writing nothing", func() {
		dir := GinkgoT().TempDir()
		Expect(c.run("component", "init", "Bad_Name", "--dir", dir)).To(Equal(exitcode.Usage))

		// The path-safety argument — validate the manifest before touching disk — only
		// holds if nothing was created, so pin it: a reorder that wrote first would fail here.
		entries, err := os.ReadDir(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty(), "a rejected name must not create anything")
	})

	It("refuses a target path that is already a file, as a usage error", func() {
		dir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "pricing"), []byte("x"), 0o600)).To(Succeed())

		// os.ReadDir on a file returns ENOTDIR, not IsNotExist: it must still exit 2, the
		// same class as the non-empty-directory refusal, not the generic 1 a bare error gives.
		Expect(c.run("component", "init", "pricing", "--dir", dir)).To(Equal(exitcode.Usage))
	})

	It("v-prefixes the go.mod require of a Go transport on a release build", func() {
		// GoReleaser stamps buildinfo.Version without the leading v; a module version needs
		// one. This exercises the real pinning branch scaffoldFromFlags takes, which the
		// Scaffold.Build unit test cannot, since a dev build never reaches it.
		c.asVersion("1.4.2")

		dir := GinkgoT().TempDir()
		Expect(c.run("component", "init", "ship", "--kind", "transport", "--dir", dir)).To(Equal(exitcode.OK))

		data, err := os.ReadFile(filepath.Join(dir, "ship", "go.mod"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(ContainSubstring("require github.com/Joessst-Dev/fft-cli v1.4.2"))
	})

	// The whole point, end to end: a freshly-initialised command component installs by
	// path and runs, printing the arguments it was handed. The generated executable is a
	// real shell script, which does not run on Windows — the same reason the fake
	// component is a re-executed binary rather than a script.
	It("produces a component that installs and runs", func() {
		if runtime.GOOS == "windows" {
			Skip("the generated command is a shell script")
		}

		dir := GinkgoT().TempDir()
		Expect(c.run("component", "init", "pricing", "--dir", dir)).To(Equal(exitcode.OK))

		c.answer("y")
		Expect(c.run("component", "install", "--path", filepath.Join(dir, "pricing"))).To(Equal(exitcode.OK))

		Expect(c.run("pricing", "hello", "--city", "Berlin")).To(Equal(exitcode.OK))
		Expect(c.out()).To(ContainSubstring("args: hello --city Berlin"))
	})

	// The shell transport reads the frame's op and id with sed, so it has to answer the
	// frame's own id — not a nested one a send frame carries in its target or data. The
	// emulator kills a transport that answers request N as request M, so a scaffold that
	// did would die on its first real delivery.
	It("produces a shell transport that answers the frame's own id past a nested one", func() {
		if runtime.GOOS == "windows" {
			Skip("the generated transport is a shell script")
		}

		dir := GinkgoT().TempDir()
		Expect(c.run("component", "init", "relay", "--kind", "transport", "--lang", "shell", "--dir", dir)).
			To(Equal(exitcode.OK))

		script := filepath.Join(dir, "relay", "bin", "fft-relay")
		frame := `{"id":7,"op":"send","target":{"id":"nested"},"event":"order.created","data":{"id":99,"op":"noop"}}` + "\n"

		cmd := exec.Command(script)
		cmd.Stdin = strings.NewReader(frame)
		out, err := cmd.Output()
		Expect(err).NotTo(HaveOccurred())

		// The response is the frame's id (7), not the data's (99) or the sed default (0).
		Expect(string(out)).To(ContainSubstring(`"id":7`))
		Expect(string(out)).NotTo(ContainSubstring(`"id":99`))
		Expect(string(out)).NotTo(ContainSubstring(`"id":0`))
	})
})
