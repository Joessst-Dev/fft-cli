package main

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/network"
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

// defaultSnapshotPath is where a tenant's snapshot lives unless --out or
// --snapshot says otherwise: the cache directory, because a snapshot can be
// taken again, and never the working directory, because a snapshot is the
// tenant's whole network and a working directory is one 'git add .' away from
// a public repository.
//
// The name is injective in the tenant. The readable slug alone is lossy ("acme
// prod" and "acme/prod" collapse), and every headless run is called "env"
// whatever FFT_BASE_URL says, so two tenants would share one file and the
// second crawl would truncate the first's journal and overwrite its snapshot.
// The hash covers exactly what Source holds, so a name differs whenever the
// tenant does.
func defaultSnapshotPath(deps *Deps, src network.Source) (string, error) {
	slug := unsafeName.ReplaceAllString(src.Project, "_")
	if slug == "" {
		slug = "default"
	}
	sum := sha256.Sum256([]byte(src.Project + "\x00" + src.BaseURL))
	return xdg.Cache().LookupPath(deps.env(), "network/"+slug+"-"+hex.EncodeToString(sum[:4])+".snapshot.json.gz")
}
