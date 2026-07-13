package main

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const miniSpec = "testdata/mini.yaml"

var _ = Describe("specgen", func() {
	var ops []operation

	BeforeEach(func() {
		var err error
		ops, err = load(miniSpec)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("reading an operation", func() {
		It("carries the method, the path template and the tags", func() {
			op := find(ops, "getThing")

			Expect(op.Method).To(Equal("GET"))
			Expect(op.Path).To(Equal("/api/things/{thingId}"))
			Expect(op.Tags).To(Equal([]string{"Things (Core)"}))
			Expect(op.Summary).To(Equal("Get thing"))
		})

		It("carries x-fft-permissions, which 303 of the real spec's operations declare", func() {
			Expect(find(ops, "getThing").Permissions).To(Equal([]string{"AUDIT_READ", "THING_READ"}))
		})

		It("leaves the permissions empty for an operation that declares none", func() {
			Expect(find(ops, "queryThings").Permissions).To(BeEmpty())
		})

		It("strips the HTML out of a description, because a terminal is not a browser", func() {
			description := find(ops, "getThing").Description

			Expect(description).To(Equal("Fetch a thing. By id."))
			Expect(description).NotTo(ContainSubstring("<a href"))
			Expect(description).NotTo(ContainSubstring("<br"))
		})

		It("records that an operation is deprecated", func() {
			Expect(find(ops, "queryThings").Deprecated).To(BeTrue())
			Expect(find(ops, "getThing").Deprecated).To(BeFalse())
		})

		It("marks a path parameter required even when the spec does not bother to say so", func() {
			Expect(paramOf(find(ops, "getThing"), "path", "thingId").Required).To(BeTrue())
		})

		It("inherits a path-level parameter into every operation on the path", func() {
			// The spec declares `tenant` once, on the path, and it applies to the GET and
			// the POST alike (OpenAPI 3.0 §4.7.9.1). An operation that lost it would
			// silently query the wrong tenant.
			Expect(paramOf(find(ops, "queryThings"), "query", "tenant").Type).To(Equal("string"))
		})

		It("sorts operations by id, so the generated file does not churn", func() {
			ids := make([]string, 0, len(ops))
			for _, op := range ops {
				ids = append(ids, op.ID)
			}
			Expect(ids).To(Equal([]string{"addNode", "addShape", "addThing", "getThing", "queryThings"}))
		})
	})

	// This is the silent-bug class the whole table exists to prevent: the API accepts
	// either encoding and answers 200, so a wrong one is not an error, it is a filter
	// that matches the wrong rows.
	Describe("the explode flag", func() {
		var op operation

		BeforeEach(func() { op = find(ops, "queryThings") })

		It("is false for a parameter the spec explicitly says is not exploded", func() {
			status := paramOf(op, "query", "status")

			Expect(status.Type).To(Equal("array"))
			Expect(status.Explode).To(BeFalse(), "status must be comma-joined")
		})

		It("is true for a parameter the spec explicitly says is exploded", func() {
			Expect(paramOf(op, "query", "label").Explode).To(BeTrue())
		})

		It("defaults to true for an array query parameter that says nothing", func() {
			// `form` is the default style for a query parameter and it defaults explode to
			// true. Defaulting it to false instead would comma-join 60 of the real spec's
			// 77 array parameters.
			Expect(paramOf(op, "query", "tag").Explode).To(BeTrue())
		})

		It("lifts the enum off an array's items, so the values can be checked", func() {
			Expect(paramOf(op, "query", "status").Enum).To(Equal([]string{"OPEN", "CLOSED"}))
		})
	})

	Describe("synthesizing a request body", func() {
		var body map[string]any

		decode := func(id string) map[string]any {
			GinkgoHelper()

			op := find(ops, id)
			Expect(op.HasBody).To(BeTrue())
			Expect(op.BodyRequired).To(BeTrue())

			var out map[string]any
			Expect(json.Unmarshal([]byte(op.SampleBody), &out)).
				To(Succeed(), "the sample body is not valid JSON: %s", op.SampleBody)
			return out
		}

		When("the schema is an allOf with sibling properties", func() {
			BeforeEach(func() { body = decode("addThing") })

			It("keeps the inherited required field", func() {
				// `name` comes from the $ref'd parent. A generator that collapses the allOf
				// to a bare alias of its parent loses the siblings; one that keeps only the
				// siblings loses this.
				Expect(body).To(HaveKeyWithValue("name", "Hamburg NW2"))
			})

			It("keeps the sibling required field", func() {
				Expect(body).To(HaveKey("mode"))
			})

			It("falls back to the first enum value when there is no example and no default", func() {
				Expect(body).To(HaveKeyWithValue("mode", "FAST"))
			})

			It("coerces an example that contradicts its own declared type", func() {
				// type: integer, example: '240' — a *quoted* 240. Copied verbatim it is a
				// JSON string, and the API rejects the body with a 400. A sample body the
				// user cannot send is worse than none, because they will believe it.
				Expect(body).To(HaveKeyWithValue("buffer", BeNumerically("==", 240)))
			})

			It("emits an optional field that carries a default", func() {
				// The default is the spec saying what to send. It is also how the facility
				// `type` discriminator gets into a create body at all.
				Expect(body).To(HaveKeyWithValue("colour", "RED"))
			})

			It("omits an optional field the spec says nothing concrete about", func() {
				// A null for the user to delete is not help.
				Expect(body).NotTo(HaveKey("note"))
			})

			It("caps an array at one element", func() {
				Expect(body).To(HaveKeyWithValue("tags", ConsistOf("alpha")))
			})

			It("resolves a $ref to a nested object", func() {
				Expect(body).To(HaveKeyWithValue("child", HaveKeyWithValue("name", "Hamburg NW2")))
			})
		})

		When("the schema is a oneOf routed by a discriminator", func() {
			BeforeEach(func() { body = decode("addShape") })

			It("picks the mapping's first branch by key, not the file's first branch", func() {
				// CIRCLE sorts before SQUARE. The choice has to come from a sorted walk:
				// Go randomises map order, and CI fails the build on a diff.
				Expect(body).To(HaveKey("radius"))
				Expect(body).NotTo(HaveKey("side"))
			})

			It("sets the discriminator property to the key that chose the branch", func() {
				// A oneOf body that omits its discriminator is unroutable however
				// well-formed the rest of it is.
				Expect(body).To(HaveKeyWithValue("kind", "CIRCLE"))
			})

			It("chooses the same branch every time", func() {
				for range 20 {
					again, err := load(miniSpec)
					Expect(err).NotTo(HaveOccurred())
					Expect(find(again, "addShape").SampleBody).To(Equal(find(ops, "addShape").SampleBody))
				}
			})
		})

		When("the schema references itself", func() {
			It("terminates rather than recursing forever", func() {
				// Node.next is a Node. This spec passing at all is the assertion; if the
				// guard were gone, load() would not return.
				body = decode("addNode")

				Expect(body).To(HaveKeyWithValue("id", "n1"))
				Expect(body).To(HaveKeyWithValue("next", BeNil()))
			})
		})

		It("gives an operation that takes no body no sample body", func() {
			op := find(ops, "getThing")

			Expect(op.HasBody).To(BeFalse())
			Expect(op.SampleBody).To(BeEmpty())
		})
	})

	Describe("rendering the Go file", func() {
		It("emits source that parses", func() {
			src, err := render("api", miniSpec, ops)
			Expect(err).NotTo(HaveOccurred())

			_, err = parser.ParseFile(token.NewFileSet(), "opmeta.gen.go", src, parser.AllErrors)
			Expect(err).NotTo(HaveOccurred())
		})

		It("says it is generated, so nobody edits it by hand", func() {
			src, err := render("api", miniSpec, ops)
			Expect(err).NotTo(HaveOccurred())

			Expect(string(src)).To(HavePrefix("// Code generated by tools/specgen from mini.yaml. DO NOT EDIT."))
		})

		It("renders the explode flag only where it means something", func() {
			src, err := render("api", miniSpec, ops)
			Expect(err).NotTo(HaveOccurred())

			// An exploded array carries the flag; a comma-joined one is the zero value and
			// carries nothing; a scalar never carries it, because it has no meaning there.
			Expect(string(src)).To(ContainSubstring(`{Name: "label", In: InQuery, Type: TypeArray, Item: TypeString, Explode: true}`))
			Expect(string(src)).To(ContainSubstring(`{Name: "status", In: InQuery, Type: TypeArray, Item: TypeString, Enum: []string{"OPEN", "CLOSED"}}`))
			Expect(string(src)).NotTo(ContainSubstring(`{Name: "size", In: InQuery, Type: TypeInteger, Explode`))
		})

		It("is a deterministic function of the spec, because CI fails the build on a diff", func() {
			first, err := render("api", miniSpec, ops)
			Expect(err).NotTo(HaveOccurred())

			for range 10 {
				again, err := load(miniSpec)
				Expect(err).NotTo(HaveOccurred())

				src, err := render("api", miniSpec, again)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(src)).To(Equal(string(first)))
			}
		})
	})

	Describe("prose", func() {
		DescribeTable("strips the spec's HTML down to something a terminal can print",
			func(in, want string) { Expect(prose(in)).To(Equal(want)) },

			Entry("a link", `See <a href="https://x.example">the docs</a>.`, "See the docs."),
			Entry("a line break", "One.<br />Two.", "One. Two."),
			Entry("an entity", "Stock &amp; listings", "Stock & listings"),
			Entry("collapsed whitespace", "a\n\n   b", "a b"),
			Entry("nothing at all", "", ""),
		)
	})

	Describe("the whole real spec", func() {
		It("loads, and every sample body it produces is valid JSON", func() {
			// The synthesizer is recursive over 2,725 schemas. "It terminates" and "what it
			// emits parses" are not things a mini-spec can prove.
			real, err := load("../../api/openapi/fft.api.swagger.yaml")
			Expect(err).NotTo(HaveOccurred())
			Expect(len(real)).To(BeNumerically(">", 500))

			bodies := 0
			for _, op := range real {
				if op.SampleBody == "" {
					continue
				}
				bodies++
				Expect(json.Valid([]byte(op.SampleBody))).
					To(BeTrue(), "%s: the sample body is not valid JSON:\n%s", op.ID, op.SampleBody)
			}
			Expect(bodies).To(BeNumerically(">", 200))
		})

		It("gives every operation a unique operationId, which is the name fft api takes", func() {
			real, err := load("../../api/openapi/fft.api.swagger.yaml")
			Expect(err).NotTo(HaveOccurred())

			seen := make(map[string]bool, len(real))
			for _, op := range real {
				Expect(seen[op.ID]).To(BeFalse(), "duplicate operationId %s", op.ID)
				Expect(strings.TrimSpace(op.ID)).NotTo(BeEmpty())
				seen[op.ID] = true
			}
		})
	})
})
