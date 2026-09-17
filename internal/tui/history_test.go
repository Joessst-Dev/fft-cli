package tui

import (
	"errors"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/history"
)

// fakeHistory answers reads with what a spec put in it, and counts them. It is
// read from the goroutine a command runs on.
type fakeHistory struct {
	mu      sync.Mutex
	entries []history.Entry
	off     string
	err     error
	reads   int

	// panics has the next read panic with this value, once.
	panics any
}

func (f *fakeHistory) Read() ([]history.Entry, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if p := f.panics; p != nil {
		f.panics = nil
		panic(p)
	}
	return append([]history.Entry(nil), f.entries...), f.off, f.err
}

func (f *fakeHistory) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

func (f *fakeHistory) add(e ...history.Entry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, e...)
}

// historyCatalog is the fake catalog, and an operation only `fft api` sends.
type historyCatalog struct{ fakeCatalog }

func (c historyCatalog) Groups() []OperationGroup {
	return append(c.fakeCatalog.Groups(),
		OperationGroup{Tag: "Picking (API)", Operations: []Operation{opAPIGetPickJobs}})
}

var sentAt = time.Date(2026, 7, 12, 11, 0, 0, 0, time.UTC)

// entry is a recorded request, sent minutes after sentAt.
func entry(minutes int, project, op, command string, args ...string) history.Entry {
	return history.Entry{
		V: history.Version, TS: sentAt.Add(time.Duration(minutes) * time.Minute), Source: history.SourceCLI,
		Project: project, OperationID: op, Command: command, Args: args, Status: 200, DurationMS: 120,
	}
}

