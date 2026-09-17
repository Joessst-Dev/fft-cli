package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// opCreateFacility is a write that accepts either of two permissions, and whose
// command asks no question of its own.
var opCreateFacility = Operation{
	ID: "addFacility", Summary: "Create a facility", Method: "POST", Path: "/api/facilities",
	Tag: "Facilities (Core)", Mutates: true, Permissions: []string{"FACILITY_WRITE", "FACILITY_ADMIN"},
	Command: Command{Path: []string{"facility", "create"}, Curated: true, Body: true},
}

// roleCatalog is the fake catalog, and opCreateFacility.
type roleCatalog struct{ fakeCatalog }

func (c roleCatalog) Groups() []OperationGroup {
	return append(c.fakeCatalog.Groups(),
		OperationGroup{Tag: "Facilities (Admin)", Operations: []Operation{opCreateFacility}})
}

// readOnlyRoles is `auth whoami` for a user who may read facilities and nothing else.
const readOnlyRoles = `{"userId":"user-42","email":"bot@ocff-acme-staging.com","roles":[
  {"name":"VIEWER","permissions":["FACILITY_READ","PICKJOB_READ"]},
  {"name":"AUDITOR","permissions":[]}
]}`

// whoamiFor answers `auth whoami` with doc, as the run for project reports it.
func (h *harness) whoamiFor(project, doc string) {
	GinkgoHelper()
	h.finish(Result{ExitCode: exitcode.OK, Stdout: []byte(doc), Project: project}, "auth", "whoami")
}

