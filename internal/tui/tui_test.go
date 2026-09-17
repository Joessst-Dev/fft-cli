package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("the UI", func() {
	var h *harness

	BeforeEach(func() {
		h = newHarness(Options{})
	})

	It("refuses to start without a runner to execute commands with", func() {
		Expect(Run(context.Background(), Options{})).To(MatchError(ContainSubstring("no runner")))
	})

	It("draws on the alternate screen", func() {
		Expect(h.m.View().AltScreen).To(BeTrue())
	})

	Describe("starting up", func() {
		It("lists the projects and reads the credential state, side by side", func() {
			Expect(h.r.commandLines()).To(Equal([]string{"project list", "auth status"}))
			Expect(h.r.exclusive(1)).To(BeFalse())
			Expect(h.r.exclusive(2)).To(BeFalse())
			Expect(h.view()).To(ContainSubstring("Loading projects"))
		})

		It("shows the projects, marking the one it acts on, and the token's remaining life", func() {
			h.loaded(twoProjects, validToken)

			view := h.view()
			Expect(view).To(MatchRegexp(`> \* staging\s+https://staging.example.com`))
			Expect(view).To(MatchRegexp(`  prod\s+https://prod.example.com\s+\S+\s+keyring\s+read-only`))
			Expect(view).To(ContainSubstring("fft · staging · token 42m left"))
			Expect(view).NotTo(ContainSubstring("RO"))
		})

		It("signs in first for the environment's project too, whose token only the session keeps", func() {
			h.loaded(`[{"name":"env","active":true,"ephemeral":true}]`,
				`{"project":"env","store":"env","signIn":"password","token":"none"}`)

			Expect(h.r.exclusive(h.lookup("auth", "whoami"))).To(BeTrue())
			// The store's "none" is about the store, not about the token the session holds.
			Expect(h.view()).To(ContainSubstring("token not stored (environment)"))
			Expect(h.view()).NotTo(ContainSubstring("not signed in"))
		})

		It("signs in first, alone, when the next run would have to", func() {
			h.loaded(twoProjects, `{"project":"staging","store":"keyring","signIn":"password","token":"expired"}`)

			id := h.lookup("auth", "whoami")
			Expect(h.r.exclusive(id)).To(BeTrue())
			Expect(h.r.invocation(id).Background).To(BeTrue(), "the user sent nothing, so history keeps nothing")

			h.finishID(id, ok(`{}`))
			Expect(h.r.commandLines()).To(HaveLen(4))
			Expect(h.r.commandLines()[3]).To(Equal("auth status"))
		})

		DescribeTable("does not sign in ahead of time when it would buy nothing",
			func(status string) {
				h.loaded(twoProjects, status)
				Expect(h.r.commandLines()).To(Equal([]string{"project list", "auth status"}))
			},
			Entry("a valid token", validToken),
			Entry("a fixed id token", `{"project":"env","store":"env","signIn":"idToken","token":"unknown"}`),
		)

		It("shows no project and no error when none is configured", func() {
			h.finish(ok(`[]`), "project", "list")
			h.finish(failed(exitcode.Config, "Error: no active project"), "auth", "status")

			view := h.view()
			Expect(view).To(ContainSubstring("No projects are configured. Press a to add one."))
			Expect(view).To(ContainSubstring("fft · no project"))
			Expect(view).NotTo(ContainSubstring("failed"))
		})
	})

	Describe("the read-only badge", func() {
		It("shows when the current project is configured read-only", func() {
			h = newHarness(Options{Project: "prod"})
			h.loaded(twoProjects, validToken)

			Expect(h.view()).To(ContainSubstring("fft · prod · RO"))
		})

		It("shows for every project when the session itself is read-only", func() {
			h = newHarness(Options{ReadOnly: true})
			h.loaded(twoProjects, validToken)

			Expect(h.view()).To(ContainSubstring("fft · staging · RO"))
			Expect(h.view()).To(MatchRegexp(`staging\s+.*read-only`))
		})
	})

	Describe("a project name edited into the config file by hand", func() {
		const evil = "evil\x1b]52;c;cGF5bG9hZA==\a\u202e"

		BeforeEach(func() {
			h.loaded(`[{"name":"evil\u001b]52;c;cGF5bG9hZA==\u0007\u202e","active":true,"readOnly":true}]`,
				`{"project":"evil\u001b]52;c;cGF5bG9hZA==\u0007\u202e","store":"keyring","signIn":"password",`+
					`"token":"\u001b[2Jvalid"}`)
		})

		expectHarmless := func() {
			GinkgoHelper()
			content := h.m.View().Content
			Expect(content).NotTo(ContainSubstring("\x1b]"))
			Expect(content).NotTo(ContainSubstring("\x1b[2J"))
			Expect(content).NotTo(ContainSubstring("\a"))
			Expect(content).NotTo(ContainSubstring("\u202e"))
		}

		It("does not steer the terminal from the list or the status bar", func() {
			Expect(h.view()).To(ContainSubstring("evil]52;c;cGF5bG9hZA=="))
			expectHarmless()
		})

		It("does not steer it from a dialog", func() {
			h.press("r")
			Expect(h.view()).To(ContainSubstring("Allow writes to evil]52;c;cGF5bG9hZA== again?"))
			expectHarmless()
		})

		It("does not steer it from a notice or a failure", func() {
			h.press("enter")
			Expect(h.view()).To(ContainSubstring("Switching to evil]52"))
			expectHarmless()

			h.finish(failed(exitcode.Config, ""), "project", "use", evil)
			Expect(h.view()).To(ContainSubstring("switching to evil]52;c;cGF5bG9hZA== failed"))
			expectHarmless()
		})
	})

	Describe("switching screens", func() {
		It("goes to a screen by its number", func() {
			h.press("2")
			Expect(h.view()).To(ContainSubstring("[2 Operations]"))

			h.press("5")
			Expect(h.view()).To(ContainSubstring("[5 Templates]"))

			h.press("6")
			Expect(h.view()).To(ContainSubstring("[6 History]"))

			h.press("7")
			Expect(h.view()).To(ContainSubstring("[7 Roles]"))
		})

		It("cycles with tab and shift+tab, and ctrl+p goes back to Projects", func() {
			h.press("tab")
			Expect(h.view()).To(ContainSubstring("[2 Operations]"))
			h.press("shift+tab", "shift+tab")
			Expect(h.view()).To(ContainSubstring("[7 Roles]"))
			h.press("ctrl+p")
			Expect(h.view()).To(ContainSubstring("[1 Projects]"))
		})

		It("toggles the full help with ?", func() {
			Expect(h.view()).NotTo(ContainSubstring("previous"))
			h.press("?")
			Expect(h.view()).To(ContainSubstring("switch project"))
			Expect(h.view()).To(ContainSubstring("running commands"))
		})
	})

	Describe("using a project", func() {
		BeforeEach(func() {
			h.loaded(twoProjects, validToken)
			h.press("down")
		})

		It("shows the command enter would run", func() {
			Expect(h.view()).To(ContainSubstring("$ fft project use prod"))
		})

		It("switches with project use, alone, and only then selects it for later runs", func() {
			h.press("enter")

			id := h.lookup("project", "use", "prod")
			Expect(h.r.exclusive(id)).To(BeTrue())
			Expect(h.r.projects).To(BeEmpty(), "selected before the switch had happened")

			h.finishID(id, ok(""))
			Expect(h.r.projects).To(Equal([]string{"prod"}))
			Expect(h.view()).To(ContainSubstring("Now using prod."))
			Expect(h.view()).To(ContainSubstring("fft · prod"))

			whoami := h.lookup("auth", "whoami")
			Expect(h.r.exclusive(whoami)).To(BeTrue())
			Expect(h.r.invocation(whoami).Background).To(BeTrue())
			h.lookup("project", "list")

			h.finishID(whoami, ok(`{}`))
			h.lookup("auth", "status")
		})

		It("takes u as well as enter", func() {
			h.press("u")
			h.lookup("project", "use", "prod")
		})

		It("ignores a credential state read before the switch and answered after it", func() {
			h.press("ctrl+r")
			stale := h.lookup("auth", "status")
			h.press("enter")
			h.finish(ok(""), "project", "use", "prod")
			h.finishID(stale, ok(validToken))

			Expect(h.view()).To(ContainSubstring("fft · prod · RO · token ?"))
		})

		It("keeps the fresh credential state when a read from before the switch answers last", func() {
			h.press("ctrl+r")
			stale := h.lookup("auth", "status")
			h.press("enter")
			h.finish(ok(""), "project", "use", "prod")
			h.finish(ok(`{}`), "auth", "whoami")
			h.finish(ok(`{"project":"prod","store":"keyring","signIn":"password","token":"valid",
				"expiresAt":"2026-07-12T13:00:00Z"}`), "auth", "status")
			Expect(h.view()).To(ContainSubstring("token 1h00m left"))

			h.finishID(stale, ok(validToken))
			Expect(h.view()).To(ContainSubstring("fft · prod · RO · token 1h00m left"))
		})

		It("does not report a failed read from before the switch", func() {
			h.press("ctrl+r")
			stale := h.lookup("auth", "status")
			h.press("enter")
			h.finish(ok(""), "project", "use", "prod")
			h.finishID(stale, failed(exitcode.Auth, "Error: staging's keychain entry is unreadable"))

			Expect(h.view()).NotTo(ContainSubstring("failed"))
			Expect(h.view()).NotTo(ContainSubstring("staging's keychain"))
		})

		It("names the project a sign-in failed for, even after switching on", func() {
			h.press("enter")
			h.finish(ok(""), "project", "use", "prod")
			signIn := h.lookup("auth", "whoami")
			h.press("up", "enter")
			h.finish(ok(""), "project", "use", "staging")
			h.finishID(signIn, failed(exitcode.Auth, "Error: INVALID_PASSWORD"))

			Expect(h.view()).To(ContainSubstring("signing in to prod failed: exit 4"))
			Expect(h.view()).NotTo(ContainSubstring("signing in to staging failed"))
		})

		It("shows why a switch failed, with the exit code's meaning, and selects nothing", func() {
			h.press("enter")
			h.finish(failed(exitcode.Config, "Error: project \"prod\" is not configured\nRun 'fft project list'."),
				"project", "use", "prod")

			view := h.view()
			Expect(view).To(ContainSubstring("switching to prod failed: exit 3 (no active project, or the config is unusable)"))
			Expect(view).To(ContainSubstring(`Error: project "prod" is not configured`))
			Expect(view).To(ContainSubstring("Run 'fft project list'."))
			Expect(h.r.projects).To(BeEmpty())
		})

		It("keeps what a failed command said from steering the terminal", func() {
			h.press("enter")
			h.finish(failed(exitcode.General, "Error: \x1b[2Jboom\x07"), "project", "use", "prod")

			Expect(h.m.View().Content).NotTo(ContainSubstring("\x1b[2J"))
			Expect(h.m.View().Content).NotTo(ContainSubstring("\x07"))
			Expect(h.view()).To(ContainSubstring("Error: [2Jboom"))
		})

		It("reports a runner that would not take the command", func() {
			h.r.startErr = errShutDown
			h.press("enter")

			Expect(h.view()).To(ContainSubstring("switching to prod failed: exit 1"))
			Expect(h.view()).To(ContainSubstring("shut down"))
		})
	})

	Describe("toggling read-only", func() {
		BeforeEach(func() {
			h.loaded(twoProjects, validToken)
		})

		It("asks first, naming the project, and sends nothing on no", func() {
			h.press("r")
			Expect(h.view()).To(ContainSubstring("Make staging read-only?"))
			Expect(h.view()).To(ContainSubstring("runs: fft project read-only staging"))

			for _, no := range []string{"n", "esc", "enter"} {
				h.press(no)
				Expect(h.view()).NotTo(ContainSubstring("Make staging read-only?"))
				h.press("r")
			}
			Expect(h.r.commandLines()).To(HaveLen(2))
		})

		It("is not answered by pasted text", func() {
			h.press("r")
			h.send(tea.PasteMsg{Content: "y"})

			Expect(h.view()).To(ContainSubstring("Make staging read-only?"))
			Expect(h.r.commandLines()).To(HaveLen(2))
		})

		It("makes a project read-only on yes, without a --yes it does not need", func() {
			h.press("r", "y")

			id := h.lookup("project", "read-only", "staging")
			Expect(h.r.exclusive(id)).To(BeTrue())
			h.finishID(id, ok(""))
			Expect(h.view()).To(ContainSubstring("staging is read-only."))
			h.lookup("project", "list")
		})

		When("the project is read-only", func() {
			BeforeEach(func() {
				h.press("down", "r")
			})

			It("asks for the name to be typed, and takes no y for an answer", func() {
				Expect(h.view()).To(ContainSubstring("Allow writes to prod again?"))
				Expect(h.view()).To(ContainSubstring("Type prod to confirm."))
				Expect(h.view()).To(ContainSubstring("runs: fft project read-only prod --off --yes"))

				h.press("y", "enter")
				Expect(h.view()).To(ContainSubstring("That is not the name; nothing was sent."))
				Expect(h.r.commandLines()).To(HaveLen(2))
			})

			It("sends nothing when cancelled", func() {
				h.typeText("prod")
				h.press("esc")

				Expect(h.view()).NotTo(ContainSubstring("Allow writes"))
				Expect(h.r.commandLines()).To(HaveLen(2))
			})

			It("allows writes again once the name matches, with the typed name as the command's --yes", func() {
				h.typeText("prod")
				h.press("enter")

				id := h.lookup("project", "read-only", "prod", "--off", "--yes")
				Expect(h.r.exclusive(id)).To(BeTrue())
				h.finishID(id, ok(""))
				Expect(h.view()).To(ContainSubstring("prod accepts writes again."))
				Expect(h.view()).NotTo(ContainSubstring("still refuses"))
			})
		})

		It("says the session still refuses writes when it was started read-only", func() {
			h = newHarness(Options{ReadOnly: true})
			h.loaded(twoProjects, validToken)
			h.press("down", "r")
			h.typeText("prod")
			h.press("enter")
			h.finish(ok(""), "project", "read-only", "prod", "--off", "--yes")

			Expect(h.view()).To(ContainSubstring("prod accepts writes again, but this session still refuses every write"))
		})
	})

	Describe("removing a project", func() {
		BeforeEach(func() {
			h.loaded(twoProjects, validToken)
			h.press("down", "d")
		})

		It("asks for the name to be typed, and sends nothing until it is", func() {
			Expect(h.view()).To(ContainSubstring("Remove prod and its stored credentials?"))
			Expect(h.view()).To(ContainSubstring("Type prod to confirm."))

			h.typeText("staging")
			h.press("enter")
			Expect(h.view()).To(ContainSubstring("That is not the name; nothing was sent."))
			Expect(h.r.commandLines()).To(HaveLen(2))
		})

		It("sends nothing when cancelled", func() {
			h.typeText("prod")
			h.press("esc")

			Expect(h.view()).NotTo(ContainSubstring("Type prod to confirm."))
			Expect(h.r.commandLines()).To(HaveLen(2))
		})

		It("takes the keyboard, so a q in the name is typed rather than quitting", func() {
			h.typeText("q")
			Expect(h.view()).To(ContainSubstring("> q"))
		})

		It("takes a pasted name, and still waits for enter", func() {
			h.send(tea.PasteMsg{Content: "prod"})
			Expect(h.view()).To(ContainSubstring("> prod"))
			Expect(h.r.commandLines()).To(HaveLen(2))

			h.press("enter")
			h.lookup("project", "remove", "prod", "--yes")
		})

		It("removes the project with --yes once the name matches", func() {
			h.typeText("prod")
			h.press("enter")

			id := h.lookup("project", "remove", "prod", "--yes")
			Expect(h.r.exclusive(id)).To(BeTrue())
			h.finishID(id, ok(""))
			Expect(h.view()).To(ContainSubstring("Removed prod."))
			Expect(h.r.projects).To(BeEmpty(), "prod was not the UI's project")
		})

		It("gives the choice back to fft when the UI's own project goes", func() {
			h = newHarness(Options{Project: "prod"})
			h.loaded(twoProjects, validToken)
			h.press("d")
			h.typeText("prod")
			h.press("enter")
			h.finish(ok(""), "project", "remove", "prod", "--yes")

			Expect(h.r.projects).To(Equal([]string{""}))
			h.lookup("auth", "status")
		})
	})

	Describe("refreshing the token", func() {
		It("runs auth refresh alone, shows the project a shell would need, and rereads the state", func() {
			h = newHarness(Options{Project: "prod"})
			h.loaded(twoProjects, validToken)

			h.press("R")
			id := h.lookup("auth", "refresh")
			Expect(h.r.exclusive(id)).To(BeTrue())
			Expect(h.m.s.runs.byID[id].display.String()).To(Equal("fft auth refresh --project prod"))

			h.finishID(id, failed(exitcode.Auth, "Error: cannot authenticate"))
			Expect(h.view()).To(ContainSubstring("refreshing the token failed: exit 4 (authentication failed)"))
			h.lookup("auth", "status")
		})
	})

	Describe("the add form", func() {
		const (
			apiKey   = "AIzaSyTypedKey"
			password = "hunter2 with spaces"
		)

		fill := func() {
			h.typeText("qa")
			h.press("tab")
			h.typeText("https://qa.example.com")
			h.press("tab")
			h.typeText(apiKey)
			h.press("tab", "tab")
			h.typeText("bot")
			h.press("tab")
			h.typeText("acme")
			h.press("tab")
			h.typeText("pre")
			h.press("tab", "tab")
			h.typeText(password)
		}

		BeforeEach(func() {
			h.loaded(twoProjects, validToken)
			h.press("a")
		})

		It("accepts http:// only as far as project add does, and says so", func() {
			h.press("tab")
			h.typeText("ftp://qa.example.com")
			h.press("ctrl+s")
			Expect(h.view()).To(ContainSubstring("the base URL must start with https:// (http:// only for localhost)"))
		})

		It("refuses to send what project add would refuse, and says what is missing", func() {
			h.press("ctrl+s")

			view := h.view()
			Expect(view).To(ContainSubstring("the name is required"))
			Expect(view).To(ContainSubstring("the API key is required"))
			Expect(view).To(ContainSubstring("the password is required"))
			Expect(h.r.commandLines()).To(HaveLen(2))
		})

		It("sends the secrets on stdin and nowhere else", func() {
			fill()
			h.press("ctrl+s")

			id := RunID(3)
			Expect(h.r.args(id)).To(Equal([]string{
				"project", "add", "qa",
				"--base-url", "https://qa.example.com",
				"--username", "bot",
				"--project-id", "acme",
				"--env", "pre",
				"--api-key-stdin", "--password-stdin",
			}))
			Expect(h.r.stdin(id)).To(Equal(apiKey + "\n" + password))
			Expect(h.r.exclusive(id)).To(BeTrue())

			for _, secret := range []string{apiKey, password} {
				Expect(strings.Join(h.r.args(id), " ")).NotTo(ContainSubstring(secret))
				Expect(h.view()).NotTo(ContainSubstring(secret))
				Expect(h.m.s.runs.byID[id].display.String()).NotTo(ContainSubstring(secret))
			}
		})

		It("masks the secrets as they are typed, and keeps q a letter", func() {
			fill()
			view := h.view()
			Expect(view).NotTo(ContainSubstring(apiKey))
			Expect(view).NotTo(ContainSubstring("hunter2"))
			Expect(view).To(ContainSubstring("•••"))
			Expect(view).To(MatchRegexp(`Name\s+qa`))
		})

		It("takes a pasted value into the focused field, without the line break it was copied with", func() {
			h.send(tea.PasteMsg{Content: "pasted-name"})
			Expect(h.view()).To(MatchRegexp(`Name\s+pasted-name`))

			fill()
			h.send(tea.PasteMsg{Content: "-and-more\r\n"})
			h.press("ctrl+s")
			Expect(h.r.stdin(3)).To(Equal(apiKey + "\n" + password + "-and-more"))
		})

		It("shows each empty field's placeholder in full", func() {
			Expect(h.view()).To(ContainSubstring("https://acme.api.fulfillmenttools.com"))
			Expect(h.view()).To(ContainSubstring("the Firebase Web API key"))
		})

		It("signs in with an email when switched to one, and passes the optional flags", func() {
			h.typeText("qa")
			h.press("tab")
			h.typeText("https://qa.example.com")
			h.press("tab")
			h.typeText(apiKey)
			h.press("tab", "space", "tab")
			h.typeText("someone@example.com")
			h.press("tab", "tab", "tab")
			h.typeText("Acme")
			h.press("tab")
			h.typeText(password)
			h.press("tab", "space", "tab", "space", "enter")

			Expect(h.r.args(3)).To(Equal([]string{
				"project", "add", "qa",
				"--base-url", "https://qa.example.com",
				"--email", "someone@example.com",
				"--tenant", "Acme",
				"--read-only", "--force",
				"--api-key-stdin", "--password-stdin",
			}))
		})

		It("keeps the form open with the command's complaint when the add fails", func() {
			fill()
			h.press("ctrl+s")
			h.finishID(3, failed(exitcode.Auth, "Error: verify the credentials for \"qa\": INVALID_PASSWORD"))

			view := h.view()
			Expect(view).To(ContainSubstring("Add a project"))
			Expect(view).To(ContainSubstring("adding qa failed: exit 4 (authentication failed)"))
			Expect(view).To(ContainSubstring("INVALID_PASSWORD"))
		})

		It("offers to switch to the new project, and switches on yes", func() {
			fill()
			h.press("ctrl+s")
			h.finishID(3, ok(`{"name":"qa","active":false}`))

			Expect(h.view()).To(ContainSubstring("Switch to qa now?"))
			h.lookup("project", "list")
			h.press("y")
			h.lookup("project", "use", "qa")
		})

		When("the form is no longer open when the add finishes", func() {
			BeforeEach(func() {
				fill()
				h.press("ctrl+s", "esc")
			})

			It("says the project was added, and asks nothing the next key could answer", func() {
				h.press("i")
				h.finishID(3, ok(`{"name":"qa","active":false}`))

				Expect(h.view()).NotTo(ContainSubstring("Switch to qa now?"))
				Expect(clipboard(h.press("y"))).To(Equal("fft project list"), "y copies the panel's selected run")
				Expect(h.r.commandLines()).NotTo(ContainElement("project use qa"))

				h.press("esc")
				Expect(h.view()).To(ContainSubstring("Added qa. Select it and press enter to switch to it."))
			})

			It("leaves a second form's keys to that form", func() {
				h.press("a")
				h.finishID(3, ok(`{"name":"qa","active":false}`))

				h.typeText("yard")
				Expect(h.view()).To(MatchRegexp(`Name\s+yard`))
				Expect(h.r.commandLines()).NotTo(ContainElement("project use qa"))
			})
		})

		It("follows fft when the new project became the active one", func() {
			fill()
			h.press("ctrl+s")
			h.finishID(3, ok(`{"name":"qa","active":true}`))

			Expect(h.r.projects).To(Equal([]string{"qa"}))
			h.lookup("auth", "whoami")
		})

		It("is closed by esc without sending anything", func() {
			fill()
			h.press("esc")

			Expect(h.view()).NotTo(ContainSubstring("Add a project"))
			Expect(h.r.commandLines()).To(HaveLen(2))
		})
	})

	When("fft is running from the environment and the list has not loaded yet", func() {
		BeforeEach(func() {
			h = newHarness(Options{Headless: true})
		})

		It("refuses the add form already, sending nothing", func() {
			h.press("a")

			Expect(h.view()).NotTo(ContainSubstring("Add a project"))
			Expect(h.view()).To(ContainSubstring("Nothing was sent: fft is running from the environment"))
			Expect(h.r.commandLines()).To(HaveLen(2))
		})

		It("stays headless when the list does not show the environment's project", func() {
			h.loaded(twoProjects, validToken)
			h.press("a")
			Expect(h.view()).NotTo(ContainSubstring("Add a project"))
		})
	})

	When("fft is running from the environment", func() {
		BeforeEach(func() {
			h.loaded(`[{"name":"env","active":true,"baseUrl":"http://localhost:8080","credential":"env","ephemeral":true}]`,
				`{"project":"env","store":"env","signIn":"idToken","token":"unknown"}`)
		})

		It("says the projects are read-only here, and offers no key that would change them", func() {
			Expect(h.view()).To(ContainSubstring("Running from the environment"))
			Expect(h.view()).To(ContainSubstring("R refresh token"))
			Expect(h.view()).NotTo(ContainSubstring("enter use"))
			Expect(h.view()).NotTo(ContainSubstring("a add"))
			Expect(h.view()).To(ContainSubstring("fft · env (environment) · fixed token expiry unknown"))
		})

		DescribeTable("refuses every change to the config file, sending nothing",
			func(key string) {
				h.press(key)

				Expect(h.view()).To(ContainSubstring("Nothing was sent: fft is running from the environment"))
				Expect(h.view()).NotTo(ContainSubstring("Add a project"))
				Expect(h.r.commandLines()).To(HaveLen(2))
			},
			Entry("use", "enter"),
			Entry("read-only", "r"),
			Entry("remove", "d"),
			Entry("add", "a"),
		)

		It("still refreshes the token, which is not a config change", func() {
			h.press("R")
			h.lookup("auth", "refresh")
		})
	})

	Describe("the command panel", func() {
		BeforeEach(func() {
			h.loaded(twoProjects, validToken)
			h.press("down", "enter")
		})

		It("lists the runs, newest first, with their state", func() {
			id := h.lookup("project", "use", "prod")
			h.send(runEventMsg{ID: id, State: RunRunning, At: h.now})
			h.now = h.now.Add(1500 * time.Millisecond)
			h.press("i")

			view := h.view()
			Expect(view).To(ContainSubstring("Commands — 1 running"))
			Expect(view).To(MatchRegexp(`> #3 +fft project use prod +\S* ?running 1.5s`))
			Expect(view).To(MatchRegexp(`#1 +fft project list +ok`))
			Expect(view).To(ContainSubstring("1 running"))
		})

		It("cancels the selected run with c", func() {
			h.press("i", "c")
			Expect(h.r.cancelled).To(Equal([]RunID{3}))
		})

		It("does not cancel a run that has finished", func() {
			h.press("i", "down", "c")
			Expect(h.r.cancelled).To(BeEmpty())
		})

		It("shows a finished run's exit code and what it means", func() {
			h.finish(failed(exitcode.Auth, ""), "project", "use", "prod")
			h.press("i")

			Expect(h.view()).To(MatchRegexp(`#3 +fft project use prod +exit 4 authentication failed`))
		})

		It("copies the selected run's command", func() {
			Expect(clipboard(h.press("i", "y"))).To(Equal("fft project use prod"))
		})

		It("closes with esc or i", func() {
			h.press("i", "esc")
			Expect(h.view()).NotTo(ContainSubstring("Commands —"))
			h.press("i", "i")
			Expect(h.view()).NotTo(ContainSubstring("Commands —"))
		})
	})

	Describe("the part of the UI that has the keyboard", func() {
		BeforeEach(func() {
			h.loaded(twoProjects, validToken)
		})

		It("is drawn in place of the command panel, when a question opens under it", func() {
			h.press("a")
			h.typeText("qa")
			h.press("tab")
			h.typeText("https://qa.example.com")
			h.press("tab")
			h.typeText("AIzaKey")
			h.press("tab", "tab")
			h.typeText("bot@example.com")
			h.press("shift+tab", "space", "tab", "tab", "tab", "tab", "tab")
			h.typeText("pw")
			h.press("ctrl+s")
			h.m.showPanel = true
			h.finishID(3, ok(`{"name":"qa","active":false}`))

			view := h.view()
			Expect(view).To(ContainSubstring("Switch to qa now?"))
			Expect(view).NotTo(ContainSubstring("Commands —"))
			Expect(view).NotTo(ContainSubstring("copy command"))
			Expect(view).NotTo(ContainSubstring("running commands"))

			h.press("n")
			Expect(h.view()).To(ContainSubstring("Commands —"), "the panel is back once the question is answered")
		})

		DescribeTable("offers only its own keys while it is a dialog, a form or the quit question",
			func(open func(h *harness), own string) {
				open(h)

				view := h.view()
				Expect(view).To(ContainSubstring(own))
				Expect(view).NotTo(ContainSubstring("copy command"))
				Expect(view).NotTo(ContainSubstring("switch screen"))
				Expect(view).NotTo(ContainSubstring("$ fft"), "the status bar offers a command to copy")
			},
			Entry("a yes/no dialog", func(h *harness) { h.press("r") }, "y yes"),
			Entry("a type-the-name dialog", func(h *harness) { h.press("d") }, "enter confirm"),
			Entry("the add form", func(h *harness) { h.press("a") }, "ctrl+s add project"),
			Entry("the quit question", func(h *harness) { h.press("enter", "q") }, "n/esc no"),
		)

		It("shows the command a form would run on the form itself", func() {
			h.press("a")
			h.typeText("qa")
			Expect(h.view()).To(ContainSubstring("runs: fft project add qa --base-url '' --username ''"))
		})
	})

	It("reads nothing into pasted text while no field has the keyboard", func() {
		h.loaded(twoProjects, validToken)
		h.send(tea.PasteMsg{Content: "dq"})

		Expect(h.view()).NotTo(ContainSubstring("Remove"))
		Expect(h.r.commandLines()).To(HaveLen(2))
	})

	DescribeTable("never draws past the terminal's last row",
		func(width, height int, open func(h *harness), visible string) {
			h.loaded(twoProjects, validToken)
			open(h)
			h.send(tea.WindowSizeMsg{Width: width, Height: height})

			content := h.m.View().Content
			Expect(strings.Count(content, "\n") + 1).To(BeNumerically("<=", height))
			Expect(h.view()).To(ContainSubstring(visible))
		},
		Entry("the list, one row", 80, 1, func(*harness) {}, "1 Projects"),
		Entry("the list, three rows", 80, 3, func(*harness) {}, "1 Projects"),
		Entry("the list with the panel open", 80, 7, func(h *harness) { h.press("i") }, "Commands"),
		Entry("a dialog on a short terminal", 40, 9, func(h *harness) { h.press("r") }, "Make staging"),
		Entry("the panel on a narrow one", 12, 20, func(h *harness) { h.press("i") }, "Comm"),
	)

	Describe("copying the equivalent command", func() {
		It("puts the focused action's command on the clipboard and says so", func() {
			h.loaded(twoProjects, validToken)

			Expect(clipboard(h.press("y"))).To(Equal("fft project use staging"))
			Expect(h.view()).To(ContainSubstring("Copied: fft project use staging"))
		})

		It("refuses to copy a command no shell quoting keeps intact, and marks it", func() {
			h.loaded(`[{"name":"it's","active":true,"baseUrl":"https://a.example.com","credential":"keyring"}]`,
				`{"project":"it's","store":"keyring","signIn":"password","token":"valid","expiresAt":"2026-07-12T12:42:00Z"}`)

			Expect(h.view()).To(ContainSubstring(`$ fft project use 'it'\''s'  (cannot be copied safely)`))
			Expect(clipboard(h.press("y"))).To(BeEmpty())
			Expect(h.view()).To(ContainSubstring("This command cannot be copied safely"))
		})

		It("says when there is nothing to copy", func() {
			// The Request screen, before an operation has been chosen.
			h.press("3")
			Expect(clipboard(h.press("y"))).To(BeEmpty())
			Expect(h.view()).To(ContainSubstring("Nothing to copy here."))
		})
	})

	Describe("quitting", func() {
		It("quits at once when nothing is running", func() {
			h.loaded(twoProjects, validToken)
			Expect(quits(h.press("q"))).To(BeTrue())
		})

		When("commands are still running", func() {
			It("asks first, and stays on no", func() {
				Expect(quits(h.press("q"))).To(BeFalse())
				Expect(h.view()).To(ContainSubstring("2 commands are still running. Quit and cancel them?"))

				Expect(quits(h.press("n"))).To(BeFalse())
				Expect(h.view()).NotTo(ContainSubstring("still running"))
			})

			It("quits on yes", func() {
				h.press("q")
				Expect(quits(h.press("y"))).To(BeTrue())
			})

			It("quits on a second ctrl+c", func() {
				Expect(quits(h.press("ctrl+c"))).To(BeFalse())
				Expect(quits(h.press("ctrl+c"))).To(BeTrue())
			})
		})

		It("leaves a q typed into a form in the form, and still quits on ctrl+c", func() {
			h.loaded(twoProjects, validToken)
			h.press("a")

			Expect(quits(h.press("q"))).To(BeFalse())
			Expect(h.view()).To(MatchRegexp(`Name\s+q`))
			Expect(quits(h.press("ctrl+c"))).To(BeTrue())
		})
	})

	It("waits for the next event after each one", func() {
		cmd := h.send(runEventMsg{ID: 1, State: RunRunning, At: h.now})
		Expect(cmd).NotTo(BeNil())
		Expect(h.m.s.runs.byID[1].state).To(Equal(RunRunning))
	})

	It("stops waiting once the runner has shut down", func() {
		h.loaded(twoProjects, validToken)
		Expect(h.send(runnerClosedMsg{})).To(BeNil())
	})
})
