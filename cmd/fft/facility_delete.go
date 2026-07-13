package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/prompt"
)

const facilityDeleteLong = `Delete a facility.

This cannot be undone, so fft asks first. -y/--yes answers for you.

On a non-interactive terminal — a CI job, a pipe — there is nobody to ask, and
fft refuses rather than assuming yes. Pass --yes if you mean it.`

func newFacilityDeleteCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <id>",
		Short:   "Delete a facility",
		Long:    facilityDeleteLong,
		Args:    usageArgs(cobra.ExactArgs(1)),
		Aliases: []string{"rm"},

		Annotations: map[string]string{annotationOperationID: "deleteFacility"},

		RunE: func(cmd *cobra.Command, args []string) error {
			ref := client.FacilityRef(args[0])

			ok, err := confirmDestructive(deps, fmt.Sprintf("Delete facility %s? This cannot be undone.", ref))
			if err != nil {
				return err
			}
			if !ok {
				deps.Printer.Notef("Aborted; %s was not deleted.", ref)
				return nil
			}

			c, err := tenantClient(deps)
			if err != nil {
				return err
			}

			ctx, cancel := deps.Context(cmd)
			defer cancel()

			// A DELETE is idempotent, so Do may safely retry it on a dropped
			// connection — unlike the create, which it may not.
			if _, err := c.Do(ctx, "delete facility "+ref, func(ctx context.Context) (*http.Response, error) {
				return c.API().DeleteFacility(ctx, ref, &api.DeleteFacilityParams{})
			}); err != nil {
				return err
			}

			// Nothing on stdout: a delete has no data to emit, and printing a cheerful
			// sentence there would land in the pipe of anyone who scripted it.
			deps.Printer.Notef("Deleted facility %s.", ref)
			return nil
		},
	}
}

// confirmDestructive asks before something irreversible, unless -y/--yes already
// answered.
//
// On a non-interactive stdin it refuses instead of proceeding. A prompt nobody
// can see is not consent, and defaulting to yes is how a pipeline deletes a
// production facility at 3am.
func confirmDestructive(deps *Deps, question string) (bool, error) {
	if deps.AssumeYes {
		return true, nil
	}

	if !deps.Prompt.Interactive() {
		return false, exitcode.UsageError{Err: fmt.Errorf(
			"%w: pass --yes to confirm this without being asked", prompt.ErrNotInteractive)}
	}

	return deps.Prompt.Confirm(question)
}
