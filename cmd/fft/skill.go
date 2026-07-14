package main

import (
	"github.com/spf13/cobra"
)

const skillLong = `Install the fft agent skill, so an AI coding assistant can drive fft.

A skill is documentation an agent loads when it needs it: what the commands are, how
to discover the API's 557 operations, how to read an exit code — and the things no
--help can teach it, like the fact that stdout is data, that a POST is not
necessarily a write, and that it must ask before it changes anything.

  fft skill install              # ~/.claude/skills/fft   (Claude Code, personal)
  fft skill install --local      # ./.claude/skills/fft   (this project only)
  fft skill show >> AGENTS.md    # any other agent

The skill ships inside this binary, so it is always the one that describes the
commands you actually have. Every fft invocation in it is resolved against the real
command tree by a spec, so a renamed flag fails fft's build rather than quietly
making the skill lie to your agent.`

func newSkillCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the agent skill that teaches an AI to use fft",
		Long:  skillLong,
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newSkillInstallCmd(deps),
		newSkillShowCmd(deps),
	)

	return cmd
}
