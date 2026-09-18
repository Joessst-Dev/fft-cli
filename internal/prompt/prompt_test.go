package prompt_test

import (
	"bytes"
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/prompt"
)

var _ = Describe("a Prompter", func() {
	var out *bytes.Buffer

	BeforeEach(func() {
		out = &bytes.Buffer{}
	})

	When("its questions go to a Confirmer", func() {
		var asked []string

		confirmer := func(yes bool, err error) prompt.Confirmer {
			return func(q string) (bool, error) {
				asked = append(asked, q)
				return yes, err
			}
		}

		BeforeEach(func() {
			asked = nil
		})

		It("can confirm without a terminal, and still cannot ask anything else", func() {
			p := prompt.New(strings.NewReader(""), out, prompt.WithConfirmer(confirmer(true, nil)))

			Expect(p.CanConfirm()).To(BeTrue())
			Expect(p.Interactive()).To(BeFalse())
		})

		It("asks it the question rather than reading an answer from its input", func() {
			p := prompt.New(strings.NewReader("n\n"), out, prompt.WithConfirmer(confirmer(true, nil)))

			Expect(p.Confirm("Delete facility BER-01?")).To(BeTrue())
			Expect(asked).To(Equal([]string{"Delete facility BER-01?"}))
			Expect(out.String()).To(Equal("Delete facility BER-01? [y/N]: y\n"))
		})

		It("records a no as a no", func() {
			p := prompt.New(strings.NewReader(""), out, prompt.WithConfirmer(confirmer(false, nil)))

			Expect(p.Confirm("Delete facility BER-01?")).To(BeFalse())
			Expect(out.String()).To(Equal("Delete facility BER-01? [y/N]: n\n"))
		})

		It("never turns a failure into a yes", func() {
			p := prompt.New(strings.NewReader(""), out, prompt.WithConfirmer(confirmer(true, context.Canceled)))

			yes, err := p.Confirm("Delete facility BER-01?")
			Expect(err).To(MatchError(context.Canceled))
			Expect(yes).To(BeFalse())
			Expect(out.String()).To(BeEmpty())
		})
	})

	It("cannot confirm without a terminal or a Confirmer", func() {
		p := prompt.New(strings.NewReader("y\n"), out)
		Expect(p.CanConfirm()).To(BeFalse())
	})

	It("can confirm on what it is told is a terminal, and reads the answer there", func() {
		p := prompt.New(strings.NewReader("yes\n"), out, prompt.WithInteractive(true))

		Expect(p.CanConfirm()).To(BeTrue())
		Expect(p.Confirm("Go?")).To(BeTrue())
		Expect(out.String()).To(Equal("Go? [y/N]: "))
	})
})
