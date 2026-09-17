package tui

import (
	"image/color"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// isBackgroundQuery matches the message tea.RequestBackgroundColor produces.
var isBackgroundQuery = BeAssignableToTypeOf(tea.RequestBackgroundColor())

// replyTail is the rest of an OSC 11 reply split after its head, as the terminal
// reader decodes it: one key per character, and ESC \ as alt+\.
var replyTail = []tea.KeyPressMsg{
	{Code: 'e', Text: "e"}, {Code: '/', Text: "/"},
	{Code: '1', Text: "1"}, {Code: 'e', Text: "e"}, {Code: '1', Text: "1"}, {Code: 'e', Text: "e"},
	{Code: '/', Text: "/"},
	{Code: '1', Text: "1"}, {Code: 'e', Text: "e"}, {Code: '1', Text: "1"}, {Code: 'e', Text: "e"},
	{Code: '\\', Mod: tea.ModAlt},
}

var _ = Describe("the terminal's background", func() {
	var h *harness

	BeforeEach(func() {
		h = newHarness(Options{Color: true})
		h.loaded(twoProjects, validToken)
	})

	It("is asked for when the UI starts in colour, and not without colour", func() {
		Expect(msgsOf(newHarness(Options{Color: true}).m.Init())).To(ContainElement(isBackgroundQuery))
		Expect(msgsOf(newHarness(Options{}).m.Init())).NotTo(ContainElement(isBackgroundQuery))
	})

	Describe("while the answer is awaited", func() {
		It("takes no keys", func() {
			h.press("2")
			Expect(h.m.current).To(Equal(tabProjects))
		})

		It("takes keys once the terminal has answered", func() {
			h.send(tea.BackgroundColorMsg{Color: color.Black})
			h.press("2")
			Expect(h.m.current).To(Equal(tabOperations))
		})

		It("takes keys once a terminal that never answers has had its moment", func() {
			h.now = h.now.Add(backgroundWait)
			h.press("2")
			Expect(h.m.current).To(Equal(tabOperations))
		})

		It("still quits on ctrl+c", func() {
			Expect(quits(h.press("ctrl+c"))).To(BeTrue())
		})

		It("waits for nothing without colour", func() {
			plain := newHarness(Options{})
			plain.loaded(twoProjects, validToken)
			plain.press("2")
			Expect(plain.m.current).To(Equal(tabOperations))
		})
	})

	Describe("an answer split in transit", func() {
		BeforeEach(func() {
			// The head comes after the wait, as it does over a slow link.
			h.now = h.now.Add(backgroundWait)
			h.send(uv.UnknownEvent("\x1b]11;rgb:1e"))
		})

		It("is not read as keys, so no screen changes and no dialog opens", func() {
			h.press("1", "e")
			h.press("d")
			for _, k := range replyTail {
				h.send(k)
			}
			Expect(h.m.current).To(Equal(tabProjects))
			Expect(h.m.projects.dialog).To(BeNil())
			Expect(h.r.commandLines()).To(HaveLen(2), "a key started a command")

			// The user's keys are the user's again after the terminator.
			h.press("2")
			Expect(h.m.current).To(Equal(tabOperations))
		})

		DescribeTable("ends at whichever terminator the terminal sends",
			func(end tea.KeyPressMsg) {
				h.send(tea.KeyPressMsg{Code: '1', Text: "1"})
				h.send(end)
				h.press("2")
				Expect(h.m.current).To(Equal(tabOperations))
			},
			Entry("ST", tea.KeyPressMsg{Code: '\\', Mod: tea.ModAlt}),
			Entry("BEL", tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}),
			Entry("the backslash of an ST whose ESC came alone", tea.KeyPressMsg{Code: '\\', Text: "\\"}),
			Entry("8-bit ST", tea.KeyPressMsg{Code: '\\', Mod: tea.ModCtrl | tea.ModAlt}),
		)

		It("gives back a key that cannot be part of it", func() {
			h.press("1")
			Expect(quits(h.press("q"))).To(BeTrue())
		})

		It("stops being dropped after a moment, even without its terminator", func() {
			h.press("1")
			h.now = h.now.Add(replyWait + 1)
			h.press("2")
			Expect(h.m.current).To(Equal(tabOperations))
		})

		It("drops no more keys than a reply can have", func() {
			for range replyMaxKeys {
				h.press("1")
			}
			h.press("2")
			Expect(h.m.current).To(Equal(tabOperations))
		})

		It("still quits on ctrl+c", func() {
			Expect(quits(h.press("ctrl+c"))).To(BeTrue())
		})
	})

	It("reads a head split right after ESC ] the same way", func() {
		h.now = h.now.Add(backgroundWait)
		h.send(tea.KeyPressMsg{Code: ']', Mod: tea.ModAlt})
		h.press("1", "1", "d")
		h.send(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
		Expect(h.m.projects.dialog).To(BeNil())

		h.press("2")
		Expect(h.m.current).To(Equal(tabOperations))
	})

	It("takes an unknown event that is no answer, or one after the answer, for nothing", func() {
		h.send(tea.BackgroundColorMsg{Color: color.Black})
		h.send(uv.UnknownEvent("\x1b]11;rgb:1e"))
		h.press("2")
		Expect(h.m.current).To(Equal(tabOperations))

		other := newHarness(Options{Color: true})
		other.loaded(twoProjects, validToken)
		other.now = other.now.Add(backgroundWait)
		other.send(uv.UnknownEvent("\x1b[99;99X"))
		other.press("2")
		Expect(other.m.current).To(Equal(tabOperations))
	})
})
