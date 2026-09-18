package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Joessst-Dev/fft-cli/internal/output"
)

// tableBuilder turns what a curated command printed under -o json back into the
// rows of the table the same command prints in a shell.
type tableBuilder func(style output.Style, stdout []byte) (output.Rows, error)

// commandTables are the curated commands whose table the TUI can show next to the
// JSON, keyed by command path without the program name. Each entry uses the row
// builder its command renders with, so the two tables cannot drift apart; a
// command missing here only lacks the tab.
var commandTables = map[string]tableBuilder{
	"facility list":               entityTable(facilityRows),
	"facility search":             entityTable(facilityRows),
	"facility get":                entityTable(facilityRows),
	"facility create":             entityTable(facilityRows),
	"facility update":             entityTable(facilityRows),
	"facility patch":              entityTable(facilityRows),
	"facility coordinates set":    entityTable(facilityRows),
	"facility coordinates remove": entityTable(facilityRows),

	"connection list":   entityTable(connectionRows),
	"connection get":    entityTable(connectionRows),
	"connection create": entityTable(connectionRows),
	"connection update": entityTable(connectionRows),

	"listing list":   entityTable(listingRows),
	"listing search": entityTable(listingRows),
	"listing get":    entityTable(listingRows),
	"listing patch":  entityTable(listingRows),

	"order list":   entityTable(orderRows),
	"order search": entityTable(orderRows),
	"order get":    entityTable(orderRows),
	"order create": entityTable(orderRows),
	"order update": entityTable(orderRows),
	"order cancel": entityTable(orderRows),
	"order unlock": entityTable(orderRows),

	"stock list":   entityTable(stockRows),
	"stock search": entityTable(stockRows),
	"stock get":    entityTable(stockRows),
	"stock create": entityTable(stockRows),
	"stock update": entityTable(stockRows),

	"routing category list":   entityTable(routingCategoryRows),
	"routing category get":    entityTable(routingCategoryRows),
	"routing category create": entityTable(routingCategoryRows),
	"routing category update": entityTable(routingCategoryRows),

	"routing strategy list":     entityTable(routingStrategyRows),
	"routing strategy get":      entityTable(routingStrategyRows),
	"routing strategy create":   entityTable(routingStrategyRows),
	"routing strategy update":   entityTable(routingStrategyRows),
	"routing strategy activate": entityTable(routingStrategyRows),
	"routing strategy actions":  entityTable(routingStrategyRows),

	"routing decision-logs": entityTable(routingDecisionLogRows),

	"sourcing get":      sourcingTable,
	"sourcing simulate": sourcingTable,
}

// entityTable builds the table of a command that prints one entity as an object,
// or a list of them as an array. Some single-entity writes answer with an array of
// one, so the shape is read off the document rather than assumed.
func entityTable(rows func(output.Style, []json.RawMessage) (output.Rows, error)) tableBuilder {
	return func(style output.Style, stdout []byte) (output.Rows, error) {
		body := bytes.TrimSpace(stdout)
		if len(body) == 0 {
			return output.Rows{}, nil
		}
		if body[0] != '[' {
			return rows(style, []json.RawMessage{body})
		}
		// A fresh slice: decoding a json.RawMessage over one that holds body would
		// write into stdout's own bytes, which the JSON tab shows. See renderListing.
		var items []json.RawMessage
		if err := json.Unmarshal(body, &items); err != nil {
			return output.Rows{}, fmt.Errorf("decode the list: %w", err)
		}
		return rows(style, items)
	}
}

// sourcingTable builds the options table of a sourcing run, best option first,
// exactly as [renderSourcing] orders it.
func sourcingTable(style output.Style, stdout []byte) (output.Rows, error) {
	var res sourcingResponse
	if err := json.Unmarshal(stdout, &res); err != nil {
		return output.Rows{}, fmt.Errorf("decode the sourcing options: %w", err)
	}
	return sourcingRows(style, rankedOptions(res.Result.Options))
}

// renderTable renders stdout as path's table, uncoloured: the UI styles its own
// frame, and a cell's colour would be one more escape sequence to account for.
// It returns "" when the table has no rows, which is also what the command prints.
func renderTable(path []string, stdout []byte) (string, error) {
	build, ok := commandTables[strings.Join(path, " ")]
	if !ok {
		return "", fmt.Errorf("fft %s has no table", strings.Join(path, " "))
	}
	var out bytes.Buffer
	printer := output.New(&out, io.Discard, output.Table, false)
	rows, err := build(printer.Style(), stdout)
	if err != nil {
		return "", err
	}
	if err := printer.RenderRaw(rows, stdout); err != nil {
		return "", err
	}
	return out.String(), nil
}
