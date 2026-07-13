package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

// The two actions POST /api/facilities/{id}/actions accepts.
//
// That endpoint is not a state machine, despite the name. Its body is an anyOf of
// exactly these two parameters, and both are about coordinates — so fft exposes
// two narrow commands rather than a generic `fft facility action` that would
// invite users to look for transitions that do not exist.
const (
	actionUpdateCoordinates = "UPDATE_FACILITY_COORDINATES"
	actionRemoveCoordinates = "REMOVE_FACILITY_COORDINATES"
)

const facilityCoordinatesLong = `Set or remove a facility's geographic coordinates.

These are the only two things POST /api/facilities/{id}/actions can do — it is
not a state machine, and there is no transition hiding behind it. To change a
facility's state, use 'fft facility patch --status'.

Coordinates carry a version like any other mutation, so fft reads the facility
first unless --if-version tells it what the version is.`

func newFacilityCoordinatesCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "coordinates",
		Aliases: []string{"coords"},
		Short:   "Set or remove a facility's coordinates",
		Long:    facilityCoordinatesLong,
		Args:    usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newFacilityCoordinatesSetCmd(deps),
		newFacilityCoordinatesRemoveCmd(deps),
	)

	return cmd
}

func newFacilityCoordinatesSetCmd(deps *Deps) *cobra.Command {
	var (
		lat, lon float64
		version  versionFlag
	)

	cmd := &cobra.Command{
		Use:   "set <id> --lat <latitude> --lon <longitude>",
		Short: "Set a facility's coordinates",
		Long: `Set a facility's geographic coordinates.

  fft facility coordinates set BER-01 --lat 52.5219 --lon 13.4132

Latitude runs from -90 to 90 and longitude from -180 to 180; fft checks that
before sending, because the API's complaint about a swapped pair is not obvious.`,
		Args: usageArgs(cobra.ExactArgs(1)),

		Annotations: map[string]string{annotationOperationID: "facilityAction"},

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(cmd, "lat"); err != nil {
				return err
			}
			if err := requireFlag(cmd, "lon"); err != nil {
				return err
			}
			if err := checkCoordinates(lat, lon); err != nil {
				return err
			}

			// The generated api.UpdateFacilityCoordinatesActionParameter has no
			// coordinates field: the swagger declares the schema as an allOf with
			// sibling properties, and oapi-codegen collapses that to a bare alias of
			// the abstract parent. So the body is built here, from the schema at
			// swagger:84033, rather than from a model that would silently drop the one
			// field the action is about.
			body := entityDoc{
				"name":        actionUpdateCoordinates,
				"coordinates": map[string]float64{"lat": lat, "lon": lon},
			}

			return runFacilityAction(cmd, deps, args[0], body, &version,
				fmt.Sprintf("Set the coordinates of %%s to %g, %g.", lat, lon))
		},
	}

	f := cmd.Flags()
	f.Float64Var(&lat, "lat", 0, "Latitude, -90 to 90")
	f.Float64Var(&lon, "lon", 0, "Longitude, -180 to 180")
	version.register(f)

	return cmd
}

func newFacilityCoordinatesRemoveCmd(deps *Deps) *cobra.Command {
	var version versionFlag

	cmd := &cobra.Command{
		Use:     "remove <id>",
		Short:   "Remove a facility's coordinates",
		Args:    usageArgs(cobra.ExactArgs(1)),
		Aliases: []string{"rm", "unset"},

		Annotations: map[string]string{annotationOperationID: "facilityAction"},

		RunE: func(cmd *cobra.Command, args []string) error {
			body := entityDoc{"name": actionRemoveCoordinates}
			return runFacilityAction(cmd, deps, args[0], body, &version, "Removed the coordinates of %s.")
		},
	}

	version.register(cmd.Flags())

	return cmd
}

// runFacilityAction posts an action, supplying the version the API requires.
//
// The action body is the "entity" here: [client.UpdateVersioned] reads the
// facility only to learn its version, then hands the body to the POST with that
// version filled in. On a 409 it reads the version again and retries once, which
// is exactly the behaviour the coordinates action needs and none of the code it
// would have taken to write twice.
func runFacilityAction(cmd *cobra.Command, deps *Deps, id string, body entityDoc,
	version *versionFlag, notice string,
) error {
	if err := version.check(); err != nil {
		return err
	}

	c, err := tenantClient(deps)
	if err != nil {
		return err
	}

	ctx, cancel := deps.Context(cmd)
	defer cancel()

	ref := client.FacilityRef(id)
	var raw []byte

	// Only the version is wanted from the facility; the action body replaces it
	// entirely, so an empty document is the honest thing to carry forward.
	get := func(ctx context.Context) (entityDoc, int, error) {
		_, v, err := getFacilityDoc(ctx, c, ref)
		if err != nil {
			return nil, 0, err
		}
		return entityDoc{}, v, nil
	}

	post := func(ctx context.Context, doc entityDoc, v int) (entityDoc, error) {
		doc["version"] = v

		answer, err := sendEntity(ctx, c, "run the action on facility "+ref, doc,
			func(ctx context.Context, body io.Reader) (*http.Response, error) {
				return c.API().FacilityActionWithBody(ctx, ref, contentTypeJSON, body)
			})
		if err != nil {
			return nil, err
		}
		raw = answer
		return nil, nil
	}

	action := func(doc *entityDoc) error {
		*doc = body
		return nil
	}

	if _, err := client.UpdateVersioned(ctx, get, post, action, version.value()); err != nil {
		return err
	}

	deps.Printer.Notef(notice, ref)
	return renderFacility(deps, raw)
}

// checkCoordinates refuses a point that is not on the planet. The commonest
// mistake is a swapped pair, and a longitude of 52.5 in the latitude slot is
// perfectly valid — so this catches the half of that mistake it can.
//
// NaN and Inf are checked first, and explicitly: pflag parses both, and every
// comparison against NaN is false, so a range check alone waves them through —
// to die later inside json.Marshal as an unclassified "unsupported value".
func checkCoordinates(lat, lon float64) error {
	for _, c := range []struct {
		flag  string
		value float64
		limit float64
	}{
		{"lat", lat, 90},
		{"lon", lon, 180},
	} {
		switch {
		case math.IsNaN(c.value) || math.IsInf(c.value, 0):
			return exitcode.UsageError{Err: fmt.Errorf("--%s must be a number, and %g is not", c.flag, c.value)}
		case c.value < -c.limit || c.value > c.limit:
			return exitcode.UsageError{Err: fmt.Errorf("--%s must be between -%g and %g, and %g is not",
				c.flag, c.limit, c.limit, c.value)}
		}
	}
	return nil
}
