// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// FakeGovernanceAuditAppender is an audit-sink double for handler tests. It
// satisfies the single-method appender port handlers depend on, recording every
// event it is handed instead of writing to storage.
//
// It lives here rather than in a package query test file because of a Go rule
// that shapes all of epic #6053 (#6060): a symbol declared in a _test.go file
// is not part of the importable package, so a handler family that moves out of
// package query cannot reach it. The auth, SSO, setup, and local-identity
// families all assert on audit events, so each would otherwise re-declare its
// own copy -- and a re-declared double that drifts from the real port keeps
// passing while guarding nothing.
//
// Events is exported because an unexported field is unreachable from another
// package: a type alias would carry the type without the ability to read what
// the handler recorded.
//
// The zero value is usable. Most callers construct it empty just to satisfy the
// port and assert on Events afterwards.
//
// It carries no lock. Handlers append from the request goroutine, and the
// consuming tests read Events after the request returns. A test that shares one
// appender across concurrent requests needs its own synchronization.
type FakeGovernanceAuditAppender struct {
	// Events holds every event passed to Append, in call order.
	Events []governanceaudit.Event
}

// Append records events and reports success.
//
// Every event in the batch is recorded, not just the first: a handler that
// emits a denial alongside an allow would otherwise pass a test asserting on a
// single event. Batches accumulate across calls, because a test wires one
// appender for a whole request and asserts once at the end.
//
// It reports success unconditionally. Handlers treat an audit write failure as
// a distinct path, so a test covering that path needs a double that fails on
// purpose rather than this one.
func (f *FakeGovernanceAuditAppender) Append(_ context.Context, events []governanceaudit.Event) error {
	f.Events = append(f.Events, events...)
	return nil
}
