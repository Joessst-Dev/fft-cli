package main

import (
	"regexp"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/xdg"
)

const networkLong = `Find the gaps in a fulfillment network before an order does.

Orders reach the wrong facility mostly because the connection rules have gaps: a
store no hub reaches, a NOT filter that shuts out a kind of order, a connection to
a facility that has gone offline. Nothing in the API reports those. This group
reads the whole network once into a local snapshot, and works on that:

  fft network snapshot      read every facility and connection into a local file

Everything here only reads from the tenant. Fixing a gap is a change to the
network, and that is its owner's decision, not a linter's.`

func newNetworkCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "network",
		Short: "Snapshot a fulfillment network and find its routing gaps",
		Long:  networkLong,
		Args:  usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newNetworkSnapshotCmd(deps),
	)

	return cmd
}

// unsafeName is everything a project name may hold that a file name should not.
var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// defaultSnapshotPath is where a project's snapshot lives unless --out or
// --snapshot says otherwise: the cache directory, because a snapshot can be
// taken again, and never the working directory, because a snapshot is the
// tenant's whole network and a working directory is one 'git add .' away from
// a public repository.
func defaultSnapshotPath(deps *Deps, project string) (string, error) {
	name := unsafeName.ReplaceAllString(project, "_")
	if name == "" {
		name = "default"
	}
	return xdg.Cache().LookupPath(deps.env(), "network/"+name+".snapshot.json.gz")
}
