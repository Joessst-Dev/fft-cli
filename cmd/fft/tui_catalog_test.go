package main

import (
	"net/http"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/tui"
)

// catalogOps is every operation in cat, keyed by id.
func catalogOps(cat tui.Catalog) map[string]tui.Operation {
	ops := make(map[string]tui.Operation)
	for _, g := range cat.Groups() {
		for _, op := range g.Operations {
			ops[op.ID] = op
		}
	}
	return ops
}

// flagNamed is the flag of cmd with that name.
func flagNamed(cmd tui.Command, name string) tui.Flag {
	GinkgoHelper()
	i := slices.IndexFunc(cmd.Flags, func(f tui.Flag) bool { return f.Name == name })
	Expect(i).NotTo(BeNumerically("<", 0), "fft %s has no --%s in the form", strings.Join(cmd.Path, " "), name)
	return cmd.Flags[i]
}

func flagNames(cmd tui.Command) []string {
	names := make([]string, len(cmd.Flags))
	for i, f := range cmd.Flags {
		names[i] = f.Name
	}
	return names
}

var _ = Describe("the TUI's catalog of operations", func() {
	var (
		root *cobra.Command
		cat  *cliCatalog
		ops  map[string]tui.Operation
	)

	BeforeEach(func() {
		c := newCLI()
		root = newRootCmd(c.deps)
		cat = newCLICatalog(newRootCmd(c.deps))
		ops = catalogOps(cat)
	})

	It("lists every operation once, under the tag its command is grouped by", func() {
		seen := make(map[string]bool)
		for _, g := range cat.Groups() {
			Expect(g.Operations).NotTo(BeEmpty(), "group %q", g.Tag)
			for _, op := range g.Operations {
				Expect(seen).NotTo(HaveKey(op.ID), "%s is listed twice", op.ID)
				seen[op.ID] = true
				Expect(op.Tag).To(Equal(g.Tag))
			}
		}
		Expect(seen).To(HaveLen(len(api.Operations())))
	})

	It("lists the groups in tag order", func() {
		tags := make([]string, 0, len(cat.Groups()))
		for _, g := range cat.Groups() {
			tags = append(tags, g.Tag)
		}
		Expect(slices.IsSorted(tags)).To(BeTrue(), "%v", tags)
	})

	It("resolves every operation to the one command that sends it, or else to fft api", func() {
		claims := operationCommands(root)
		for _, op := range api.Operations() {
			got := ops[op.ID]
			if len(claims[op.ID]) > 1 {
				continue
			}
			Expect(got.Command.Path).NotTo(BeEmpty(), op.ID)
			Expect("fft "+strings.Join(got.Command.Path, " ")).To(Equal(commandPath(root, op)), op.ID)

			resolved, rest, err := root.Find(got.Command.Path)
			Expect(err).NotTo(HaveOccurred(), op.ID)
			Expect(rest).To(BeEmpty(), op.ID)
			Expect(resolved.Annotations).To(HaveKeyWithValue(annotationOperationID, op.ID))
		}
	})

	When("more than one command sends the operation", func() {
		// sharedSenders is every operation several commands claim, and the command
		// the form sends it through. An operation that joins the list fails the
		// census until someone has decided which of its claimants, if any, may stand
		// for it.
		sharedSenders := map[string][]string{
			"actionsRoutingStrategy": {"api", "actionsRoutingStrategy"},
			"facilityAction":         {"api", "facilityAction"},
			"orderAction":            {"api", "orderAction"},
			"searchFacility":         {"facility", "search"},
			"searchListing":          {"listing", "search"},
			"searchStock":            {"stock", "search"},
		}

		It("sends each through the command the census names", func() {
			shared := make(map[string][]string)
			for id, claimants := range operationCommands(root) {
				if len(claimants) > 1 {
					shared[id] = ops[id].Command.Path
				}
			}
			Expect(shared).To(Equal(sharedSenders))
		})

		It("sends none that writes through one of its claimants", func() {
			for id, claimants := range operationCommands(root) {
				op, ok := api.LookupOperation(id)
				Expect(ok).To(BeTrue(), id)
				if len(claimants) < 2 {
					continue
				}
				if op.Mutates() {
					Expect(ops[id].Command.Path).To(Equal([]string{"api", id}), "%s writes", id)
					continue
				}
				if path := ops[id].Command.Path; path[0] != "api" {
					sender, _, err := root.Find(path)
					Expect(err).NotTo(HaveOccurred(), id)
					Expect(claimants).To(ContainElement(sender), id)
				}
			}
		})

		It("sends a write through fft api, body and all, rather than through one of them", func() {
			claims := operationCommands(root)
			Expect(claims["orderAction"]).To(HaveLen(2), "the spec needs an operation two curated commands send")
			Expect(claims["facilityAction"]).To(HaveLen(2))

			for _, id := range []string{"orderAction", "facilityAction"} {
				op, ok := api.LookupOperation(id)
				Expect(ok).To(BeTrue())
				Expect(op.Mutates()).To(BeTrue())

				cmd := ops[id].Command
				Expect(cmd.Path).To(Equal([]string{"api", id}))
				Expect(cmd.Curated).To(BeFalse())
				Expect(cmd.Confirms).To(BeFalse())
				Expect(cmd.Args).To(BeEmpty())
				Expect(cmd.Body).To(Equal(op.HasBody))
				Expect(cmd.BodyRequired).To(Equal(op.BodyRequired))
				Expect(flagNames(cmd)).To(ConsistOf("header", "param", "query"))
				for _, f := range cmd.Flags {
					Expect(f.Kind).To(Equal(tui.FlagPairs), "--%s takes name=value pairs, which may hold commas", f.Name)
				}
			}
		})

		It("marks only commands that send a read with a body as able to stand for it", func() {
			var marked []string
			var walk func(*cobra.Command)
			walk = func(cmd *cobra.Command) {
				if cmd.Annotations[annotationSharedReadSender] != "" {
					path := cmd.CommandPath()
					marked = append(marked, path)

					op, ok := api.LookupOperation(cmd.Annotations[annotationOperationID])
					Expect(ok).To(BeTrue(), "%s is marked but claims no operation", path)
					Expect(op.Mutates()).To(BeFalse(), "%s is marked but %s writes", path, op.ID)
					Expect(op.HasBody).To(BeTrue(), "%s is marked but %s takes no body", path, op.ID)
					Expect(cmd.Annotations).NotTo(HaveKey(annotationGenerated), path)
				}
				for _, child := range cmd.Commands() {
					walk(child)
				}
			}
			walk(root)
			Expect(marked).To(ConsistOf("fft facility search", "fft listing search", "fft stock search"))
		})

		It("sends a read through the claimant marked to stand for it, with its table", func() {
			claims := operationCommands(root)
			Expect(claims["searchFacility"]).To(HaveLen(2), "the spec needs a read two curated commands send")

			op, ok := api.LookupOperation("searchFacility")
			Expect(ok).To(BeTrue())
			Expect(op.Mutates()).To(BeFalse())

			cmd := ops["searchFacility"].Command
			Expect(cmd.Path).To(Equal([]string{"facility", "search"}))
			Expect(cmd.Curated).To(BeTrue())
			Expect(cmd.Table).To(BeTrue())
			Expect(cmd.Example).To(BeTrue())
			Expect(cmd.Args).To(BeEmpty())
			Expect(cmd.Body).To(BeTrue())
			Expect(cmd.BodyRequired).To(BeTrue())
			Expect(flagNames(cmd)).To(ConsistOf("all", "max-items", "size", "total"))
		})
	})

	When("an installed component claims the operation", func() {
		It("sends it through fft api", func() {
			c := newCLI()
			m := fakeManifest("pickjob")
			m.Commands[0].Claims = []string{"getPickJob"}
			c.installFake(m)

			got := catalogOps(newCLICatalog(newRootCmd(c.deps)))["getPickJob"].Command
			Expect(got.Path).To(Equal([]string{"api", "getPickJob"}))
			Expect(got.Curated).To(BeFalse())
			Expect(got.Example).To(BeFalse())
			Expect(got.Body).To(BeFalse())
			Expect(flagNames(got)).To(ConsistOf("header", "param", "query"))
		})
	})

	It("tells a write from a read exactly as the read-only gate does", func() {
		for _, op := range api.Operations() {
			Expect(ops[op.ID].Mutates).To(Equal(op.Mutates()), op.ID)
		}
	})

	It("carries what the spec says about each operation", func() {
		op, ok := api.LookupOperation("deleteFacility")
		Expect(ok).To(BeTrue())

		got := ops["deleteFacility"]
		Expect(got.Method).To(Equal(http.MethodDelete))
		Expect(got.Path).To(Equal(op.Path))
		Expect(got.Summary).To(Equal(op.Summary))
		Expect(got.Permissions).To(Equal(op.Permissions))
	})

	When("a curated command covers an operation", func() {
		It("names the curated command, whose generated twin does not exist", func() {
			get, _, err := root.Find([]string{"facility", "get"})
			Expect(err).NotTo(HaveOccurred())
			opID := get.Annotations[annotationOperationID]
			Expect(opID).NotTo(BeEmpty())

			got := ops[opID].Command
			Expect(got.Path).To(Equal([]string{"facility", "get"}))
			Expect(got.Curated).To(BeTrue())
			Expect(got.Table).To(BeTrue())

			twin := commandName(api.Operation{ID: opID})
			Expect(get.Parent().Commands()).NotTo(ContainElement(HaveField("Use", twin)),
				"the generated twin was registered")
		})

		It("reads its positional arguments and the flags it needs off its usage line", func() {
			get := ops["getFacilityConnection"].Command
			Expect(get.Path).To(Equal([]string{"connection", "get"}))
			Expect(get.Args).To(Equal([]tui.Arg{{Name: "id", Required: true}}))
			Expect(flagNamed(get, "facility").Required).To(BeTrue())
			Expect(get.Flags[0].Name).To(Equal("facility"), "a required flag is listed first")

			del := ops["deleteFacilityListing"].Command
			Expect(del.Args).To(Equal([]tui.Arg{{Name: "tenantArticleId", Required: true}}))
			Expect(flagNamed(del, "facility").Required).To(BeTrue())
		})

		It("says whether it needs a body, and whether it prints an example of its own", func() {
			update := ops["replaceFacility"].Command
			Expect(update.Body).To(BeTrue())
			Expect(update.BodyRequired).To(BeTrue())
			Expect(update.Example).To(BeTrue())

			create := ops["addFacility"].Command
			Expect(create.Body).To(BeTrue())
			Expect(create.BodyRequired).To(BeFalse(), "fft facility create can build its body from flags")

			get := ops["getFacility"].Command
			Expect(get.Body).To(BeFalse())
			Expect(get.Example).To(BeFalse())
		})

		It("knows which flags may shape the example it prints", func() {
			create := ops["createConnectionToFacility"].Command
			Expect(flagNamed(create, "type").WithExample).To(BeTrue())
			Expect(flagNamed(create, "type").Default).NotTo(BeEmpty())

			// fft stock create refuses its flags together with --example.
			stock := ops["createStock"].Command
			Expect(stock.Path).To(Equal([]string{"stock", "create"}))
			Expect(stock.Flags).NotTo(BeEmpty())
			for _, f := range stock.Flags {
				Expect(f.WithExample).To(BeFalse(), "--%s", f.Name)
			}
		})

		It("says whether it asks before it acts", func() {
			Expect(ops["deleteFacility"].Command.Confirms).To(BeTrue())
			Expect(ops["getFacility"].Command.Confirms).To(BeFalse())
		})
	})

	When("the command is generated", func() {
		It("offers a flag per parameter, marking the required ones and their values", func() {
			var (
				found bool
				op    api.Operation
				param api.Param
			)
			for _, candidate := range api.Operations() {
				if ops[candidate.ID].Command.Curated {
					continue
				}
				for _, p := range candidate.Params {
					if p.Required && len(p.Enum) > 0 {
						op, param, found = candidate, p, true
					}
				}
				if found {
					break
				}
			}
			Expect(found).To(BeTrue(), "the spec has no generated operation with a required enum parameter")

			cmd := ops[op.ID].Command
			Expect(cmd.Curated).To(BeFalse())
			Expect(cmd.Args).To(BeEmpty())
			f := flagNamed(cmd, kebab(param.Name))
			Expect(f.Required).To(BeTrue())
			Expect(f.Enum).To(Equal(param.Enum))
		})

		It("takes the body's requirement from the spec, and its example from the spec's sample", func() {
			op, ok := api.LookupOperation("addPickJob")
			Expect(ok).To(BeTrue())

			got := ops["addPickJob"]
			Expect(got.Command.Body).To(BeTrue())
			Expect(got.Command.BodyRequired).To(Equal(op.BodyRequired))
			Expect(got.Command.Example).To(BeFalse())
			Expect(got.SampleBody).To(Equal(op.SampleBody))
		})

		It("types its flags", func() {
			for _, op := range api.Operations() {
				cmd := ops[op.ID].Command
				if cmd.Curated || cmd.Path[0] == "api" {
					continue
				}
				// The names registerParamFlags gives, disambiguated the same way.
				taken := reservedFlags(root)
				for _, p := range op.Params {
					name := flagName(p, taken)
					taken[name] = true
					f := flagNamed(cmd, name)
					want := map[api.ParamType]tui.FlagKind{
						api.TypeArray:   tui.FlagList,
						api.TypeBoolean: tui.FlagBool,
						api.TypeInteger: tui.FlagInt,
						api.TypeNumber:  tui.FlagFloat,
					}[p.Type]
					Expect(f.Kind).To(Equal(want), "%s --%s", op.ID, f.Name)
				}
			}
		})
	})

	It("never offers a flag the UI decides, or one that carries the body", func() {
		for _, op := range ops {
			names := flagNames(op.Command)
			for _, taken := range append(slices.Clone(uiOwnedFlags), "file", "data", "example", "help", "yes", "read-only") {
				Expect(names).NotTo(ContainElement(taken), op.ID)
			}
		}
	})

	It("reads cobra's own record of flags that exclude each other", func() {
		cmd, _, err := root.Find([]string{"stock", "create"})
		Expect(err).NotTo(HaveOccurred())
		Expect(cmd.Flags().Lookup("file").Annotations).To(HaveKey(cobraMutuallyExclusive))
	})

	It("names every flag a curated usage line requires as a flag of its command", func() {
		var walk func(*cobra.Command)
		walk = func(cmd *cobra.Command) {
			if cmd.Annotations[annotationOperationID] != "" && cmd.Annotations[annotationGenerated] == "" {
				_, required := usageSyntax(cmd.Use)
				for name := range required {
					Expect(cmd.Flags().Lookup(name)).NotTo(BeNil(), "%s: --%s", cmd.CommandPath(), name)
				}
			}
			for _, child := range cmd.Commands() {
				walk(child)
			}
		}
		walk(root)
	})

	DescribeTable("reading a usage line",
		func(use string, args []tui.Arg, flags []string) {
			gotArgs, gotFlags := usageSyntax(use)
			Expect(gotArgs).To(Equal(args))
			Expect(gotFlags).To(HaveLen(len(flags)))
			for _, f := range flags {
				Expect(gotFlags).To(HaveKey(f))
			}
		},
		Entry("nothing", "list", []tui.Arg(nil), nil),
		Entry("an argument", "get <id>", []tui.Arg{{Name: "id", Required: true}}, nil),
		Entry("a required flag", "list --facility <id>", []tui.Arg(nil), []string{"facility"}),
		Entry("a flag, then an argument", "delete --facility <id> <tenantArticleId>",
			[]tui.Arg{{Name: "tenantArticleId", Required: true}}, []string{"facility"}),
		Entry("two arguments", "evaluate-node <id> <nodeId>",
			[]tui.Arg{{Name: "id", Required: true}, {Name: "nodeId", Required: true}}, nil),
		Entry("an optional argument", "show [<name>]", []tui.Arg{{Name: "name", Required: false}}, nil),
		Entry("alternatives in brackets", "install [<name>|<owner>/<repo>[@<version>]]",
			[]tui.Arg{{Name: "name", Required: false}}, nil),
		Entry("an optional flag", "list [--facility <id>]", []tui.Arg(nil), nil),
		Entry("a placeholder with a dot", "evaluate <id> --file <order.json>",
			[]tui.Arg{{Name: "id", Required: true}}, []string{"file"}),
	)
})

