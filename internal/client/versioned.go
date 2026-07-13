package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// GetFn reads the entity to be updated, and the version the server currently holds
// for it.
//
// The version comes back separately rather than being read off the entity, because
// T is a type parameter and Go cannot reach a field through one. The call site
// knows the field — it is one line — and this way [UpdateVersioned] works for every
// versioned entity in the API without an interface the generated models would have
// to implement.
type GetFn[T any] func(ctx context.Context) (entity T, version int, err error)

// PutFn writes the entity back, carrying version in the *body*.
//
// There is no ETag and no If-Match anywhere in the API: optimistic locking is a
// required `version` field in the request body. That is why the version is a
// parameter here and not a header somewhere below.
type PutFn[T any] func(ctx context.Context, entity T, version int) (T, error)

// UpdateVersioned is the read-then-write that every mutation of a versioned entity
// has to be.
//
// It reads the entity, applies mutate to it, and writes it back with the version it
// just read. If the server answers 409 — someone else wrote in between — it reads
// again, re-applies mutate to the *fresh* entity, and writes once more. A second
// 409 is not retried: at that point the entity is being written by something else
// faster than fft can read it, and the honest answer is to say so.
//
// expected is the --if-version escape hatch. When it is set, the read is skipped
// and that version is sent as it stands: a CI job that already knows the version
// pays for one request instead of two, and gets a 409 rather than a silent
// overwrite if it was wrong. mutate is then applied to the zero value of T, so it
// must produce the complete entity — which is what `--file` gives it.
//
// (The flag is --if-version, never --version: cobra owns --version on the root
// command, and a subcommand-local one would read as "print the version" to every
// user and every script.)
func UpdateVersioned[T any](ctx context.Context, get GetFn[T], put PutFn[T], mutate func(*T) error, expected *int) (T, error) {
	var zero T

	if put == nil {
		return zero, fmt.Errorf("there is no update to send")
	}

	if expected != nil {
		entity, err := apply(zero, mutate)
		if err != nil {
			return zero, err
		}
		return put(ctx, entity, *expected)
	}

	if get == nil {
		return zero, fmt.Errorf("there is nothing to read the current version from")
	}

	// retried is a bool and not a counter for the same reason the 401 retry is: it
	// makes a second pass structurally impossible rather than merely unlikely.
	for retried := false; ; retried = true {
		current, version, err := get(ctx)
		if err != nil {
			return zero, err
		}

		entity, err := apply(current, mutate)
		if err != nil {
			return zero, err
		}

		updated, err := put(ctx, entity, version)
		if err == nil {
			return updated, nil
		}
		if retried || !isConflict(err) {
			return zero, err
		}
	}
}

// apply runs mutate on a copy of entity. A nil mutate means "write it back as it
// is", which is what a --file update does.
func apply[T any](entity T, mutate func(*T) error) (T, error) {
	if mutate == nil {
		return entity, nil
	}
	if err := mutate(&entity); err != nil {
		var zero T
		return zero, err
	}
	return entity, nil
}

func isConflict(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict
}
