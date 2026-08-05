package main

import (
	"errors"
	"fmt"

	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

// explainKeyring replaces the backend's account of an unreachable keychain with
// one the user can act on.
//
// The store cannot write this message itself: the way out is fft's own flag and
// fft's own config file, and it is different under WSL — which is a fact about
// the machine, not about secrets.
func explainKeyring(err error) error {
	if err == nil || !errors.Is(err, secrets.ErrKeyringUnavailable) {
		return err
	}
	return config.NewError(err, keyringHint(runningOnWSL(), hintConfigPath()))
}

// hintConfigPath names the file the user should edit. Resolved, not written out
// literally: $XDG_CONFIG_HOME moves it, and a hint that points at a file fft does
// not read is worse than no hint at all.
func hintConfigPath() string {
	path, err := config.DefaultPath()
	if err != nil {
		return "~/.config/fft/config.yaml"
	}
	return path
}

// keyringHint names the ways out, in the order a user should consider them: the
// flag for right now, the setting for every run after, and the keychain itself
// for anyone who would rather not store a password in cleartext at all.
func keyringHint(wsl bool, configPath string) string {
	permanent := `Pass --no-keyring (or set FFT_NO_KEYRING=1) to store credentials in a 0600 file
instead, or add "noKeyring: true" under "settings:" in ` + configPath + ` to
make that permanent — that file holds your password and refresh token in cleartext.`

	if wsl {
		return "WSL ships no Secret Service, so there is no keychain for fft to use.\n\n" +
			permanent + "\nTo keep a real keychain, install gnome-keyring and run fft inside a session D-Bus."
	}
	return permanent + "\nTo keep a real keychain, install a Secret Service (gnome-keyring, kwallet) and\n" +
		"run fft inside a session D-Bus."
}

// sweepResult is what became of a project's credentials in the store fft has
// stopped using.
//
// Three outcomes and not two, because "the store said nothing was wrong" and
// "fft could not open the store" are the same silence and mean opposite things.
// Only one caller can tell them apart from context, so neither may be folded into
// the other here.
type sweepResult int

const (
	// swept: the store no longer holds anything of this project's.
	swept sweepResult = iota
	// unreachable: fft could not open the store, so it cannot say what is in it.
	unreachable
	// refused: the store is there and would not give the credentials up.
	refused
)

// sweep empties a project out of a credential store fft is not using any more.
//
// Two callers, one job. `project add` uses it for the keychain that took a
// password and then went away, so persistProject's own rollback failed the same
// way the write did; `project remove` uses it for the store the machine is not
// configured for, because switching stores does not move what was already in the
// old one. Either way, what is left behind sits in a place no fft command looks.
//
// It must run only after the new store has the credentials, never before:
// deleting first would mean a write that fails for its own reasons leaves the
// project with credentials in neither store.
//
// The refusal is reported here because it reads the same for either caller:
// something real is being left behind, in a place only the user can clear. What
// an unreachable store means is the caller's to say, or not — so the failure is
// handed back rather than swallowed, for the caller that does say something.
func sweep(deps *Deps, store secrets.Store, project string) (sweepResult, error) {
	err := secrets.DeleteAll(store, project)
	switch {
	case err == nil:
		return swept, nil
	case onlyUnavailable(err):
		return unreachable, err
	}
	deps.Printer.Notef(
		"Left %q's credentials in %s (%v); remove them there if you no longer want them.",
		project, storeLocation(store), err)
	return refused, err
}

// onlyUnavailable reports whether every failure in err is the store being absent.
//
// DeleteAll joins one error per kind, and errors.Is is satisfied by any single
// member — so a bus that dropped part-way through a loop that had already been
// refused once would read as "nothing there" and lose the refusal. A refusal is
// the outcome with something to say, so it wins ties.
//
// One level down and no further: the members of that join are ordinary wrapped
// errors, and errors.Is walks the rest. Recursing would mean descending into
// anything else that spells Unwrap() []error — which is why the secrets package
// keeps that shape for lists of independent failures and nothing else.
func onlyUnavailable(err error) bool {
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return errors.Is(err, secrets.ErrKeyringUnavailable)
	}
	for _, e := range joined.Unwrap() {
		if !errors.Is(e, secrets.ErrKeyringUnavailable) {
			return false
		}
	}
	return true
}

