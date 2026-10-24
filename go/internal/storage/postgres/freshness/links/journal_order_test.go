// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"context"
	"slices"
	"testing"
)

// TestJournalBoundsOnlyTheSweeperStatement pins the statement order of a pass
// with no backfill (#8932): the generation lock timeout is set immediately
// before the sweeper statement, which locks the generations it names, and is
// reset to 0 before the cursor statement, which waits for a link
// transaction's uncommitted cursor insert as it always did. The live proof is
// TestJournalSweeperLockWaitIsBounded; this hermetic test runs in the pull
// request lane, where the live links tests do not.
func TestJournalBoundsOnlyTheSweeperStatement(t *testing.T) {
	d := &recordingDB{path: "incremental"}
	if _, err := NewJournalStore(d).Journal(context.Background(), 0); err != nil {
		t.Fatalf("Journal: %v", err)
	}
	want := []string{
		trySlotQuery,
		setGenerationLockTimeoutStatement,
		journalActiveGenerationsQuery,
		resetLockTimeoutStatement,
		createCursorsQuery,
	}
	if !slices.Equal(d.statements, want) {
		t.Fatalf("Journal statements = %.60q\nwant     %.60q", d.statements, want)
	}
}