var _ = Describe("the Roles screen", func() {
	var h *harness

	BeforeEach(func() {
		h = newHarness(Options{Catalog: roleCatalog{}})
		h.loaded(twoProjects, validToken)
	})

	whoamiRuns := func() int {
		return strings.Count(strings.Join(h.r.commandLines(), "\n"), "auth whoami")
	}

	It("reads the roles when it is first shown, for the project on screen", func() {
		Expect(whoamiRuns()).To(BeZero(), "nothing is read before anybody looks")

		h.press("7")
		id := h.lookup("auth", "whoami")
		Expect(h.r.invocation(id).Project).To(Equal("staging"))
		Expect(h.view()).To(ContainSubstring("Reading your roles…"))
		Expect(h.view()).To(ContainSubstring("$ fft auth whoami"))

		h.whoamiFor("staging", readOnlyRoles)
		h.press("1", "7")
		Expect(whoamiRuns()).To(Equal(1), "shown again, the roles are not read again")
	})

	It("shows each role with its permissions, and the operations they do not permit", func() {
		h.press("7")
		h.whoamiFor("staging", readOnlyRoles)

		view := h.view()
		Expect(view).To(ContainSubstring("Signed in as bot@ocff-acme-staging.com (user user-42) on staging."))
		Expect(view).To(MatchRegexp(`ROLE\s+PERMISSIONS`))
		Expect(view).To(MatchRegexp(`VIEWER\s+FACILITY_READ, PICKJOB_READ`))
		Expect(view).To(MatchRegexp(`AUDITOR\s+none`))

		Expect(view).To(ContainSubstring("You appear to lack the permission for 2 operations"))
		Expect(view).To(MatchRegexp(`deleteFacility\s+DELETE /api/facilities/\{facilityId\}\s+needs FACILITY_WRITE`))
		Expect(view).To(MatchRegexp(`addFacility\s+POST /api/facilities\s+needs any of FACILITY_WRITE, FACILITY_ADMIN`))
		Expect(view).NotTo(ContainSubstring("searchFacility"), "held")
		Expect(view).NotTo(ContainSubstring("replaceFacility"), "documents no permission")
		Expect(view).To(ContainSubstring("Greying is only a hint"))
	})

	It("scrolls what does not fit", func() {
		h.send(tea.WindowSizeMsg{Width: 140, Height: 12})
		h.press("7")
		h.whoamiFor("staging", readOnlyRoles)
		Expect(h.view()).NotTo(ContainSubstring("Greying is only a hint"))

		h.press("down", "down", "down", "down", "down", "down")
		Expect(h.view()).To(ContainSubstring("Greying is only a hint"))
		Expect(h.view()).To(ContainSubstring("Signed in as"), "the heading stays")
	})

	It("reads them again with r", func() {
		h.press("7")
		h.whoamiFor("staging", readOnlyRoles)
		h.press("r")
		Expect(whoamiRuns()).To(Equal(2))
	})

	It("reads them in the background, but for an r, which the user asked for", func() {
		h.press("7")
		Expect(h.r.invocation(h.lookup("auth", "whoami")).Background).To(BeTrue(),
			"nobody asked for this request, so history does not keep it")
		h.whoamiFor("staging", readOnlyRoles)

		h.press("r")
		Expect(h.r.invocation(h.lookup("auth", "whoami")).Background).To(BeFalse(),
			"r is the user's own request, as if they had typed fft auth whoami")
	})

	It("reads them again for the project switched to, and drops the old project's", func() {
		h.press("7")
		h.whoamiFor("staging", readOnlyRoles)
		Expect(h.m.hint(opDeleteFacility).lacking).NotTo(BeEmpty())

		h.m.projects.selectProject("prod")
		Expect(h.m.hint(opDeleteFacility).lacking).To(BeEmpty(), "staging's roles say nothing about prod")

		h.press("7")
		id := h.lookup("auth", "whoami")
		Expect(h.r.invocation(id).Project).To(Equal("prod"))
		Expect(h.view()).To(ContainSubstring("Reading your roles…"))
		Expect(h.view()).NotTo(ContainSubstring("VIEWER"))
	})

	It("ignores an answer for a project the UI has left", func() {
		h.press("7")
		stale := h.lookup("auth", "whoami")
		h.m.projects.selectProject("prod")
		h.finishID(stale, Result{ExitCode: exitcode.OK, Stdout: []byte(readOnlyRoles), Project: "staging"})

		Expect(h.m.s.grants).To(BeNil())
		Expect(h.view()).To(ContainSubstring("Reading your roles…"))
		Expect(h.r.invocation(h.lookup("auth", "whoami")).Project).To(Equal("prod"))
	})

	It("says a failed read failed, and greys nothing", func() {
		h.press("7")
		h.finish(failed(exitcode.Auth, "Error: sign-in failed: INVALID_PASSWORD"), "auth", "whoami")

		Expect(h.view()).To(ContainSubstring("reading your roles failed: exit 4"))
		Expect(h.view()).To(ContainSubstring("INVALID_PASSWORD"))
		Expect(h.view()).To(ContainSubstring("Your roles are unknown, so no operation is greyed."))
		Expect(h.m.hint(opDeleteFacility).lacking).To(BeEmpty())
	})

	Describe("after a failed read", func() {
		BeforeEach(func() {
			h.press("7")
			h.finish(failed(exitcode.Auth, "Error: sign-in failed: INVALID_PASSWORD"), "auth", "whoami")
			Expect(whoamiRuns()).To(Equal(1))
		})

		It("reads them again, in the background, once the credentials check out", func() {
			h.press("1", "ctrl+r")
			h.finish(ok(twoProjects), "project", "list")
			h.finish(ok(validToken), "auth", "status")
			h.press("7")

			Expect(whoamiRuns()).To(Equal(2))
			id := h.lookup("auth", "whoami")
			Expect(h.r.invocation(id).Background).To(BeTrue())
			h.whoamiFor("staging", readOnlyRoles)
			Expect(h.m.hint(opDeleteFacility).lacking).NotTo(BeEmpty())
		})

		It("reads them again once the token was refreshed", func() {
			h.press("1", "R")
			h.finish(ok(""), "auth", "refresh")
			h.finish(ok(validToken), "auth", "status")
			h.press("7")

			Expect(whoamiRuns()).To(Equal(2))
		})

		It("does not read them again while the credentials still fail", func() {
			h.press("1", "ctrl+r")
			h.finish(ok(twoProjects), "project", "list")
			h.finish(failed(exitcode.Auth, "Error: no credentials"), "auth", "status")
			h.press("7")

			Expect(whoamiRuns()).To(Equal(1))
			Expect(h.view()).To(ContainSubstring("Press r to try again."))
		})

		It("does not read them again for another project's credentials", func() {
			h.press("1", "ctrl+r")
			h.finish(ok(twoProjects), "project", "list")
			h.finish(ok(`{"project":"prod","store":"keyring","signIn":"password","token":"valid"}`), "auth", "status")
			h.press("7")

			Expect(whoamiRuns()).To(Equal(1))
		})

		It("reads them only once, however often the credentials check out meanwhile", func() {
			h.press("1", "ctrl+r")
			h.finish(ok(twoProjects), "project", "list")
			h.finish(ok(validToken), "auth", "status")
			h.press("7")
			h.press("1", "ctrl+r")
			h.finish(ok(twoProjects), "project", "list")
			h.finish(ok(validToken), "auth", "status")
			h.press("7")

			Expect(whoamiRuns()).To(Equal(2), "the second read is still on its way")
		})
	})

	It("does not read them again after a read that succeeded, when the credentials check out", func() {
		h.press("7")
		h.whoamiFor("staging", readOnlyRoles)
		h.press("1", "ctrl+r")
		h.finish(ok(twoProjects), "project", "list")
		h.finish(ok(validToken), "auth", "status")
		h.press("7")

		Expect(whoamiRuns()).To(Equal(1))
	})

	It("says there is no project rather than failing", func() {
		h.press("7")
		h.finish(failed(exitcode.Config, "Error: no active project"), "auth", "whoami")
		Expect(h.view()).To(ContainSubstring("No project is selected, so there are no roles to show."))
		Expect(h.view()).NotTo(ContainSubstring("failed"))
	})

	It("says a document it cannot read is one it cannot read", func() {
		h.press("7")
		h.whoamiFor("staging", `not json`)
		Expect(h.view()).To(ContainSubstring("reading your roles failed"))
		Expect(h.m.s.grants).To(BeNil())
	})

	It("says the project comes from the environment in headless mode", func() {
		h = newHarness(Options{Catalog: roleCatalog{}, Headless: true})
		h.loaded(`[{"name":"ci","active":true,"ephemeral":true,"baseUrl":"https://ci.example.com"}]`, validToken)
		h.press("7")
		id := h.lookup("auth", "whoami")
		Expect(h.r.invocation(id).Project).To(BeEmpty(), "the environment names the project")
		h.whoamiFor("ci", readOnlyRoles)

		Expect(h.view()).To(ContainSubstring("on ci (environment)."))
		Expect(h.m.hint(opDeleteFacility).lacking).To(Equal([]string{"FACILITY_WRITE"}))
	})
})

