package api

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// knownMutatingPOSTs is the other half of the POST census: the 109 POST operations
// that write. It is a fixture, not production code, and it exists so that the two
// lists together must account for every POST in the spec.
//
// That is the whole point. Regenerating opmeta.gen.go against a newer spec can
// introduce a POST nobody has looked at. Fail-closed classification would treat it
// as a mutation, which is safe but silent — and silence is how a read ends up
// blocked in a read-only project for a release nobody understands. So the census
// below fails the build instead: the new operation is in neither list, and someone
// has to decide which.
var knownMutatingPOSTs = []string{
	"Post StowJob Actions",
	"actionsRoutingStrategy",
	"addCarrier",
	"addDiscountToFacility",
	"addDocument",
	"addDocumentDeprecated",
	"addDocumentToProcess",
	"addFacility",
	"addFacilityGroup",
	"addHandoverjob",
	"addLabel",
	"addLoadUnit",
	"addNestedServiceJobLink",
	"addOrder",
	"addPackJob",
	"addPackingContainerType",
	"addPackingSourceContainer",
	"addParcel",
	"addPickJob",
	"addPickJobLoadUnits",
	"addPickRun",
	"addReceiptToInboundProcess",
	"addRemoteConfigurationScope",
	"addServiceJobLink",
	"addShipment",
	"addSubscription",
	"addTag",
	"addTargetContainers",
	"addTargetcontainer",
	"applyOperativeProcessAction",
	"assignFacility",
	"calculateBestCarrier",
	"createAllocationUnderGroup",
	"createAttachment",
	"createAvailabilityChannel",
	"createCarrierCountryServiceMapping",
	"createCarrierToFacility",
	"createCategory",
	"createConnectionToFacility",
	"createCustomService",
	"createCustomServiceAdditionalInformation",
	"createDeliveryNote",
	"createEventScopeConfig",
	"createExpiry",
	"createFacilityCustomServiceConnection",
	"createGroupUnderChannel",
	"createHandoverContainer",
	"createInboundProcess",
	"createItemReturn",
	"createItemReturnJob",
	"createItemReturnParcel",
	"createLoadUnitTypes",
	"createLookupRecord",
	"createMeasurementUnit",
	"createOidcProvider",
	"createOperativeContainerType",
	"createPackagingUnitDetail",
	"createParcelInformation",
	"createPurchaseOrder",
	"createReceipt",
	"createRemoteConfiguration",
	"createReturnNote",
	"createRole",
	"createServiceContainer",
	"createServiceJob",
	"createShippingInformation",
	"createSimulationOrder",
	"createStack",
	"createStock",
	"createStowJob",
	"createUser",
	"directCreateParcel",
	"executeGraphQLCommand",
	"executeMeAction",
	"executeNotificationCenterConfigAction",
	"executeNotificationCenterFacilityConfigAction",
	"facilityAction",
	"facilityGroupAction",
	"handoverJobAction",
	"itemReturnActions",
	"loadUnitActions",
	"orderAction",
	"orderLineItemAction",
	"packJobAction",
	"parcelAction",
	"performLabelAction",
	"performReservationActions",
	"performStocksActions",
	"pickJobAction",
	"pickRunAction",
	"postCancelationReason",
	"postDeliveryPromise",
	"postExternalAction",
	"postExternalActionLog",
	"postExternalStockChangeReason",
	"postFacilityStorageLocations",
	"postFacilityZone",
	"postOrderSourceConfiguration",
	"postRerouteDescription",
	"postRoutingStrategy",
	"postRoutingStrategyNodeConfigCategory",
	"processAction",
	"reRoute",
	"rerouteProcess",
	"restowItemAction",
	"shipmentActions",
	"triggerRetryNotRoutable",
	"updateItemReturnJob",
	"updateServiceData",
	"updateServiceJob",
}

