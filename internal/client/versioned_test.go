package client_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// warehouse is a versioned entity, standing in for a facility or a stock. Every
// one of them locks the same way: `version` is required, it travels in the body,
// and there is no ETag anywhere in the API.
type warehouse struct {
	Name    string
	Version int
}

// server is the entity as the API holds it, and the conflicts it will raise.
type server struct {
	entity warehouse

	// conflicts is how many more PUTs will be answered with a 409, each one
	// bumping the version underneath as a competing writer would.
	conflicts int

	gets int
	puts []warehouse
}

func (s *server) get(context.Context) (warehouse, int, error) {
	s.gets++
	return s.entity, s.entity.Version, nil
}

func (s *server) put(_ context.Context, w warehouse, version int) (warehouse, error) {
	s.puts = append(s.puts, warehouse{Name: w.Name, Version: version})

	if s.conflicts > 0 {
		s.conflicts--
		s.entity.Version++

		return warehouse{}, client.Check(http.StatusConflict, []byte(fmt.Sprintf(
			`[{"summary":"stale version","version":%d,"requestVersion":%d}]`,
			s.entity.Version, version)))
	}

	s.entity = warehouse{Name: w.Name, Version: version + 1}
	return s.entity, nil
}

func rename(to string) func(*warehouse) error {
	return func(w *warehouse) error {
		w.Name = to
		return nil
	}
}

