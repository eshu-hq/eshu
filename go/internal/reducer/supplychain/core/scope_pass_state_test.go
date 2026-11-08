// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"strings"
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

// TestSupplyChainImpactScopePassStateQueryPinsFencingGuard pins the two
// load-bearing SQL choices in the scope pass-state upsert: the fencing-token
// guard keeps an older admitted pass from overwriting a newer pass's state
// when passes for one scope interleave across generations, and the COALESCE
// preserves the original partial_since across further partial passes. The
// query text is the contract here; the hermetic tests above locate the
// statement by const identity and would stay green if either choice were
// dropped. (Review F1 on #10389.)
func TestSupplyChainImpactScopePassStateQueryPinsFencingGuard(t *testing.T) {
	t.Parallel()

	if !strings.Contains(supplyChainImpactScopePassStateQuery, "fencing_token <= EXCLUDED.fencing_token") {
		t.Fatalf("scope pass-state upsert must guard on the fencing token (<=):\n%s", supplyChainImpactScopePassStateQuery)
	}
	if !strings.Contains(supplyChainImpactScopePassStateQuery, "COALESCE(supply_chain_impact_scope_pass_state.partial_since") {
		t.Fatalf("scope pass-state upsert must preserve the original partial_since:\n%s", supplyChainImpactScopePassStateQuery)
	}
	if !strings.Contains(supplyChainImpactScopePassStateQuery, "THEN $5::timestamptz ELSE NULL") {
		t.Fatalf("scope pass-state upsert must cast the CASE branch (unknown-type params resolve to text, rejected by timestamptz, 42804):\n%s", supplyChainImpactScopePassStateQuery)
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
