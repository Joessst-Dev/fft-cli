package main

import (
	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/skill"
)

// reportSkill mentions, on stderr, an installed skill that describes a different
// fft from the one that just ran.
//
// After `brew upgrade fft` the skill on disk still describes the old CLI, and
// nothing about it looks wrong — an agent reading it will run a renamed flag with
// complete confidence. Neither the user nor the agent has a reason to check, so
// fft says so, once per command, until it is fixed.
//
// It is gated exactly as the update notice is — release builds, table output, a
// terminal on stderr, FFT_NO_UPDATE_CHECK and settings.updateCheck — because it is
// the same kind of message, about the same kind of thing, and a user who has
// turned one off has said what they think of both. It costs two small file reads
// and no network, and any failure is silent: this is a chore, not a command.
func (d *Deps) reportSkill(cmd *cobra.Command) {
	// The skill commands are excluded on top of the update notice's own list:
	// they report the skill's state themselves, and `fft skill install` is the
	// fix the notice would be recommending in the middle of being run.
	if topCommand(cmd).Name() == "skill" || !d.updateAllowed(cmd) {
		return
	}

	for _, loc := range defaultSkillLocations() {
		if loc.err != nil {
			continue
		}
		meta, ok, err := skill.Installed(loc.root)
		if err != nil || !ok || skill.Current(meta) {
			continue
		}
		d.Printer.Notef("%s", staleSkill(skillDir(loc.root), meta.Version(), loc.fix))
	}
}
