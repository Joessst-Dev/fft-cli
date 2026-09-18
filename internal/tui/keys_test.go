package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"image/color"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// hintRows is the hint block at the bottom of the view: its last rows, as many as
// the hint takes.
func (h *harness) hintRows() []string {
	GinkgoHelper()
	lines := strings.Split(h.view(), "\n")
	n := len(strings.Split(ansi.Strip(h.m.hintLine()), "\n"))
	Expect(len(lines)).To(BeNumerically(">=", n))
	return lines[len(lines)-n:]
}

// shortHelp is each key the hint offers right now, as the hint spells it.
func (h *harness) shortHelp() []string {
	b := h.m.bindings()
	var items []string
	for _, k := range slices.Concat(b.local, b.global) {
		if help := k.Help(); help.Key != "" {
			items = append(items, help.Key+" "+help.Desc)
		}
	}
	return items
}

var _ = Describe("the key hint", func() {
	var h *harness

	BeforeEach(func() {
		h = newHarness(Options{})
		h.loaded(twoProjects, validToken)
	})

	DescribeTable("shows every key that works, whole, and always the way to the legend",
		func(width, height int, open func(h *harness)) {
			open(h)
			h.send(tea.WindowSizeMsg{Width: width, Height: height})

			rows := h.hintRows()
			Expect(rows[len(rows)-1]).To(HaveSuffix("? all keys"))
			for _, item := range h.shortHelp() {
				Expect(rows).To(ContainElement(ContainSubstring(item)), "the hint lost or split %q", item)
			}
			for _, row := range rows {
				Expect(row).NotTo(ContainSubstring("…"))
				Expect(ansi.StringWidth(row)).To(BeNumerically("<=", width))
			}
			Expect(strings.Count(h.m.View().Content, "\n") + 1).To(BeNumerically("<=", height))
		},
		Entry("on Projects at 120 columns", 120, 30, func(*harness) {}),
		Entry("on Projects at 80 columns", 80, 30, func(*harness) {}),
		Entry("on Request at 120 columns", 120, 30, func(h *harness) { h.request(opReplaceFacility) }),
		Entry("on Request at 80 columns", 80, 30, func(h *harness) { h.request(opReplaceFacility) }),
		Entry("on Request at 40 columns", 40, 30, func(h *harness) { h.request(opReplaceFacility) }),
		Entry("with the panel open at 80 columns", 80, 30, func(h *harness) { h.press("i") }),
		Entry("on Request at 20x12, where the hint leaves no row for the tabs", 20, 12, func(h *harness) { h.request(opReplaceFacility) }),
		Entry("on Projects at 20x12", 20, 12, func(*harness) {}),
		Entry("on Request at 30x8", 30, 8, func(h *harness) { h.request(opReplaceFacility) }),
	)

	DescribeTable("keeps the way to the legend on a terminal too short for the whole hint",
		func(width, height int, open func(h *harness)) {
			open(h)
			h.send(tea.WindowSizeMsg{Width: width, Height: height})

			lines := strings.Split(h.view(), "\n")
			Expect(len(lines)).To(BeNumerically("<=", height))
			Expect(lines[len(lines)-1]).To(HaveSuffix("? all keys"))
			for _, line := range strings.Split(h.m.View().Content, "\n") {
				Expect(ansi.StringWidth(line)).To(BeNumerically("<=", width))
			}
		},
		Entry("on Request at 20x6", 20, 6, func(h *harness) { h.request(opReplaceFacility) }),
		Entry("on Request at 20x3", 20, 3, func(h *harness) { h.request(opReplaceFacility) }),
		Entry("on Projects at 20x10", 20, 10, func(*harness) {}),
		Entry("on Projects at 12x4", 12, 4, func(*harness) {}),
		Entry("on Projects with one row", 80, 1, func(*harness) {}),
	)

	It("puts the screen's keys and the global ones on rows of their own", func() {
		h.send(tea.WindowSizeMsg{Width: 120, Height: 30})

		Expect(h.hintRows()).To(Equal([]string{
			strings.Repeat("─", 120),
			"↑/↓ select • enter use • r read-only on/off • d remove • R refresh token • a add • ctrl+r reload",
			"1-7/tab screens • ctrl+p project • i running • y copy • q quit • ? all keys",
		}))
	})

	It("gives the body the rows the hint does not take", func() {
		h.request(opReplaceFacility)
		for _, width := range []int{120, 60, 30} {
			h.send(tea.WindowSizeMsg{Width: width, Height: 30})
			rows := len(h.hintRows())
			Expect(h.m.bodyHeight(h.m.hintLine())).To(Equal(30 - 3 - rows))
			Expect(strings.Count(h.m.View().Content, "\n") + 1).To(Equal(30))
		}
	})

	It("sets itself apart with a rule, which a short terminal gives to the body instead", func() {
		h.send(tea.WindowSizeMsg{Width: 60, Height: 30})
		rule := strings.Repeat("─", 60)
		Expect(h.hintRows()[0]).To(Equal(rule))
		Expect(h.view()).To(ContainSubstring(" fft · staging · token 42m left"))

		h.send(tea.WindowSizeMsg{Width: 60, Height: 12})
		rows := h.hintRows()
		Expect(h.view()).NotTo(ContainSubstring(rule))
		Expect(h.m.bodyHeight(h.m.hintLine())).To(Equal(12 - 3 - len(rows)))
		Expect(rows[0]).To(HavePrefix("↑/↓ select"))
		Expect(rows[len(rows)-1]).To(HaveSuffix("? all keys"))
	})

	It("draws its keys for the terminal's background once the terminal says what it is", func() {
		coloured := newHarness(Options{Color: true})
		coloured.loaded(twoProjects, validToken)
		dark := coloured.m.hintLine()

		coloured.send(tea.BackgroundColorMsg{Color: color.White})
		light := coloured.m.hintLine()
		Expect(light).NotTo(Equal(dark))
		Expect(ansi.Strip(light)).To(Equal(ansi.Strip(dark)))

		coloured.send(tea.BackgroundColorMsg{Color: color.Black})
		Expect(coloured.m.hintLine()).To(Equal(dark))

		// Without colour there is nothing to pick.
		before := h.m.hintLine()
		h.send(tea.BackgroundColorMsg{Color: color.White})
		Expect(h.m.hintLine()).To(Equal(before))
		Expect(before).To(Equal(ansi.Strip(before)))
	})

	It("does not offer the legend while a field has the keyboard, where ? is typed", func() {
		h.press("2", "/", "?")

		Expect(h.m.legendOpen).To(BeFalse())
		Expect(h.view()).To(ContainSubstring("Search: ?"))
		Expect(h.view()).NotTo(ContainSubstring("all keys"))
	})

	It("draws the keys plainly without colour, and within the terminal with it", func() {
		Expect(h.m.View().Content).To(Equal(h.view()), "no escape sequences without colour")
		Expect(h.view()).To(ContainSubstring("1-7/tab screens • ctrl+p project"))

		coloured := newHarness(Options{Color: true})
		coloured.loaded(twoProjects, validToken)
		coloured.request(opReplaceFacility)
		coloured.send(tea.WindowSizeMsg{Width: 80, Height: 20})

		content := coloured.m.View().Content
		Expect(content).NotTo(Equal(coloured.view()))
		lines := strings.Split(content, "\n")
		Expect(lines).To(HaveLen(20))
		for _, line := range lines {
			Expect(ansi.StringWidth(line)).To(BeNumerically("<=", 80))
		}
		Expect(coloured.view()).To(ContainSubstring("? all keys"))
	})
})

