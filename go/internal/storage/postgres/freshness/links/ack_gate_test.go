// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"fmt"
	"strings"
	"testing"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// TestAckJournalGateUsesTheJournalKey pins the Ack's gate (#7127 PR-3b) to
// the shared side of the journal pass's own key: SlotLockClass and slot 0.
// A gate on any other key would let a journal pass run between an Ack's
// ledger insert and its commit and invert activation_seq against commit
// order (arbiter ruling, 2026-10-15).
func TestAckJournalGateUsesTheJournalKey(t *testing.T) {
	want := fmt.Sprintf("pg_try_advisory_xact_lock_shared(%d, %d)",
		linksfreshnessstore.SlotLockClass, linksfreshnessstore.JournalLockSlotForTest)
	if linksfreshnessstore.AckJournalGateSQL != want {
		t.Fatalf("AckJournalGateSQL = %q, want %q", linksfreshnessstore.AckJournalGateSQL, want)
	}
	// The gate must try, never wait: a blocking shared lock would make an
	// Ack wait for a whole journal pass while it holds its scope row.
	if !strings.HasPrefix(linksfreshnessstore.AckJournalGateSQL, "pg_try_") {
		t.Fatalf("AckJournalGateSQL %q must be a try-lock", linksfreshnessstore.AckJournalGateSQL)
	}
	// The pass takes the same key exclusively, also as a try-lock.
	if got := linksfreshnessstore.TrySlotQueryForTest; !strings.Contains(got, "pg_try_advisory_xact_lock(") {
		t.Fatalf("journal pass lock %q is not an exclusive try-lock", got)
	}
}

// TestAckActivationSource pins the source value an Ack writes.
func TestAckActivationSource(t *testing.T) {
	if linksfreshnessstore.SourceAck != "ack" {
		t.Fatalf("SourceAck = %q, want %q", linksfreshnessstore.SourceAck, "ack")
	}
}
