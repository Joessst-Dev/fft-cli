package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// facility is what a command decodes a search result into: the fields it renders,
// not the polymorphic union underneath. The generic takes whatever T it is given,
// which is the whole point of it.
type facility struct {
	Name             string `json:"name"`
	TenantFacilityID string `json:"tenantFacilityId"`
	Version          int    `json:"version"`
}

// searched is a search body, decoded far enough to assert on the cursor, the page
// size and the sort.
type searched struct {
	After   *string         `json:"after"`
	Size    *int            `json:"size"`
	Sort    []any           `json:"sort"`
	Query   json.RawMessage `json:"query"`
	Options *struct {
		WithTotal *bool `json:"withTotal"`
	} `json:"options"`
}

func fixture(name string) string {
	GinkgoHelper()

	data, err := os.ReadFile(filepath.Join("testdata", name))
	Expect(err).NotTo(HaveOccurred())
	return string(data)
}

// serve answers every request with the named fixture.
func serve(name string) *tenant {
	return newTenant(func(w http.ResponseWriter, _ *http.Request, _ int) {
		fmt.Fprint(w, fixture(name))
	})
}

// unauthenticated is the client the search specs use: the token is auth's business
// and is tested there, so a search spec that had to mint one would be testing two
// things at once.
func unauthenticated(t *tenant) *client.Client {
	GinkgoHelper()

	c, err := client.New(t.URL, client.WithRetry((&noWait{}).retry()))
	Expect(err).NotTo(HaveOccurred())
	return c
}

var facilities = client.FacilitySearch[facility]

var _ = Describe("searching by cursor", func() {
	var ctx context.Context

	BeforeEach(func() { ctx = context.Background() })

	Describe("one page", func() {
		It(`sends {"query":{}} for an unfiltered search, which the API reads as everything`, func() {
			t := serve("facilities_page1.json")

			_, err := client.Search(ctx, unauthenticated(t), facilities(), client.FacilitySearchPayload{})

			Expect(err).NotTo(HaveOccurred())
			Expect(string(t.sent(0).Query)).To(Equal("{}"))
		})

		It("decodes the entities into whatever type the caller asked for", func() {
			t := serve("facilities_page1.json")

			page, err := client.Search(ctx, unauthenticated(t), facilities(), client.FacilitySearchPayload{})

			Expect(err).NotTo(HaveOccurred())
			Expect(page.Items).To(HaveLen(2))
			Expect(page.Items[0].Name).To(Equal("Berlin Mitte"))
			Expect(page.Items[0].TenantFacilityID).To(Equal("BER-01"))
			Expect(page.PageInfo.HasNextPage).To(BeTrue())
			Expect(page.PageInfo.EndCursor).To(Equal("cursor-1"))
		})
	})

	Describe("the total", func() {
		// `total` is absent unless the search asked for it. A command that renders 0
		// for "the API did not count" is telling the user something false.
		It("is nil when the API was not asked to count", func() {
			t := serve("facilities_page1.json")

			page, err := client.Search(ctx, unauthenticated(t), facilities(), client.FacilitySearchPayload{})

			Expect(err).NotTo(HaveOccurred())
			Expect(page.Total).To(BeNil())
		})

		It("is a pointer to zero when the API counted and found nothing", func() {
			t := serve("facilities_empty.json")

			page, err := client.Search(ctx, unauthenticated(t), facilities(),
				client.FacilitySearchPayload{}.WithTotal())

			Expect(err).NotTo(HaveOccurred())
			Expect(page.Items).To(BeEmpty())
			Expect(page.Total).NotTo(BeNil())
			Expect(*page.Total).To(Equal(0))
			Expect(*t.sent(0).Options.WithTotal).To(BeTrue())
		})

		It("is the count the API reported when it was asked for one", func() {
			t := serve("facilities_total.json")

			page, err := client.Search(ctx, unauthenticated(t), facilities(),
				client.FacilitySearchPayload{}.WithTotal())

			Expect(err).NotTo(HaveOccurred())
			Expect(*page.Total).To(Equal(2133))
		})
	})

	Describe("a payload the API would refuse with an opaque 400", func() {
		// The schema says minItems: 1, maxItems: 1. An empty array comes back as a 400
		// with nothing in it, so the request is not worth sending.
		DescribeTable("refuses a sort that is not exactly one field, before any request is sent",
			func(sort []api.FacilitySort) {
				t := newTenant(func(http.ResponseWriter, *http.Request, int) {
					Fail("the request should never have been sent")
				})

				_, err := client.Search(ctx, unauthenticated(t), facilities(),
					client.FacilitySearchPayload{Sort: sort})

				Expect(err).To(MatchError(ContainSubstring("exactly one sort field")))
				Expect(exitcode.FromError(err)).To(Equal(exitcode.Usage))
				Expect(t.hits()).To(BeZero())
			},
			Entry("an empty sort array", []api.FacilitySort{}),
			Entry("two sort fields", []api.FacilitySort{{}, {}}),
		)

		It("omits the field entirely when no sort is given, and sorts by the server's default", func() {
			t := serve("facilities_page1.json")

			_, err := client.Search(ctx, unauthenticated(t), facilities(), client.FacilitySearchPayload{})

			Expect(err).NotTo(HaveOccurred())
			Expect(t.sent(0).Sort).To(BeNil())
		})

		DescribeTable("refuses a page size the API does not allow",
			func(size int) {
				t := newTenant(func(http.ResponseWriter, *http.Request, int) {
					Fail("the request should never have been sent")
				})

				_, err := client.Search(ctx, unauthenticated(t), facilities(),
					client.FacilitySearchPayload{Size: &size})

				Expect(err).To(MatchError(ContainSubstring("between 1 and 250")))
				Expect(exitcode.FromError(err)).To(Equal(exitcode.Usage))
			},
			Entry("zero", 0),
			Entry("negative", -1),
			Entry("above the maximum of 250", 251),
		)
	})
})

