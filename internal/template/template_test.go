package template_test

import (
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/template"
)

var _ = Describe("decoding a template", func() {
	// A project-scope template arrives via git clone, so Decode is the actual
	// trust boundary — not cmd/fft's save-time declaredParams, which a
	// hand-written file never goes through.
	It("refuses a declared parameter whose name collides with a different top-level field", func() {
		_, err := template.Decode([]byte(
			`{"schemaVersion":1,"body":{"facilityRef":"ATTACKER","decoy":null},
			  "params":{"facilityRef":{"path":"decoy"}}}`))
		Expect(err).To(MatchError(ContainSubstring("already a top-level field")))
	})

	It("refuses a declared parameter name containing a dot", func() {
		_, err := template.Decode([]byte(
			`{"schemaVersion":1,"body":{"a":1},"params":{"a.b":{"path":"a"}}}`))
		Expect(err).To(HaveOccurred())
	})

	// --set cuts its argument at the first '=' and trims the name, so each of these
	// is a name the file declares and no --set can reach: the value lands on some
	// other key instead.
	DescribeTable("refuses a parameter name --set could not address as itself",
		func(name, says string) {
			_, err := template.Decode([]byte(
				`{"schemaVersion":1,"body":{"a":1},"params":{` + strconv.Quote(name) + `:{"path":"status"}}}`))
			Expect(err).To(MatchError(ContainSubstring(says)))
		},
		Entry("an equals sign", "a=b", `cannot contain "="`),
		Entry("leading white space", " a", "white space"),
		Entry("trailing white space", "a\t", "white space"),
		Entry("a leading dash", "-a", "cannot start with a dash"),
		Entry("a backslash", `a\b`, `cannot contain "\\"`),
	)

	It("still accepts a dash or an equals-free name inside it", func() {
		Expect(template.ValidateParamName("rush-order_id2")).To(Succeed())
	})

	It("accepts a parameter name that matches the top-level field it points at", func() {
		_, err := template.Decode([]byte(
			`{"schemaVersion":1,"body":{"facilityRef":"BER-01"},
			  "params":{"facilityRef":{"path":"facilityRef"}}}`))
		Expect(err).NotTo(HaveOccurred())
	})

	It("accepts a parameter with no collision at all", func() {
		_, err := template.Decode([]byte(
			`{"schemaVersion":1,"body":{"order":{}},"params":{"email":{"path":"order.consumer.email"}}}`))
		Expect(err).NotTo(HaveOccurred())
	})

	// --require refuses a default at save time. Missing treats a defaulted
	// parameter as satisfied, so a hand-written file declaring both would send
	// the default with the "required" its reader is relying on never firing.
	It("refuses a parameter that is both required and defaulted, which nothing would ever ask for", func() {
		_, err := template.Decode([]byte(
			`{"schemaVersion":1,"body":{"order":{}},
			  "params":{"email":{"path":"order.consumer.email","required":true,"default":"a@b.de"}}}`))
		Expect(err).To(MatchError(ContainSubstring("required and also carries a default")))
	})

	It("accepts a defaulted parameter that is not required", func() {
		_, err := template.Decode([]byte(
			`{"schemaVersion":1,"body":{"order":{}},
			  "params":{"qty":{"path":"order.items.0.quantity","default":1}}}`))
		Expect(err).NotTo(HaveOccurred())
	})

	It("accepts a required parameter that carries no default", func() {
		_, err := template.Decode([]byte(
			`{"schemaVersion":1,"body":{"order":{}},
			  "params":{"email":{"path":"order.consumer.email","required":true}}}`))
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("a template's digest", func() {
	const file = `{"schemaVersion":1,"operationId":"addOrder","params":{"qty":{"path":"order.qty","default":1}},
	  "body":{"order":{"id":9007199254740993,"qty":2}}}`

	digestOf := func(data string) string {
		GinkgoHelper()
		t, err := template.Decode([]byte(data))
		Expect(err).NotTo(HaveOccurred())
		d, err := template.Digest(t)
		Expect(err).NotTo(HaveOccurred())
		return d
	}

	It("is the same for a template and its encoding decoded again", func() {
		t, err := template.Decode([]byte(file))
		Expect(err).NotTo(HaveOccurred())
		encoded, err := template.Encode(t)
		Expect(err).NotTo(HaveOccurred())

		Expect(digestOf(string(encoded))).To(Equal(digestOf(file)))
		Expect(digestOf(file)).To(MatchRegexp(`^[0-9a-f]{64}$`))
	})

	DescribeTable("changes with anything the template says",
		func(changed string) {
			Expect(digestOf(changed)).NotTo(Equal(digestOf(file)))
		},
		Entry("the operation", strings.Replace(file, "addOrder", "addPickJob", 1)),
		Entry("a digit of a 64-bit id", strings.Replace(file, "993", "994", 1)),
		Entry("a default", strings.Replace(file, `"default":1`, `"default":"1"`, 1)),
	)
})
