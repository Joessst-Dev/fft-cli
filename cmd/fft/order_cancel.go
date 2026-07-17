package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const orderCancelLong = `Cancel an order.

This is the CANCEL order action. It is versioned like every write: fft reads the
order to learn its version, sends that back, and retries once on a 409. Pass
--reason-id to record a configured cancelation reason.

--force sends FORCE_CANCEL instead, which cancels an order past the point normal
cancellation allows. It only works if the tenant has enabled forced cancellation;
otherwise the API refuses it. FORCE_CANCEL takes no reason, so --force and
--reason-id cannot be combined.

Cancelling cannot be undone, so fft asks first. -y/--yes answers for you; on a
non-interactive terminal fft refuses rather than assuming yes.

  fft order cancel 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 --reason-id out-of-stock
  fft order cancel 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 --force --yes`

func newOrderCancelCmd(deps *Deps) *cobra.Command {
	var (
		force    bool
		reasonID string
		version  versionFlag
	)

	cmd := &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel an order",
		Long:  orderCancelLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "orderAction"},

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := version.check(); err != nil {
				return err
			}

			// FORCE_CANCEL has no cancelationReasonId field, so a reason given with
			// --force would be silently dropped or rejected. Say so instead.
			if force && reasonID != "" {
				return exitcode.UsageError{Err: fmt.Errorf(
					"--reason-id cannot be combined with --force: FORCE_CANCEL takes no cancelation reason")}
			}

			id := args[0]

			name := "CANCEL"
			if force {
				name = "FORCE_CANCEL"
			}

			ok, err := confirmDestructive(deps, fmt.Sprintf("Cancel order %s? This cannot be undone.", id))
			if err != nil {
				return err
			}
			if !ok {
				deps.Printer.Notef("Aborted; %s was not cancelled.", id)
				return nil
			}

			extra := entityDoc{}
			if reasonID != "" {
				extra["cancelationReasonId"] = reasonID
			}

			return runOrderAction(cmd, deps, id, name, extra, version.value(),
				fmt.Sprintf("Cancelled order %s.", id))
		},
	}

	f := cmd.Flags()
	f.BoolVar(&force, "force", false,
		"Force-cancel past the point normal cancellation allows (needs the tenant to permit it)")
	f.StringVar(&reasonID, "reason-id", "", "The id of a configured cancelation reason")
	version.register(f)

	return cmd
}
