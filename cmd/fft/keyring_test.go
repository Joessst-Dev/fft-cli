package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

// noKeyringStore is a machine with no keychain on it: every write reports the
// sentinel, which is what the real keyring store does under WSL.
type noKeyringStore struct {
	secrets.Store
}

func (noKeyringStore) Set(key, _ string) error {
	return fmt.Errorf("store %q: %w", key, secrets.ErrKeyringUnavailable)
}

// unreachableKeyring is a keychain fft cannot open at all — no session bus, an
// SSH login, a machine that never had one. Every operation reports the sentinel.
type unreachableKeyring struct{ secrets.Store }

func (unreachableKeyring) Kind() string        { return "keyring" }
func (unreachableKeyring) Delete(string) error { return secrets.ErrKeyringUnavailable }
func (unreachableKeyring) Get(string) (string, error) {
	return "", secrets.ErrKeyringUnavailable
}

// refusingFileStore is the cleartext file, refusing to give up what it holds — a
// credentials.json that will not parse, say. It reports its kind as the file
// store, because that is what the user has to be sent to.
type refusingFileStore struct{ secrets.Store }

func (refusingFileStore) Kind() string        { return "file" }
func (refusingFileStore) Delete(string) error { return errors.New("the file will not parse") }

// flickeringKeyring is the only shape in which a credential can be left behind
// *and* recovered: it takes the password, refuses the write after it, is still
// refusing when persistProject tries to roll that password back — and is
// reachable again by the time the sweep runs.
//
// A keychain that simply stays gone leaves residue nothing can reach, which is
// why the sweep is best effort rather than checked.
type flickeringKeyring struct {
	secrets.Store

	writes int
	// refusals is how many deletes are still to be refused: one per kind, which is
	// exactly persistProject's rollback. The sweep that follows finds a bus again.
	// A negative count never runs out, for the keychain that is there and simply
	// will not co-operate.
	refusals int
	// refusal is what a refused delete says. The zero value is the sentinel — a
	// keychain that is not there — which is what the ordinary flicker looks like.
	refusal error
}

// Kind is the keychain's, not the in-memory store it is built on: it stands in
// for a keychain, and messages now name the store they are about.
func (*flickeringKeyring) Kind() string { return "keyring" }

func (s *flickeringKeyring) refuse(key string) error {
	if s.refusal != nil {
		return s.refusal
	}
	return fmt.Errorf("delete %q: %w", key, secrets.ErrKeyringUnavailable)
}

func (s *flickeringKeyring) Set(key, val string) error {
	s.writes++
	if s.writes > 1 {
		return fmt.Errorf("store %q: %w", key, secrets.ErrKeyringUnavailable)
	}
	return s.Store.Set(key, val)
}

func (s *flickeringKeyring) Delete(key string) error {
	if s.refusals < 0 {
		return s.refuse(key)
	}
	if s.refusals > 0 {
		s.refusals--
		return s.refuse(key)
	}
	return s.Store.Delete(key)
}

var _ = Describe("the advice given when there is no keychain", func() {
	// keyringHint is asserted directly rather than through the environment,
	// because hermeticEnv does not clear WSL_* — a developer running this suite
	// inside WSL would otherwise get the other branch and a green build for the
	// wrong reason.
	It("names the flag, the setting, and what the file costs", func() {
		hint := keyringHint(false, "/home/jane/.config/fft/config.yaml")

		Expect(hint).To(ContainSubstring("--no-keyring"))
		Expect(hint).To(ContainSubstring("FFT_NO_KEYRING=1"))
		Expect(hint).To(ContainSubstring(`"noKeyring: true"`))
		Expect(hint).To(ContainSubstring("cleartext"))
	})

	It("says why there is no keychain, when the machine is WSL", func() {
		Expect(keyringHint(true, "/tmp/config.yaml")).To(ContainSubstring("WSL"))
		Expect(keyringHint(false, "/tmp/config.yaml")).NotTo(ContainSubstring("WSL"))
	})

	// A hint that names a file fft does not read is worse than no hint: the user
	// edits it, nothing changes, and the setting looks broken.
	It("names the config file fft would actually read, not a hardcoded path", func() {
		dir := GinkgoT().TempDir()
		GinkgoT().Setenv("XDG_CONFIG_HOME", dir)

		Expect(keyringHint(false, hintConfigPath())).To(ContainSubstring(dir))
	})
})

