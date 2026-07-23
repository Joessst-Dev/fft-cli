package main

import (
	"github.com/Joessst-Dev/fft-cli/internal/config"
	"github.com/Joessst-Dev/fft-cli/internal/secrets"
)

// migrateAPIKeys moves every project's Firebase API key out of the plaintext
// config file and into the secret store, the one-time upgrade from a pre-v2
// config. It runs at startup for every command, and is a no-op once done.
//
// It is fail-soft: a key the store refuses (a locked or unavailable keychain) is
// left in the config file with a warning, so the next run retries and a wedged
// keychain never bricks the CLI. Set is idempotent, so a retry after a
// half-finished migration re-writes the same value harmlessly.
//
// Headless runs never reach here — [Deps.complete] guards the call on a
// non-ephemeral project, because a CI job's env store is read-only and its key
// comes from FFT_FIREBASE_API_KEY, not the config file.
func (d *Deps) migrateAPIKeys() error {
	cfg, err := d.LoadConfig()
	if err != nil {
		return err
	}

	migrated := false
	cleartextRemains := false
	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		if p.LegacyFirebaseAPIKey == "" {
			continue
		}

		if err := d.Secrets.Set(secrets.Key(p.Name, secrets.KindAPIKey), p.LegacyFirebaseAPIKey); err != nil {
			d.Printer.Notef("Could not move the API key for %q into the credential store (%v); it stays in the config file for now.", p.Name, err)
			cleartextRemains = true
			continue
		}
		// Hydrate the in-memory copy so this same process can use the project, and
		// clear the plaintext field so the save below drops it.
		p.FirebaseAPIKey = p.LegacyFirebaseAPIKey
		p.LegacyFirebaseAPIKey = ""
		migrated = true
	}

	if !migrated {
		return nil
	}
	// Stamp the new schema version — but only once no plaintext key is left on
	// disk. The key has left the file, so an older build that no longer
	// understands the layout should be turned away by Load's guard rather than
	// sign in with an empty key. If some project's write failed, its cleartext key
	// gets written straight back below, so the file must stay at its old version:
	// a v2 stamp on a file that still holds a plaintext key would be a lie, and the
	// next run (which keys off LegacyFirebaseAPIKey, not the version) retries it.
	// Save only stamps a zero version, so a loaded v1 config needs this explicitly.
	if !cleartextRemains {
		cfg.Version = config.Version
	}
	if err := d.SaveConfig(cfg); err != nil {
		// The keys are already in the store; only the plaintext cleanup and the
		// version stamp failed. Warn and move on — the next run retries the save.
		d.Printer.Notef("Moved the API key into the credential store but could not rewrite the config file (%v); it will be cleaned up on the next run.", err)
	}
	return nil
}
