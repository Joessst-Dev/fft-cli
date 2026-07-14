package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/client"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
	"github.com/Joessst-Dev/fft-cli/internal/output"
)

const connectionLong = `Manage the connections that leave a facility.

A connection is an edge of the fulfillment graph: an outbound lane from one
facility to a SUPPLIER, to another MANAGED_FACILITY, or to the CUSTOMER. The
routing engine can only source along an edge that exists — no connection means
that fulfillment path is not reachable at all, which makes this the first place
to look when an order routes somewhere surprising, or refuses to route.

A connection belongs to the facility it leaves, so every command needs
--facility as well as the connection's own id:

  fft connection list --facility BER-01
  fft connection get 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10 --facility BER-01

--facility takes your own tenantFacilityId or the platform's UUID.

'fft sourcing simulate' is the other half of this: it shows which of these edges
the router would actually use, and names each one by its id.`

func newConnectionCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "connection",
		Aliases: []string{"connections"},
		Short:   "Manage interfacility connections",
		Long:    connectionLong,
		Args:    usageArgs(cobra.NoArgs),

		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newConnectionListCmd(deps),
		newConnectionGetCmd(deps),
		newConnectionCreateCmd(deps),
		newConnectionUpdateCmd(deps),
		newConnectionDeleteCmd(deps),
	)

	return cmd
}

// The connection types. A connection is one of the three, and the type is what the
// API matches the rest of the body against.
//
// typeSupplier and typeManagedFacility are the facility's own constants: a
// connection's target is a facility (or the consumer), so the vocabulary is
// deliberately the same one. Only CUSTOMER is new — a facility has no such type,
// because the customer is not a facility. It is where the goods stop.
const typeCustomer = "CUSTOMER"

func connectionTypes() []string {
	return []string{typeSupplier, typeManagedFacility, typeCustomer}
}

// connectionFlag is --facility, which every connection command needs: a connection
// has no tenant-wide address. There is no GET /api/connections — the only way to
// reach one is through the facility it leaves.
func registerFacilityFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, "facility", "",
		"The facility the connection leaves, by tenantFacilityId or platform UUID (required)")
}