var _ = Describe("updating a versioned entity", func() {
	var (
		ctx context.Context
		api *server
	)

	BeforeEach(func() {
		ctx = context.Background()
		api = &server{entity: warehouse{Name: "Berlin Mitte", Version: 41}}
	})

	When("nobody else is writing", func() {
		It("reads the entity, applies the change, and writes it back with the version it read", func() {
			updated, err := client.UpdateVersioned(ctx, api.get, api.put, rename("Berlin Hbf"), nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Name).To(Equal("Berlin Hbf"))
			Expect(api.gets).To(Equal(1))
			Expect(api.puts).To(Equal([]warehouse{{Name: "Berlin Hbf", Version: 41}}))
		})
	})

	When("somebody else writes between the read and the write", func() {
		BeforeEach(func() { api.conflicts = 1 })

		It("re-reads and tries once more, and succeeds", func() {
			updated, err := client.UpdateVersioned(ctx, api.get, api.put, rename("Berlin Hbf"), nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Name).To(Equal("Berlin Hbf"))
			Expect(api.gets).To(Equal(2))
		})

		It("writes the second time with the version it just re-read, not the stale one", func() {
			_, err := client.UpdateVersioned(ctx, api.get, api.put, rename("Berlin Hbf"), nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(api.puts).To(Equal([]warehouse{
				{Name: "Berlin Hbf", Version: 41},
				{Name: "Berlin Hbf", Version: 42},
			}))
		})

		It("re-applies the change to the entity it re-read, not to the one that went stale", func() {
			// The mutation has to be replayed against fresh state: applying it to the
			// stale copy would write back the other writer's fields as they were
			// before their change, which is the overwrite optimistic locking exists to
			// prevent.
			api.conflicts = 1
			seen := make([]string, 0, 2)

			_, err := client.UpdateVersioned(ctx, api.get, api.put, func(w *warehouse) error {
				seen = append(seen, fmt.Sprintf("%s@%d", w.Name, w.Version))
				w.Name = "Berlin Hbf"
				return nil
			}, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(seen).To(Equal([]string{"Berlin Mitte@41", "Berlin Mitte@42"}))
		})
	})

	When("the conflict happens again on the retry", func() {
		BeforeEach(func() { api.conflicts = 2 })

		It("gives up rather than reading and writing forever", func() {
			_, err := client.UpdateVersioned(ctx, api.get, api.put, rename("Berlin Hbf"), nil)

			Expect(err).To(HaveOccurred())
			Expect(api.gets).To(Equal(2))
			Expect(api.puts).To(HaveLen(2))
		})

		It("says exactly how stale the request was, instead of 'HTTP 409'", func() {
			_, err := client.UpdateVersioned(ctx, api.get, api.put, rename("Berlin Hbf"), nil)

			Expect(err).To(MatchError("version conflict: you sent v42, current is v43"))
			Expect(exitcode.FromError(err)).To(Equal(exitcode.Conflict))
		})

		It("tells the user the version they would have to pass to force it through", func() {
			_, err := client.UpdateVersioned(ctx, api.get, api.put, rename("Berlin Hbf"), nil)

			var hinted interface{ Hint() string }
			Expect(errors.As(err, &hinted)).To(BeTrue())
			Expect(hinted.Hint()).To(ContainSubstring("--if-version 43"))
		})
	})

	When("the caller already knows the version (--if-version)", func() {
		// The escape hatch for CI: one request instead of two, and a 409 rather than a
		// silent overwrite if the version was wrong.
		It("skips the read and sends the version it was given", func() {
			expected := 41

			_, err := client.UpdateVersioned(ctx, api.get, api.put, rename("Berlin Hbf"), &expected)

			Expect(err).NotTo(HaveOccurred())
			Expect(api.gets).To(BeZero())
			Expect(api.puts).To(Equal([]warehouse{{Name: "Berlin Hbf", Version: 41}}))
		})

		It("surfaces the conflict rather than re-reading behind the user's back", func() {
			api.conflicts = 1
			expected := 7

			_, err := client.UpdateVersioned(ctx, api.get, api.put, rename("Berlin Hbf"), &expected)

			Expect(err).To(MatchError("version conflict: you sent v7, current is v42"))
			Expect(api.gets).To(BeZero())
			Expect(api.puts).To(HaveLen(1))
		})
	})

	When("the change itself fails", func() {
		It("does not write anything", func() {
			boom := errors.New("the file is not a facility")

			_, err := client.UpdateVersioned(ctx, api.get, api.put, func(*warehouse) error {
				return boom
			}, nil)

			Expect(err).To(MatchError(boom))
			Expect(api.puts).To(BeEmpty())
		})
	})

	When("the failure is not a conflict", func() {
		It("surfaces it as it is, without a second attempt", func() {
			forbidden := client.Check(http.StatusForbidden, []byte(`[{"summary":"not permitted"}]`))

			_, err := client.UpdateVersioned(ctx, api.get,
				func(context.Context, warehouse, int) (warehouse, error) {
					return warehouse{}, forbidden
				},
				rename("Berlin Hbf"), nil)

			Expect(err).To(MatchError(forbidden))
			Expect(api.gets).To(Equal(1))
		})
	})
})

var _ = Describe("addressing a facility", func() {
	// Every facility path parameter also accepts urn:fft:facility:tenantFacilityId:…,
	// so a user never has to look up a UUID for an id they chose themselves.
	DescribeTable("FacilityRef",
		func(given, want string) {
			Expect(client.FacilityRef(given)).To(Equal(want))
		},
		Entry("a platform UUID is passed through untouched",
			"6f1b2c4e-9a3d-4f5e-8b7a-1c2d3e4f5a6b",
			"6f1b2c4e-9a3d-4f5e-8b7a-1c2d3e4f5a6b"),
		Entry("an upper-case UUID is still a UUID",
			"6F1B2C4E-9A3D-4F5E-8B7A-1C2D3E4F5A6B",
			"6F1B2C4E-9A3D-4F5E-8B7A-1C2D3E4F5A6B"),
		Entry("the tenant's own id is wrapped as a URN",
			"BER-01",
			"urn:fft:facility:tenantFacilityId:BER-01"),
		Entry("an id that merely looks UUID-ish is still the tenant's",
			"6f1b2c4e-9a3d-4f5e-8b7a",
			"urn:fft:facility:tenantFacilityId:6f1b2c4e-9a3d-4f5e-8b7a"),
		Entry("a URN is not wrapped twice",
			"urn:fft:facility:tenantFacilityId:BER-01",
			"urn:fft:facility:tenantFacilityId:BER-01"),
		Entry("surrounding whitespace is not part of the id",
			"  BER-01  ",
			"urn:fft:facility:tenantFacilityId:BER-01"),
		Entry("an empty id stays empty, so the caller's own validation reports it",
			"", ""),
	)
})