var _ = Describe("searching every page", func() {
	var ctx context.Context

	BeforeEach(func() { ctx = context.Background() })

	// pages serves three pages, the third of which reports no next page.
	pages := func() *tenant {
		return newTenant(func(w http.ResponseWriter, _ *http.Request, n int) {
			fmt.Fprint(w, fixture(fmt.Sprintf("facilities_page%d.json", n)))
		})
	}

	// collect drains the iterator, stopping at the first error as a command would.
	collect := func(seq func(func(facility, error) bool)) ([]facility, error) {
		var (
			items []facility
			last  error
		)
		for item, err := range seq {
			if err != nil {
				last = err
				break
			}
			items = append(items, item)
		}
		return items, last
	}

	It("follows the cursor across three pages and stops when there is no next one", func() {
		t := pages()

		items, err := collect(client.SearchAll(ctx, unauthenticated(t), facilities(),
			client.FacilitySearchPayload{}))

		Expect(err).NotTo(HaveOccurred())
		Expect(t.hits()).To(Equal(3))
		Expect(items).To(HaveLen(5))
		Expect(items[4].TenantFacilityID).To(Equal("MUC-01"))
	})

	It("sends each page's endCursor as the next page's after", func() {
		t := pages()

		_, err := collect(client.SearchAll(ctx, unauthenticated(t), facilities(),
			client.FacilitySearchPayload{}))

		Expect(err).NotTo(HaveOccurred())
		Expect(t.sent(0).After).To(BeNil())
		Expect(*t.sent(1).After).To(Equal("cursor-1"))
		Expect(*t.sent(2).After).To(Equal("cursor-2"))
	})

	It("asks for a page size that does not cost a round trip per twenty entities", func() {
		t := pages()

		_, err := collect(client.SearchAll(ctx, unauthenticated(t), facilities(),
			client.FacilitySearchPayload{}))

		Expect(err).NotTo(HaveOccurred())
		Expect(*t.sent(0).Size).To(Equal(client.PageSize))
	})

	When("there are more matches than the cap allows", func() {
		// A runaway cursor must not be able to hang a terminal — and a list that was
		// cut short must never look like a complete one.
		It("stops at the cap and says that it did", func() {
			t := pages()

			items, err := collect(client.SearchAll(ctx, unauthenticated(t), facilities(),
				client.FacilitySearchPayload{}, client.MaxItems(3)))

			Expect(items).To(HaveLen(3))

			var truncated *client.TruncatedError
			Expect(errors.As(err, &truncated)).To(BeTrue())
			Expect(truncated.MaxItems).To(Equal(3))
			Expect(err).To(MatchError(ContainSubstring("there are more results")))
		})

		It("does not claim truncation when the cap is exactly the number of matches", func() {
			t := pages()

			items, err := collect(client.SearchAll(ctx, unauthenticated(t), facilities(),
				client.FacilitySearchPayload{}, client.MaxItems(5)))

			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(5))
		})
	})

	When("the API reports another page but gives no cursor to it", func() {
		It("stops, rather than fetching the same page until the user gives up", func() {
			t := serve("facilities_no_cursor.json")

			items, err := collect(client.SearchAll(ctx, unauthenticated(t), facilities(),
				client.FacilitySearchPayload{}))

			Expect(items).To(HaveLen(1))
			Expect(err).To(MatchError(ContainSubstring("no new cursor")))
			Expect(t.hits()).To(Equal(1))
		})
	})

	When("a page fails", func() {
		It("yields the error and stops, keeping the items it already had", func() {
			t := newTenant(func(w http.ResponseWriter, _ *http.Request, n int) {
				if n == 1 {
					fmt.Fprint(w, fixture("facilities_page1.json"))
					return
				}
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `[{"summary":"missing the FACILITIES_READ permission"}]`)
			})

			items, err := collect(client.SearchAll(ctx, unauthenticated(t), facilities(),
				client.FacilitySearchPayload{}))

			Expect(items).To(HaveLen(2))
			Expect(err).To(MatchError(ContainSubstring("FACILITIES_READ")))
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Forbidden))
		})
	})
})
