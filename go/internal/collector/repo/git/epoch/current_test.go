// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package epoch

import (
	"strings"
	"testing"
)

// TestMustCurrentRejectsUnsealedLedger is the regression for the #9154 P2:
// Current must not fold a wrong epoch from a hand-merged ledger. Parse
// accepts the merge (it does not check the seal) and the epoch shape stays
// valid, so only an explicit Sealed check in the Current path fails closed.
func TestMustCurrentRejectsUnsealedLedger(t *testing.T) {
	sealed := baseLedger()
	handMerged := sealed
	handMerged.Buckets = append(append([]Bucket{}, sealed.Buckets...),
		bucket("src/zz-hand-merged.go", "in-hand", 1, "out-hand"))
	// Keep byte order so Parse accepts the merge.
	for i := 1; i < len(handMerged.Buckets); i++ {
		if handMerged.Buckets[i-1].Name >= handMerged.Buckets[i].Name {
			t.Fatalf("test setup: merged buckets out of order at %d", i)
		}
	}
	raw := handMerged.Marshal()

	ledger, err := Parse(raw)
	if err != nil {
		t.Fatalf("test setup: hand-merged ledger must parse, got %v", err)
	}
	if !ValidEpoch(ledger.Epoch) {
		t.Fatalf("test setup: merged epoch %q must stay valid-shaped", ledger.Epoch)
	}
	if ledger.Sealed() {
		t.Fatal("test setup: hand-merged ledger must NOT be sealed")
	}

	defer func() {
		if recover() == nil {
			t.Fatal("mustCurrent folded an unsealed ledger instead of panicking")
		}
	}()
	mustCurrent(raw)
}

// TestMustCurrentAcceptsSealedLedger pins the happy path: a sealed ledger
// keeps flowing into the freshness hint.
func TestMustCurrentAcceptsSealedLedger(t *testing.T) {
	raw := baseLedger().Marshal()
	if got := mustCurrent(raw); got != baseEpoch {
		t.Fatalf("mustCurrent = %q, want %q", got, baseEpoch)
	}
}

// TestMustCurrentRejectsMalformedLedger pins the existing fail-closed
// behavior for ledgers Parse itself refuses.
func TestMustCurrentRejectsMalformedLedger(t *testing.T) {
	raw := []byte(strings.Replace(string(baseLedger().Marshal()), "\nepoch\t", "\nepochX\t", 1))
	defer func() {
		if recover() == nil {
			t.Fatal("mustCurrent folded a malformed ledger instead of panicking")
		}
	}()
	mustCurrent(raw)
}