var _ = Describe("report", func() {
	It("turns an unreachable keychain into exit 3 and a way out", func() {
		var stderr bytes.Buffer

		code := report(&stderr, fmt.Errorf("store the password: %w", secrets.ErrKeyringUnavailable))

		// Not exit 4: nobody refused our credentials, and re-authenticating cannot
		// help. This is a fact about the machine, which is what exit 3 means.
		Expect(code).To(Equal(exitcode.Config))
		Expect(stderr.String()).To(ContainSubstring("no OS keychain is available"))
		Expect(stderr.String()).To(ContainSubstring("--no-keyring"))
	})

	It("leaves every other error exactly as it was", func() {
		var stderr bytes.Buffer

		code := report(&stderr, errors.New("something else went wrong"))

		Expect(code).To(Equal(exitcode.General))
		Expect(stderr.String()).To(ContainSubstring("something else went wrong"))
		Expect(stderr.String()).NotTo(ContainSubstring("--no-keyring"))
	})

	It("says nothing at all when nothing went wrong", func() {
		var stderr bytes.Buffer

		Expect(report(&stderr, nil)).To(Equal(exitcode.OK))
		Expect(stderr.String()).To(BeEmpty())
	})
})

var _ = Describe("choosing the credential store", func() {
	var c *cli

	// seedConfig writes a config file with the given settings block and no
	// projects, which is enough to exercise the store choice.
	seedConfig := func(settings string) {
		Expect(os.WriteFile(c.configPath, []byte("version: 2\nsettings:\n"+settings), 0o600)).To(Succeed())
	}

	BeforeEach(func() {
		c = newCLI()
		// Left unset so the spec exercises fft's own choice rather than one handed
		// to it.
		c.deps.Secrets = nil
		Expect(os.MkdirAll(filepath.Dir(c.configPath), 0o700)).To(Succeed())
	})

	It("uses the keychain by default", func() {
		seedConfig("    output: table\n")

		Expect(c.run("project", "list")).To(Equal(exitcode.OK))

		Expect(c.deps.Secrets.Kind()).To(Equal("keyring"))
	})

	It("uses the file store when the config says so, so nobody has to keep typing the flag", func() {
		seedConfig("    output: table\n    noKeyring: true\n")

		Expect(c.run("project", "list")).To(Equal(exitcode.OK))

		Expect(c.deps.Secrets.Kind()).To(Equal("file"))
	})

	DescribeTable("but the flag and the variable still win over the file",
		func(setup func(), args ...string) {
			seedConfig("    output: table\n    noKeyring: true\n")
			setup()

			Expect(c.run(append([]string{"project", "list"}, args...)...)).To(Equal(exitcode.OK))

			Expect(c.deps.Secrets.Kind()).To(Equal("keyring"))
		},
		Entry("--no-keyring=false", func() {}, "--no-keyring=false"),
		Entry("FFT_NO_KEYRING=0", func() { c.setenv("FFT_NO_KEYRING", "0") }),
	)

	// The setting travels in a file this repo tells people is safe to commit to a
	// dotfiles repo. On the second machine to read it nobody is ever asked — the
	// file store is open before the first command runs — so the run says what it is
	// doing rather than leaving the CREDENTIAL column as the only sign.
	It("says when the config, and not the user, chose cleartext", func() {
		seedConfig("    output: table\n    noKeyring: true\n")

		Expect(c.run("project", "list")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("cleartext"))
		Expect(c.errOut()).To(ContainSubstring("settings.noKeyring"))
	})

	DescribeTable("but stays quiet when the user said so for this run",
		func(setup func(), args ...string) {
			seedConfig("    output: table\n    noKeyring: true\n")
			setup()

			Expect(c.run(append([]string{"project", "list"}, args...)...)).To(Equal(exitcode.OK))

			// Repeating an answer back to the person who just gave it is noise — and it
			// is the way out for anyone who wants the file store on this machine.
			Expect(c.errOut()).NotTo(ContainSubstring("settings.noKeyring"))
		},
		Entry("--no-keyring", func() {}, "--no-keyring"),
		Entry("FFT_NO_KEYRING=1", func() { c.setenv("FFT_NO_KEYRING", "1") }),
	)

	// An answer given on one run must not go on answering for the next. Deps is
	// reused across commands by the spec harness, so a --no-keyring left over from
	// an earlier run would silently suppress the warning here — and these are the
	// specs the rest of this feature leans on for its own correctness.
	It("does not let one run's flag speak for the next", func() {
		seedConfig("    output: table\n    noKeyring: true\n")

		Expect(c.run("project", "list", "--no-keyring")).To(Equal(exitcode.OK))
		Expect(c.errOut()).NotTo(ContainSubstring("settings.noKeyring"))

		// The store is re-opened, as a second process would. What must not carry over
		// is the *answer*: production gets a fresh Deps per command and the harness
		// does not, so this is where a field that is only ever set true shows up.
		c.deps.Secrets = nil

		Expect(c.run("project", "list")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring("settings.noKeyring"))
	})

	// Viper ignores an empty env value and falls through to the default, so an
	// exported-but-empty FFT_NO_KEYRING leaves the config's cleartext choice
	// standing. Counting it as an answer would silence the one warning about it.
	It("does not treat an empty FFT_NO_KEYRING as an answer", func() {
		seedConfig("    output: table\n    noKeyring: true\n")
		c.setenv("FFT_NO_KEYRING", "")

		Expect(c.run("project", "list")).To(Equal(exitcode.OK))

		Expect(c.deps.Secrets.Kind()).To(Equal("file"))
		Expect(c.errOut()).To(ContainSubstring("settings.noKeyring"))
	})

	It("says nothing at all when the keychain is in use", func() {
		seedConfig("    output: table\n")

		Expect(c.run("project", "list")).To(Equal(exitcode.OK))

		Expect(c.errOut()).NotTo(ContainSubstring("cleartext"))
	})

	It("is overridden by headless mode, which touches neither", func() {
		seedConfig("    output: table\n    noKeyring: true\n")
		c.headless()

		Expect(c.run("project", "current")).To(Equal(exitcode.OK))

		Expect(c.deps.Secrets.Kind()).To(Equal("env"))
	})
})

