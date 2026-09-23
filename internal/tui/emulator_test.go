package tui

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// emulatorInstalled is `fft component list` with the emulator present.
const emulatorInstalled = `[
  {"name":"emulator","kind":"command","version":"1.4.0","status":"installed","origin":"first-party",
   "source":"Joessst-Dev/fft-cli","commands":["fft emulator"]}
]`

var _ = Describe("the emulator pane", func() {
	var h *harness

	BeforeEach(func() {
		h = newHarness(Options{})
		h.loaded(twoProjects, validToken)
	})

	// open shows the pane and answers the component read it starts.
	open := func(components string) {
		GinkgoHelper()
		h.press("e")
		h.finish(ok(components), "component", "list")
	}

	It("opens from the Projects screen and says how to point a shell at it", func() {
		open(emulatorInstalled)
		Expect(h.view()).To(ContainSubstring("Emulator"))
		Expect(h.view()).To(ContainSubstring("Status: stopped"))
		Expect(h.view()).To(ContainSubstring("Component: installed"))
		Expect(h.view()).To(ContainSubstring("export FFT_BASE_URL=http://localhost:8080"))
		Expect(h.view()).To(ContainSubstring("export FFT_ID_TOKEN=emulator-token"))
		Expect(h.view()).To(ContainSubstring("$ fft emulator --port 8080"))
	})

	It("leaves the project list alone while it is open", func() {
		open(emulatorInstalled)
		before := len(h.r.started)
		// a would open the add form, d the remove dialog, on the list underneath.
		h.press("a", "d")
		Expect(len(h.r.started)).To(Equal(before))
		Expect(h.view()).To(ContainSubstring("Status: stopped"))

		h.press("esc")
		Expect(h.view()).To(ContainSubstring("BASE URL"))
	})

	It("copies the recipe with c, which is not an fft command", func() {
		open(emulatorInstalled)
		Expect(clipboard(h.press("c"))).To(ContainSubstring("export FFT_FIREBASE_API_KEY=emulator"))
	})

	Describe("running it", func() {
		It("starts a streaming run, and reports it listening once it says so", func() {
			open(emulatorInstalled)
			h.press("s")

			id := h.lookup("emulator", "--port", "8080")
			inv := h.r.invocation(id)
			Expect(inv.Stream).To(BeTrue(), "a server that never ends must report as it goes")
			Expect(inv.Background).To(BeFalse(), "the user asked for it")
			Expect(h.view()).To(ContainSubstring("Status: starting"))

			h.chunk(id, "fft emulator listening on http://localhost:8080\n\nPoint fft at it:\n")
			Expect(h.view()).To(ContainSubstring("Status: running"))
			Expect(h.view()).To(ContainSubstring("The emulator is listening on http://localhost:8080."))
			Expect(h.view()).To(ContainSubstring("fft emulator listening on http://localhost:8080"))
		})

		It("holds back a line until it is whole", func() {
			open(emulatorInstalled)
			h.press("s")
			id := h.lookup("emulator", "--port", "8080")

			h.chunk(id, "GET /api/facilities -> ")
			Expect(h.view()).NotTo(ContainSubstring("GET /api/facilities"))
			h.chunk(id, "200 (0 bytes in)\n")
			Expect(h.view()).To(ContainSubstring("GET /api/facilities -> 200 (0 bytes in)"))
		})

		It("says when output was dropped rather than pretending it arrived", func() {
			open(emulatorInstalled)
			h.press("s")
			id := h.lookup("emulator", "--port", "8080")
			h.send(runEventMsg{ID: id, State: RunRunning, Invocation: h.r.invocation(id), At: h.now,
				Chunk: &Chunk{Stderr: true, Bytes: []byte("later\n"), Dropped: true}})
			Expect(h.view()).To(ContainSubstring("some output was dropped"))
			Expect(h.view()).To(ContainSubstring("later"))
		})

		It("stops the run it started, and reports an interrupt as a clean stop", func() {
			open(emulatorInstalled)
			h.press("s")
			id := h.lookup("emulator", "--port", "8080")
			h.chunk(id, "fft emulator listening on http://localhost:8080\n")

			h.press("s")
			Expect(h.r.cancelled).To(ContainElement(id))

			h.finishID(id, Result{ExitCode: exitcode.Interrupted})
			Expect(h.view()).To(ContainSubstring("Status: stopped"))
			Expect(h.view()).To(ContainSubstring("The emulator has stopped."))
		})

		It("reports a failure to start as one", func() {
			open(emulatorInstalled)
			h.press("s")
			h.finish(failed(exitcode.General, "listen tcp 127.0.0.1:8080: address already in use"),
				"emulator", "--port", "8080")
			Expect(h.view()).To(ContainSubstring("Status: failed"))
			Expect(h.view()).To(ContainSubstring("running the emulator failed: exit 1"))
			Expect(h.view()).To(ContainSubstring("address already in use"))
		})

		It("does not start a second one while the first is running", func() {
			open(emulatorInstalled)
			h.press("s")
			id := h.lookup("emulator", "--port", "8080")
			started := len(h.r.started)

			h.press("s")
			Expect(len(h.r.started)).To(Equal(started), "the second s stops it rather than starting another")
			Expect(h.r.cancelled).To(ContainElement(id))
		})
	})

	It("sends nothing and says where to install when the component is missing", func() {
		open(twoComponents)
		Expect(h.view()).To(ContainSubstring("Component: not installed"))

		before := len(h.r.started)
		h.press("s")
		Expect(len(h.r.started)).To(Equal(before))
		Expect(h.view()).To(ContainSubstring("press 8 for Components"))
	})
})
