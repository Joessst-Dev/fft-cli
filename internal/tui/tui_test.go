package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the placeholder UI", func() {
	var m model

	BeforeEach(func() {
		m = newModel(Options{})
	})

	quits := func(msg tea.Msg) bool {
		_, cmd := m.Update(msg)
		if cmd == nil {
			return false
		}
		_, isQuit := cmd().(tea.QuitMsg)
		return isQuit
	}

	DescribeTable("quits on the quit keys",
		func(msg tea.KeyPressMsg) { Expect(quits(msg)).To(BeTrue()) },
		Entry("q", tea.KeyPressMsg{Code: 'q', Text: "q"}),
		Entry("ctrl+c", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}),
	)

	It("ignores any other key", func() {
		Expect(quits(tea.KeyPressMsg{Code: 'x', Text: "x"})).To(BeFalse())
	})

	It("draws a frame that says how to leave, on the alternate screen", func() {
		v := m.View()

		Expect(v.Content).To(ContainSubstring("fft"))
		Expect(v.Content).To(ContainSubstring("quit"))
		Expect(v.AltScreen).To(BeTrue())
	})

	It("refuses to start without a runner to execute commands with", func() {
		Expect(Run(context.Background(), Options{})).To(MatchError(ContainSubstring("no runner")))
	})
})