var _ = Describe("the TUI's table of a curated command", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
	})

	It("is offered only for curated commands that exist", func() {
		root := newRootCmd(c.deps)
		for path := range commandTables {
			cmd, rest, err := root.Find(strings.Fields(path))
			Expect(err).NotTo(HaveOccurred(), path)
			Expect(rest).To(BeEmpty(), path)
			Expect(cmd.CommandPath()).To(Equal("fft "+path), path)
			Expect(cmd.Annotations).NotTo(HaveKey(annotationGenerated), path)
		}
	})

	It("is offered for exactly the commands in the registry", func() {
		cat := newCLICatalog(newRootCmd(c.deps))
		for _, op := range catalogOps(cat) {
			_, registered := commandTables[strings.Join(op.Command.Path, " ")]
			Expect(op.Command.Table).To(Equal(registered), op.ID)
		}
	})

	DescribeTable("is the table the command prints in a shell",
		func(path []string, answer string, rest ...string) {
			c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(w, http.StatusOK, answer)
			})
			args := slices.Concat(path, rest)

			Expect(c.run(append(slices.Clone(args), "-o", "table")...)).To(Equal(exitcode.OK), c.errOut())
			shell := c.out()
			Expect(shell).NotTo(BeEmpty())

			Expect(c.run(append(slices.Clone(args), "-o", "json")...)).To(Equal(exitcode.OK), c.errOut())
			table, err := renderTable(path, []byte(c.out()))
			Expect(err).NotTo(HaveOccurred())
			Expect(table).To(Equal(shell))
		},
		Entry("a facility list", []string{"facility", "list"}, searchPage(
			[]string{fixture("facility_managed.json"), fixture("facility_supplier.json")}, false, "", nil)),
		Entry("one facility", []string{"facility", "get"}, fixture("facility_managed.json"), "BER-01"),
		Entry("a connection list", []string{"connection", "list"}, connectionPage([]string{
			connection("c1", typeManagedFacility, "fra-uuid", "DHL_V2", 3),
			connection("c3", typeCustomer, "", "DHL_V2", 7),
		}, 2), "--facility", "BER-01"),
		Entry("one order", []string{"order", "get"}, order("o1", "OPEN", "T-1", 2, 4), "o1"),
		Entry("one stock", []string{"stock", "get"}, fixture("stock.json"), "s1"),
		Entry("a sourcing run, best option first", []string{"sourcing", "get"}, twoOptions, "run-1"),
	)

	It("does not write into the document it reads", func() {
		document := []byte(`[` + fixture("listing.json") + `]`)
		before := slices.Clone(document)

		_, err := renderTable([]string{"listing", "list"}, document)
		Expect(err).NotTo(HaveOccurred())
		Expect(document).To(Equal(before))
	})

	It("has no rows for an empty list, as the command prints none", func() {
		table, err := renderTable([]string{"facility", "list"}, []byte(`[]`))
		Expect(err).NotTo(HaveOccurred())
		Expect(table).To(BeEmpty())
	})

	It("refuses a command it has no table for", func() {
		_, err := renderTable([]string{"picking", "get-pick-job"}, []byte(`{}`))
		Expect(err).To(MatchError(ContainSubstring("no table")))
	})
})