var _ = Describe("permission greying", func() {
	var h *harness

	BeforeEach(func() {
		h = newHarness(Options{Catalog: roleCatalog{}})
		h.loaded(twoProjects, validToken)
	})

	// knowRoles has the Operations screen read the roles, and answers with doc.
	knowRoles := func(doc string) {
		GinkgoHelper()
		h.press("2")
		h.whoamiFor("staging", doc)
	}

	It("has the Operations screen read the roles, and hints at what is lacking", func() {
		h.press("2")
		Expect(h.r.commandLines()).To(ContainElement("auth whoami"))
		Expect(h.m.hint(opDeleteFacility).lacking).To(BeEmpty(), "unknown until whoami answers")

		h.whoamiFor("staging", readOnlyRoles)
		Expect(h.m.hint(opDeleteFacility).lacking).To(Equal([]string{"FACILITY_WRITE"}))
		Expect(h.m.hint(opListFacilities).lacking).To(BeEmpty())
		Expect(h.m.hint(opReplaceFacility).lacking).To(BeEmpty(), "no permission documented")
	})

	It("draws a lacking operation dimmed, and the others plain", func() {
		h = newHarness(Options{Catalog: roleCatalog{}, Color: true})
		h.loaded(twoProjects, validToken)
		knowRoles(readOnlyRoles)
		// Off the lacking rows, so that the selection's own style is not what is seen.
		h.m.operations.list.Select(2)

		dim := h.m.st.dim.Render("x")
		dimStart := dim[:strings.Index(dim, "x")]
		var deleteRow, searchRow string
		for line := range strings.SplitSeq(h.m.View().Content, "\n") {
			switch {
			case strings.Contains(line, "deleteFacility"):
				deleteRow = line
			case strings.Contains(line, "searchFacility"):
				searchRow = line
			}
		}
		Expect(deleteRow).To(HavePrefix(dimStart))
		Expect(searchRow).NotTo(HavePrefix(dimStart))
	})

	Describe("the question before a write", func() {
		It("says what the user appears to lack, and still sends on yes", func() {
			knowRoles(readOnlyRoles)
			h.request(opCreateFacility)
			h.press("s")

			Expect(h.view()).To(ContainSubstring("Send Create a facility to staging?"))
			Expect(h.view()).To(ContainSubstring("You appear to lack any of FACILITY_WRITE, FACILITY_ADMIN"))
			h.wait()
			h.press("y")
			Expect(h.last().Args).To(Equal([]string{"facility", "create"}))
		})

		It("says nothing while the roles are unknown", func() {
			h.request(opCreateFacility)
			h.press("s")
			Expect(h.view()).To(ContainSubstring("Send Create a facility to staging?"))
			Expect(h.view()).NotTo(ContainSubstring("appear to lack"))
		})

		It("says nothing when a role permits it", func() {
			knowRoles(`{"roles":[{"name":"ADMIN","permissions":["FACILITY_ADMIN"]}]}`)
			h.request(opCreateFacility)
			h.press("s")
			Expect(h.view()).NotTo(ContainSubstring("appear to lack"))
		})

		It("is said again when the request is sent again", func() {
			knowRoles(readOnlyRoles)
			h.request(opCreateFacility)
			h.press("s")
			h.wait()
			h.press("y")
			h.finishID(RunID(len(h.r.started)), Result{ExitCode: exitcode.Forbidden, Project: "staging"})

			h.press("r")
			Expect(h.view()).To(ContainSubstring("Send Create a facility to staging again?"))
			Expect(h.view()).To(ContainSubstring("You appear to lack any of FACILITY_WRITE, FACILITY_ADMIN"))
		})

		It("is said under the question a command asks for itself", func() {
			knowRoles(readOnlyRoles)
			h.request(opDeleteFacility)
			h.fill(0, "BER-01")
			h.press("s")
			id := h.lookup("facility", "delete", "BER-01")

			h.ask(id, "Delete facility BER-01 (Berlin)?", "delete")
			Expect(h.view()).To(ContainSubstring("Type delete to confirm."))
			Expect(h.view()).To(ContainSubstring("You appear to lack FACILITY_WRITE"))
		})
	})

	It("says on the Request screen what a read appears to lack, and sends it without asking", func() {
		knowRoles(`{"roles":[{"name":"PICKER","permissions":["PICKJOB_READ"]}]}`)
		h.request(opListFacilities)
		Expect(h.view()).To(ContainSubstring("You appear to lack FACILITY_READ"))

		h.press("s")
		Expect(h.last().Args).To(Equal([]string{"facility", "list"}))
	})
})
