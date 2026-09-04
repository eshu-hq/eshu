// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// This file holds the root adapters for the shared doubles that #6060 promoted
// into querytestutil. They live apart from auth_test.go because that file was
// already at the repository's 500-line cap; the file-length linter skips
// _test.go, so nothing would have reported the overflow.

// fakeGovernanceAuditAppender adapts querytestutil.FakeGovernanceAuditAppender
// to the field name this package's tests already use. 19 root files in package
// query build it with keyed literals and read back audit.events, so the field
// stays lowercase and none of them changed.
//
// The recording rule itself is NOT duplicated here. It lives in querytestutil,
// which is where a handler family's tests reach it once the family moves out of
// this package for #6060 -- a symbol declared in a _test.go file cannot be
// imported across a package boundary, so a moved family could not otherwise use
// this double. Two copies of the rule would drift, and a double that no longer
// matches the real port keeps passing while guarding nothing.
type fakeGovernanceAuditAppender struct {
	events []governanceaudit.Event
}

// Append delegates to the shared double and takes back what it recorded.
//
// The copy out and back exists because this adapter owns the slice its callers
// read by its old name, while the shared double owns the decision about what
// gets recorded. Nothing here decides which events land or whether the write
// succeeds.
func (f *fakeGovernanceAuditAppender) Append(ctx context.Context, events []governanceaudit.Event) error {
	delegate := querytestutil.FakeGovernanceAuditAppender{Events: f.events}
	if err := delegate.Append(ctx, events); err != nil {
		return err
	}
	f.events = delegate.Events
	return nil
}

// fakeScopedTokenResolver adapts querytestutil.FakeScopedTokenResolver to the
// field names this package's tests already use. 52 root files in package query
// build it with keyed literals over context/ok/err, so those field names stay
// lowercase and none of those literals changed.
//
// The recorded call is NOT tracked here. It lives in querytestutil, along with
// the lock guarding it, which is where a handler family's tests reach it once
// the family moves out of this package for #6060 -- a symbol declared in a
// _test.go file cannot be imported across a package boundary, so a moved family
// could not otherwise use this double. Two copies of the locking would drift,
// and a double that races under -race takes unrelated tests down with it.
//
// The answer travels as arguments rather than being written into the delegate.
// Assigning delegate.Context on each call would be exactly the concurrent write
// the lock exists to prevent, since one resolver is shared across parallel
// subtests.
type fakeScopedTokenResolver struct {
	context AuthContext
	ok      bool
	err     error

	delegate querytestutil.FakeScopedTokenResolver
}

func (f *fakeScopedTokenResolver) ResolveScopedToken(
	ctx context.Context,
	token string,
) (AuthContext, bool, error) {
	return f.delegate.ResolveAnswering(ctx, token, f.context, f.ok, f.err)
}

// called reports whether middleware reached the resolver at all. It is a method
// rather than a field because reading the recorded call has to take the same
// lock the recording does, and that lock lives in the shared double.
func (f *fakeScopedTokenResolver) called() bool { return f.delegate.Called() }

// token returns the credential middleware presented to the resolver.
func (f *fakeScopedTokenResolver) token() string { return f.delegate.Token() }
