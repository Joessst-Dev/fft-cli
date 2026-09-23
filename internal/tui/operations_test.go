package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// row is the line of the view that lists the operation with id, "" if none does.
func (h *harness) row(id string) string {
	for line := range strings.SplitSeq(h.view(), "\n") {
		if strings.Contains(line, " "+id+" ") {
			return line
		}
	}
	return ""
}

var _ = Describe("the Operations screen", func() {
	var h *harness

	BeforeEach(func() {
		h = newHarness(Options{})
		h.loaded(twoProjects, validToken)
		h.press("2")
	})

	It("sizes its list to the terminal before anything is drawn", func() {
		h.send(tea.WindowSizeMsg{Width: 100, Height: 30})

		Expect(h.m.operations.list.Width()).To(Equal(100))
		Expect(h.m.operations.list.Height()).To(Equal(h.m.bodyHeight(h.m.hintLine()) - 2))
		Expect(h.m.operations.list.Height()).To(BeNumerically(">", 0))
	})

	It("lists every operation, naming each group on its first row", func() {
		view := h.view()
		Expect(view).To(ContainSubstring("5 operations"))

		Expect(h.row("deleteFacility")).To(ContainSubstring("Facilities (Core)"))
		Expect(h.row("replaceFacility")).NotTo(ContainSubstring("Facilities (Core)"))
		Expect(h.row("addPickJob")).To(ContainSubstring("Picking (Operations)"))
		Expect(h.row("getPickJob")).NotTo(ContainSubstring("Picking"))
	})

	It("shows what each operation does and the command that sends it", func() {
		Expect(h.row("searchFacility")).To(MatchRegexp(`Search facilities\s+fft facility list`))
		Expect(h.row("addPickJob")).To(ContainSubstring("fft picking add-pick-job"))
	})

	It("marks the writes, and nothing else", func() {
		Expect(h.row("deleteFacility")).To(MatchRegexp(`\)\s+W\s+deleteFacility`))
		Expect(h.row("searchFacility")).NotTo(ContainSubstring(" W "))
		Expect(h.view()).NotTo(ContainSubstring(lockBadge))
	})

	DescribeTable("locks the writes that would be refused",
		func(opts Options) {
			h = newHarness(opts)
			h.loaded(twoProjects, validToken)
			h.press("2")

			Expect(h.row("deleteFacility")).To(ContainSubstring("W" + lockBadge))
			Expect(h.row("searchFacility")).NotTo(ContainSubstring(lockBadge))
			Expect(h.view()).To(ContainSubstring("a write marked " + lockBadge + " would be refused"))
		},
		Entry("on a project configured read-only", Options{Project: "prod"}),
		Entry("in a session started read-only", Options{ReadOnly: true}),
	)

	It("shows the command for the selected operation, with the project the UI chose", func() {
		h = newHarness(Options{Project: "staging"})
		h.loaded(twoProjects, validToken)
		h.press("2")

		Expect(h.view()).To(ContainSubstring("$ fft facility delete --project staging"))
		h.press("down")
		Expect(h.view()).To(ContainSubstring("$ fft facility update --project staging"))
	})

	Describe("searching", func() {
		It("finds an operation by what it does", func() {
			h.search("pick job")

			Expect(h.row("addPickJob")).NotTo(BeEmpty())
			Expect(h.row("getPickJob")).NotTo(BeEmpty())
			Expect(h.row("deleteFacility")).To(BeEmpty())
		})

		It("finds an operation by its id, and by the command that sends it", func() {
			h.search("replaceFac")
			Expect(h.row("replaceFacility")).NotTo(BeEmpty())
			Expect(h.row("deleteFacility")).To(BeEmpty())

			h.press("esc")
			h.search("facility list")
			Expect(h.row("searchFacility")).To(HavePrefix("> "))
		})

		It("finds an operation by another hand-written command that sends it, but not by fft api", func() {
			filter := newOpItem(opListFacilities).FilterValue()
			Expect(filter).To(ContainSubstring("fft facility search"))
			Expect(filter).NotTo(ContainSubstring("fft api"))
		})

		It("takes every key as text while the search is open", func() {
			h.search("q2y")

			Expect(h.view()).To(ContainSubstring("[2 Operations]"))
			Expect(h.view()).To(ContainSubstring("Search: q2y"))
			Expect(h.m.confirmQuit).To(BeFalse())
		})

		It("offers only its own keys while it has the keyboard", func() {
			h.search("pick")

			Expect(h.view()).To(ContainSubstring("enter done"))
			Expect(h.view()).NotTo(ContainSubstring("1-8/tab screens"))
			Expect(h.view()).NotTo(ContainSubstring("$ fft"))
		})

		It("keeps the result once applied, and clears it with esc", func() {
			h.search("pick job")
			h.press("enter")

			Expect(h.m.owner()).To(Equal(ownerScreen))
			Expect(h.row("deleteFacility")).To(BeEmpty())
			Expect(h.view()).To(ContainSubstring("esc clear search"))

			h.press("esc")
			Expect(h.row("deleteFacility")).NotTo(BeEmpty())
		})
	})

	Describe("describing an operation", func() {
		BeforeEach(func() {
			h.press("down", "down", "down")
			Expect(h.row("addPickJob")).To(HavePrefix("> "))
			h.press("D")
		})

		It("shows the whole of it in place of the list", func() {
			view := h.view()
			Expect(view).To(ContainSubstring("Create a pick job"))
			Expect(view).To(ContainSubstring("POST /api/pickjobs"))
			Expect(view).To(ContainSubstring("operation  addPickJob"))
			Expect(view).To(ContainSubstring("fft picking add-pick-job (generated"))
			Expect(view).To(ContainSubstring("write — sending it asks first"))
			Expect(view).To(ContainSubstring("permission none documented"))
			Expect(view).To(ContainSubstring("body       required"))
			Expect(view).To(ContainSubstring("Creates a pick job for the given line items."))
			Expect(view).NotTo(ContainSubstring("5 operations"))
		})

		It("lists a command's arguments and flags, with what they take", func() {
			h.press("esc", "up", "up", "up", "D")
			view := h.view()
			Expect(view).To(ContainSubstring("Delete a facility"))
			Expect(view).To(ContainSubstring("permission any of FACILITY_WRITE"))
			Expect(view).To(ContainSubstring("<id> (required)"))

			h.press("esc", "down", "D")
			view = h.view()
			Expect(view).To(ContainSubstring("Replace a facility"))
			Expect(view).To(ContainSubstring("--kind text"))
			Expect(view).To(ContainSubstring("--if-version integer"))
		})

		It("names the other hand-written commands that send it, and not fft api", func() {
			h.press("esc", "up", "D")
			view := h.view()
			Expect(view).To(ContainSubstring("command    fft facility list (hand-written)"))
			Expect(view).To(ContainSubstring("also       fft facility search (hand-written)"))
			Expect(view).NotTo(ContainSubstring("fft api searchFacility"))
		})

		It("goes back to the list with esc", func() {
			h.press("esc")
			Expect(h.view()).To(ContainSubstring("5 operations"))
		})

		It("opens the request with enter", func() {
			h.press("enter")
			Expect(h.view()).To(ContainSubstring("[3 Request]"))
			Expect(h.view()).To(ContainSubstring("Create a pick job"))
		})
	})

	It("opens the selected operation's request with enter", func() {
		h.press("down", "down", "enter")

		Expect(h.view()).To(ContainSubstring("[3 Request]"))
		Expect(h.view()).To(ContainSubstring("Search facilities"))
		Expect(h.view()).To(ContainSubstring("$ fft facility list"))
	})

	Describe("the hints a later release fills in", func() {
		It("shows how often an operation was used", func() {
			h.m.operations.hint = func(op Operation) opHint {
				if op.ID == "getPickJob" {
					return opHint{uses: 7}
				}
				return opHint{}
			}
			Expect(h.row("getPickJob")).To(ContainSubstring("★7"))
			Expect(h.row("addPickJob")).NotTo(ContainSubstring("★"))
		})

		It("keeps an operation the user appears to lack a permission for selectable", func() {
			h.m.operations.hint = func(op Operation) opHint {
				return opHint{lacking: []string{"FACILITY_WRITE"}}
			}
			h.press("enter")
			Expect(h.view()).To(ContainSubstring("[3 Request]"))
		})
	})
})