var _ = Describe("operationSender", func() {
	// claimant is a command claiming an operation, as the tree walk finds one.
	claimant := func(name string, annotations ...string) *cobra.Command {
		cmd := &cobra.Command{Use: name, Annotations: map[string]string{annotationOperationID: "op"}}
		for _, a := range annotations {
			cmd.Annotations[a] = "true"
		}
		return cmd
	}
	var (
		marked    = claimant("marked", annotationSharedReadSender)
		other     = claimant("other", annotationSharedReadSender)
		plain     = claimant("plain")
		generated = claimant("generated", annotationGenerated)
	)
	// A search is the read-POST the spec lists; any other POST is a write.
	read := api.Operation{ID: "searchFacility", Method: http.MethodPost, HasBody: true}
	write := api.Operation{ID: "orderAction", Method: http.MethodPost, HasBody: true}
	readNoBody := api.Operation{ID: "getFacility", Method: http.MethodGet}

	DescribeTable("picks the command the request form runs",
		func(op api.Operation, claimants []*cobra.Command, want *cobra.Command) {
			Expect(op.Mutates()).To(Equal(op.ID == write.ID), "the operations must be what they claim")
			Expect(operationSender(op, claimants)).To(BeIdenticalTo(want))
		},
		Entry("the only claimant, whatever it is", readNoBody, []*cobra.Command{plain}, plain),
		Entry("the only claimant of a write", write, []*cobra.Command{plain}, plain),
		Entry("no one, when nothing claims it", read, nil, nil),
		Entry("no one for a shared write, even with every claimant marked", write, []*cobra.Command{marked, other}, nil),
		Entry("no one for a shared read that takes no body", readNoBody, []*cobra.Command{marked, plain}, nil),
		Entry("no one for a shared read with two marked claimants", read, []*cobra.Command{marked, other}, nil),
		Entry("no one for a shared read with no marked claimant", read, []*cobra.Command{plain, generated}, nil),
		Entry("the marked curated command over a generated one", read, []*cobra.Command{generated, marked}, marked),
		Entry("the marked command over an unmarked curated one", read, []*cobra.Command{plain, marked}, marked),
	)
})
