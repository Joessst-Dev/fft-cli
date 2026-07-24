package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/Joessst-Dev/fft-cli/internal/component"
)

// A component is another process, and the specs have to prove things about the one
// fft actually spawns: which arguments reached it, which environment it was given,
// which stream its output landed on, what its exit code did.
//
// So the fake component is this test binary, re-executed. A shell script would not
// run on Windows, which CI treats as first-class; building a helper binary per spec
// would cost a compile each time. Copying the test binary into the component
// directory and having it recognise itself costs one file copy for the whole suite,
// and produces a component that is a real executable on all three platforms.
// None of these are FFT_ names, and that is not cosmetic: [component.Environ]
// strips the whole FFT_ namespace out of a component's environment, so a fake
// component keyed on an FFT_ variable would never learn that it was one. Which is
// the boundary working — a component's configuration lives outside the namespace
// fft reserves for the session it hands over.
const (
	// envFakeComponent turns this binary into a component instead of a test run.
	envFakeComponent = "SPEC_COMPONENT"

	// envFakeExit is the status the fake component exits with.
	envFakeExit = "SPEC_COMPONENT_EXIT"

	// envFakeStdout is what it writes to stdout instead of its usual report, for the
	// specs about the output contract.
	envFakeStdout = "SPEC_COMPONENT_STDOUT"
)

// init makes this binary a component when it is run as one.
//
// Before Ginkgo, before the flag parsing a test binary does: by the time either had
// happened the process would have printed something of its own, and a component's
// stdout is a contract.
func init() {
	if os.Getenv(envFakeComponent) == "" {
		return
	}
	runFakeComponent(os.Stdout, os.Stderr, os.Args[1:])
}

// fakeReport is what the fake component prints, and what a spec reads back to find
// out how it was invoked.
type fakeReport struct {
	Args []string          `json:"args"`
	Env  map[string]string `json:"env"`
	Dir  string            `json:"dir"`
}

// runFakeComponent is the whole of the fake component's behaviour: report how it was
// called on stdout, say hello on stderr, exit with what it was told to.
//
// The two streams matter as much as the report. A spec proves that fft passed the
// streams straight through by asserting that this stdout is fft's stdout, byte for
// byte, with fft's own notices nowhere in it.
func runFakeComponent(stdout, stderr io.Writer, args []string) {
	report := fakeReport{Args: args, Env: map[string]string{}}
	for _, entry := range os.Environ() {
		if name, value, ok := strings.Cut(entry, "="); ok && strings.HasPrefix(name, "FFT_") {
			report.Env[name] = value
		}
	}
	report.Dir, _ = os.Getwd()

	if custom := os.Getenv(envFakeStdout); custom != "" {
		fmt.Fprint(stdout, custom)
	} else {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	}

	fmt.Fprintf(stderr, "fake component %s speaking\n", os.Getenv(component.EnvName))

	code, _ := strconv.Atoi(os.Getenv(envFakeExit))
	os.Exit(code)
}

// fakeBinary is the copy of this test binary the fake components run, made once for
// the whole suite because it is tens of megabytes.
var (
	fakeBinaryOnce sync.Once
	fakeBinaryDir  string
	fakeBinaryPath string
	fakeBinaryErr  error
)

// The copy outlives every spec, so it is removed when the suite is — not with
// DeferCleanup, which inside the sync.Once below would bind to whichever spec
// happened to be first and delete the binary out from under all the others.
var _ = AfterSuite(func() {
	if fakeBinaryDir != "" {
		Expect(os.RemoveAll(fakeBinaryDir)).To(Succeed())
	}
})

// fakeComponentBinary returns a path to an executable copy of this test binary.
func fakeComponentBinary() string {
	GinkgoHelper()

	fakeBinaryOnce.Do(func() {
		self, err := os.Executable()
		if err != nil {
			fakeBinaryErr = err
			return
		}

		// Not GinkgoT().TempDir(): that one is removed after the spec that asked for it,
		// and this copy outlives every spec. See the AfterSuite above.
		fakeBinaryDir, fakeBinaryErr = os.MkdirTemp("", "fft-fake-component-")
		if fakeBinaryErr != nil {
			return
		}

		fakeBinaryPath = filepath.Join(fakeBinaryDir, "component")
		fakeBinaryErr = copyExecutable(self, fakeBinaryPath)
	})

	Expect(fakeBinaryErr).NotTo(HaveOccurred(), "could not make a fake component binary")
	return fakeBinaryPath
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // the path is this test binary's own
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755) //nolint:gosec // a temp dir this spec made
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// installFake writes a component into the spec's component root: the manifest as
// given, and an executable copy of this test binary at the path it names.
//
// It writes the directory directly rather than going through `fft component
// install`, because most specs are about what happens *after* a component is
// installed, and routing every one of them through the installer would make them
// specs about the installer.
func (c *cli) installFake(m component.Manifest) string {
	GinkgoHelper()

	root := c.componentRoot()
	dir := filepath.Join(root, m.Name)
	Expect(os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(m.Exec))), 0o755)).To(Succeed())

	Expect(copyExecutable(fakeComponentBinary(), componentExecPath(dir, m.Exec))).To(Succeed())
	writeManifestFile(dir, m)

	// The child is this test binary, so it has to be told to be a component rather
	// than to run the suite again. It inherits the variable from fft's own
	// environment, which is exactly what a component does with anything outside the
	// FFT_ namespace.
	c.setenv(envFakeComponent, "1")

	return dir
}

// componentExecPath is where the executable of a component with this manifest goes,
// with the suffix Windows needs.
func componentExecPath(dir, exec string) string {
	path := filepath.Join(dir, filepath.FromSlash(exec))
	if filepath.Ext(path) == "" && os.PathSeparator == '\\' {
		return path + ".exe"
	}
	return path
}

// writeManifestFile writes a component.yaml, whether or not it is valid — an invalid
// one is a thing the specs need to be able to produce.
func writeManifestFile(dir string, m component.Manifest) {
	GinkgoHelper()

	data, err := yaml.Marshal(m)
	Expect(err).NotTo(HaveOccurred())
	Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(dir, component.ManifestName), data, 0o600)).To(Succeed())
}

// componentRoot is where this spec's components are installed. It is inside the
// temp home hermeticEnv set, so it is the spec's alone.
func (c *cli) componentRoot() string {
	GinkgoHelper()

	root, enabled, err := component.Root(os.LookupEnv)
	Expect(err).NotTo(HaveOccurred())
	Expect(enabled).To(BeTrue())
	Expect(os.MkdirAll(root, 0o755)).To(Succeed())

	return root
}

// fakeManifest is the manifest most specs start from: one command, no session, and
// the test binary behind it.
func fakeManifest(name string) component.Manifest {
	return component.Manifest{
		APIVersion:  component.APIVersion,
		Name:        name,
		Version:     "1.0.0",
		Description: "a component the specs made up",
		Kind:        component.KindCommand,
		Exec:        "bin/" + name,

		Commands: []component.Command{{
			Name:    name,
			Short:   "a command the specs made up",
			Session: component.SessionNone,
		}},
	}
}

// report reads back what the fake component printed to stdout.
func (c *cli) report() fakeReport {
	GinkgoHelper()

	var r fakeReport
	Expect(json.Unmarshal([]byte(c.out()), &r)).To(Succeed(), "stdout was not the fake component's report: %s", c.out())
	return r
}
