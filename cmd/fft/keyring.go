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

// sweep empties a project out of a credential store fft is not using any more,
// and reports whether the store can now be said to hold nothing of that project's.
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
func sweep(deps *Deps, store secrets.Store, project string) bool {
	err := secrets.DeleteAll(store, project)
	switch {
	case err == nil:
		return true
	// A keychain that is not there cannot be tidied, and there is nothing in it to
	// tidy — that is what unavailable means. Saying so on every removal on a WSL
	// box would be noise about a store the user has already been told fft cannot
	// reach.
	case errors.Is(err, secrets.ErrKeyringUnavailable):
		return true
	}
	// Anything else and the keychain *is* there and refused — locked, access
	// denied — so something real is being left behind and only the user can clear
	// it. Never fatal: the caller has already done the thing it was asked to do.
	deps.Printer.Notef(
		"Left %q's credentials in the keychain (%v); remove them there if you no longer want them.",
		project, err)
	return false
}

// unusedSecrets opens the credential store this machine is *not* configured to
// use, or nil when there is no second one to speak of.
//
// It exists because settings.noKeyring switches where fft looks without moving
// what is already stored: a machine that falls back to the file store leaves
// every existing project's secrets in the keychain, and a command that says it
// removed a project's credentials has to mean it.
//
// A spec's in-memory store, and headless mode's environment, have no counterpart
// — and headless never reaches the commands that call this, which refuse to
// touch the config file at all.
func (d *Deps) unusedSecrets() secrets.Store {
	if d.unused != nil {
		return d.unused
	}
	switch d.Secrets.Kind() {
	case "keyring":
		path, err := secrets.DefaultFilePath()
		if err != nil {
			return nil
		}
		return secrets.NewFile(path)
	case "file":
		return secrets.NewKeyring()
	default:
		return nil
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