// storeLocation names where a store keeps things, for a message telling someone
// to go and look there.
//
// It has to ask the store rather than assume, because sweep runs against whichever
// one this machine is not using: on a machine with a keychain, that is the
// cleartext file. Sending a user to their keychain to clean up a password sitting
// in ~/.local/state would be pointing them away from the one that matters.
func storeLocation(store secrets.Store) string {
	switch kind := store.Kind(); kind {
	case "keyring":
		return "the keychain"
	case "file":
		return credentialsFilePath()
	default:
		return "the " + kind + " store"
	}
}

// credentialsFilePath names the 0600 fallback file, or describes it when the path
// cannot be resolved — a message must not fail a command.
func credentialsFilePath() string {
	path, err := secrets.DefaultFilePath()
	if err != nil {
		return "the fallback credentials file"
	}
	return path
}

// unusedSecrets opens the credential store this machine is *not* configured to
// use, or nil when there is no second one to speak of.
//
// It exists because settings.noKeyring switches where fft looks without moving
// what is already stored: a machine that falls back to the file store leaves
// every existing project's secrets in the keychain, and a command that says it
// removed a project's credentials has to mean it.
//
// A nil store and an error are different answers, and the difference is the same
// one sweepResult draws: nil means there is genuinely no second store — a spec's
// in-memory one, headless mode's environment — where nothing can have been left
// behind, and an error means there is one that could not be opened. Returning nil
// for both would quietly claim the credentials were cleared from a store nobody
// looked in.
//
// Headless never reaches the commands that call this; they refuse to touch the
// config file at all.
func (d *Deps) unusedSecrets() (secrets.Store, error) {
	if d.unused != nil {
		return d.unused, nil
	}
	switch d.Secrets.Kind() {
	case "keyring":
		path, err := secrets.DefaultFilePath()
		if err != nil {
			return nil, fmt.Errorf("locate the credentials file: %w", err)
		}
		return secrets.NewFile(path), nil
	case "file":
		return secrets.NewKeyring(), nil
	default:
		return nil, nil
	}
}

// offerFileStore asks whether to fall back to the 0600 file, and switches the
// store over if the answer is yes. interactive is the caller's, not the
// prompter's: `project add --password-stdin` has a terminal but no stdin left to
// read an answer from.
//
// others is how many projects are already configured, because the question says
// so when there are any. settings.noKeyring is machine-wide, not per project, so
// a yes moves every project's credentials — and a user who thought they were
// answering about the one in front of them would next see the others reported as
// "missing", with nothing pointing at why.
//
// --yes deliberately does not answer this one, unlike every other confirmation in
// the tree. -y is consent to the questions a command was always going to ask;
// downgrading credential storage to cleartext on the strength of a blanket flag
// is exactly the accident the explicit opt-in exists to prevent. A provisioning
// script that wants the file store passes --no-keyring, which says so.
func offerFileStore(deps *Deps, interactive bool, others int) (bool, error) {
	if !interactive {
		return false, nil
	}

	question := "No OS keychain is available. Store credentials in a 0600 file, in cleartext, instead?"
	if others > 0 {
		// Not just "they will read from the file too". Switching stores does not move
		// what is already in the old one, so those projects will look in a file their
		// credentials are not in and stop being able to sign in. That is the part a
		// user needs before answering, not after.
		question += fmt.Sprintf(
			"\nThis setting is machine-wide, and it moves nothing: the %d %s already configured"+
				"\nwill look in that file too, where their credentials are not, and will need adding again.",
			others, plural(others, "project", "projects"))
	}

	confirmed, err := deps.Prompt.Confirm(question)
	if err != nil || !confirmed {
		return false, err
	}

	if err := deps.reopenSecrets(); err != nil {
		return false, err
	}
	return true, nil
}
