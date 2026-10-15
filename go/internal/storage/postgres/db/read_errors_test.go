// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import (
	"errors"
	"fmt"
	"testing"
)

// TestSnapshotExportUnqualifiedIsADistinctPermanentIdentity pins the #7957
// sentinel: its fixed text, its identity through wrapping, and that it is not
// the capacity marker callers may fall back on, nor a reader fence sentinel the
// query layer answers with a retry hint.
func TestSnapshotExportUnqualifiedIsADistinctPermanentIdentity(t *testing.T) {
	t.Parallel()
	const want = "shared snapshot export refused: PostgreSQL reader build is not qualified"
	if got := ErrSnapshotExportUnqualified.Error(); got != want {
		t.Fatalf("ErrSnapshotExportUnqualified text = %q, want %q", got, want)
	}
	wrapped := fmt.Errorf("investigate code topic: %w", ErrSnapshotExportUnqualified)
	if !errors.Is(wrapped, ErrSnapshotExportUnqualified) {
		t.Fatal("wrapped sentinel lost its identity")
	}
	for name, other := range map[string]error{
		"capacity":    ErrSnapshotReservationCapacity,
		"stale":       ErrReaderStale,
		"unavailable": ErrReaderUnavailable,
		"topology":    ErrWrongTopology,
	} {
		if errors.Is(ErrSnapshotExportUnqualified, other) || errors.Is(other, ErrSnapshotExportUnqualified) {
			t.Fatalf("ErrSnapshotExportUnqualified aliases %s", name)
		}
	}
}
