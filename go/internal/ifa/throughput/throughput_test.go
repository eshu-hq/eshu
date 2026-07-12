// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package throughput_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/ifa/throughput"
)

func smallSlot(t *testing.T) ifa.ScaleSlot {
	t.Helper()
	slot, ok := ifa.ScaleSlotByID("small/single_repo_multidomain")
	if !ok {
		t.Fatal("small slot missing")
	}
	return slot
}

// TestSmallSlotDrainsEveryScopeHermetically is the hermetic throughput gate for
// the small slot: an amplified Odù drives all its scopes through the P2 driver
// into an in-memory committer with zero credentials. Every amplified scope must
// commit and facts must flow.
func TestSmallSlotDrainsEveryScopeHermetically(t *testing.T) {
	t.Parallel()

	slot := smallSlot(t)
	report, err := throughput.Run(context.Background(), ifa.FamilyGCP, slot, 4592, 2)
	if err != nil {
		t.Fatalf("throughput.Run error = %v, want nil", err)
	}
	if report.ScopesCommitted != slot.Scopes {
		t.Fatalf("ScopesCommitted = %d, want %d (every amplified scope must drain)", report.ScopesCommitted, slot.Scopes)
	}
	if report.FactsCommitted <= 0 {
		t.Fatalf("FactsCommitted = %d, want > 0", report.FactsCommitted)
	}
}

// TestCommittedTotalsAreWorkerCountInvariant is the throughput determinism
// proof: the amplified corpus drains completely and to identical committed
// totals regardless of concurrent worker count. A worker-count-sensitive
// committed total would mean the driver dropped or double-counted work under
// concurrency — exactly the kind of race Ifá exists to catch.
func TestCommittedTotalsAreWorkerCountInvariant(t *testing.T) {
	t.Parallel()

	slot := smallSlot(t)
	var baseScopes int
	var baseFacts int64
	for i, workers := range []int{1, 2, 4} {
		report, err := throughput.Run(context.Background(), ifa.FamilyGCP, slot, 4592, workers)
		if err != nil {
			t.Fatalf("workers=%d: throughput.Run error = %v", workers, err)
		}
		if i == 0 {
			baseScopes = report.ScopesCommitted
			baseFacts = report.FactsCommitted
			continue
		}
		if report.ScopesCommitted != baseScopes {
			t.Fatalf("workers=%d: ScopesCommitted = %d, want %d (worker count changed the committed scope total)", workers, report.ScopesCommitted, baseScopes)
		}
		if report.FactsCommitted != baseFacts {
			t.Fatalf("workers=%d: FactsCommitted = %d, want %d (worker count changed the committed fact total)", workers, report.FactsCommitted, baseFacts)
		}
	}
}

// TestNonAmplifiableSlotFailsClosed proves the throughput runner surfaces the
// amplifier's fail-closed error for the schema-only smoke slot rather than
// reporting a meaningless empty run.
func TestNonAmplifiableSlotFailsClosed(t *testing.T) {
	t.Parallel()

	smoke, ok := ifa.ScaleSlotByID("smoke/synthetic_contracts")
	if !ok {
		t.Fatal("smoke slot missing")
	}
	if _, err := throughput.Run(context.Background(), ifa.FamilyGCP, smoke, 4592, 2); err == nil {
		t.Fatal("throughput.Run(smoke) error = nil, want a not-an-amplification-target error")
	}
}