var _ = Describe("the History screen", func() {
	var (
		h   *harness
		src *fakeHistory
	)

	BeforeEach(func() {
		src = &fakeHistory{entries: []history.Entry{
			entry(1, "staging", "searchFacility", "fft facility list", "--status=ONLINE"),
			entry(2, "prod", "deleteFacility", "fft facility delete", "BER-02"),
			entry(3, "staging", "getPickJob", "fft picking get-pick-job", "--pick-job-id=pj-1"),
			entry(4, "staging", "searchFacility", "fft facility list", "--size=50"),
		}}
		h = newHarness(Options{Catalog: historyCatalog{}, History: src})
		h.loaded(twoProjects, validToken)
	})

	// show goes to the History screen, and lets its read answer.
	show := func() {
		GinkgoHelper()
		h.pump(h.press("6"))
	}

	It("is read when it is first shown, and not again until something ran", func() {
		Expect(src.readCount()).To(BeZero())
		show()
		Expect(src.readCount()).To(Equal(1))

		h.pump(h.press("1"))
		h.pump(h.press("6"))
		Expect(src.readCount()).To(Equal(1), "nothing ran, so nothing can have been recorded")
		Expect(h.r.started).To(HaveLen(2), "the history is read from the file, not through a command")
	})

	It("lists the current project's requests, newest first", func() {
		show()
		view := h.view()
		Expect(view).To(ContainSubstring("History · recent requests  on staging"))
		Expect(view).To(MatchRegexp(`WHEN\s+OPERATION\s+STATUS\s+EXIT\s+TOOK\s+COMMAND`))
		Expect(view).To(MatchRegexp(`(?s)> .*searchFacility\s+200\s+0\s+120ms\s+fft facility list --size=50.*` +
			`getPickJob.*searchFacility.*--status=ONLINE`))
		Expect(view).NotTo(ContainSubstring("deleteFacility"), "sent to prod")
		Expect(view).To(ContainSubstring("$ fft history list --project staging"))
	})

	It("lists the operations used most with t, and back", func() {
		show()
		h.press("t")
		view := h.view()
		Expect(view).To(ContainSubstring("History · most used operations"))
		Expect(view).To(MatchRegexp(`(?s)USES\s+LAST USED\s+OPERATION\s+COMMAND.*> 2\s.*searchFacility.*1\s.*getPickJob`))
		Expect(view).To(ContainSubstring("$ fft history top --project staging"))

		h.press("t")
		Expect(h.view()).To(ContainSubstring("History · recent requests"))
	})

	It("is read again once a run has finished, while it is on display", func() {
		show()
		src.add(entry(5, "staging", "getPickJob", "fft picking get-pick-job", "--pick-job-id=pj-2"))
		h.send(h.m.projects.reload())
		h.pump(h.finish(ok(twoProjects), "project", "list"))
		Expect(h.view()).To(ContainSubstring("--pick-job-id=pj-2"))
		Expect(src.readCount()).To(Equal(2))
	})

	It("is read again with ctrl+r", func() {
		show()
		h.pump(h.press("ctrl+r"))
		Expect(src.readCount()).To(Equal(2))
	})

	It("follows the project switched to", func() {
		show()
		h.m.projects.selectProject("prod")
		h.pump(h.press("6"))
		Expect(h.view()).To(ContainSubstring("deleteFacility"))
		Expect(h.view()).NotTo(ContainSubstring("searchFacility"))
	})

	It("says so when the project has no requests yet", func() {
		src.entries = nil
		show()
		Expect(h.view()).To(ContainSubstring("No requests have been recorded for this project yet."))
	})

	Describe("when history is off", func() {
		BeforeEach(func() {
			src.off = "FFT_HISTORY is off"
		})

		It("says it is off, rather than looking like nothing was sent", func() {
			src.entries = nil
			show()
			Expect(h.view()).To(ContainSubstring("Requests are not being recorded: FFT_HISTORY is off."))
			Expect(h.view()).To(ContainSubstring("this is not a sign that nothing was sent"))
			Expect(h.view()).NotTo(ContainSubstring("No requests have been recorded"))
		})

		It("still shows what was recorded before", func() {
			show()
			Expect(h.view()).To(ContainSubstring("Requests are not being recorded: FFT_HISTORY is off."))
			Expect(h.view()).To(ContainSubstring("--status=ONLINE"))
		})
	})

	It("says a history it cannot read failed", func() {
		src.err = errors.New("read history.jsonl: permission denied")
		show()
		Expect(h.view()).To(ContainSubstring("reading the history failed"))
		Expect(h.view()).To(ContainSubstring("permission denied"))
		Expect(h.view()).NotTo(ContainSubstring("Reading the history…"))

		src.mu.Lock()
		src.err = nil
		src.mu.Unlock()
		h.pump(h.press("ctrl+r"))
		Expect(src.readCount()).To(Equal(2), "a failed read is tried again")
		Expect(h.view()).NotTo(ContainSubstring("reading the history failed"))
		Expect(h.view()).To(ContainSubstring("--size=50"))
	})

	It("says a read that panicked failed, and reads again", func() {
		src.panics = "index out of range"
		show()
		Expect(h.view()).To(ContainSubstring("reading the history failed"))
		Expect(h.view()).To(ContainSubstring("index out of range"))

		h.pump(h.press("ctrl+r"))
		Expect(src.readCount()).To(Equal(2))
		Expect(h.view()).To(ContainSubstring("--size=50"))
	})

	It("says there is none without a history to read", func() {
		h = newHarness(Options{Catalog: historyCatalog{}})
		h.loaded(twoProjects, validToken)
		h.pump(h.press("6"))
		Expect(h.view()).To(ContainSubstring("The request history is not available here."))
	})

	Describe("enter", func() {
		// reopen has e be the one recorded request, and opens it.
		reopen := func(e history.Entry) {
			GinkgoHelper()
			src.entries = []history.Entry{e}
			show()
			h.pump(h.press("ctrl+r"))
			h.press("enter")
			Expect(h.m.current).To(Equal(tabRequest))
		}

		// field is what the Request form holds for label.
		field := func(label string) *requestField {
			GinkgoHelper()
			for _, f := range h.m.request.fields {
				if f.label() == label {
					return f
				}
			}
			Fail("the form has no field " + label)
			return nil
		}

		It("fills in the form the way the request was sent, and sends nothing", func() {
			reopen(entry(1, "staging", "searchFacility", "fft facility list",
				"--all", "--size=50", "--status=ONLINE", "--status=OFFLINE"))

			Expect(h.m.request.op.ID).To(Equal("searchFacility"))
			Expect(field("--status").value()).To(Equal("ONLINE,OFFLINE"))
			Expect(field("--size").value()).To(Equal("50"))
			Expect(field("--all").switched).To(Equal(switchOn))
			Expect(h.m.request.args()).To(Equal([]string{
				"facility", "list", "--status", "ONLINE", "--status", "OFFLINE", "--size", "50", "--all",
			}))
			Expect(h.view()).To(ContainSubstring("Filled in from the request of"))
			Expect(h.r.commandLines()).NotTo(ContainElement(HavePrefix("facility")))
		})

		It("fills in an on/off flag given as off", func() {
			reopen(entry(1, "staging", "searchFacility", "fft facility list", "--all=false"))
			Expect(field("--all").switched).To(Equal(switchOff))
		})

		It("replaces a form it filled in without asking, until the user changes it", func() {
			reopen(entry(1, "staging", "searchFacility", "fft facility list", "--size=50"))
			h.press("6")
			reopen(entry(1, "staging", "deleteFacility", "fft facility delete", "BER-01"))
			Expect(field("<id>").value()).To(Equal("BER-01"))

			h.fill(0, "-02")
			h.press("6", "enter")
			Expect(h.m.current).To(Equal(tabHistory))
			Expect(h.view()).To(ContainSubstring("Replace the Request form for Delete a facility?"))
		})

		It("leaves a value history did not keep empty, and says which", func() {
			reopen(entry(1, "staging", "getPickJobs", "fft api",
				"getPickJobs", "--header=Authorization: <redacted>", "--query=status=OPEN", "--query=size=5"))

			Expect(field("--header").value()).To(BeEmpty())
			Expect(field("--query").value()).To(Equal("status=OPEN,size=5"))
			Expect(h.view()).NotTo(ContainSubstring("<redacted>"))
			Expect(h.view()).To(ContainSubstring("History keeps no value for --header, so it is left empty."))
		})

		It("leaves a redacted argument empty", func() {
			reopen(entry(1, "staging", "deleteFacility", "fft facility delete", history.Redacted))
			Expect(field("<id>").value()).To(BeEmpty())
			Expect(h.view()).To(ContainSubstring("History keeps no value for <id>"))
		})

		It("says the body was not kept, and what the form has no field for", func() {
			reopen(entry(1, "staging", "addPickJob", "fft picking add-pick-job", "--file=-", "--debug"))
			Expect(h.m.request.body).To(BeNil())
			Expect(h.view()).To(ContainSubstring("History keeps no request bodies: press e to write it again."))
			Expect(h.view()).To(ContainSubstring("Not part of this form, so not carried over: --debug."))
		})

		It("opens an empty form for a request sent through another command", func() {
			reopen(entry(1, "staging", "searchFacility", "fft api", "searchFacility", "--param=x=1"))
			Expect(h.m.request.op.ID).To(Equal("searchFacility"))
			Expect(h.m.request.args()).To(Equal([]string{"facility", "list"}))
			Expect(h.view()).To(ContainSubstring("was sent as 'fft api', whose flags this form does not have"))
		})

		It("opens the last request of an operation used most", func() {
			show()
			h.press("t", "enter")
			Expect(h.m.request.op.ID).To(Equal("searchFacility"))
			Expect(field("--size").value()).To(Equal("50"))
		})

		It("says so for an operation this fft does not know", func() {
			src.entries = []history.Entry{entry(1, "staging", "getGone", "fft api", "getGone")}
			show()
			h.press("enter")
			Expect(h.m.current).To(Equal(tabHistory))
			Expect(h.view()).To(ContainSubstring("This fft has no operation getGone"))
		})

		Describe("over a form holding work that was not sent", func() {
			BeforeEach(func() {
				h.request(opGetPickJob)
				h.fill(0, "pj-9")
				src.entries = []history.Entry{entry(1, "staging", "searchFacility", "fft facility list", "--size=50")}
				show()
				h.press("enter")
			})

			It("asks first, and keeps the form on no", func() {
				Expect(h.m.current).To(Equal(tabHistory))
				Expect(h.view()).To(ContainSubstring("Replace the Request form for Get a pick job?"))
				Expect(h.view()).To(ContainSubstring("It holds 1 filled-in field that you have not sent."))

				h.wait()
				h.press("n")
				Expect(h.m.request.op.ID).To(Equal("getPickJob"))
				Expect(field("--pick-job-id").value()).To(Equal("pj-9"))
			})

			It("replaces it on yes", func() {
				h.wait()
				h.press("y")
				Expect(h.m.current).To(Equal(tabRequest))
				Expect(h.m.request.op.ID).To(Equal("searchFacility"))
			})
		})
	})

	Describe("c", func() {
		BeforeEach(func() {
			show()
			h.press("c")
		})

		It("clears through fft history clear, whose question is the one asked", func() {
			id := h.lookup("history", "clear")
			Expect(h.m.owner()).To(Equal(ownerScreen), "the command asks, not the screen")
			Expect(h.view()).To(ContainSubstring("Clearing the history…"))

			q := h.ask(id, "Delete the whole request history?", "")
			Expect(h.view()).To(ContainSubstring("Delete the whole request history?"))
			h.wait()
			h.press("y")
			Expect(h.r.answers).To(ConsistOf(answer{run: id, question: q, yes: true}))

			src.entries = nil
			h.pump(h.finishID(id, Result{ExitCode: exitcode.OK, Stderr: []byte("Cleared 4 entries from the request history.\n")}))
			Expect(h.view()).To(ContainSubstring("Cleared 4 entries from the request history."))
			Expect(h.view()).To(ContainSubstring("No requests have been recorded"))
			Expect(src.readCount()).To(Equal(2))
		})

		It("says nothing was cleared when the answer was no", func() {
			id := h.lookup("history", "clear")
			h.ask(id, "Delete the whole request history?", "")
			h.wait()
			h.press("n")
			h.pump(h.finishID(id, failed(exitcode.General, "Error: cancelled")))
			Expect(h.view()).To(ContainSubstring("Nothing was cleared."))
			Expect(h.view()).NotTo(ContainSubstring("failed"))
		})

		It("says a clear that failed failed", func() {
			h.pump(h.finish(failed(exitcode.General, "Error: remove history.jsonl: permission denied"), "history", "clear"))
			Expect(h.view()).To(ContainSubstring("clearing the history failed"))
		})
	})
})

