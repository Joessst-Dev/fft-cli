package emulator

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Store", func() {
	var store *Store

	BeforeEach(func() {
		store = NewStore(map[string]collectionMeta{})
	})

	Describe("Create", func() {
		It("assigns an id and version 1, and echoes the body", func() {
			got := store.Create("facilities", entityDoc{"name": "Hamburg"})

			Expect(idOf(got)).NotTo(BeEmpty())
			Expect(got).To(HaveKeyWithValue("name", "Hamburg"))
			v, ok := versionOf(got)
			Expect(ok).To(BeTrue())
			Expect(v).To(Equal(1))
		})

		It("keeps an id the body already carries", func() {
			got := store.Create("facilities", entityDoc{"id": "f-custom"})
			Expect(idOf(got)).To(Equal("f-custom"))
		})

		It("gives generated ids that do not collide", func() {
			a := store.Create("facilities", entityDoc{})
			b := store.Create("facilities", entityDoc{})
			Expect(idOf(a)).NotTo(Equal(idOf(b)))
		})

		It("shapes a generated id like a platform UUID, so it passes through FacilityRef", func() {
			created := store.Create("facilities", entityDoc{})
			Expect(idOf(created)).To(MatchRegexp(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`))
		})
	})

	Describe("FindBy", func() {
		It("resolves an entity by a non-id field, as URN path resolution needs", func() {
			store.Create("facilities", entityDoc{"tenantFacilityId": "K12345"})
			id, ok := store.FindBy("facilities", "tenantFacilityId", "K12345")
			Expect(ok).To(BeTrue())

			got, _ := store.Get("facilities", id)
			Expect(got).To(HaveKeyWithValue("tenantFacilityId", "K12345"))
		})

		It("reports a miss when nothing matches", func() {
			_, ok := store.FindBy("facilities", "tenantFacilityId", "nope")
			Expect(ok).To(BeFalse())
		})
	})

	Describe("Get", func() {
		It("returns a stored entity", func() {
			created := store.Create("facilities", entityDoc{"name": "Hamburg"})
			got, ok := store.Get("facilities", idOf(created))
			Expect(ok).To(BeTrue())
			Expect(got).To(HaveKeyWithValue("name", "Hamburg"))
		})

		It("reports a miss for an unknown id", func() {
			_, ok := store.Get("facilities", "nope")
			Expect(ok).To(BeFalse())
		})

		It("does not hand back the stored map, so a caller cannot mutate state", func() {
			created := store.Create("facilities", entityDoc{"name": "Hamburg"})
			got, _ := store.Get("facilities", idOf(created))
			got["name"] = "tampered"

			again, _ := store.Get("facilities", idOf(created))
			Expect(again).To(HaveKeyWithValue("name", "Hamburg"))
		})
	})

	Describe("List", func() {
		It("returns entities in insertion order", func() {
			a := store.Create("facilities", entityDoc{"name": "a"})
			b := store.Create("facilities", entityDoc{"name": "b"})

			list := store.List("facilities")
			Expect(list).To(HaveLen(2))
			Expect(idOf(list[0])).To(Equal(idOf(a)))
			Expect(idOf(list[1])).To(Equal(idOf(b)))
		})

		It("returns nothing for an untouched collection", func() {
			Expect(store.List("facilities")).To(BeEmpty())
		})
	})

	Describe("Update", func() {
		var id string

		BeforeEach(func() {
			created := store.Create("facilities", entityDoc{"name": "Hamburg"})
			id = idOf(created)
		})

		It("replaces the body and bumps the version on a PUT", func() {
			got, err := store.Update("facilities", id, entityDoc{"version": 1, "name": "Bremen"}, false)
			Expect(err).NotTo(HaveOccurred())

			v, _ := versionOf(got)
			Expect(v).To(Equal(2))
			Expect(got).To(HaveKeyWithValue("name", "Bremen"))
		})

		It("merges into the existing entity on a PATCH", func() {
			got, err := store.Update("facilities", id, entityDoc{"version": 1, "city": "HB"}, true)
			Expect(err).NotTo(HaveOccurred())

			Expect(got).To(HaveKeyWithValue("name", "Hamburg"))
			Expect(got).To(HaveKeyWithValue("city", "HB"))
		})

		It("refuses a stale version with a conflict carrying both numbers", func() {
			_, err := store.Update("facilities", id, entityDoc{"version": 99, "name": "x"}, false)

			var conflict *conflictError
			Expect(errors.As(err, &conflict)).To(BeTrue())
			Expect(conflict.requestVersion).To(Equal(99))
			Expect(conflict.version).To(Equal(1))
		})

		It("re-reading the current version and retrying succeeds, as the client does", func() {
			_, err := store.Update("facilities", id, entityDoc{"version": 99}, false)
			Expect(err).To(HaveOccurred())

			current, _ := store.Get("facilities", id)
			v, _ := versionOf(current)
			_, err = store.Update("facilities", id, entityDoc{"version": v, "name": "retry"}, false)
			Expect(err).NotTo(HaveOccurred())
		})

		It("reports a miss for an unknown id", func() {
			_, err := store.Update("facilities", "nope", entityDoc{}, false)
			var notFound *notFoundError
			Expect(errors.As(err, &notFound)).To(BeTrue())
		})
	})

	Describe("Delete", func() {
		It("removes an entity and reports it was there", func() {
			created := store.Create("facilities", entityDoc{})
			id := idOf(created)

			Expect(store.Delete("facilities", id)).To(BeTrue())
			_, ok := store.Get("facilities", id)
			Expect(ok).To(BeFalse())
		})

		It("reports a miss when the entity was not there", func() {
			Expect(store.Delete("facilities", "nope")).To(BeFalse())
		})

		It("drops the entity from the listed order", func() {
			a := store.Create("facilities", entityDoc{})
			b := store.Create("facilities", entityDoc{})
			store.Delete("facilities", idOf(a))

			list := store.List("facilities")
			Expect(list).To(HaveLen(1))
			Expect(idOf(list[0])).To(Equal(idOf(b)))
		})
	})
})
