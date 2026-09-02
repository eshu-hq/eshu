// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
)

// FakeScopedTokenResolver is a scoped-token resolver double for middleware
// tests. It satisfies the single-method resolver port, answering from fields a
// caller installs and recording which credential it was asked about.
//
// It lives here rather than in a package query test file because of a Go rule
// that shapes all of epic #6053 (#6060): a symbol declared in a _test.go file
// is not part of the importable package, so a handler family that moves out of
// package query cannot reach it. Nearly every scope-enforcement test in the
// package builds one of these, so each moved family would otherwise re-declare
// its own copy -- and a re-declared double that drifts from the real port keeps
// passing while guarding nothing.
//
// The answer fields are exported because an unexported field is unreachable
// from another package: a type alias would carry the type without the ability
// to configure the verdict the middleware under test has to see.
//
// The recorded call is read through Called and Token rather than exposed as
// fields, because reading it has to take the same lock the recording does.
//
// The zero value is usable: it answers "not a scoped token" with no error,
// which is what a test wiring the resolver purely to satisfy the port wants.
type FakeScopedTokenResolver struct {
	// Context is the auth context ResolveScopedToken hands back.
	Context queryauth.AuthContext
	// OK is the recognized-credential verdict ResolveScopedToken hands back.
	OK bool
	// Err is the failure ResolveScopedToken hands back. Middleware treats a
	// resolver error differently from an unrecognized token, so the two are
	// separate fields rather than one.
	Err error

	// mu guards the recorded call. A single resolver instance is shared across
	// parallel subtests -- the package-registry adjacent-route table does
	// exactly this -- which calls the resolver concurrently. Without the lock
	// the shared fake data-races under -race and aborts unrelated tests in the
	// package.
	mu     sync.Mutex
	token  string
	called bool
}

// ResolveScopedToken satisfies the resolver port, recording the presented token
// and answering from Context, OK, and Err.
func (f *FakeScopedTokenResolver) ResolveScopedToken(
	ctx context.Context,
	token string,
) (queryauth.AuthContext, bool, error) {
	return f.ResolveAnswering(ctx, token, f.Context, f.OK, f.Err)
}

// ResolveAnswering records the presented token and returns the supplied answer
// instead of the fake's own fields.
//
// It exists for root query's adapter. That adapter keeps its answer in
// lowercase fields the package's 50 consuming test files already name in keyed
// literals, so it cannot fill in Context, OK, and Err without renaming every
// one of them. Routing through here lets it reuse this recording rather than
// keeping a second copy of the locking, which is the copy that would drift.
//
// Passing the answer per call rather than writing it into the fake matters
// under -race: an adapter that assigned Context on every call would introduce
// the concurrent write mu exists to prevent.
func (f *FakeScopedTokenResolver) ResolveAnswering(
	_ context.Context,
	token string,
	authContext queryauth.AuthContext,
	ok bool,
	err error,
) (queryauth.AuthContext, bool, error) {
	f.mu.Lock()
	f.called = true
	f.token = token
	f.mu.Unlock()
	return authContext, ok, err
}

// Called reports whether the resolver has been asked to resolve anything.
// Tests use it to prove middleware skipped the resolver entirely, which is the
// only observable difference between a public path and one that resolved to no
// grants.
func (f *FakeScopedTokenResolver) Called() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.called
}

// Token returns the credential presented on the most recent call, or the empty
// string before the first. Tests use it to prove middleware passed the scoped
// credential rather than the raw Authorization header.
func (f *FakeScopedTokenResolver) Token() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.token
}