// Switching stores moves nothing, so a project configured before the switch
// still has its password in the keychain. `project remove` is the command that
// promises to leave nothing behind, and it has to mean it — otherwise it reports
// having removed credentials it never looked at, and they sit in a store no fft
// command opens again.
var _ = Describe("fft project remove after the machine switched stores", func() {
	var c *cli

	BeforeEach(func() {
		c = newCLI()
		Expect(addStaging(c, "s3cret")).To(Equal(exitcode.OK))
	})

	// The pairing itself: which store is "the other one" is decided by the kind of
	// the one in use, and getting it backwards would sweep the store fft is about
	// to rely on.
	DescribeTable("pairs the store in use with the one that is not",
		func(inUse secrets.Store, want string) {
			deps := &Deps{Secrets: inUse}

			other := deps.unusedSecrets()

			if want == "" {
				Expect(other).To(BeNil())
				return
			}
			Expect(other).NotTo(BeNil())
			Expect(other.Kind()).To(Equal(want))
		},
		Entry("the keychain, so the file", secrets.NewKeyring(), "file"),
		Entry("the file, so the keychain", secrets.NewFile("/tmp/none.json"), "keyring"),
		// A spec's store, and headless mode's, have no counterpart — and headless
		// never reaches a command that removes anything.
		Entry("memory has none", secrets.NewMem(), ""),
		Entry("the environment has none", secrets.NewEnv(nil), ""),
	)

	It("empties the store this machine is not using either", func() {
		// The keychain the project was configured against, before the switch. It is
		// what unusedSecrets resolves to once the file store is the one in use.
		keychain := secrets.NewMem()
		Expect(keychain.Set(secrets.Key("staging", secrets.KindPassword), "s3cret")).To(Succeed())
		c.deps.unused = keychain

		Expect(c.run("project", "remove", "staging", "--yes")).To(Equal(exitcode.OK))

		Expect(keychain.Snapshot()).To(BeEmpty(), "credentials were left in the keychain")
		Expect(c.errOut()).To(ContainSubstring("and its stored credentials"))
	})

	// The sentinel means "fft could not open that store", which is not the same as
	// "that store is empty". On `project add` it may as well be — the write to it
	// just failed on this machine — but here it is the difference between a
	// password destroyed and a password still sitting in a keychain the next
	// desktop login can read. A laptop driven over SSH with no session bus is the
	// ordinary way to hit it.
	It("does not claim to have destroyed credentials it could not even look at", func() {
		c.deps.unused = unreachableKeyring{}

		Expect(c.run("project", "remove", "staging", "--yes")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring(`Removed project "staging".`))
		Expect(c.errOut()).NotTo(ContainSubstring("and its stored credentials"))
		Expect(c.errOut()).To(ContainSubstring("could not open the keychain"))
		Expect(c.errOut()).To(ContainSubstring("is still there"))
	})

	// sweep runs against whichever store this machine is not using, so on a machine
	// with a working keychain the leftover is the cleartext file. Telling that user
	// to go and look in their keychain would point them away from the password in
	// ~/.local/state, which is the one that matters.
	It("names the file, not the keychain, when the file is the store left behind", func() {
		path := filepath.Join(GinkgoT().TempDir(), "state")
		c.setenv("XDG_STATE_HOME", path)
		c.deps.unused = refusingFileStore{secrets.NewMem()}

		Expect(c.run("project", "remove", "staging", "--yes")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring(filepath.Join(path, "fft", "credentials.json")))
		Expect(c.errOut()).NotTo(ContainSubstring("in the keychain"))
	})

	It("does not claim to have removed what it could not reach", func() {
		keychain := &flickeringKeyring{
			Store:    secrets.NewMem(),
			refusals: -1,
			refusal:  errors.New("the collection is locked"),
		}
		c.deps.unused = keychain

		Expect(c.run("project", "remove", "staging", "--yes")).To(Equal(exitcode.OK))

		Expect(c.errOut()).To(ContainSubstring(`Removed project "staging".`))
		Expect(c.errOut()).NotTo(ContainSubstring("and its stored credentials"))
		Expect(c.errOut()).To(ContainSubstring("the collection is locked"))
	})
})

