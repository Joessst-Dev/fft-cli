package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

const orderUnlockLong = `Unlock a locked order.

This is the UNLOCK order action: it releases an order the platform has put in the
LOCKED state so it can be sourced and routed again. It is versioned like every
write — fft reads the order to learn its version, sends that back, and retries once
on a 409.

--target-time optionally sets a delivery targetTime while unlocking.

  fft order unlock 8f14e45f-ceea-467a-9575-25a1b5c8b3a1
  fft order unlock 8f14e45f-ceea-467a-9575-25a1b5c8b3a1 --target-time 2026-07-20T12:00:00Z`

func newOrderUnlockCmd(deps *Deps) *cobra.Command {
	var (
		targetTime string
		version    versionFlag
	)

	cmd := &cobra.Command{
		Use:   "unlock <id>",
		Short: "Unlock a locked order",
		Long:  orderUnlockLong,
		Args:  usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "orderAction"},

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := version.check(); err != nil {
				return err
			}

			id := args[0]

			extra := entityDoc{}
			if targetTime != "" {
				extra["targetTime"] = targetTime
			}

			return runOrderAction(cmd, deps, id, "UNLOCK", extra, version.value(),
				fmt.Sprintf("Unlocked order %s.", id))
		},
	}

	f := cmd.Flags()
	f.StringVar(&targetTime, "target-time", "", "Set this delivery targetTime while unlocking (RFC 3339)")
	version.register(f)

	return cmd
}
