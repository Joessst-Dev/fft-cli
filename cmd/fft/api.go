package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Joessst-Dev/fft-cli/internal/api"
	"github.com/Joessst-Dev/fft-cli/internal/exitcode"
)

const apiLong = `Call any operation of the fulfillmenttools API by its operationId.

This is the escape hatch. fft curates the entities it knows well (facility,
listing, stock) and generates a command for every other operation, but when the
spec moves faster than fft does, this reaches whatever the spec says exists —
all 557 operations of it.

  fft api list --tag picking              # what is there?
  fft api describe getPickJob             # what does it take?
  fft api getPickJob --param pickJobId=abc123
  fft api queryPickJobs --query status=OPEN --query size=10
  fft api addPickJob --example > job.json && fft api addPickJob --file job.json

--param fills the path (/api/pickjobs/{pickJobId}); --query fills the query
string; --header adds a header. A missing required parameter is a usage error and
nothing is sent.

Array query parameters are encoded the way the spec says *that* parameter is
encoded — comma-joined for some, repeated for others. Pass several values either
as --query status=A,B or by repeating --query; both come out right.

The answer is printed as the API sent it. -o table has nothing to render an
arbitrary operation into, so it prints JSON too; -o yaml converts it.`

func newAPICmd(deps *Deps) *cobra.Command {
	var (
		params  []string
		query   []string
		headers []string
		file    string
		data    string
		example bool
	)

	cmd := &cobra.Command{
		Use:   "api <operationId> [flags]",
		Short: "Call any API operation by its operationId",
		Long:  apiLong,

		// One argument, the operationId. `fft api list` and `fft api describe` are
		// subcommands, and cobra reaches them before this ever runs.
		Args: usageArgs(cobra.MaximumNArgs(1)),

		// The operationId is not a subcommand, so completing it has to be done here.
		ValidArgsFunction: completeOperationID,

		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}

			op, err := findOperation(args[0])
			if err != nil {
				return err
			}

			if example {
				return printExample(cmd, op)
			}

			// `fft api` is the one command whose operation is an argument rather than
			// an annotation, so the gate in root.go cannot see it and it gates itself.
			// Here, before the body is read: `fft api addPickJob --file -` against a
			// read-only project should refuse, not sit blocking on a stdin whose
			// contents it was never going to send.
			if err := deps.guardOperation(cmd, op); err != nil {
				return err
			}

			path, err := pairs("param", params)
			if err != nil {
				return err
			}
			header, err := pairs("header", headers)
			if err != nil {
				return err
			}
			values, err := multiPairs("query", query)
			if err != nil {
				return err
			}

			body, err := requestBody(deps, file, data)
			if err != nil {
				return err
			}

			in := opInput{Path: path, Query: values, Header: header, Body: body}

			// The request is built — and every check in it run — before the client is
			// constructed, so a usage error costs no sign-in and sends no request.
			req, err := buildRequest(op, in, deps.Printer.Warnf)
			if err != nil {
				return err
			}
			return runOperation(cmd, deps, op, req)
		},
	}

	f := cmd.Flags()
	f.StringArrayVar(&params, "param", nil, "Path parameter, as name=value (repeatable)")
	f.StringArrayVar(&query, "query", nil, "Query parameter, as name=value (repeatable; name=a,b for a list)")
	f.StringArrayVar(&headers, "header", nil, "Request header, as name=value (repeatable)")
	f.StringVar(&file, "file", "", "JSON file holding the request body ('-' for stdin)")
	f.StringVar(&data, "data", "", "Request body: inline JSON, @file, or '-' for stdin")
	f.BoolVar(&example, "example", false, "Print a sample request body and exit")

	cmd.MarkFlagsMutuallyExclusive("file", "data")
	cmd.MarkFlagsMutuallyExclusive("file", "example")
	cmd.MarkFlagsMutuallyExclusive("data", "example")

	cmd.AddCommand(newAPIListCmd(deps), newAPIDescribeCmd(deps))
	return cmd
}

// findOperation resolves an operationId, or says what the user probably meant.
//
// A typo is exit 2, not a 404: the operation does not exist in the spec, so nothing
// was ever going to be sent. The suggestion is the point — there are 557 ids and
// nobody remembers whether it is getPickJob or getPickjob.
func findOperation(id string) (api.Operation, error) {
	op, ok := api.LookupOperation(strings.TrimSpace(id))
	if ok {
		return op, nil
	}

	err := fmt.Errorf("there is no operation %q", id)
	if suggestions := api.SuggestOperations(id); len(suggestions) > 0 {
		err = fmt.Errorf("%w. Did you mean %s?", err, strings.Join(suggestions, ", "))
	} else {
		err = fmt.Errorf("%w. Run 'fft api list' to see what there is", err)
	}
	return api.Operation{}, exitcode.UsageError{Err: err}
}

// pairs parses repeated name=value flags into a map. A name given twice is a
// mistake worth naming: silently keeping one of the two values is how a user's
// second thought disappears.
func pairs(flag string, args []string) (map[string]string, error) {
	out := make(map[string]string, len(args))

	for _, arg := range args {
		name, value, err := pair(flag, arg)
		if err != nil {
			return nil, err
		}
		if _, dup := out[name]; dup {
			return nil, exitcode.UsageError{Err: fmt.Errorf(
				"--%s %s is given twice", flag, name)}
		}
		out[name] = value
	}
	return out, nil
}

// multiPairs parses repeated name=value flags, keeping every value: a query
// parameter may legitimately be a list.
func multiPairs(flag string, args []string) (map[string][]string, error) {
	out := make(map[string][]string, len(args))

	for _, arg := range args {
		name, value, err := pair(flag, arg)
		if err != nil {
			return nil, err
		}
		out[name] = append(out[name], value)
	}
	return out, nil
}

// pair splits one name=value. The value may contain '=' — a filter on a base64 id
// does — so only the first one separates.
func pair(flag, arg string) (name, value string, err error) {
	name, value, found := strings.Cut(arg, "=")

	name = strings.TrimSpace(name)
	if !found || name == "" {
		return "", "", exitcode.UsageError{Err: fmt.Errorf(
			"--%s wants name=value, not %q", flag, arg)}
	}
	return name, value, nil
}

// requestBody reads the body from --file or --data, or returns nil when neither was
// given.
//
// --data is the curl spelling and takes all three forms a user will try: inline
// JSON, @file, and '-' for stdin. --file is fft's own, and takes a path (or '-').
func requestBody(deps *Deps, file, data string) ([]byte, error) {
	switch {
	case file != "":
		return readBody(deps, file)

	case data == "":
		return nil, nil

	case data == "-", data == "@-":
		return readBody(deps, "-")

	case strings.HasPrefix(data, "@"):
		return readBody(deps, strings.TrimPrefix(data, "@"))

	default:
		raw := []byte(data)
		if !json.Valid(raw) {
			return nil, exitcode.UsageError{Err: fmt.Errorf(
				"--data is neither valid JSON nor @file: %.40s", data)}
		}
		return raw, nil
	}
}

// completeOperationID completes `fft api <TAB>` from the spec.
func completeOperationID(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var out []string
	for _, op := range api.Operations() {
		if strings.HasPrefix(strings.ToLower(op.ID), strings.ToLower(prefix)) {
			out = append(out, op.ID+"\t"+op.Summary)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
