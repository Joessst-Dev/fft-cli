package main

import (
	"encoding/json"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// resolve walks the real command tree the way cobra does when a user types the
// command, and returns what they would actually have run.
func resolve(args ...string) *cobra.Command {
	GinkgoHelper()

	cmd, _, err := newRootCmd(&Deps{}).Find(args)
	Expect(err).NotTo(HaveOccurred())
	return cmd
}

var _ = Describe("the generated commands", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	answers := func(body string) *tenant {
		return c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(body))
			Expect(err).NotTo(HaveOccurred())
		})
	}

	// Shadowing is what makes promoting an endpoint from Tier 2 to Tier 1 a pure
	// upgrade rather than a breaking rename. Without it, `fft facility list` would one
	// day mean two different commands, and which one you got would depend on
	// registration order.
	Describe("shadowing a curated command", func() {
		It("resolves 'fft facility list' to the hand-written command", func() {
			cmd := resolve("facility", "list")

			Expect(cmd.CommandPath()).To(Equal("fft facility list"))
			Expect(cmd.Annotations).NotTo(HaveKey(annotationGenerated))
			Expect(cmd.Annotations).To(HaveKeyWithValue(annotationOperationID, "searchFacility"))
		})

		It("registers no generated twin for an operation a curated command has claimed", func() {
			// searchFacility is `fft facility list` and `fft facility search`. There must be
			// no third command for it.
			root := newRootCmd(&Deps{})

			var found []string

			var walk func(*cobra.Command)
			walk = func(cmd *cobra.Command) {
				if cmd.Annotations[annotationOperationID] == "searchFacility" &&
					cmd.Annotations[annotationGenerated] == "true" {
					found = append(found, cmd.CommandPath())
				}
				for _, child := range cmd.Commands() {
					walk(child)
				}
			}
			walk(root)

			Expect(found).To(BeEmpty(), "a generated command shadows the curated searchFacility")
		})

		DescribeTable("leaves every curated command reachable at the name it has always had",
			func(path ...string) {
				cmd := resolve(path...)

				Expect(cmd.Annotations).NotTo(HaveKey(annotationGenerated),
					"%s resolved to a generated command", cmd.CommandPath())
			},

			Entry("facility list", "facility", "list"),
			Entry("facility get", "facility", "get"),
			Entry("facility create", "facility", "create"),
			Entry("listing list", "listing", "list"),
			Entry("listing purge", "listing", "purge"),
			Entry("stock list", "stock", "list"),
			Entry("stock summary", "stock", "summary"),
			Entry("ping", "ping"),
		)

		It("puts the generated siblings under the same noun as the curated ones", func() {
			// getAllFacilities is the legacy GET list. Nobody curated it, so it lands next
			// to `fft facility list` rather than in a namespace of its own.
			cmd := resolve("facility", "get-all-facilities")

			Expect(cmd.CommandPath()).To(Equal("fft facility get-all-facilities"))
			Expect(cmd.Annotations).To(HaveKeyWithValue(annotationGenerated, "true"))
		})

		It("gives every operation in the spec a command", func() {
			root := newRootCmd(&Deps{})

			var missing []string
			for _, op := range api.Operations() {
				if commandPath(root, op) == "" {
					missing = append(missing, op.ID)
				}
			}
			Expect(missing).To(BeEmpty())
		})
	})

	Describe("a generated command", func() {
		It("calls the endpoint its operation names", func() {
			t := answers(`{"id":"pj-1"}`)

			Expect(c.run("picking", "get-pick-job", "--pick-job-id", "pj-1")).To(Equal(exitcode.OK))

			Expect(t.only().Method).To(Equal(http.MethodGet))
			Expect(t.only().Path).To(Equal("/api/pickjobs/pj-1"))
		})

		It("exits 2 without sending anything when a required flag is missing", func() {
			t := answers(`{}`)

			code := c.run("picking", "get-pick-job")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("pickJobId"))
			Expect(t.calls).To(BeEmpty(), "a request was sent although the required flag was missing")
		})

		It("encodes a comma-joined array flag the way the spec says", func() {
			t := answers(`{"stowJobs":[]}`)

			Expect(c.run("stowing", "get-stow-jobs",
				"--status", "OPEN", "--status", "IN_PROGRESS")).To(Equal(exitcode.OK))

			Expect(t.only().RawQuery).To(Equal("status=OPEN%2CIN_PROGRESS"))
		})

		It("encodes a repeated array flag the way the spec says", func() {
			t := answers(`{"serviceJobs":[]}`)

			Expect(c.run("service", "get-service-jobs",
				"--status", "OPEN", "--status", "IN_PROGRESS")).To(Equal(exitcode.OK))

			Expect(t.only().RawQuery).To(Equal("status=OPEN&status=IN_PROGRESS"))
		})

		It("refuses an enum value the API does not have", func() {
			t := answers(`{}`)

			code := c.run("stowing", "get-stow-jobs", "--status", "OPNE")

			Expect(code).To(Equal(exitcode.Usage))
			Expect(t.calls).To(BeEmpty())
		})

		It("sends a zero the user asked for, rather than reading it as 'not given'", func() {
			// The flag guard is Changed(), never the value. `--size 0` is a request for
			// zero results; a zero-value guard would silently drop it and the API would
			// apply its own default instead.
			t := answers(`{"pickJobs":[]}`)

			Expect(c.run("picking", "query-pick-jobs", "--size", "0")).To(Equal(exitcode.OK))

			Expect(t.only().RawQuery).To(ContainSubstring("size=0"))
		})

		It("sends a false the user asked for", func() {
			t := answers(`{"pickJobs":[]}`)

			Expect(c.run("picking", "query-pick-jobs", "--anonymized=false")).To(Equal(exitcode.OK))

			Expect(t.only().RawQuery).To(ContainSubstring("anonymized=false"))
		})

		It("sends nothing at all for a flag the user did not give", func() {
			t := answers(`{"pickJobs":[]}`)

			Expect(c.run("picking", "query-pick-jobs")).To(Equal(exitcode.OK))

			Expect(t.only().RawQuery).To(BeEmpty())
		})

		It("marks its required flags as required, and its optional ones as not", func() {
			cmd := resolve("picking", "get-pick-job")

			usage := cmd.Flags().Lookup("pick-job-id").Usage
			Expect(usage).To(ContainSubstring("required"))

			cmd = resolve("picking", "query-pick-jobs")
			Expect(cmd.Flags().Lookup("size").Usage).NotTo(ContainSubstring("required"))
		})

		It("says in the flag's own help how a list is encoded", func() {
			// It is the one thing about an array parameter that is silently wrong when it
			// is wrong, so it is not left to the command's Long text.
			Expect(resolve("stowing", "get-stow-jobs").Flags().Lookup("status").Usage).
				To(ContainSubstring("comma-joined"))

			Expect(resolve("service", "get-service-jobs").Flags().Lookup("status").Usage).
				To(ContainSubstring("repeated"))
		})

		It("takes a body from --file and prints an example with --example", func() {
			Expect(c.run("picking", "add-pick-job", "--example")).To(Equal(exitcode.OK))

			printed := c.out()
			Expect(json.Valid([]byte(printed))).To(BeTrue())

			t := answers(`{"id":"pj-1"}`)

			_, err := c.stdin.WriteString(printed)
			Expect(err).NotTo(HaveOccurred())

			Expect(c.run("picking", "add-pick-job", "--file", "-")).To(Equal(exitcode.OK))
			Expect(t.only().Method).To(Equal(http.MethodPost))
			Expect(t.only().json()).To(HaveKey("pickLineItems"))
		})

		It("has no --example on an operation that takes no body", func() {
			Expect(resolve("picking", "get-pick-job").Flags().Lookup("example")).To(BeNil())
		})
	})

	Describe("naming", func() {
		DescribeTable("kebab-cases a spec name into a flag or command",
			func(in, want string) { Expect(kebab(in)).To(Equal(want)) },

			Entry("a camelCase id", "getPickJob", "get-pick-job"),
			Entry("a parameter", "pickJobId", "pick-job-id"),
			Entry("a run of capitals is one word", "getOIDCConfiguration", "get-oidc-configuration"),
			Entry("a trailing capital", "getOIDC", "get-oidc"),
			Entry("an already-lower name", "status", "status"),
			Entry("a header", "X-Trace-Id", "x-trace-id"),
			Entry("nothing", "", ""),
		)

		DescribeTable("singularises a tag into a noun",
			func(in, want string) { Expect(singular(in)).To(Equal(want)) },

			Entry("facilities", "facilities", "facility"),
			Entry("listings", "listings", "listing"),
			Entry("stocks", "stocks", "stock"),
			Entry("processes", "processes", "process"),
			Entry("categories", "categories", "category"),
			Entry("services", "services", "service"),
			Entry("packaging-units", "packaging-units", "packaging-unit"),
			Entry("only the last word", "carriers-configuration", "carriers-configuration"),
			Entry("a word that is not a plural", "picking", "picking"),
			Entry("health", "health", "health"),
			Entry("a word ending in -us stays put", "status", "status"),
		)

		DescribeTable("derives the group from the operation's first tag",
			func(tag, want string) {
				Expect(groupFor(api.Operation{Tags: []string{tag}})).To(Equal(want))
			},

			Entry("Facilities (Core)", "Facilities (Core)", "facility"),
			Entry("Picking (Operations)", "Picking (Operations)", "picking"),
			Entry("Stocks (Inventory)", "Stocks (Inventory)", "stock"),
			Entry("OrderRecords", "OrderRecords", "order-record"),
			Entry("ShippingInformation (carrier)", "ShippingInformation (carrier)", "shipping-information"),
			Entry("an operation with no tag at all", "", "other"),
		)

		It("gives no generated flag a name the root command already owns", func() {
			// pflag panics on a redefined flag — at startup, for every user, not just the
			// one who typed it. So a spec parameter that one day collides with --timeout
			// must cost a suffix and not a broken binary.
			root := newRootCmd(&Deps{})

			var walk func(*cobra.Command)
			walk = func(cmd *cobra.Command) {
				for _, child := range cmd.Commands() {
					walk(child)
				}
			}
			// Building the tree at all is the assertion: a collision would have panicked
			// before this line. Walking it forces every flag set to be merged.
			walk(root)

			Expect(root.Commands()).NotTo(BeEmpty())
		})
	})

	Describe("--help on a generated command", func() {
		It("explains the endpoint, its permissions and how to call it", func() {
			Expect(c.run("stowing", "get-stow-jobs", "--help")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("ENDPOINT"))
			Expect(c.out()).To(ContainSubstring("GET /api/stowjobs"))
			Expect(c.out()).To(ContainSubstring("PERMISSIONS"))
			Expect(c.out()).To(ContainSubstring("STOW_JOB_READ"))
			Expect(c.out()).To(ContainSubstring("EXAMPLES"))
			Expect(c.out()).To(ContainSubstring("fft stowing get-stow-jobs"))
		})

		It("shows the sample body for an operation that takes one", func() {
			Expect(c.run("picking", "add-pick-job", "--help")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("EXAMPLE BODY"))
			Expect(c.out()).To(ContainSubstring("tenantOrderId"))
			Expect(c.out()).To(ContainSubstring("--example > body.json"))
		})
	})

	Describe("--help on a curated command", func() {
		It("carries the endpoint's permissions, which the curated Long text never said", func() {
			Expect(c.run("stock", "summary", "--help")).To(Equal(exitcode.OK))

			Expect(c.out()).To(ContainSubstring("PERMISSIONS"))
			Expect(c.out()).To(ContainSubstring("STOCK_AVAILABILITIES_READ"))
		})

		It("keeps its own hand-written prose and gains the endpoint's", func() {
			Expect(c.run("facility", "create", "--help")).To(Equal(exitcode.OK))

			// The curated Long text.
			Expect(c.out()).To(ContainSubstring("Create a facility from a JSON file"))
			// And the spec's, because the command declares the operation it calls.
			Expect(c.out()).To(ContainSubstring("ENDPOINT"))
			Expect(c.out()).To(ContainSubstring("POST /api/facilities"))
			Expect(c.out()).To(ContainSubstring("EXAMPLE BODY"))
		})
	})
})