// getConnection reads one connection, as the API wrote it.
func getConnection(ctx context.Context, c *client.Client, facility, id string) ([]byte, error) {
	res, err := c.Do(ctx, fmt.Sprintf("get connection %s of facility %s", id, facility),
		func(ctx context.Context) (*http.Response, error) {
			return c.API().GetFacilityConnection(ctx, facility, id, &api.GetFacilityConnectionParams{})
		})
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// getConnectionDoc reads one connection and its version together, which is what the
// read-then-write update starts with.
func getConnectionDoc(ctx context.Context, c *client.Client, facility, id string) (entityDoc, int, error) {
	raw, err := getConnection(ctx, c, facility, id)
	if err != nil {
		return nil, 0, err
	}

	doc, err := decodeDoc(raw, "the connection")
	if err != nil {
		return nil, 0, err
	}

	version, err := docVersion(doc, "connection")
	if err != nil {
		return nil, 0, err
	}
	return doc, version, nil
}

// connectionBody reads a connection request body and makes it something the API can
// answer, rather than something it will reject for a reason it does not name.
//
// It also repairs the one asymmetry in this resource's schema. The connection the
// API *returns* has no top-level `type` — the type lives only in target.type. The
// body the API *accepts* requires both, at the top level and inside the target, and
// they must agree, because the top-level one is the discriminator it matches the
// body against.
//
// So the obvious workflow — read it, edit it, send it back —
//
//	fft connection get 3f9c... --facility BER-01 -o json > c.json
//	fft connection update 3f9c... --facility BER-01 --file c.json
//
// produces a body with no discriminator and gets a 400 that does not say which field
// was missing. Filling the top-level type in from target.type is not a guess: the
// target already carries it, the two are required to be equal, and there is no third
// value they could take. It is the round trip working the way the user already
// believes it does.
func connectionBody(deps *Deps, path string) (entityDoc, error) {
	raw, err := readBody(deps, path)
	if err != nil {
		return nil, err
	}

	doc, err := decodeDoc(raw, path)
	if err != nil {
		return nil, exitcode.UsageError{Err: err}
	}

	if err := normaliseConnectionType(doc); err != nil {
		return nil, exitcode.UsageError{Err: fmt.Errorf("%s: %w", path, err)}
	}
	return doc, nil
}

// normaliseConnectionType checks the three things the API's 400 will not tell you,
// and fills in the one it does not have to be told.
func normaliseConnectionType(doc entityDoc) error {
	target, ok := doc["target"].(map[string]any)
	if !ok {
		return fmt.Errorf("the body has no %q object: a connection is an edge, and the API needs to be told where it goes", "target")
	}

	top := docString(doc, "type")
	inner, _ := target["type"].(string)

	switch {
	case top == "" && inner == "":
		return fmt.Errorf("the body has no %q: a connection goes to a %s, and the API needs to be told which",
			"type", strings.Join(connectionTypes(), ", a "))

	// The read model carries the type only inside the target, so this is what a body
	// that came straight out of `fft connection get` looks like. Complete it.
	case top == "":
		top = inner
		doc["type"] = inner

	case inner == "":
		target["type"] = top

	// Both given and disagreeing. The API reads the top-level one and would happily
	// build a connection the file did not describe, so this is refused rather than
	// resolved: fft cannot know which of the two the user meant.
	case top != inner:
		return fmt.Errorf("the body says %q at the top level and %q inside %q: they are the same field twice and must agree",
			top, inner, "target")
	}

	if err := checkConnectionType(top); err != nil {
		return err
	}

	// A supplier or a managed facility that names no facility is an edge to nowhere.
	// The schema does not mark facilityRef required — but the target is the entire
	// point of the target, and the API's answer to one without it is not a sentence
	// anybody can act on.
	if top != typeCustomer {
		if ref, _ := target["facilityRef"].(string); strings.TrimSpace(ref) == "" {
			return fmt.Errorf("a %s connection needs %q inside %q: it is the facility the edge goes to",
				top, "facilityRef", "target")
		}
	}
	return nil
}

// checkConnectionType refuses a type the API does not have, naming the three that
// work.
func checkConnectionType(v string) error {
	switch v {
	case typeSupplier, typeManagedFacility, typeCustomer:
		return nil
	default:
		return fmt.Errorf("unknown connection type %q: want one of %s", v, strings.Join(connectionTypes(), ", "))
	}
}

// connectionIdentity is who a connection is: enough to recognise it in a question
// that is about to destroy it.
//
// "Delete connection 3f9c1e77-2b4a-4f0e-9d61-8a2c5b7e4d10?" is not a question
// anybody can answer safely. "Delete the SUPPLIER connection to FRA-02 via DHL_V2?"
// is — and it is the same reasoning as facilityIdentity.
type connectionIdentity struct {
	Type    string
	Target  string
	Carrier string
	ID      string
}

func (c connectionIdentity) String() string {
	var b strings.Builder

	if c.Type != "" {
		fmt.Fprintf(&b, "the %s connection ", c.Type)
	} else {
		b.WriteString("the connection ")
	}

	if c.Target != "" {
		fmt.Fprintf(&b, "to %s ", c.Target)
	}
	if c.Carrier != "" {
		fmt.Fprintf(&b, "via %s ", c.Carrier)
	}

	fmt.Fprintf(&b, "(%s)", c.ID)
	return b.String()
}

// lookupConnection reads a connection so the prompt can name it. It costs one GET
// before a delete, and it buys a question the user can actually answer.
func lookupConnection(ctx context.Context, c *client.Client, facility, id string) (connectionIdentity, error) {
	raw, err := getConnection(ctx, c, facility, id)
	if err != nil {
		return connectionIdentity{}, err
	}

	var v connectionView
	if err := json.Unmarshal(raw, &v); err != nil {
		return connectionIdentity{}, fmt.Errorf("decode connection %s: %w", id, err)
	}

	return connectionIdentity{
		Type:    v.Target.Type,
		Target:  v.Target.FacilityRef,
		Carrier: v.CarrierKey,
		ID:      id,
	}, nil
}

// connectionView is the table's model of a connection: the handful of fields a human
// scanning a list actually reads.
//
// It is hand-written because the generated model cannot be used at all. oapi-codegen
// collapses this schema's allOf-with-siblings into `type InterFacilityConnection =
// VersionedResource` — every field but created, lastModified and version is simply
// gone — and it flattens the target union down to its discriminator, so the generated
// target cannot even express the facility it points at. See the note on entityDoc.
type connectionView struct {
	ID                string `json:"id"`
	SourceFacilityRef string `json:"sourceFacilityRef"`
	CarrierKey        string `json:"carrierKey"`
	Version           int64  `json:"version"`

	// Target is where the edge goes. The *type* of the connection lives here and
	// nowhere else on a connection the API returned — the top-level `type` exists only
	// on the bodies fft sends. See connectionBody.
	Target struct {
		Type        string `json:"type"`
		FacilityRef string `json:"facilityRef"`
	} `json:"target"`

	// FallbackTransitTime is absent on a connection that has none, which is why the
	// table falls back to a dash rather than rendering "0–0 d" and calling it same-day.
	FallbackTransitTime *struct {
		MinTransitDays float64 `json:"minTransitDays"`
		MaxTransitDays float64 `json:"maxTransitDays"`
	} `json:"fallbackTransitTime"`

	// Context scopes when the connection applies. The table shows how many rules there
	// are, not what they say: a reader scanning a list wants to know that this edge is
	// conditional, and `fft connection get` is where they find out how.
	Context []json.RawMessage `json:"context"`
}

// connectionList is the view `fft connection list` renders.
func connectionList() listView {
	return listView{Noun: "connections", Rows: connectionRows}
}

// renderConnection renders one connection: the API's own JSON under -o json, a
// one-row table otherwise.
func renderConnection(deps *Deps, raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	rows, err := connectionRows(deps.Printer.Style(), []json.RawMessage{raw})
	if err != nil {
		return err
	}
	return deps.Printer.RenderRaw(rows, raw)
}

var connectionHeaders = []string{"ID", "TYPE", "TARGET", "CARRIER", "TRANSIT", "CONTEXT", "VERSION"}

func connectionRows(style output.Style, items []json.RawMessage) (output.Rows, error) {
	rows := make([][]string, 0, len(items))

	for i, item := range items {
		var v connectionView
		if err := json.Unmarshal(item, &v); err != nil {
			return output.Rows{}, fmt.Errorf("decode connection %d of %d: %w", i+1, len(items), err)
		}

		rows = append(rows, []string{
			field(style, v.ID),
			field(style, v.Target.Type),
			connectionTargetCell(style, v.Target.Type, v.Target.FacilityRef),
			field(style, v.CarrierKey),
			field(style, transitCell(v.FallbackTransitTime)),
			field(style, contextCell(v.Context)),
			fmt.Sprintf("%d", v.Version),
		})
	}
	return output.Rows{Headers: connectionHeaders, Rows: rows}, nil
}

// connectionTargetCell names where the edge goes.
//
// A CUSTOMER target has no facilityRef and is not supposed to: the consumer is not a
// facility. So the cell says so, rather than showing the dash that means "the API did
// not send one" — here, there was nothing to send.
func connectionTargetCell(style output.Style, typ, ref string) string {
	if typ == typeCustomer && ref == "" {
		return style.Faint("(the customer)")
	}
	return field(style, ref)
}

// transitCell renders the fallback transit time as a range of days.
func transitCell(t *struct {
	MinTransitDays float64 `json:"minTransitDays"`
	MaxTransitDays float64 `json:"maxTransitDays"`
}) string {
	if t == nil {
		return ""
	}
	if t.MinTransitDays == t.MaxTransitDays {
		return fmt.Sprintf("%g d", t.MinTransitDays)
	}
	return fmt.Sprintf("%g–%g d", t.MinTransitDays, t.MaxTransitDays)
}

// contextCell counts the rules that scope this connection. An unconditional edge has
// none, and an empty cell would read as a rendering bug rather than as "this one
// always applies".
func contextCell(rules []json.RawMessage) string {
	if len(rules) == 0 {
		return ""
	}
	if len(rules) == 1 {
		return "1 rule"
	}
	return fmt.Sprintf("%d rules", len(rules))
}