var _ = Describe("the key legend", func() {
	var h *harness

	BeforeEach(func() {
		h = newHarness(Options{})
		h.loaded(twoProjects, validToken)
		h.send(tea.WindowSizeMsg{Width: 120, Height: 60})
	})

	It("replaces the screen with every key of it, its modes, and the global keys", func() {
		h.request(opReplaceFacility)
		h.press("?")

		view := h.view()
		Expect(view).NotTo(ContainSubstring("Replace a facility"), "the form is still drawn")
		Expect(view).To(ContainSubstring("Keys · Request"))
		Expect(view).To(MatchRegexp(`(?m)^  ↑/↓\s+select a field$`))
		Expect(view).To(MatchRegexp(`(?m)^  s, ctrl\+s\s+send the request$`))
		Expect(view).To(MatchRegexp(`(?m)^  esc\s+back to Operations$`))
		Expect(view).To(ContainSubstring(" While typing in a field\n" +
			"  enter done · tab/shift+tab next/previous field · esc undo · ctrl+s send"))
		Expect(view).To(MatchRegexp(`(?m)^ Everywhere\n  1-7, tab\s+switch screen\s+shift\+tab\s+previous screen$`))
		Expect(view).To(MatchRegexp(`(?m)^  y\s+copy the fft command\s+q, ctrl\+c\s+quit$`))
		Expect(view).To(MatchRegexp(`(?m)^  \?\s+open or close this legend$`))
		Expect(view).To(ContainSubstring("n/esc no · enter no on a yes/no question; confirms a typed name or value"))
		Expect(view).To(ContainSubstring("↑/↓, j/k, pgup/pgdn scroll"))
		Expect(view).To(ContainSubstring("In the running commands panel (i)"))
		Expect(view).To(ContainSubstring("In a question"))
	})

	It("lists the body keys even for an operation without a body, and says they do nothing", func() {
		h.request(opListFacilities)
		h.press("?")

		Expect(h.view()).To(MatchRegexp(`(?m)^  e\s+edit the body`))
		Expect(h.view()).To(ContainSubstring("This operation takes no body, so e and t do nothing here."))
	})

	DescribeTable("names the screen it is about",
		func(tab, title string, keyLine string) {
			h.press(tab, "?")
			Expect(h.view()).To(ContainSubstring("Keys · " + title))
			Expect(h.view()).To(MatchRegexp(keyLine))
		},
		Entry(nil, "1", "Projects", `(?m)^  enter, u\s+use it`),
		Entry(nil, "2", "Operations", `(?m)^  D\s+describe it`),
		Entry(nil, "3", "Request", `(?m)^  x\s+clear the field`),
		Entry(nil, "4", "Response", `(?m)^  r\s+send the same request again`),
		Entry(nil, "5", "Templates", `(?m)^  S\s+render it and send the body`),
		Entry(nil, "6", "History", `(?m)^  t\s+switch between recent and most used`),
		Entry(nil, "7", "Roles", `(?m)^  r\s+read your roles again`),
	)

	DescribeTable("closes with ? or esc, and leaves the screen as it was",
		func(closeKey string) {
			h.request(opListFacilities)
			h.press("?")
			Expect(h.m.legendOpen).To(BeTrue())

			h.press(closeKey)
			Expect(h.m.legendOpen).To(BeFalse())
			Expect(h.m.current).To(Equal(tabRequest))
			Expect(h.view()).To(ContainSubstring("Search facilities"))
		},
		Entry("?", "?"),
		Entry("esc", "esc"),
	)

	It("lets no other key act on the screen underneath", func() {
		h.request(opListFacilities)
		started := len(h.r.started)
		h.press("?")

		h.press("s", "ctrl+s", "enter", "x", "e", "t", "2", "tab", "i", "y", "ctrl+p")
		Expect(h.r.started).To(HaveLen(started), "a key sent the request")
		Expect(h.m.legendOpen).To(BeTrue())
		Expect(h.m.current).To(Equal(tabRequest))
		Expect(h.m.showPanel).To(BeFalse())
		Expect(h.m.request.editing).To(BeFalse())

		h.press("esc")
		Expect(h.view()).NotTo(ContainSubstring("Nothing to copy"))
	})

	It("types nothing pasted into a question waiting behind it", func() {
		h.press("enter", "?")
		id := h.lookup("project", "use", "staging")
		h.ask(id, "Purge the listing?", "purge")

		// Long enough that the question would take it, were it in front.
		h.wait()
		h.send(tea.PasteMsg{Content: "purge"})
		h.press("esc")
		h.wait()
		Expect(h.view()).To(ContainSubstring("Purge the listing?"))
		Expect(h.view()).NotTo(ContainSubstring("> purge"))

		h.press("enter")
		Expect(h.r.answers).To(BeEmpty(), "the pasted verb answered the question")
	})

	It("offers only its own keys, and no command to copy", func() {
		h.press("?")

		Expect(h.hintRows()).To(Equal([]string{strings.Repeat("─", 120), "q quit • ?/esc close"}))
		Expect(h.view()).NotTo(ContainSubstring("$ fft"))
	})

	It("offers to scroll only when it does not fit, as its title does", func() {
		h.press("?")
		Expect(h.view()).NotTo(ContainSubstring("↑/↓ scroll"))

		seen := map[bool]bool{}
		for height := 30; height >= 10; height-- {
			h.send(tea.WindowSizeMsg{Width: 120, Height: height})
			scrolls := strings.Contains(h.view(), "↓ more")
			seen[scrolls] = true
			keys := slices.DeleteFunc(h.hintRows(), func(row string) bool { return strings.HasPrefix(row, "─") })
			want := []string{"q quit • ?/esc close"}
			if scrolls {
				want = []string{"↑/↓ scroll", "q quit • ?/esc close"}
			}
			Expect(keys).To(Equal(want), "at %d rows", height)
			Expect(strings.Contains(h.view(), "↑/↓ scroll · ? or esc closes")).To(Equal(scrolls), "at %d rows", height)
		}
		Expect(seen).To(HaveKey(true))
		Expect(seen).To(HaveKey(false))
	})

	It("draws its keys in the hint's colour, which follows the terminal's background", func() {
		coloured := newHarness(Options{Color: true})
		coloured.send(tea.BackgroundColorMsg{Color: color.White})
		coloured.loaded(twoProjects, validToken)
		coloured.send(tea.WindowSizeMsg{Width: 120, Height: 60})
		coloured.press("?")

		light := coloured.m.st.hint.key.Render("?")
		Expect(light).NotTo(Equal(newHintStyles(true, true).key.Render("?")))
		Expect(coloured.m.View().Content).To(MatchRegexp(`(?m)^  ` + regexp.QuoteMeta(light) + ` +open or close this legend$`))
	})

	It("leaves out the add form when the projects come from the environment", func() {
		headless := newHarness(Options{Headless: true})
		headless.send(tea.WindowSizeMsg{Width: 120, Height: 60})
		headless.press("?")
		Expect(headless.view()).To(ContainSubstring("Running from the environment"))
		Expect(headless.view()).NotTo(ContainSubstring("In the add form"))

		h.press("?")
		Expect(h.view()).To(ContainSubstring("In the add form"))
	})

	It("quits at once when nothing is running", func() {
		h.press("?")
		Expect(quits(h.press("q"))).To(BeTrue())
	})

	It("asks before quitting while a command runs, and is still open after a no", func() {
		h.press("enter", "?")
		Expect(quits(h.press("q"))).To(BeFalse())
		Expect(h.view()).To(ContainSubstring("still running. Quit and cancel them?"))

		h.press("n")
		Expect(h.view()).To(ContainSubstring("Keys · Projects"))
	})

	It("opens from the command panel, and gives the panel back when it closes", func() {
		h.press("i", "?")
		Expect(h.view()).To(ContainSubstring("Keys · Projects"))
		Expect(h.view()).NotTo(ContainSubstring("Commands —"))

		h.press("esc")
		Expect(h.m.showPanel).To(BeTrue())
		Expect(h.view()).To(ContainSubstring("Commands —"))
	})

	It("keeps a command's question waiting until it is closed", func() {
		h.press("enter", "?")
		h.ask(h.lookup("project", "use", "staging"), "Delete it?", "")

		Expect(h.view()).To(ContainSubstring("Keys · Projects"))
		h.press("esc")
		Expect(h.view()).To(ContainSubstring("Delete it?"))
	})

	Describe("on a short terminal", func() {
		BeforeEach(func() {
			h.request(opReplaceFacility)
			h.send(tea.WindowSizeMsg{Width: 120, Height: 16})
			h.press("?")
		})

		It("scrolls, and says there is more", func() {
			Expect(h.view()).To(ContainSubstring("↑/↓ scroll · ? or esc closes"))
			Expect(h.view()).To(ContainSubstring("↓ more"))
			Expect(h.view()).NotTo(ContainSubstring("In this legend"))

			for range 50 {
				h.press("down")
			}
			Expect(h.view()).To(ContainSubstring("In this legend"))
			Expect(h.view()).NotTo(ContainSubstring("↓ more"))
			Expect(h.view()).NotTo(ContainSubstring("select a field"))

			h.send(keyPress("up"))
			h.send(tea.KeyPressMsg{Code: tea.KeyPgUp})
			h.send(tea.KeyPressMsg{Code: tea.KeyPgUp})
			h.send(tea.KeyPressMsg{Code: tea.KeyPgUp})
			Expect(h.view()).To(ContainSubstring("select a field"))
			Expect(h.m.legendScroll).To(BeZero())
		})

		It("scrolls no further than its last line", func() {
			for range 50 {
				h.press("down")
			}
			bottom := h.m.legendScroll
			h.press("down")
			Expect(h.m.legendScroll).To(Equal(bottom))
			h.press("up")
			Expect(h.m.legendScroll).To(Equal(bottom - 1))
		})
	})

	It("does not scroll while the terminal's height is unknown, since all of it is drawn", func() {
		h.request(opReplaceFacility)
		h.send(tea.WindowSizeMsg{Width: 120})
		h.press("?")
		for range 100 {
			h.press("down")
		}
		Expect(h.m.legendScroll).To(BeZero())
		Expect(h.view()).To(ContainSubstring("select a field"))
	})

	DescribeTable("never draws past the terminal's last row",
		func(width, height int) {
			h.request(opReplaceFacility)
			h.press("?")
			h.send(tea.WindowSizeMsg{Width: width, Height: height})

			lines := strings.Split(h.m.View().Content, "\n")
			Expect(len(lines)).To(BeNumerically("<=", height))
			for _, line := range lines {
				Expect(ansi.StringWidth(line)).To(BeNumerically("<=", width))
			}
			Expect(h.view()).To(ContainSubstring("Keys"), "the legend's title is cut")
		},
		Entry("one row", 80, 1),
		Entry("three rows", 80, 3),
		Entry("too few for the chrome and a legend", 80, 8),
		Entry("just enough", 80, 9),
		Entry("narrow", 20, 24),
		Entry("roomy", 200, 80),
	)

	It("takes the whole of a terminal too short for it beside the chrome, and says how to close", func() {
		h.request(opReplaceFacility)
		h.press("?")
		h.send(tea.WindowSizeMsg{Width: 80, Height: 6})

		view := h.view()
		Expect(view).To(HavePrefix(" Keys · Request"))
		Expect(view).To(ContainSubstring("? or esc closes"))
		Expect(strings.Count(view, "\n") + 1).To(Equal(6))
	})

	Describe("the keys it names for a component the screen hands keys to", func() {
		It("scroll the response", func() {
			id := h.sent(opGetPickJob, "pj-1")
			h.finishID(id, ok("["+strings.Repeat(`"line",`, 200)+`"end"]`))
			h.send(tea.WindowSizeMsg{Width: 80, Height: 30})
			h.view()
			offset := func() int { return h.m.response.vp.YOffset() }

			steps := []struct {
				key  tea.KeyPressMsg
				down bool
			}{
				{tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, true},
				{tea.KeyPressMsg{Code: 'f', Text: "f"}, true},
				{tea.KeyPressMsg{Code: 'b', Text: "b"}, false},
				{tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}, true},
				{tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, false},
				{tea.KeyPressMsg{Code: 'd', Text: "d"}, true},
				{tea.KeyPressMsg{Code: 'u', Text: "u"}, false},
			}
			for _, step := range steps {
				before := offset()
				h.send(step.key)
				if step.down {
					Expect(offset()).To(BeNumerically(">", before), "%s did not scroll down", step.key)
				} else {
					Expect(offset()).To(BeNumerically("<", before), "%s did not scroll up", step.key)
				}
			}
		})

		It("page the operations", func() {
			h.press("2")
			h.send(tea.WindowSizeMsg{Width: 80, Height: 12})
			Expect(h.m.operations.list.Paginator.TotalPages).To(BeNumerically(">", 3))
			page := func() int { return h.m.operations.list.Paginator.Page }

			for _, k := range []string{"l", "f", "d"} {
				before := page()
				h.press(k)
				Expect(page()).To(Equal(before+1), "%s did not turn the page", k)
			}
			for _, k := range []string{"h", "b", "u"} {
				before := page()
				h.press(k)
				Expect(page()).To(Equal(before-1), "%s did not turn the page back", k)
			}
		})
	})

	Describe("as the screens declare it", func() {
		// legendOf is the legend section that lists an owner's bindings: the screen
		// its keys belong to, or the shared section for keys that work on every
		// screen. An owner is a keys struct type, or a package-level binding's name.
		legendOf := map[string]func(m *app) []legendSection{
			"globalKeys":    func(m *app) []legendSection { return []legendSection{m.keys.legend()} },
			"legendKeys":    func(m *app) []legendSection { return []legendSection{m.legendKeys.legend()} },
			"runsKeys":      func(m *app) []legendSection { return []legendSection{m.panel.legend()} },
			"yesKey":        func(*app) []legendSection { return []legendSection{questionLegend()} },
			"noKey":         func(*app) []legendSection { return []legendSection{questionLegend()} },
			"submitKey":     func(*app) []legendSection { return []legendSection{questionLegend()} },
			"cancelKey":     func(*app) []legendSection { return []legendSection{questionLegend()} },
			"projectKeys":   func(m *app) []legendSection { return m.projects.legend() },
			"formKeys":      func(m *app) []legendSection { return m.projects.legend() },
			"operationKeys": func(m *app) []legendSection { return m.operations.legend() },
			"requestKeys":   func(m *app) []legendSection { return m.request.legend() },
			"responseKeys":  func(m *app) []legendSection { return m.response.legend() },
			"templateKeys":  func(m *app) []legendSection { return m.templates.legend() },
			"historyKeys":   func(m *app) []legendSection { return m.history.legend() },
			"roleKeys":      func(m *app) []legendSection { return m.roles.legend() },
		}

		It("lists every key a screen gives help text for, in that screen's own legend", func() {
			declared := helpBindings()
			Expect(declared).NotTo(BeEmpty())
			for owner, bindings := range declared {
				sections, known := legendOf[owner]
				Expect(known).To(BeTrue(), "%s declares keys with help, but no legend is known to list them: add it to legendOf", owner)

				var listed []declaredBinding
				for _, sec := range sections(h.m) {
					for _, e := range sec.entries {
						listed = append(listed, declaredBinding{keys: e.of.Keys(), help: e.of.Help()})
					}
				}
				for _, b := range bindings {
					// Keys and help together tell the fields of one owner apart, so that
					// esc to go back is not taken as listed because esc to undo is.
					Expect(slices.ContainsFunc(bindings, func(o declaredBinding) bool { return o.field != b.field && o.same(b) })).
						To(BeFalse(), "%s.%s has the keys and help of another field, so the legend cannot tell them apart", owner, b.field)
					Expect(slices.ContainsFunc(listed, b.same)).
						To(BeTrue(), "the legend does not list %s.%s (%s)", owner, b.field, strings.Join(b.keys, ", "))
				}
			}
		})

		It("spells and explains every entry", func() {
			for tab := range screenNames {
				h.m.current = tab
				for _, sec := range h.m.legendSections() {
					for _, e := range sec.entries {
						Expect(e.label()).NotTo(BeEmpty(), "an entry on %s has no keys", screenNames[tab])
						Expect(e.desc).NotTo(BeEmpty(), "%s on %s says nothing", e.label(), screenNames[tab])
						if len(e.of.Keys()) == 0 {
							Expect(e.keys).NotTo(BeEmpty())
						}
					}
				}
			}
		})
	})
})

