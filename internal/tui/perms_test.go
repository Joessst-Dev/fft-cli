package tui

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("permission hints", func() {
	picker := newGrants("staging", []roleGrant{
		{Name: "PICKER", Permissions: []string{"PICKJOB_READ", "PICKJOB_WRITE"}},
		{Name: "VIEWER", Permissions: []string{"FACILITY_READ"}},
	})
	needing := func(perms ...string) Operation { return Operation{ID: "op", Permissions: perms} }

	DescribeTable("what the roles say about an operation",
		func(g *grants, op Operation, want access) {
			Expect(g.check(op)).To(Equal(want))
		},
		Entry("one of its permissions is held", picker, needing("FACILITY_READ"), accessGranted),
		Entry("any one of several is enough, not the first",
			picker, needing("FACILITY_WRITE", "PICKJOB_WRITE"), accessGranted),
		Entry("none of them is held", picker, needing("FACILITY_WRITE", "FACILITY_ADMIN"), accessLacking),
		Entry("the API documents none: nobody can tell", picker, needing(), accessUnknown),
		Entry("the roles are not known", (*grants)(nil), needing("FACILITY_READ"), accessUnknown),
		Entry("the user holds no role at all", newGrants("staging", nil), needing("FACILITY_READ"), accessLacking),
	)

	It("lists what is lacking only when it is lacking", func() {
		Expect(picker.lacking(needing("FACILITY_WRITE", "FACILITY_ADMIN"))).
			To(Equal([]string{"FACILITY_WRITE", "FACILITY_ADMIN"}))
		Expect(picker.lacking(needing("FACILITY_READ"))).To(BeEmpty())
		Expect(picker.lacking(needing())).To(BeEmpty())
		Expect((*grants)(nil).lacking(needing("FACILITY_READ"))).To(BeEmpty())
	})

	DescribeTable("the note a question adds",
		func(missing []string, want string) {
			Expect(lackingNote(missing)).To(Equal(want))
		},
		Entry("nothing lacking", nil, ""),
		Entry("one permission", []string{"FACILITY_WRITE"},
			"You appear to lack FACILITY_WRITE, so the tenant may refuse this with 403. "+
				"Your roles may still allow it for some facilities."),
		Entry("several, any of which would do", []string{"A", "B"},
			"You appear to lack any of A, B, so the tenant may refuse this with 403. "+
				"Your roles may still allow it for some facilities."),
	)

	Describe("on the session", func() {
		var s *session

		BeforeEach(func() {
			s = newSession(Options{Runner: newFakeRunner()}, newStyles(false))
			s.resolved = "staging"
			s.grants = picker
		})

		It("applies the roles to the project they were read for", func() {
			Expect(s.lacking(needing("FACILITY_WRITE"))).To(Equal([]string{"FACILITY_WRITE"}))
			Expect(s.lackingNotes(needing("FACILITY_WRITE"), "")).To(HaveLen(1))
			Expect(s.lackingNotes(needing("FACILITY_WRITE"), "staging")).To(HaveLen(1))
		})

		It("says nothing about another project, whose roles it has not read", func() {
			Expect(s.lackingNotes(needing("FACILITY_WRITE"), "prod")).To(BeEmpty())

			s.resolved = "prod"
			Expect(s.lacking(needing("FACILITY_WRITE"))).To(BeEmpty())
		})

		It("forgets the roles when the UI switches project", func() {
			s.selectProject("prod")
			Expect(s.grants).To(BeNil())
		})
	})
})
