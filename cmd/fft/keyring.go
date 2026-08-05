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
		question += fmt.Sprintf(
			"\nThis setting is machine-wide: the %d %s already configured will read from that file too.",
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
