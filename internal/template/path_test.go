package template_test

import (
	"encoding/json"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/template"
)

// apply parses a path and applies a value, returning the document as JSON so a
// spec can assert on exactly what would be sent.
func apply(doc any, path, value string) (string, error) {
	p, err := template.ParsePath(path)
	if err != nil {
		return "", err
	}
	out, err := template.Apply(doc, p, template.ParseValue(value))
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// decode reads a JSON document the way fft does, so that numbers stay numbers.
func decode(raw string) any {
	var v any
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	Expect(dec.Decode(&v)).To(Succeed())
	return v
}

var _ = Describe("the --set path grammar", func() {
	Describe("parsing", func() {
		It("splits on dots", func() {
			p, err := template.ParsePath("order.consumer.email")
			Expect(err).NotTo(HaveOccurred())
			Expect([]string(p)).To(Equal([]string{"order", "consumer", "email"}))
		})

		It("takes an escaped dot as part of the key", func() {
			p, err := template.ParsePath(`customAttributes.order\.source`)
			Expect(err).NotTo(HaveOccurred())
			Expect([]string(p)).To(Equal([]string{"customAttributes", "order.source"}))
		})

		It("takes an escaped backslash as a backslash", func() {
			p, err := template.ParsePath(`a\\b`)
			Expect(err).NotTo(HaveOccurred())
			Expect([]string(p)).To(Equal([]string{`a\b`}))
		})

		It("refuses an unknown escape rather than swallowing it", func() {
			_, err := template.ParsePath(`a\b`)
			Expect(err).To(MatchError(ContainSubstring("is not an escape")))
		})

		It("refuses a trailing backslash", func() {
			_, err := template.ParsePath(`a\`)
			Expect(err).To(MatchError(ContainSubstring("lone backslash")))
		})

		It("refuses an empty segment", func() {
			for _, bad := range []string{"", ".", "a.", ".a", "a..b"} {
				_, err := template.ParsePath(bad)
				Expect(err).To(HaveOccurred(), "expected %q to be refused", bad)
			}
		})

		It("round-trips through String with the escapes intact", func() {
			p, err := template.ParsePath(`customAttributes.order\.source`)
			Expect(err).NotTo(HaveOccurred())
			Expect(p.String()).To(Equal(`customAttributes.order\.source`))
		})

		It("refuses a path with an implausible number of segments", func() {
			raw := strings.Repeat("a.", 100) + "a"
			_, err := template.ParsePath(raw)
			Expect(err).To(MatchError(ContainSubstring("more than")))
		})

		It("refuses an implausibly long path outright", func() {
			_, err := template.ParsePath(strings.Repeat("a", 5000))
			Expect(err).To(MatchError(ContainSubstring("characters")))
		})
	})

	Describe("applying", func() {
		It("sets a nested value", func() {
			Expect(apply(decode(`{"order":{"consumer":{"email":"old@x.de"}}}`),
				"order.consumer.email", "new@x.de")).
				To(Equal(`{"order":{"consumer":{"email":"new@x.de"}}}`))
		})

		It("creates a missing object on the way", func() {
			Expect(apply(decode(`{"order":{}}`), "order.consumer.email", "a@b.de")).
				To(Equal(`{"order":{"consumer":{"email":"a@b.de"}}}`))
		})

		It("creates an array when the next segment is an index", func() {
			Expect(apply(decode(`{}`), "items.0.qty", "2")).
				To(Equal(`{"items":[{"qty":2}]}`))
		})

		It("replaces an explicit null with the container it needs", func() {
			Expect(apply(decode(`{"address":null}`), "address.city", "Berlin")).
				To(Equal(`{"address":{"city":"Berlin"}}`))
		})

		It("indexes an existing array", func() {
			Expect(apply(decode(`{"items":[{"qty":1},{"qty":1}]}`), "items.1.qty", "5")).
				To(Equal(`{"items":[{"qty":1},{"qty":5}]}`))
		})

		It("appends at exactly the end", func() {
			Expect(apply(decode(`{"items":[{"qty":1}]}`), "items.1.qty", "5")).
				To(Equal(`{"items":[{"qty":1},{"qty":5}]}`))
		})

		It("refuses an index past the end, naming the length", func() {
			_, err := apply(decode(`{"items":[{"qty":1}]}`), "items.5", "x")
			Expect(err).To(MatchError(ContainSubstring("has 1 element")))
		})

		It("treats a numeric segment as a key when the container is an object", func() {
			Expect(apply(decode(`{"counts":{}}`), "counts.7", "3")).
				To(Equal(`{"counts":{"7":3}}`))
		})

		It("refuses a non-numeric segment into an array", func() {
			_, err := apply(decode(`{"items":[]}`), "items.first", "x")
			Expect(err).To(MatchError(ContainSubstring("is not an index")))
		})

		It("refuses to traverse into a scalar rather than deleting it", func() {
			_, err := apply(decode(`{"name":"BER-01"}`), "name.first", "x")
			Expect(err).To(MatchError(ContainSubstring("name is a string")))
		})

		// Nothing has been walked at the root, so the traversed prefix is the empty
		// path — which as a subject reads as a rendering bug ("  has 2 elements")
		// rather than as a report about the body. Matched exactly, so that the
		// missing word and the double space it leaves behind both fail.
		Describe("when the failure is at the root of the document", func() {
			It("names the body as the array that is too short", func() {
				_, err := apply(decode(`["a","b"]`), "9", "x")
				Expect(err).To(MatchError("the body has 2 elements, so index 9 is past its end"))
			})

			It("names the body as the array a key cannot index", func() {
				_, err := apply(decode(`[]`), "first", "x")
				Expect(err).To(MatchError(`the body is an array, and "first" is not an index into one`))
			})

			It("names the body as the scalar the path has nowhere to go inside", func() {
				_, err := apply(decode(`"BER-01"`), "a", "x")
				Expect(err).To(MatchError("the body is a string, so a has nowhere to go"))
			})
		})
	})

	Describe("parsing a value", func() {
		DescribeTable("reads a JSON literal as itself and anything else as a string",
			func(raw string, want any) {
				Expect(template.ParseValue(raw)).To(Equal(want))
			},
			Entry("a number", "3", json.Number("3")),
			Entry("a decimal", "1.50", json.Number("1.50")),
			Entry("true", "true", true),
			Entry("an object", `{"a":1}`, map[string]any{"a": json.Number("1")}),
			Entry("an array", `[1,2]`, []any{json.Number("1"), json.Number("2")}),
			Entry("a quoted number, the escape hatch", `"3"`, "3"),
			Entry("a bare word", "BER-01", "BER-01"),
			Entry("a date", "2024-01-01", "2024-01-01"),
			Entry("the empty string", "", ""),
			Entry("two values", "3 4", "3 4"),
			Entry("broken JSON", "{not json", "{not json"),
		)

		It("reads null as a null", func() {
			Expect(template.ParseValue("null")).To(BeNil())
		})

		It("keeps a 19-digit id exact, which float64 would not", func() {
			Expect(apply(decode(`{}`), "id", "9007199254740993")).
				To(Equal(`{"id":9007199254740993}`))
		})
	})
})