var _ = Describe("Mutates", func() {
	Describe("the method law", func() {
		It("never calls a GET, HEAD or OPTIONS a mutation", func() {
			for _, op := range Operations() {
				switch op.Method {
				case http.MethodGet, http.MethodHead, http.MethodOptions:
					Expect(op.Mutates()).To(BeFalse(),
						"%s %s %s reads, but was classified as a write", op.ID, op.Method, op.Path)
				}
			}
		})

		It("always calls a PUT, PATCH or DELETE a mutation", func() {
			for _, op := range Operations() {
				switch op.Method {
				case http.MethodPut, http.MethodPatch, http.MethodDelete:
					Expect(op.Mutates()).To(BeTrue(),
						"%s %s %s writes, but was classified as a read", op.ID, op.Method, op.Path)
				}
			}
		})
	})

	// The census. POST is the only method the CLI cannot classify from the method
	// alone, so every POST in the spec must appear in exactly one of the two lists —
	// the production allowlist, or the fixture above. Neither is a new operation
	// nobody has classified; both is a contradiction.
	Describe("the POST census", func() {
		mutating := func() map[string]bool {
			m := make(map[string]bool, len(knownMutatingPOSTs))
			for _, id := range knownMutatingPOSTs {
				m[id] = true
			}
			return m
		}

		It("classifies every POST in the spec exactly once", func() {
			known := mutating()

			for _, op := range Operations() {
				if op.Method != http.MethodPost {
					continue
				}

				read, write := readPOSTs[op.ID], known[op.ID]

				Expect(read && write).To(BeFalse(),
					"%q is in both readPOSTs and knownMutatingPOSTs; it cannot be both", op.ID)
				Expect(read || write).To(BeTrue(),
					"the spec has a POST this build has never classified: %q (%s). Add it to readPOSTs "+
						"if it reads, or to knownMutatingPOSTs if it writes — and do not guess from the name",
					op.ID, op.Path)

				Expect(op.Mutates()).To(Equal(write),
					"%q is classified %v by the lists and %v by Mutates()", op.ID, write, op.Mutates())
			}
		})

		// An upstream rename that orphans an entry must be loud. Fail-closed would
		// otherwise silently downgrade a renamed read to a write, and the only symptom
		// would be a search that stopped working on a read-only project.
		It("has no entry that the spec no longer has as a POST", func() {
			for id := range readPOSTs {
				op, ok := LookupOperation(id)
				Expect(ok).To(BeTrue(), "readPOSTs names %q, which the spec does not have", id)
				Expect(op.Method).To(Equal(http.MethodPost),
					"readPOSTs names %q, which the spec now serves as %s", id, op.Method)
			}

			for _, id := range knownMutatingPOSTs {
				op, ok := LookupOperation(id)
				Expect(ok).To(BeTrue(), "knownMutatingPOSTs names %q, which the spec does not have", id)
				Expect(op.Method).To(Equal(http.MethodPost),
					"knownMutatingPOSTs names %q, which the spec now serves as %s", id, op.Method)
			}
		})

		// The spec's own opinion, used as a check on ours. It is one-directional and
		// only useful in one direction: a *_READ scope proves nothing (addDocument
		// declares PROCESS_READ and plainly writes), but a *_WRITE scope on something
		// this file calls a read is the spec contradicting us, and that has to be
		// answered rather than passed over.
		//
		// This is not hypothetical. calculateBestCarrier — "Calculate best carriers",
		// no persistable field anywhere in its request — was allowlisted as a read on
		// the strength of exactly that reading, and the spec guards it with
		// CARRIER_WRITE. It is a mutation here now because of this check.
		It("allowlists no POST that the spec guards with a write permission", func() {
			for id := range readPOSTs {
				op, ok := LookupOperation(id)
				Expect(ok).To(BeTrue())

				for _, permission := range op.Permissions {
					Expect(permission).To(HaveSuffix("_READ"),
						"%q is in readPOSTs, but the spec guards it with %q. Either it writes — move it to "+
							"knownMutatingPOSTs — or say here why the spec is wrong", id, permission)
				}
			}
		})

		It("accounts for all 153 of the spec's POSTs", func() {
			var posts int
			for _, op := range Operations() {
				if op.Method == http.MethodPost {
					posts++
				}
			}
			Expect(posts).To(Equal(len(readPOSTs) + len(knownMutatingPOSTs)))
		})
	})

	// The operations whose name lies about what they do. Each of these was checked
	// against the API documentation; the census above would pass just as happily with
	// any of them on the wrong side, so they are pinned individually.
	DescribeTable("the operations that read like the opposite of what they are",
		func(id string, mutates bool) {
			op, ok := LookupOperation(id)
			Expect(ok).To(BeTrue())
			Expect(op.Mutates()).To(Equal(mutates))
		},
		Entry("postDeliveryPromise reserves stock and creates a PROMISED order", "postDeliveryPromise", true),
		Entry("createSimulationOrder persists the simulation", "createSimulationOrder", true),
		Entry("createLookupRecord is an upsert that answers 200", "createLookupRecord", true),
		Entry("executeGraphQLCommand can carry a mutation", "executeGraphQLCommand", true),
		Entry("calculateBestCarrier reads like a calculator and is guarded CARRIER_WRITE", "calculateBestCarrier", true),

		Entry("searchFacility is a POST that reads", "searchFacility", false),
		Entry("postCheckoutOptions only computes options", "postCheckoutOptions", false),
		Entry("evaluateRoutingStrategy is a dry run", "evaluateRoutingStrategy", false),
		Entry("createSourcingOptionsRequest reserves no stock", "createSourcingOptionsRequest", false),
		Entry("downloadMergedDocuments hands back a PDF", "downloadMergedDocuments", false),
	)

	// Three operationIds in the spec contain spaces. They are map keys and annotation
	// values, never Go identifiers, and this pins that the whole chain tolerates them.
	DescribeTable("the operationIds that are not identifiers",
		func(id string) {
			op, ok := LookupOperation(id)
			Expect(ok).To(BeTrue(), "the spec no longer has %q", id)
			Expect(op.Mutates()).To(BeTrue())
		},
		Entry("Post StowJob Actions", "Post StowJob Actions"),
		Entry("Patch InboundProcess", "Patch InboundProcess"),
		Entry("Patch StowJob", "Patch StowJob"),
	)

	Describe("failing closed", func() {
		It("calls a method it has never seen a mutation", func() {
			Expect(Operation{ID: "trace", Method: "TRACE"}.Mutates()).To(BeTrue())
		})

		It("calls a POST it has never seen a mutation", func() {
			Expect(Operation{ID: "somethingNobodyHasClassified", Method: http.MethodPost}.Mutates()).To(BeTrue())
		})
	})
})
