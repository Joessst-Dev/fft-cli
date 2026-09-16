package main

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"

	"github.com/Joessst-Dev/fft-cli/internal/auth"
	"github.com/Joessst-Dev/fft-cli/internal/config"
)

// sessionTokens holds one token source per project for the length of a `fft tui`
// session, and every run in the session signs its requests through it.
//
// Every run builds a Deps of its own, and would build a token source of its own
// with it. Where the credential store keeps tokens, the next run finds the fresh
// one there; the environment's store keeps nothing, so every run on such a project
// signed in to Google again, and runs started side by side all did so at once.
// One source per project holds the token in memory instead. Its own lock makes the
// first run mint and the others wait for that token, and a 401 renews it for every
// run that comes after.
//
// Memory only, and forgotten whenever it might be about another account: when the
// UI switches project, and when a run has rewritten the config file — a project
// removed and added again under the same name may sign in as someone else.
type sessionTokens struct {
	mu      sync.Mutex
	sources map[tokenKey]auth.TokenSource

	// building holds the source being built for a key, for the runs that ask for
	// it meanwhile to wait on.
	building map[tokenKey]*tokenBuild

	// generation counts the forgets. A source whose build began before the latest
	// one is handed to the runs that waited for it, and not kept.
	generation uint64
}

// tokenBuild is one token source being built.
type tokenBuild struct {
	done       chan struct{}
	generation uint64

	// src and err are set before done is closed.
	src auth.TokenSource
	err error
}

// tokenKey identifies a project by everything its token source is built from, so
// that a project edited outside the session — by `fft project add --force` in
// another shell — does not get the token of the account it was before.
type tokenKey [sha256.Size]byte

func tokenKeyOf(p config.Project) tokenKey {
	h := sha256.New()
	for _, part := range []string{p.Name, p.BaseURL, p.Email, p.FirebaseAPIKey} {
		// Length-prefixed, so that no two different projects hash the same fields.
		h.Write(binary.LittleEndian.AppendUint64(nil, uint64(len(part))))
		h.Write([]byte(part))
	}
	var k tokenKey
	h.Sum(k[:0])
	return k
}

// source returns the session's token source for p, building it with build the
// first time p asks.
//
// Building reads the credential store, which on macOS may raise a keychain dialog,
// so the runs that ask for p meanwhile wait for the one build rather than raising
// a dialog each. They wait without the lock: a build that waits on the user must
// not hold up a project switch, or a run for another project.
func (t *sessionTokens) source(p config.Project, build func() (auth.TokenSource, error)) (auth.TokenSource, error) {
	key := tokenKeyOf(p)

	t.mu.Lock()
	if src, ok := t.sources[key]; ok {
		t.mu.Unlock()
		return src, nil
	}
	if b, ok := t.building[key]; ok {
		t.mu.Unlock()
		<-b.done
		return b.src, b.err
	}
	b := &tokenBuild{done: make(chan struct{}), generation: t.generation}
	if t.building == nil {
		t.building = make(map[tokenKey]*tokenBuild)
	}
	t.building[key] = b
	t.mu.Unlock()

	b.src, b.err = build()

	t.mu.Lock()
	if t.building[key] == b {
		delete(t.building, key)
	}
	if b.err == nil && b.generation == t.generation {
		if t.sources == nil {
			t.sources = make(map[tokenKey]auth.TokenSource)
		}
		t.sources[key] = b.src
	}
	t.mu.Unlock()
	close(b.done)
	return b.src, b.err
}

// forget drops every token source. A run already holding one, or waiting for one
// being built, keeps it; the runs after this build their own again.
func (t *sessionTokens) forget() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.generation++
	clear(t.sources)
	clear(t.building)
}