var _ = Describe("fft project add on a machine with no keychain", func() {
	var c *cli

	add := func(extra ...string) int {
		return c.run(append([]string{"project", "add", "wsl",
			"--base-url", "https://acme.api.fulfillmenttools.com",
			"--api-key", "AIzaSyExample",
			"--project-id", "acme",
			"--env", "prd",
			"--username", "bot"}, extra...)...)
	}

	BeforeEach(func() {
		c = newCLI()
		c.deps.Secrets = noKeyringStore{secrets.NewMem()}
	})

	When("there is no terminal to ask on", func() {
		It("refuses with exit 3 and the way out, having written nothing", func() {
			c.stdin.WriteString("s3cret")

			Expect(add("--password-stdin")).To(Equal(exitcode.Config))

			Expect(c.errOut()).To(ContainSubstring("no OS keychain is available"))
			Expect(c.errOut()).To(ContainSubstring("--no-keyring"))
			// A project in the config file with no credential behind it is a project
			// every later command fails on.
			Expect(c.configPath).NotTo(BeAnExistingFile())
		})

	})

	// --yes has to be refused where it could actually have answered: on a terminal.
	// Asserting it alongside --password-stdin would prove only that stdin was
	// already spent, and would stay green if someone wired deps.AssumeYes into
	// offerFileStore "for consistency" with the other confirmations.
	It("is not answered by --yes on a terminal, which is consent to the questions a command was always going to ask", func() {
		c.answer("s3cret", "n")

		// Storing a password in cleartext on the strength of a blanket -y is exactly
		// the accident the explicit opt-in exists to prevent.
		Expect(add("--yes")).To(Equal(exitcode.Config))
		Expect(c.configPath).NotTo(BeAnExistingFile())
	})

	When("the user is asked and says yes", func() {
		BeforeEach(func() {
			c.answer("s3cret", "y")
			Expect(add()).To(Equal(exitcode.OK))
		})

		It("asks before storing anything in cleartext", func() {
			Expect(c.errOut()).To(ContainSubstring("Store credentials in a 0600 file"))
		})

		It("does not claim a blast radius it does not have, on the first project", func() {
			Expect(c.errOut()).NotTo(ContainSubstring("machine-wide"))
		})

		It("remembers the answer, so the next run does not ask again", func() {
			cfg, err := config.NewStore(c.configPath).Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Settings.NoKeyring).To(BeTrue())
		})

		It("puts the credentials in the file store the setting now points at", func() {
			Expect(c.deps.Secrets.Kind()).To(Equal("file"))
			Expect(secrets.Has(c.deps.Secrets, "wsl")).To(BeTrue())
		})
	})

	// settings.noKeyring is machine-wide, so a yes while adding one project moves
	// every project's credentials. A user who is not told that finds `pre` and
	// `prd` reporting "missing" the next time they look, with nothing pointing at
	// the answer they gave while adding something else.
	When("other projects are already configured", func() {
		// seedProjects writes a config holding the named projects and nothing else.
		seedProjects := func(names ...string) {
			body := "version: 2\nactiveProject: " + names[0] + "\nprojects:\n"
			for _, name := range names {
				body += "    - name: " + name + "\n" +
					"      baseUrl: https://" + name + ".api.fulfillmenttools.com\n" +
					"      email: bot@ocff-acme-prd.com\n"
			}
			body += "settings:\n    output: table\n    updateCheck: false\n"

			Expect(os.MkdirAll(filepath.Dir(c.configPath), 0o700)).To(Succeed())
			Expect(os.WriteFile(c.configPath, []byte(body), 0o600)).To(Succeed())
		}

		It("says the answer moves them too", func() {
			seedProjects("prd", "pre")
			c.answer("s3cret", "n")

			Expect(add()).To(Equal(exitcode.Config))

			Expect(c.errOut()).To(ContainSubstring("machine-wide"))
			Expect(c.errOut()).To(ContainSubstring("2 projects already configured"))
		})

		// The count is of bystanders, and a --force re-add is not one of them. This
		// is the rotate-a-password-after-losing-the-keychain path, where getting it
		// wrong makes the disclosure false in the case it exists for.
		It("does not count the project being re-added with --force", func() {
			seedProjects("wsl")
			c.answer("s3cret", "n")

			Expect(add("--force")).To(Equal(exitcode.Config))

			Expect(c.errOut()).NotTo(ContainSubstring("machine-wide"))
		})

		It("counts only the bystanders when one of several is re-added", func() {
			seedProjects("wsl", "prd", "pre")
			c.answer("s3cret", "n")

			Expect(add("--force")).To(Equal(exitcode.Config))

			Expect(c.errOut()).To(ContainSubstring("2 projects already configured"))
		})
	})

	// The keychain can take the password and then vanish before the API key, in
	// which case persistProject's own rollback fails the same way. Nothing must be
	// left behind in a store the user has just been told they are leaving — and
	// which `project remove` stops looking in once noKeyring is set.
	When("the keychain took the password before it went away", func() {
		var keychain *secrets.MemStore
		var flickering *flickeringKeyring

		BeforeEach(func() {
			keychain = secrets.NewMem()
			flickering = &flickeringKeyring{Store: keychain, refusals: len(secrets.AllKinds())}
			c.deps.Secrets = flickering
			c.answer("s3cret", "y")
		})

		It("sweeps it out of the old store once the new one has the credentials", func() {
			Expect(add()).To(Equal(exitcode.OK))

			Expect(secrets.Has(keychain, "wsl")).To(BeFalse(),
				"the password was left behind in a store fft has stopped looking in")
			Expect(c.deps.Secrets.Kind()).To(Equal("file"))
			Expect(secrets.Has(c.deps.Secrets, "wsl")).To(BeTrue())
		})

		// The other half of sweep: a keychain that is *there* and refuses. Nothing
		// can clear it but the user, so they have to be told — silence would leave a
		// password sitting in a store no fft command looks in any more.
		It("says so when the old store refuses to give the credentials up", func() {
			flickering.refusals = -1 // never stops refusing
			flickering.refusal = errors.New("the collection is locked")

			Expect(add()).To(Equal(exitcode.OK))

			Expect(c.errOut()).To(ContainSubstring(`Left "wsl"'s credentials in the keychain`))
			Expect(c.errOut()).To(ContainSubstring("the collection is locked"))
		})

		// The sweep must never run on the strength of a write that has not landed.
		// secrets.NewFile does no I/O, so the fallback store's first write is inside
		// the retry — and if that fails for its own reasons, deleting first would
		// leave the credentials in neither store. With --force that is a password the
		// user still needed and cannot get back.
		It("deletes nothing when the fallback store cannot be written", func() {
			// A regular file where the credentials directory has to go: the file store
			// cannot create a directory underneath it.
			blocked := filepath.Join(GinkgoT().TempDir(), "not-a-directory")
			Expect(os.WriteFile(blocked, nil, 0o600)).To(Succeed())
			c.setenv("XDG_STATE_HOME", blocked)

			Expect(add()).NotTo(Equal(exitcode.OK))

			Expect(secrets.Has(keychain, "wsl")).To(BeTrue(),
				"credentials were deleted for a write that never landed")
		})
	})

	// The sentinel guard is the whole of what keeps this prompt away from every
	// other reason a write can fail. A locked keychain, a denied prompt, a failed
	// config save — none of them are "there is nowhere to put this", and offering
	// cleartext for any of them is the accident the rest of this design exists to
	// prevent. Inverting or deleting that one `if` must not leave the suite green.
	When("the store fails for a reason that is not a missing keychain", func() {
		BeforeEach(func() {
			c.deps.Secrets = failingSetStore{secrets.NewMem()}
			c.answer("s3cret", "y")
		})

		It("never offers the cleartext file", func() {
			Expect(add()).NotTo(Equal(exitcode.OK))

			Expect(c.errOut()).NotTo(ContainSubstring("0600 file"))
			Expect(c.errOut()).NotTo(ContainSubstring("no OS keychain"))
		})

		It("passes the original failure through untouched", func() {
			Expect(add()).To(Equal(exitcode.General))

			Expect(c.errOut()).To(ContainSubstring("keychain is locked"))
			Expect(c.configPath).NotTo(BeAnExistingFile())
		})
	})

	When("the user is asked and says no", func() {
		It("stops, with the same error it would have given without a terminal", func() {
			c.answer("s3cret", "n")

			Expect(add()).To(Equal(exitcode.Config))

			Expect(c.errOut()).To(ContainSubstring("no OS keychain is available"))
			Expect(c.configPath).NotTo(BeAnExistingFile())
		})
	})
})
