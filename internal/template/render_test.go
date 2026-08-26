package template_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/template"
)

var _ = Describe("rendering a template", func() {
	// order is a template with one required parameter and one defaulted one, so
	// that every spec below exercises the interaction rather than one rule.
	order := func() *template.Template {
		return &template.Template{
			SchemaVersion: template.Version,
			OperationID:   "createOrder",
			Params: map[string]template.Param{
				"email": {Path: "order.consumer.email", Required: true},
				"qty":   {Path: "order.items.0.quantity", Default: 1},
			},
			Body: decode(`{"order":{"consumer":{},"items":[{"quantity":9}]}}`),
		}
	}

	It("applies a declared default when nobody set it", func() {
		out, err := template.Render(order(), []template.Set{{Key: "email", Value: "a@b.de"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(ContainSubstring(`"quantity": 1`))
	})

	It("lets a --set beat the default", func() {
		out, err := template.Render(order(), []template.Set{
			{Key: "email", Value: "a@b.de"},
			{Key: "qty", Value: template.ParseValue("7")},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(ContainSubstring(`"quantity": 7`))
	})

	It("resolves a declared name to its path", func() {
		out, err := template.Render(order(), []template.Set{{Key: "email", Value: "a@b.de"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(ContainSubstring(`"email": "a@b.de"`))
	})

	It("accepts the full path for a declared parameter too", func() {
		out, err := template.Render(order(), []template.Set{
			{Key: "order.consumer.email", Value: "a@b.de"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(ContainSubstring(`"email": "a@b.de"`))
	})

	It("lets the last --set for one path win", func() {
		out, err := template.Render(order(), []template.Set{
			{Key: "email", Value: "first@b.de"},
			{Key: "email", Value: "last@b.de"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(ContainSubstring(`"email": "last@b.de"`))
		Expect(string(out)).NotTo(ContainSubstring("first@b.de"))
	})

	It("names every missing required parameter at once", func() {
		t := order()
		t.Params["reference"] = template.Param{Path: "order.tenantOrderId", Required: true}

		_, err := template.Render(t, nil)
		Expect(err).To(MatchError(ContainSubstring(`"email"`)))
		Expect(err).To(MatchError(ContainSubstring(`"reference"`)))

		missing := &template.MissingParamsError{}
		Expect(errors.As(err, &missing)).To(BeTrue())
		Expect(missing.Hint()).To(Equal("Add --set email=<value> --set reference=<value>."))
	})

	It("refuses before it writes anything, so a failed render half-applies nothing", func() {
		t := order()
		_, err := template.Render(t, []template.Set{
			{Key: "email", Value: "a@b.de"},
			{Key: "order.consumer.email.first", Value: "x"},
		})
		Expect(err).To(HaveOccurred())

		// The template it was rendered from is untouched, so the next render is
		// not working from a body the failed one already edited.
		out, err := template.Render(t, []template.Set{{Key: "email", Value: "a@b.de"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(ContainSubstring(`"email": "a@b.de"`))
	})

	It("renders a template with no parameters at all", func() {
		out, err := template.Render(&template.Template{Body: decode(`{"a":1}`)}, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(Equal("{\n  \"a\": 1\n}\n"))
	})

	It("does not require a parameter that carries a default", func() {
		t := &template.Template{
			Params: map[string]template.Param{
				"qty": {Path: "quantity", Required: true, Default: 2},
			},
			Body: decode(`{}`),
		}
		out, err := template.Render(t, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(ContainSubstring(`"quantity": 2`))
	})
})
