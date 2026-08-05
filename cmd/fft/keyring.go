package main

import (
	"errors"

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
	return config.NewError(err, keyringHint(runningOnWSL()))
}

// keyringHint names the ways out, in the order a user should consider them: the
// flag for right now, the setting for every run after, and the keychain itself
// for anyone who would rather not store a password in cleartext at all.
func keyringHint(wsl bool) string {
	const permanent = `Pass --no-keyring (or set FFT_NO_KEYRING=1) to store credentials in a 0600 file
instead, or add "noKeyring: true" under "settings:" in ~/.config/fft/config.yaml to
make that permanent — that file holds your password and refresh token in cleartext.`

	if wsl {
		return "WSL ships no Secret Service, so there is no keychain for fft to use.\n\n" +
			permanent + "\nTo keep a real keychain, install gnome-keyring and run fft inside a session D-Bus."
	}
	return permanent + "\nTo keep a real keychain, install a Secret Service (gnome-keyring, kwallet) and\n" +
		"run fft inside a session D-Bus."
}

// offerFileStore asks whether to fall back to the 0600 file, and switches the
// store over if the answer is yes. interactive is the caller's, not the
// prompter's: `project add --password-stdin` has a terminal but no stdin left to
// read an answer from.
//
// --yes deliberately does not answer this one, unlike every other confirmation in
// the tree. -y is consent to the questions a command was always going to ask;
// downgrading credential storage to cleartext on the strength of a blanket flag
// is exactly the accident the explicit opt-in exists to prevent. A provisioning
// script that wants the file store passes --no-keyring, which says so.
func offerFileStore(deps *Deps, interactive bool) (bool, error) {
	if !interactive {
		return false, nil
	}

	confirmed, err := deps.Prompt.Confirm(
		"No OS keychain is available. Store credentials in a 0600 file, in cleartext, instead?")
	if err != nil || !confirmed {
		return false, err
	}

	if err := deps.reopenSecrets(); err != nil {
		return false, err
	}
	return true, nil
}