var _ = Describe("use counts on the Operations screen", func() {
	var (
		h   *harness
		src *fakeHistory
	)

	BeforeEach(func() {
		src = &fakeHistory{entries: []history.Entry{
			entry(1, "staging", "searchFacility", "fft facility list"),
			entry(2, "staging", "searchFacility", "fft facility list"),
			entry(3, "prod", "searchFacility", "fft facility list"),
			entry(4, "staging", "getPickJob", "fft picking get-pick-job"),
		}}
		h = newHarness(Options{Catalog: historyCatalog{}, History: src})
		h.loaded(twoProjects, validToken)
	})

	It("stars each operation with how often the current project sent it", func() {
		h.pump(h.press("2"))
		Expect(h.m.hint(opListFacilities).uses).To(Equal(2))
		Expect(h.m.hint(opGetPickJob).uses).To(Equal(1))
		Expect(h.m.hint(opDeleteFacility).uses).To(BeZero())
		Expect(h.view()).To(MatchRegexp(`searchFacility\s+★2`))
		Expect(h.view()).To(MatchRegexp(`getPickJob\s+★1`))
	})

	It("counts the project switched to", func() {
		h.pump(h.press("2"))
		h.m.projects.selectProject("prod")
		h.pump(h.press("2"))
		Expect(h.m.hint(opListFacilities).uses).To(Equal(1))
		Expect(h.m.hint(opGetPickJob).uses).To(BeZero())
	})

	It("counts a run that finished since, without reading on every frame", func() {
		h.pump(h.press("2"))
		for range 5 {
			_ = h.view()
			h.pump(h.press("down"))
		}
		Expect(src.readCount()).To(Equal(1))

		src.add(entry(5, "staging", "getPickJob", "fft picking get-pick-job"))
		h.pump(h.finish(ok(`{"roles":[]}`), "auth", "whoami"))
		Expect(h.m.hint(opGetPickJob).uses).To(Equal(2))
		Expect(src.readCount()).To(Equal(2))
	})

	It("is not read for a screen that shows no counts", func() {
		h.pump(h.press("5"))
		h.pump(h.finish(ok(`[]`), "template", "list"))
		Expect(src.readCount()).To(BeZero())
	})
})
