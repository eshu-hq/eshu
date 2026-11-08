// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

// TestWriteSupplyChainImpactPartialPassRecordsScopePassState proves the
// durable half of #10389: an admitted partial pass records the scope's
// per-scope pass state in the same write transaction, so the status reader
// sees stuck scopes from durable state, not the process-local headroom
// tracker.
func TestWriteSupplyChainImpactPartialPassRecordsScopePassState(t *testing.T) {
	t.Parallel()

	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	write := retractTestWrite()
	write.PartialEvidence = true
	write.PartialCauses = []string{"evidence_budget"}
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

	_, err := runSupplyChainImpactTx(context.Background(), beginner, write, nil, nil, ProtectedScanImpactPublicationSet{}, false, now)
	if err != nil {
		t.Fatalf("runSupplyChainImpactTx() error = %v", err)
	}
	call := findExecCallByQuery(beginner.state.all, supplyChainImpactScopePassStateQuery)
	if call == nil {
		t.Fatalf("no scope pass-state upsert in %d statements", len(beginner.state.all))
	}
	if got, want := call.Args[0], write.ScopeID; got != want {
		t.Fatalf("scope pass-state scope_id = %v, want %v", got, want)
	}
	if got, want := call.Args[2], true; got != want {
		t.Fatalf("scope pass-state partial = %v, want %v", got, want)
	}
}

// TestWriteSupplyChainImpactCompletePassClearsScopePassState proves the
// clearing half of #10389: a later complete pass for the scope clears the
// stuck state in the same transaction, so the degraded reason lifts.
func TestWriteSupplyChainImpactCompletePassClearsScopePassState(t *testing.T) {
	t.Parallel()

	inserts := &testutil.FakeExecer{}
	beginner := newFakeImpactBeginner(inserts)
	write := retractTestWrite()
	write.PartialEvidence = false
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

	_, err := runSupplyChainImpactTx(context.Background(), beginner, write, nil, nil, ProtectedScanImpactPublicationSet{}, false, now)
	if err != nil {
		t.Fatalf("runSupplyChainImpactTx() error = %v", err)
	}
	call := findExecCallByQuery(beginner.state.all, supplyChainImpactScopePassStateQuery)
	if call == nil {
		t.Fatalf("no scope pass-state upsert in %d statements", len(beginner.state.all))
	}
	if got, want := call.Args[2], false; got != want {
		t.Fatalf("scope pass-state partial = %v, want %v", got, want)
	}
	// A nil cause set must encode as an empty array, never NULL: the
	// column is NOT NULL and a NULL would fail the pass.
	causes, ok := call.Args[3].([]string)
	if !ok {
		t.Fatalf("scope pass-state causes = %T, want []string", call.Args[3])
	}
	if causes == nil || len(causes) != 0 {
		t.Fatalf("scope pass-state causes = %v, want an empty non-nil set", call.Args[3])
	}
}

func findExecCallByQuery(calls []testutil.ExecCall, query string) *testutil.ExecCall {
	for i := range calls {
		if calls[i].Query == query {
			return &calls[i]
		}
	}
	return nil
}
