package tui

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// twoComponents is `fft component list` with the emulator not yet installed.
const twoComponents = `[
  {"name":"emulator","kind":"command","status":"available","origin":"first-party",
   "source":"Joessst-Dev/fft-cli","description":"Run a local offline fulfillmenttools API emulator",
   "commands":["fft emulator"]},
  {"name":"shipper","kind":"transport","version":"1.2.0","status":"installed","origin":"community",
   "source":"acme/fft-shipper","targets":["WEBHOOK"]}
]`

var _ = Describe("the Components screen", func() {
	var h *harness

	BeforeEach(func() {
		h = newHarness(Options{})
		h.loaded(twoProjects, validToken)
	})

	listRuns := func() int {
		return strings.Count(strings.Join(h.r.commandLines(), "\n"), "component list")
	}

	It("reads the list only once somebody looks at it", func() {
		Expect(listRuns()).To(BeZero())

		h.press("8")
		id := h.lookup("component", "list")
		Expect(h.r.invocation(id).Background).To(BeTrue(), "a read nobody asked for stays out of the history")
		Expect(h.view()).To(ContainSubstring("Loading components…"))

		h.finishID(id, ok(twoComponents))
		Expect(h.view()).To(ContainSubstring("NAME"))
		Expect(h.view()).To(ContainSubstring("emulator"))
		Expect(h.view()).To(ContainSubstring("available"))
		Expect(h.view()).To(ContainSubstring("shipper"))
		Expect(h.view()).To(ContainSubstring("installed"))
	})

	It("shows everything the manifest says, without a second run", func() {
		h.press("8")
		h.finish(ok(twoComponents), "component", "list")

		before := len(h.r.started)
		h.press("enter")
		Expect(len(h.r.started)).To(Equal(before), "the list row is the info document")
		Expect(h.view()).To(ContainSubstring("Run a local offline fulfillmenttools API emulator"))
		Expect(h.view()).To(ContainSubstring("Commands: fft emulator"))
		Expect(h.view()).To(ContainSubstring("Not installed."))
		Expect(h.view()).To(ContainSubstring("$ fft component info emulator"))

		h.press("esc")
		Expect(h.view()).To(ContainSubstring("NAME"))
	})

	Describe("installing", func() {
		BeforeEach(func() {
			h.press("8")
			h.finish(ok(twoComponents), "component", "list")
		})

		It("opens on the selected component when it is one that is not installed", func() {
			h.press("a")
			Expect(h.view()).To(ContainSubstring("Install a component"))
			Expect(h.view()).To(ContainSubstring("emulator"))
			Expect(h.m.components.equivalent().line).To(Equal("fft component install emulator"))
		})

		It("sends the install without --yes, and lets the command ask its own question", func() {
			h.press("a", "enter")
			id := h.lookup("component", "install", "emulator")
			Expect(h.r.args(id)).NotTo(ContainElement("--yes"))

			h.ask(id, "Install emulator from Joessst-Dev/fft-cli? It runs as you.", "")
			h.wait()
			Expect(h.view()).To(ContainSubstring("It runs as you."))

			h.press("y")
			Expect(h.r.answers).To(HaveLen(1))
			Expect(h.r.answers[0].yes).To(BeTrue())

			h.finishID(id, ok(""))
			Expect(h.view()).To(ContainSubstring("Installed emulator."))
			Expect(listRuns()).To(Equal(2), "the list is read again")
		})

		It("says nothing was installed when the command's question is answered no", func() {
			h.press("a", "enter")
			id := h.lookup("component", "install", "emulator")
			h.ask(id, "Install emulator?", "")
			h.wait()
			h.press("n")
			h.finishID(id, Result{ExitCode: exitcode.Interrupted})
			Expect(h.view()).To(ContainSubstring("Nothing was installed."))
		})

		It("shows why when the install fails, back on the list", func() {
			h.press("a", "enter")
			h.finish(failed(exitcode.Unavailable, "github.com: connection refused"), "component", "install", "emulator")
			Expect(h.view()).NotTo(ContainSubstring("Install a component"))
			Expect(h.view()).To(ContainSubstring("installing emulator failed: exit 9"))
			Expect(h.view()).To(ContainSubstring("connection refused"))
		})

		It("spells a directory as --path, and anything else as the argument", func() {
			Expect(installArgs("emulator")).To(Equal([]string{"component", "install", "emulator"}))
			Expect(installArgs("acme/fft-shipper@v1.2.3")).
				To(Equal([]string{"component", "install", "acme/fft-shipper@v1.2.3"}))
			Expect(installArgs("./my-component")).To(Equal([]string{"component", "install", "--path", "./my-component"}))
			Expect(installArgs("/tmp/x")).To(Equal([]string{"component", "install", "--path", "/tmp/x"}))
			Expect(installArgs("~/x")).To(Equal([]string{"component", "install", "--path", "~/x"}))
		})

		It("cancels with esc, sending nothing", func() {
			before := len(h.r.started)
			h.press("a", "esc")
			Expect(len(h.r.started)).To(Equal(before))
			Expect(h.view()).To(ContainSubstring("NAME"))
		})
	})

	Describe("upgrading and removing", func() {
		BeforeEach(func() {
			h.press("8")
			h.finish(ok(twoComponents), "component", "list")
		})

		It("refuses on a component that is not installed, and sends nothing", func() {
			before := len(h.r.started)
			h.press("u")
			Expect(len(h.r.started)).To(Equal(before))
			Expect(h.view()).To(ContainSubstring("emulator is not installed"))
		})

		It("upgrades the selected component without --yes", func() {
			h.press("down", "u")
			id := h.lookup("component", "upgrade", "shipper")
			Expect(h.r.args(id)).NotTo(ContainElement("--yes"))
			h.finishID(id, ok(""))
			Expect(h.view()).To(ContainSubstring("Upgraded shipper."))
		})

		It("removes the selected component, and reads the list again either way", func() {
			h.press("down", "d")
			id := h.lookup("component", "remove", "shipper")
			h.finishID(id, failed(exitcode.NotFound, "no such component"))
			Expect(h.view()).To(ContainSubstring("removing shipper failed"))
			Expect(listRuns()).To(Equal(2))
		})
	})

	It("says so when the list cannot be read", func() {
		h.press("8")
		h.finish(failed(exitcode.Usage, "components are disabled: FFT_COMPONENT_DIR is set to the empty string"),
			"component", "list")
		Expect(h.view()).To(ContainSubstring("listing the components failed: exit 2"))
		Expect(h.view()).To(ContainSubstring("components are disabled"))
	})
})
