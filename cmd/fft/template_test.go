package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

var _ = Describe("fft template", func() {
	var c *cli
	var dataDir string

	// body is a request body with everything the specs below need to pick at: a
	// nested object, an array, a string that looks like nothing else, and a
	// version that saving has to drop.
	const body = `{
  "version": 3,
  "order": {
    "tenantOrderId": "A-1",
    "consumer": {"email": "old@example.de"},
    "items": [{"quantity": 9, "id": 9007199254740993}]
  }
}`

	// save writes the sample body as a template, from stdin, the way a script
	// would.
	// The buffer is reset first: a save that is refused before it reads stdin
	// leaves the body sitting there, and two bodies in one buffer is not JSON.
	save := func(args ...string) int {
		c.stdin.Reset()
		c.stdin.WriteString(body)
		return c.run(append([]string{"template", "save"}, args...)...)
	}

	BeforeEach(func() {
		c = newCLI()

		// Both scopes have to be somewhere this spec owns. XDG_DATA_HOME is
		// already hermetic; the project scope resolves from the working
		// directory, which under `go test` is the source tree.
		dataDir = GinkgoT().TempDir()
		c.setenv("XDG_DATA_HOME", dataDir)
		GinkgoT().Chdir(GinkgoT().TempDir())
	})

	userPath := func(name string) string {
		return filepath.Join(dataDir, "fft", "templates", name+".json")
	}

	Describe("save", func() {
		It("writes the body and prints where it landed", func() {
			Expect(save("rush", "--file", "-")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(userPath("rush")))
			Expect(userPath("rush")).To(BeAnExistingFile())
		})

		It("drops a top-level version and says so, because replaying one is a 409", func() {
			Expect(save("rush", "--file", "-")).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring(`Dropped the top-level "version"`))

			Expect(c.run("template", "show", "rush", "-o", "json")).To(Equal(exitcode.OK))
			Expect(c.out()).NotTo(ContainSubstring(`"version"`))
		})

		It("keeps the user's own templates owner-only", func() {
			if runtime.GOOS == "windows" {
				Skip("Windows does not carry Unix file modes")
			}
			Expect(save("rush", "--file", "-")).To(Equal(exitcode.OK))

			info, err := os.Stat(userPath("rush"))
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		})

		It("warns that a project template is going to be committed", func() {
			Expect(save("rush", "--local", "--file", "-")).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("meant to be committed"))
			Expect(c.errOut()).To(ContainSubstring("consumer emails"))
		})

		It("refuses without a terminal to confirm a credential-shaped field bound for the project scope", func() {
			c.stdin.Reset()
			c.stdin.WriteString(`{"clientSecret":"shh","facility":"BER-01"}`)
			Expect(c.run("template", "save", "creds", "--local", "--file", "-")).To(Equal(exitcode.Usage))
			Expect(userPath("creds")).NotTo(BeAnExistingFile())
			Expect(filepath.Join(".fft", "templates", "creds.json")).NotTo(BeAnExistingFile())
		})

		It("writes a credential-shaped field to the project scope with --yes", func() {
			c.stdin.Reset()
			c.stdin.WriteString(`{"clientSecret":"shh","facility":"BER-01"}`)
			Expect(c.run("template", "save", "creds", "--local", "--file", "-", "--yes")).To(Equal(exitcode.OK))
			Expect(filepath.Join(".fft", "templates", "creds.json")).To(BeAnExistingFile())
		})

		It("does not scan a template bound for the user's own scope", func() {
			c.stdin.Reset()
			c.stdin.WriteString(`{"clientSecret":"shh"}`)
			Expect(c.run("template", "save", "creds", "--file", "-")).To(Equal(exitcode.OK))
			Expect(userPath("creds")).To(BeAnExistingFile())
		})

		It("records the active project so a later render can compare", func() {
			c.readOnlyProject(false)
			Expect(save("rush", "--file", "-")).To(Equal(exitcode.OK))

			Expect(c.run("template", "show", "rush", "-o", "json")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"project": "prod"`))
		})

		It("seeds from an operation's example with --from", func() {
			Expect(c.run("template", "save", "fac", "--from", "addFacility")).To(Equal(exitcode.OK))

			Expect(c.run("template", "show", "fac", "-o", "json")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"operationId": "addFacility"`))
		})

		It("carries the operationId through show's round trip too", func() {
			Expect(c.run("template", "save", "fac", "--from", "addFacility")).To(Equal(exitcode.OK))
			Expect(c.run("template", "show", "fac", "-o", "json")).To(Equal(exitcode.OK))
			shown := c.out()

			c.stdin.WriteString(shown)
			Expect(c.run("template", "save", "copy", "--file", "-")).To(Equal(exitcode.OK))

			Expect(c.run("template", "show", "copy", "-o", "json")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"operationId": "addFacility"`))
		})

		It("refuses a body it was never given", func() {
			Expect(c.run("template", "save", "rush")).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("--file, --data or --from"))
		})

		It("refuses a name that could escape the directory, writing nothing", func() {
			for _, name := range []string{"../evil", "a/b", ".hidden"} {
				Expect(save(name, "--file", "-")).To(Equal(exitcode.Usage), "expected %q to be refused", name)
			}
			Expect(filepath.Join(dataDir, "fft", "templates")).NotTo(BeADirectory())
		})

		It("refuses to replace an existing template without --force", func() {
			Expect(save("rush", "--file", "-")).To(Equal(exitcode.OK))
			Expect(save("rush", "--file", "-")).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("--force"))

			Expect(save("rush", "--file", "-", "--force")).To(Equal(exitcode.OK))
		})

		It("refuses a parameter name that would be indistinguishable from a path", func() {
			Expect(save("rush", "--file", "-", "--param", "a.b=order.items.0.quantity")).
				To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("path and not a name"))
		})

		It("refuses a parameter name the body uses at the top level for somewhere else", func() {
			Expect(save("rush", "--file", "-", "--param", "order=order.items.0.quantity")).
				To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("two different places"))
		})

		It("allows a parameter named after the top-level field it points at", func() {
			// --require version=version would be the obvious thing to type, and it
			// is not ambiguous: both readings of --set version= land in one place.
			Expect(save("rush", "--file", "-", "--param", "order=order")).To(Equal(exitcode.OK))
		})

		It("refuses an unusable path at save time rather than at every future render", func() {
			Expect(save("rush", "--file", "-", "--param", "qty=order..quantity")).
				To(Equal(exitcode.Usage))
		})

		// The name-clash check reads the body's top-level keys, and the version is
		// dropped on the way in — so it is not a top-level key of the template that
		// gets written, and a parameter named for it is not ambiguous with anything.
		It("takes a parameter named for the version it drops, since that field is gone by then", func() {
			Expect(save("rush", "--file", "-", "--param", "version=order.items.0.quantity")).
				To(Equal(exitcode.OK))

			Expect(c.run("template", "render", "rush", "--set", "version=5")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"quantity": 5`))
			Expect(c.out()).NotTo(ContainSubstring(`"version"`))
		})
	})

	Describe("list", func() {
		It("says so on stderr and leaves stdout empty when there is nothing", func() {
			Expect(c.run("template", "list")).To(Equal(exitcode.OK))
			Expect(c.out()).To(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring("No templates found."))
		})

		It("prints [] under -o json, so `| jq length` answers 0", func() {
			Expect(c.run("template", "list", "-o", "json")).To(Equal(exitcode.OK))
			Expect(strings.TrimSpace(c.out())).To(Equal("[]"))
		})

		It("lists one row per name, with the scope it came from", func() {
			Expect(save("rush", "--file", "-", "--description", "Rush order")).To(Equal(exitcode.OK))

			Expect(c.run("template", "list")).To(Equal(exitcode.OK))
			Expect(c.out()).To(Equal(
				"NAME   SCOPE   OPERATION   PROJECT   DESCRIPTION\n" +
					"rush   user    -           -         Rush order\n"))
		})

		It("lets a project template win and reports the one it hides", func() {
			Expect(save("rush", "--file", "-", "--description", "Mine")).To(Equal(exitcode.OK))
			Expect(save("rush", "--local", "--file", "-", "--description", "Ours")).To(Equal(exitcode.OK))

			Expect(c.run("template", "list")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("project"))
			Expect(c.out()).To(ContainSubstring("Ours"))
			Expect(c.out()).NotTo(ContainSubstring("Mine"))
			Expect(c.errOut()).To(ContainSubstring(`Hidden by a project template of the same name: "rush"`))
		})

		It("strips a control byte out of a template file's fields before printing them", func() {
			Expect(save("rush", "--file", "-", "--description", "harmless\rSAFE\x1b[31m")).
				To(Equal(exitcode.OK))

			Expect(c.run("template", "list")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("harmlessSAFE"))
			Expect(c.out()).NotTo(ContainSubstring("\r"))
			Expect(c.out()).NotTo(ContainSubstring("\x1b"))
		})

		// Sanitize keeps a newline on purpose, so that multi-line text stays
		// multi-line where show wraps it. A table row is one line by construction,
		// so here the same newline is a row the printer never counted and the
		// reader cannot tell from a real one — which is the whole of "stdout is
		// data". The cell keeps its text; it just cannot leave its line.
		It("folds a newline in a template's fields into the row it belongs to", func() {
			Expect(save("rush", "--file", "-",
				"--description", "Rush order\nzz-forged   user    -   -   Not a real template")).
				To(Equal(exitcode.OK))

			Expect(c.run("template", "list")).To(Equal(exitcode.OK))
			Expect(strings.Split(strings.TrimSuffix(c.out(), "\n"), "\n")).To(HaveLen(2))
			Expect(c.out()).To(ContainSubstring("Rush order zz-forged"))
		})

		It("reports a file it cannot read and lists the rest", func() {
			Expect(save("good", "--file", "-")).To(Equal(exitcode.OK))
			Expect(os.WriteFile(userPath("bad"), []byte("{not json"), 0o600)).To(Succeed())

			Expect(c.run("template", "list")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("good"))
			Expect(c.errOut()).To(ContainSubstring("ignoring"))
		})
	})

	Describe("render", func() {
		BeforeEach(func() {
			Expect(save("rush", "--file", "-",
				"--require", "email=order.consumer.email",
				"--param", "qty=order.items.0.quantity=1")).To(Equal(exitcode.OK))
		})

		It("prints the body on stdout and nothing on stderr", func() {
			Expect(c.run("template", "render", "rush", "--set", "email=a@b.de")).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(BeEmpty())

			var doc map[string]any
			Expect(json.Unmarshal([]byte(c.out()), &doc)).To(Succeed())
		})

		It("applies a declared default, and lets --set beat it", func() {
			Expect(c.run("template", "render", "rush", "--set", "email=a@b.de")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"quantity": 1`))

			Expect(c.run("template", "render", "rush", "--set", "email=a@b.de", "--set", "qty=7")).
				To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"quantity": 7`))
		})

		It("keeps a 19-digit id exact, which float64 would not", func() {
			Expect(c.run("template", "render", "rush", "--set", "email=a@b.de")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("9007199254740993"))
		})

		It("reads a value as JSON, and --set-string as a string", func() {
			Expect(c.run("template", "render", "rush",
				"--set", "email=a@b.de", "--set", "order.tenantOrderId=12345")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"tenantOrderId": 12345`))

			Expect(c.run("template", "render", "rush",
				"--set", "email=a@b.de", "--set-string", "order.tenantOrderId=12345")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"tenantOrderId": "12345"`))
		})

		It("lets the later of --set and --set-string win, whichever flag it was", func() {
			Expect(c.run("template", "render", "rush", "--set", "email=a@b.de",
				"--set-string", "order.tenantOrderId=00123", "--set", "order.tenantOrderId=456")).
				To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"tenantOrderId": 456`))

			Expect(c.run("template", "render", "rush", "--set", "email=a@b.de",
				"--set", "order.tenantOrderId=456", "--set-string", "order.tenantOrderId=00123")).
				To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"tenantOrderId": "00123"`))
		})

		It("creates the objects a path needs on the way", func() {
			Expect(c.run("template", "render", "rush",
				"--set", "email=a@b.de", "--set", "order.address.city=Berlin")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"city": "Berlin"`))
		})

		It("appends at exactly the end of an array", func() {
			Expect(c.run("template", "render", "rush",
				"--set", "email=a@b.de", "--set", "order.items.1.quantity=4")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"quantity": 4`))
		})

		It("names every missing required parameter at once, printing nothing", func() {
			Expect(c.run("template", "render", "rush")).To(Equal(exitcode.Usage))
			Expect(c.out()).To(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring(`"email"`))
			Expect(c.errOut()).To(ContainSubstring("Add --set email=<value>."))
		})

		It("refuses to bury a scalar under a path, printing nothing", func() {
			Expect(c.run("template", "render", "rush",
				"--set", "email=a@b.de", "--set", "order.tenantOrderId.x=1")).To(Equal(exitcode.Usage))
			Expect(c.out()).To(BeEmpty())
			Expect(c.errOut()).To(ContainSubstring("has nowhere to go"))
		})

		It("refuses an index past the end, naming the length", func() {
			Expect(c.run("template", "render", "rush",
				"--set", "email=a@b.de", "--set", "order.items.5.quantity=1")).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("has 1 element"))
		})

		It("exits 6 for a template nobody saved, naming the ones that exist", func() {
			Expect(c.run("template", "render", "rsuh")).To(Equal(exitcode.NotFound))
			Expect(c.errOut()).To(ContainSubstring("Saved templates: rush."))
		})

		It("needs no project at all", func() {
			Expect(c.run("template", "render", "rush", "--set", "email=a@b.de")).To(Equal(exitcode.OK))
			Expect(c.configPath).NotTo(BeAnExistingFile())
		})

		It("prints JSON even under -o yaml, because --file - reads JSON", func() {
			Expect(c.run("template", "render", "rush", "--set", "email=a@b.de", "-o", "yaml")).
				To(Equal(exitcode.OK))
			Expect(c.out()).To(HavePrefix("{"))
		})

		It("warns when the template belongs to another project, and still renders", func() {
			c.readOnlyProject(false)
			path := userPath("rush")
			raw, err := os.ReadFile(path) //nolint:gosec // a path this spec wrote
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(path,
				[]byte(strings.Replace(string(raw), `"body"`, `"project": "staging",  "body"`, 1)),
				0o600)).To(Succeed())

			Expect(c.run("template", "render", "rush", "--set", "email=a@b.de")).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring(`saved under project "staging"`))
			Expect(c.errOut()).To(ContainSubstring(`active project is "prod"`))
			Expect(c.out()).To(ContainSubstring("a@b.de"))
		})

		It("refuses to guess which template when there is nobody to ask", func() {
			Expect(c.run("template", "render")).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("name the template to render"))
		})

		It("offers a numbered list on stderr when there is a terminal", func() {
			c.answer("1")

			Expect(c.run("template", "render", "--set", "email=a@b.de")).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("1) rush"))
			Expect(c.out()).To(ContainSubstring("a@b.de"))
		})

		// The picker is one entry per line and the human answers it by number, so a
		// description carrying a newline could write an entry of its own under a
		// name nobody saved — and the number typed decides what gets rendered.
		It("folds a newline in a description into the picker entry it belongs to", func() {
			Expect(save("rush", "--file", "-", "--force",
				"--require", "email=order.consumer.email",
				"--description", "Rush order\n  2) prod-order  the one you want")).To(Equal(exitcode.OK))
			c.answer("1")

			Expect(c.run("template", "render", "--set", "email=a@b.de")).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("1) rush"))
			Expect(c.errOut()).NotTo(MatchRegexp(`(?m)^\s*2\) `))
			Expect(c.out()).To(ContainSubstring("a@b.de"))
		})
	})

	Describe("show", func() {
		BeforeEach(func() {
			Expect(save("rush", "--file", "-",
				"--description", "Rush order",
				"--require", "email=order.consumer.email")).To(Equal(exitcode.OK))
		})

		It("describes the template rather than rendering it", func() {
			Expect(c.run("template", "show", "rush")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("TEMPLATE"))
			Expect(c.out()).To(ContainSubstring("PARAMETERS"))
			Expect(c.out()).To(ContainSubstring("order.consumer.email"))
			Expect(c.out()).To(ContainSubstring("required"))
		})

		It("keeps a 19-digit id a number under -o yaml, not a quoted string", func() {
			Expect(c.run("template", "show", "rush", "-o", "yaml")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("id: 9007199254740993"))
			Expect(c.out()).NotTo(ContainSubstring(`id: "9007199254740993"`))
			Expect(c.out()).To(ContainSubstring("schemaVersion: 1"))
		})

		// The column is padded to a width, and the width has to be measured on the
		// string that is actually printed: len() counts the two bytes of "ö" twice
		// over, so a multi-byte name pads every other row one column too far.
		It("aligns the parameter column on what prints, not on the bytes of a multi-byte name", func() {
			Expect(os.WriteFile(userPath("uml"), []byte(
				`{"schemaVersion":1,"body":{"order":{}},"params":{
				   "größe":{"path":"order.size"},"qty":{"path":"order.qty"}}}`), 0o600)).To(Succeed())

			Expect(c.run("template", "show", "uml")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("  größe  order.size\n  qty    order.qty\n"))
		})

		// OPERATION and SAVED UNDER print with only their first line indented,
		// unlike PURPOSE, which wrap() reflows into a newline-free string before
		// indent() re-adds one prefix per line. A newline surviving into either
		// would forge a second, unindented block a reader cannot tell from a real
		// one — output.SanitizeCell has to fold it before it gets that far.
		It("folds a newline in the operation id and project into the line they belong to", func() {
			Expect(os.WriteFile(userPath("evil"), []byte(
				`{"schemaVersion":1,"body":{},"operationId":"addFacility","project":"prod"}`),
				0o600)).To(Succeed())
			Expect(c.run("template", "show", "evil")).To(Equal(exitcode.OK))
			clean := c.out()

			Expect(os.WriteFile(userPath("evil"), []byte(
				`{"schemaVersion":1,"body":{},`+
					`"operationId":"addFacility\nTEMPLATE\n  zz-forged (user scope)",`+
					`"project":"prod\nOPERATION\n  zz-forged"}`), 0o600)).To(Succeed())
			Expect(c.run("template", "show", "evil")).To(Equal(exitcode.OK))
			injected := c.out()

			// The embedded newlines are folded to spaces, not dropped: the line
			// count is unchanged from the clean run, so nothing was inserted.
			Expect(strings.Count(injected, "\n")).To(Equal(strings.Count(clean, "\n")))
			Expect(injected).NotTo(ContainSubstring("\nTEMPLATE\n"))
			Expect(injected).NotTo(ContainSubstring("\nOPERATION\n  zz-forged"))
			Expect(injected).To(ContainSubstring("zz-forged"))
		})

		// The whole PARAMETERS line for one name gets a "  " prefix only at its
		// start (see describeTemplateParam), so a newline in a parameter's
		// description has the same forging reach as OPERATION/SAVED UNDER above —
		// and BODY prints unconditionally right after, which is what an attacker
		// would use it to imitate.
		It("folds a newline in a parameter's description into the line it belongs to", func() {
			Expect(os.WriteFile(userPath("evil"), []byte(
				`{"schemaVersion":1,"body":{"order":{}},"params":{`+
					`"qty":{"path":"order.qty","description":"fake"}}}`), 0o600)).To(Succeed())
			Expect(c.run("template", "show", "evil")).To(Equal(exitcode.OK))
			clean := c.out()

			Expect(os.WriteFile(userPath("evil"), []byte(
				`{"schemaVersion":1,"body":{"order":{}},"params":{`+
					`"qty":{"path":"order.qty","description":"fake\nBODY\n{\"harmless\":true}"}}}`),
				0o600)).To(Succeed())
			Expect(c.run("template", "show", "evil")).To(Equal(exitcode.OK))
			injected := c.out()

			Expect(strings.Count(injected, "\n")).To(Equal(strings.Count(clean, "\n")))
			Expect(injected).NotTo(ContainSubstring("\nBODY\n{\"harmless\":true}"))
			Expect(strings.Count(injected, "BODY\n")).To(Equal(1))
			Expect(injected).To(ContainSubstring("fake BODY"))
		})

		It("warns when a project template shadows a user template of the same name", func() {
			Expect(save("rush", "--local", "--file", "-")).To(Equal(exitcode.OK))

			Expect(c.run("template", "show", "rush")).To(Equal(exitcode.OK))
			Expect(c.errOut()).To(ContainSubstring("shadows a user template"))
		})

		It("round-trips through save under -o json, description and required params included", func() {
			Expect(c.run("template", "show", "rush", "-o", "json")).To(Equal(exitcode.OK))
			shown := c.out()

			c.stdin.WriteString(shown)
			Expect(c.run("template", "save", "copy", "--file", "-")).To(Equal(exitcode.OK))

			// copy carries rush's own --require, since round-tripping through show
			// preserves the envelope's description and declared params — not just
			// its body — and neither nests the whole shown document (schemaVersion,
			// params, path and all) one level deeper as the new template's body.
			Expect(c.run("template", "render", "copy")).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring(`"email"`))

			Expect(c.run("template", "show", "copy")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring("Rush order"))

			Expect(c.run("template", "render", "copy", "--set", "email=old@example.de")).To(Equal(exitcode.OK))
			copied := c.out()

			Expect(c.run("template", "render", "rush", "--set", "email=old@example.de")).To(Equal(exitcode.OK))
			Expect(copied).To(MatchJSON(c.out()))
		})

		It("lets an explicit --param/--require replace the params carried in from show, rather than merge", func() {
			Expect(c.run("template", "show", "rush", "-o", "json")).To(Equal(exitcode.OK))
			shown := c.out()

			c.stdin.WriteString(shown)
			Expect(c.run("template", "save", "copy", "--file", "-",
				"--param", "qty=order.items.0.quantity=1")).To(Equal(exitcode.OK))

			// No --require was passed this time, so the one carried in from rush's
			// envelope must not survive alongside it.
			Expect(c.run("template", "render", "copy")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"quantity": 1`))
		})
	})

	Describe("remove", func() {
		BeforeEach(func() {
			Expect(save("rush", "--file", "-")).To(Equal(exitcode.OK))
		})

		It("refuses without a terminal to confirm on, leaving the file there", func() {
			Expect(c.run("template", "remove", "rush")).To(Equal(exitcode.Usage))
			Expect(userPath("rush")).To(BeAnExistingFile())
		})

		It("removes it with --yes", func() {
			Expect(c.run("template", "remove", "rush", "--yes")).To(Equal(exitcode.OK))
			Expect(userPath("rush")).NotTo(BeAnExistingFile())
		})

		It("names the flag that reaches the other scope rather than reporting it absent", func() {
			Expect(c.run("template", "remove", "rush", "--local", "--yes")).To(Equal(exitcode.Usage))
			Expect(c.errOut()).To(ContainSubstring("pass no --local"))
			Expect(userPath("rush")).To(BeAnExistingFile())
		})

		It("names the templates that do exist for a typo, same as render does", func() {
			Expect(c.run("template", "remove", "rsuh", "--yes")).To(Equal(exitcode.NotFound))
			Expect(c.errOut()).To(ContainSubstring("Saved templates: rush."))
		})

		It("exits not-found for a typo without asking to confirm deleting nothing", func() {
			Expect(c.run("template", "remove", "rsuh")).To(Equal(exitcode.NotFound))
			Expect(c.errOut()).To(ContainSubstring("Saved templates: rush."))
			Expect(c.errOut()).NotTo(ContainSubstring("Remove the template"))
		})
	})

	Describe("what it does not do", func() {
		It("makes no request, on a read-only project or any other", func() {
			t := c.readOnlyProject(true)

			Expect(save("rush", "--file", "-")).To(Equal(exitcode.OK))
			Expect(c.run("template", "list")).To(Equal(exitcode.OK))
			Expect(c.run("template", "render", "rush")).To(Equal(exitcode.OK))
			Expect(c.run("template", "show", "rush")).To(Equal(exitcode.OK))
			Expect(t.calls).To(BeEmpty())
		})

		It("composes: a rendered body is what the tenant receives", func() {
			Expect(save("rush", "--file", "-", "--require", "email=order.consumer.email")).
				To(Equal(exitcode.OK))

			Expect(c.run("template", "render", "rush", "--set", "email=a@b.de")).To(Equal(exitcode.OK))
			rendered := c.out()

			t := c.fakeTenant(func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"order-1"}`))
			})

			c.stdin.WriteString(rendered)
			Expect(c.run("api", "addOrder", "--file", "-")).To(Equal(exitcode.OK))

			sent := t.only()
			Expect(sent.json()["order"]).NotTo(BeNil())
			Expect(string(sent.Body)).To(ContainSubstring("a@b.de"))
			Expect(string(sent.Body)).To(ContainSubstring("9007199254740993"))
		})
	})

	Describe("the ephemeral project", func() {
		It("records the headless project's name", func() {
			c.headless()
			Expect(save("rush", "--file", "-")).To(Equal(exitcode.OK))

			Expect(c.run("template", "show", "rush", "-o", "json")).To(Equal(exitcode.OK))
			Expect(c.out()).To(ContainSubstring(`"project": "` + config.EphemeralName + `"`))
		})
	})
})
