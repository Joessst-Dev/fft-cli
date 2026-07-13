package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const pingLong = `Check that the tenant is reachable.

This calls GET /api/status, the one endpoint that answers without a token. fft
therefore sends none: ping tests the base URL and the network, and nothing else,
so a green ping with a red 'fft auth whoami' tells you precisely where the
problem is.

Run 'fft auth whoami' to check the credentials.`

// pingView is what `fft ping` renders.
type pingView struct {
	BaseURL string `json:"baseUrl" yaml:"baseUrl"`
	Status  string `json:"status" yaml:"status"`
	Latency string `json:"latency" yaml:"latency"`
}

func newPingCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:         "ping",
		Short:       "Check that the tenant is reachable",
		Long:        pingLong,
		Args:        usageArgs(cobra.NoArgs),
		Annotations: map[string]string{annotationOperationID: "status"},

		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPing(cmd, deps)
		},
	}
}

func runPing(cmd *cobra.Command, deps *Deps) error {
	project, err := deps.ActiveProject()
	if err != nil {
		return err
	}

	// No token source: /api/status needs no credential, and a ping that fails
	// because the *password* is wrong would be a diagnostic that misdiagnoses.
	c, err := deps.apiClient(project, nil)
	if err != nil {
		return err
	}

	ctx, cancel := deps.Context(cmd)
	defer cancel()

	start := deps.Clock()

	// Through Do, like every other call: a ping that gave up on the first dropped
	// connection would report a broken tenant that is merely a flaky café network.
	res, err := client.Fetch[api.Status](ctx, c,
		fmt.Sprintf("reach %s", project.BaseURL),
		func(ctx context.Context) (*http.Response, error) {
			return c.API().Status(ctx)
		})
	if err != nil {
		return err
	}

	status := "OK"
	if res.Status != "" {
		status = string(res.Status)
	}

	view := pingView{
		BaseURL: project.BaseURL,
		Status:  status,
		Latency: deps.Clock().Sub(start).Round(time.Millisecond).String(),
	}

	return deps.Printer.Render(pingRows(view), view)
}

var pingHeaders = []string{"BASE URL", "STATUS", "LATENCY"}

func pingRows(v pingView) output.Rows {
	return output.Rows{
		Headers: pingHeaders,
		Rows:    [][]string{{v.BaseURL, v.Status, v.Latency}},
	}
}
