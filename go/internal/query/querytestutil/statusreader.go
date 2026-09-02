// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// FakeStatusReader is a status.Reader double for handler tests. It answers
// from the Snapshot and Err a caller sets instead of reaching Postgres.
//
// It lives here rather than in a package query test file for the same reason
// as FakeGraphReader: a symbol declared in a _test.go file cannot be imported
// across a package boundary, and every handler family that moves out of
// package query (#6060) takes its own status-route tests with it. Those tests
// need this fake, and a re-declared copy per family is the outcome this
// package exists to avoid.
//
// The fields are exported so another package's tests can fill them in; an
// unexported field would be unreachable from outside this package.
//
// The zero value is usable: an unset Err reports the zero Snapshot rather than
// panicking, matching how most callers only care about the success path.
type FakeStatusReader struct {
	// Snapshot is returned by ReadStatusSnapshot and ReadStatusSnapshotFiltered
	// when Err is nil.
	Snapshot status.RawSnapshot
	// Err, when set, is returned in place of Snapshot from both methods.
	Err error
}

// ReadStatusSnapshot returns Err when set, otherwise Snapshot. asOf is
// accepted to satisfy status.Reader but ignored -- the fake answers with a
// fixed snapshot regardless of the requested time.
func (f FakeStatusReader) ReadStatusSnapshot(_ context.Context, _ time.Time) (status.RawSnapshot, error) {
	if f.Err != nil {
		return status.RawSnapshot{}, f.Err
	}
	return f.Snapshot, nil
}

// ReadStatusSnapshotFiltered delegates to ReadStatusSnapshot and ignores the
// selection. Handler tests built on this fake exercise which sections a
// handler chose to request via the real status.Reader implementations, not
// via this fake, so the fake answers every selection the same way.
func (f FakeStatusReader) ReadStatusSnapshotFiltered(
	ctx context.Context,
	asOf time.Time,
	_ status.SnapshotSelection,
) (status.RawSnapshot, error) {
	return f.ReadStatusSnapshot(ctx, asOf)
}
