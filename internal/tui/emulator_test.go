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

	Describe("pointing the session at it", func() {
		// selectEmulatorRow puts the cursor on the emulator row, which is last: the
		// two configured projects come from `project list`, and the UI adds this one.
		selectEmulatorRow := func() {
			GinkgoHelper()
			h.press("down", "down")
			// The row carries a * of its own once the session is using it.
			Expect(h.view()).To(MatchRegexp(`>\s+\*?\s*emulator`))
		}

		It("starts the emulator first, and points the session only once it is listening", func() {
			open(emulatorInstalled)
			h.press("esc")
			selectEmulatorRow()
			h.press("enter")

			id := h.lookup("emulator", "--port", "8080")
			Expect(h.r.invocation(id).Stream).To(BeTrue())
			Expect(h.r.emulators).To(BeEmpty(), "pointed at a port that is not answering yet")
			// The pane is where the output goes, so it is what the row hands over to.
			Expect(h.view()).To(ContainSubstring("Status: starting"))
			Expect(h.view()).To(ContainSubstring("Session: not using it"))

			h.chunk(id, "fft emulator listening on http://localhost:8080\n")

			Expect(h.r.emulators).To(Equal([]string{"http://localhost:8080"}))
			Expect(h.view()).To(ContainSubstring("this session is using it"))
			Expect(h.view()).To(ContainSubstring("fft · emulator (http://localhost:8080)"))
			// The credential state is read again: it is the environment's now.
			h.lookup("auth", "status")

			h.press("esc")
			Expect(h.view()).To(ContainSubstring("Now using the emulator at http://localhost:8080."))
			Expect(h.view()).To(MatchRegexp(`\* emulator`))
		})

		It("points the session at once when it is already listening", func() {
			open(emulatorInstalled)
			h.press("s")
			h.chunk(h.lookup("emulator", "--port", "8080"), "fft emulator listening on http://localhost:8080\n")
			Expect(h.r.emulators).To(BeEmpty(), "s runs it; it does not point the session")

			h.press("esc")
			selectEmulatorRow()
			h.press("enter")

			Expect(h.r.emulators).To(Equal([]string{"http://localhost:8080"}))
			Expect(h.view()).To(ContainSubstring("Now using the emulator at http://localhost:8080."))
		})

		It("points nothing when the component is missing, and says where to install it", func() {
			open(twoComponents)
			h.press("esc")
			selectEmulatorRow()

			before := len(h.r.started)
			h.press("enter")
			Expect(len(h.r.started)).To(Equal(before), "nothing to run, so nothing was run")
			Expect(h.r.emulators).To(BeEmpty())
			Expect(h.view()).To(ContainSubstring("The emulator is not installed. Press 8 for Components"))
		})

		It("stays pointed at an emulator that stops, and says so rather than moving the session", func() {
			open(emulatorInstalled)
			h.press("esc")
			selectEmulatorRow()
			h.press("enter")
			id := h.lookup("emulator", "--port", "8080")
			h.chunk(id, "fft emulator listening on http://localhost:8080\n")

			h.finishID(id, Result{ExitCode: exitcode.Interrupted})

			Expect(h.r.emulators).To(Equal([]string{"http://localhost:8080"}), "the session was moved on its own")
			Expect(h.view()).To(ContainSubstring("fft · emulator (http://localhost:8080)"))
			Expect(h.view()).To(ContainSubstring("The emulator has stopped, and this session is still pointed at it"))

			h.press("esc")
			Expect(h.view()).To(ContainSubstring("Using the emulator, which is not running"))
		})

		It("goes back to a configured project, and stops pointing at the emulator", func() {
			open(emulatorInstalled)
			h.press("esc")
			selectEmulatorRow()
			h.press("enter")
			h.chunk(h.lookup("emulator", "--port", "8080"), "fft emulator listening on http://localhost:8080\n")

			h.press("esc", "up", "up")
			h.press("enter")
			h.finish(ok(`{}`), "project", "use", "staging")

			Expect(h.r.emulators).To(Equal([]string{"http://localhost:8080", ""}))
			Expect(h.view()).To(ContainSubstring("Now using staging."))
			Expect(h.view()).To(ContainSubstring("fft · staging"))
		})

		It("leaves the session alone when a project is chosen while it is still starting", func() {
			open(emulatorInstalled)
			h.press("esc")
			selectEmulatorRow()
			h.press("enter")
			id := h.lookup("emulator", "--port", "8080")

			// Changed their mind before the port was bound.
			h.press("esc", "up", "up")
			h.press("enter")
			h.finish(ok(`{}`), "project", "use", "staging")
			Expect(h.r.emulators).To(BeEmpty())

			// The ready line arrives after, and must not overrule what was asked for
			// last: the session stays on the project.
			h.chunk(id, "fft emulator listening on http://localhost:8080\n")
			Expect(h.r.emulators).To(BeEmpty(), "the session was moved after the user chose a project")
			Expect(h.view()).To(ContainSubstring("fft · staging"))
		})

		It("still points the session at it once ready, when an unrelated project removal invalidated the current project's cache while it was starting", func() {
			open(emulatorInstalled)
			h.press("esc")
			selectEmulatorRow()
			h.press("enter")
			id := h.lookup("emulator", "--port", "8080")

			// Still binding the port. staging is the project the session would go
			// back to, and removing it invalidates the cached current-project state
			// (session.switches) exactly the way a competing selection does — but the
			// user has not chosen anything else, and the pending arm must survive it.
			h.press("esc", "up", "up")
			h.press("d")
			h.wait()
			h.typeText("staging")
			h.press("enter")
			h.finish(ok(`{}`), "project", "remove", "staging", "--yes")
			Expect(h.r.emulators).To(BeEmpty(), "the emulator has not reported ready yet")

			h.chunk(id, "fft emulator listening on http://localhost:8080\n")
			Expect(h.r.emulators).To(Equal([]string{"http://localhost:8080"}),
				"an unrelated project removal silently cancelled the pending arm")
		})

		It("starts it again when the session is pointed at one that has stopped", func() {
			open(emulatorInstalled)
			h.press("esc")
			selectEmulatorRow()
			h.press("enter")
			first := h.lookup("emulator", "--port", "8080")
			h.chunk(first, "fft emulator listening on http://localhost:8080\n")
			h.finishID(first, Result{ExitCode: exitcode.Interrupted})
			Expect(h.m.s.usingEmulator()).To(BeTrue(), "stopping it does not move the session")

			h.press("esc")
			selectEmulatorRow()
			started := len(h.r.started)
			h.press("enter")

			Expect(len(h.r.started)).To(BeNumerically(">", started),
				"enter on a session pointed at a stopped emulator must start it again")
			h.lookup("emulator", "--port", "8080")
			Expect(h.view()).To(ContainSubstring("Status: starting"))
		})

		It("keeps working against the emulator when the project behind it is removed", func() {
			open(emulatorInstalled)
			h.press("esc")
			selectEmulatorRow()
			h.press("enter")
			h.chunk(h.lookup("emulator", "--port", "8080"), "fft emulator listening on http://localhost:8080\n")
			Expect(h.r.emulators).To(Equal([]string{"http://localhost:8080"}))

			// staging is the project the session would go back to, and it is removed.
			h.press("esc", "up", "up")
			h.press("d")
			h.wait()
			h.typeText("staging")
			h.press("enter")
			h.finish(ok(`{}`), "project", "remove", "staging", "--yes")

			Expect(h.r.emulators).To(Equal([]string{"http://localhost:8080"}),
				"removing a project is not a request to stop using the emulator")
			Expect(h.m.s.usingEmulator()).To(BeTrue())
		})

		It("keeps using the emulator when the first project is added, and offers the switch", func() {
			// A first run: nothing configured, working against the emulator. fft makes
			// the added project active, and the UI must not follow that onto a tenant.
			h = newHarness(Options{})
			h.loaded(`[]`, `{"project":"","store":"keyring","signIn":"none","token":"none"}`)
			h.press("e")
			h.finish(ok(emulatorInstalled), "component", "list")
			h.press("s")
			h.chunk(h.lookup("emulator", "--port", "8080"), "fft emulator listening on http://localhost:8080\n")
			h.press("esc")
			h.press("enter")
			Expect(h.m.s.usingEmulator()).To(BeTrue())

			h.press("a")
			h.typeText("demo")
			h.press("tab")
			h.typeText("https://demo.example.com")
			h.press("tab")
			h.typeText("AIzaSyKey")
			h.press("tab", "tab")
			h.typeText("bot")
			h.press("tab")
			h.typeText("acme")
			h.press("tab")
			h.typeText("pre")
			h.press("tab", "tab")
			h.typeText("secret")
			h.press("ctrl+s")
			h.finishID(RunID(len(h.r.started)), Result{ExitCode: 0, Stdout: []byte(`{"name":"demo","active":true}`)})

			Expect(h.m.s.usingEmulator()).To(BeTrue(), "adding a project moved the session off the emulator")
			// Offered, not taken: the switch is a question, and it says what it costs.
			Expect(h.view()).To(ContainSubstring("Switch to demo now?"))
			Expect(h.view()).To(ContainSubstring("This session stops using the emulator"))

			h.wait()
			h.press("n")
			Expect(h.m.s.usingEmulator()).To(BeTrue())
			Expect(h.view()).To(ContainSubstring("but this session is still using the emulator"))
			Expect(h.r.emulators).To(Equal([]string{"http://localhost:8080"}))
		})

		It("says why a restart could not bind, rather than that it stopped", func() {
			open(emulatorInstalled)
			h.press("esc")
			selectEmulatorRow()
			h.press("enter")
			first := h.lookup("emulator", "--port", "8080")
			h.chunk(first, "fft emulator listening on http://localhost:8080\n")
			h.finishID(first, Result{ExitCode: exitcode.Interrupted})
			Expect(h.view()).To(ContainSubstring("still pointed at it"))

			h.press("esc")
			selectEmulatorRow()
			h.press("enter")
			h.finish(failed(exitcode.General, "listen tcp 127.0.0.1:8080: address already in use"),
				"emulator", "--port", "8080")

			Expect(h.view()).To(ContainSubstring("running the emulator failed: exit 1"))
			Expect(h.view()).To(ContainSubstring("address already in use"))
			Expect(h.view()).NotTo(ContainSubstring("The emulator has stopped"),
				"the real failure was overwritten by the stopped notice")
		})

		It("drops a pending arm when the project already in use is chosen again", func() {
			open(emulatorInstalled)
			h.press("esc")
			selectEmulatorRow()
			h.press("enter")
			id := h.lookup("emulator", "--port", "8080")

			// staging is already the current project, so this changes nothing about the
			// session — but it is still the user contradicting the arm.
			h.press("esc", "up", "up")
			h.press("enter")
			h.finish(ok(`{}`), "project", "use", "staging")

			h.chunk(id, "fft emulator listening on http://localhost:8080\n")
			Expect(h.r.emulators).To(BeEmpty(), "the arm survived a competing selection")
		})

		It("refuses to protect or remove a row that is not in the config file", func() {
			open(emulatorInstalled)
			h.press("esc")
			selectEmulatorRow()

			before := len(h.r.started)
			h.press("r")
			Expect(h.view()).To(ContainSubstring("Nothing was sent: the emulator is not a configured project"))
			h.press("d")
			Expect(h.view()).To(ContainSubstring("Nothing was sent: the emulator is not a configured project"))
			Expect(len(h.r.started)).To(Equal(before))
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
