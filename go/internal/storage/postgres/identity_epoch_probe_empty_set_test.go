// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
)

// TestProbeIdentityEpochEmptySet drives the epoch probe against a
// globally-empty image-identity set: no fact row matches the identity filter,
// so max(observed_at) is NULL, count is 0, and the fingerprint digests the
// empty pair set (SHA-256 of the empty string).
//
// Production history: the probe wrapped the max in
// COALESCE(max(observed_at), '-infinity'), and scanned the column into a
// time.Time. On an empty set the column held -infinity, which pgx (go.mod
// pins pgx v5; see pgtype/builtin_wrappers.go: "cannot scan -Infinity into
// *time.Time") cannot scan into *time.Time, so every probe failed and the
// identity loader could never serve even the empty set. The probe must accept
// the NULL max and report the zero time instead, keeping the empty-set epoch
// stable across probes so the empty set remains cacheable.
func TestProbeIdentityEpochEmptySet(t *testing.T) {
	t.Parallel()

	const emptySetFingerprint = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	database := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{int64(0), sql.NullTime{}, emptySetFingerprint}}},
			{rows: [][]any{{int64(0), sql.NullTime{}, emptySetFingerprint}}},
		},
	}
	store := NewFactStore(database)

	first, err := store.probeIdentityEpoch(context.Background())
	if err != nil {
		t.Fatalf("probeIdentityEpoch on empty set: %v", err)
	}
	if first.count != 0 {
		t.Fatalf("empty-set epoch count = %d, want 0", first.count)
	}
	if !first.maxObservedAt.IsZero() {
		t.Fatalf("empty-set epoch maxObservedAt = %v, want zero time", first.maxObservedAt)
	}
	if first.activeFingerprint != emptySetFingerprint {
		t.Fatalf("empty-set epoch fingerprint = %q, want %q", first.activeFingerprint, emptySetFingerprint)
	}

	second, err := store.probeIdentityEpoch(context.Background())
	if err != nil {
		t.Fatalf("second probeIdentityEpoch on empty set: %v", err)
	}
	if second != first {
		t.Fatalf("empty-set epoch moved between probes: %+v vs %+v", first, second)
	}
}