// declaredBinding is a key.Binding the package's source builds with help text.
type declaredBinding struct {
	field string
	keys  []string
	help  key.Help
}

func (b declaredBinding) same(o declaredBinding) bool {
	return slices.Equal(b.keys, o.keys) && b.help == o.help
}

// helpBindings reads the package's source for the bindings built with
// key.WithHelp, and returns them by owner: the fields of a keys struct literal by
// the struct's type, and a package-level binding (yesKey and the other question
// keys in dialog.go) by its own name. The source is read rather than the values
// because the fields are unexported, and a binding added to a struct is exactly
// what a hand-kept list would miss. A binding built anywhere else, such as inside
// a function body, is not scanned.
func helpBindings() map[string][]declaredBinding {
	GinkgoHelper()
	files, err := filepath.Glob("*.go")
	Expect(err).NotTo(HaveOccurred())

	found := map[string][]declaredBinding{}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		Expect(err).NotTo(HaveOccurred())
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, value := range vs.Values {
					if b, withHelp := readBinding(value); withHelp {
						b.field = vs.Names[i].Name
						found[b.field] = append(found[b.field], b)
					}
				}
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			typ, ok := lit.Type.(*ast.Ident)
			if !ok || !strings.HasSuffix(typ.Name, "Keys") {
				return true
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if b, withHelp := readBinding(kv.Value); withHelp {
					b.field = kv.Key.(*ast.Ident).Name
					found[typ.Name] = append(found[typ.Name], b)
				}
			}
			return true
		})
	}
	return found
}

// readBinding reads a key.NewBinding call: the keys and help it is built with,
// and whether it has help at all.
func readBinding(expr ast.Expr) (b declaredBinding, withHelp bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || !isCallTo(call, "NewBinding") {
		return b, false
	}
	for _, arg := range call.Args {
		opt, ok := arg.(*ast.CallExpr)
		if !ok {
			continue
		}
		switch {
		case isCallTo(opt, "WithHelp"):
			withHelp = true
			Expect(opt.Args).To(HaveLen(2))
			b.help = key.Help{Key: stringLit(opt.Args[0]), Desc: stringLit(opt.Args[1])}
		case isCallTo(opt, "WithKeys"):
			for _, k := range opt.Args {
				b.keys = append(b.keys, stringLit(k))
			}
		}
	}
	return b, withHelp
}

func stringLit(expr ast.Expr) string {
	GinkgoHelper()
	lit, ok := expr.(*ast.BasicLit)
	Expect(ok).To(BeTrue(), "a key or its help built from something other than a literal")
	s, err := strconv.Unquote(lit.Value)
	Expect(err).NotTo(HaveOccurred())
	return s
}

func isCallTo(call *ast.CallExpr, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "key"
}
