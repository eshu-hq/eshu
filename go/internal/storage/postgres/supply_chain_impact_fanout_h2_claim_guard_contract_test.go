// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// H2 lane-split contract, claim and guard surface (#11520): what the
// hot/cold lane split must NOT change on the queue-claim and statement-guard
// surface. All GREEN on base. The LIMIT half of the claim bound is pinned by
// TestClaimBatchQueryStaysBatchBounded; this file pins the ORDER BY half and
// both fences, plus the per-statement timeout wiring.

// TestH2ClaimOrderByUntouched pins the claim ORDER BYs H2 must not reorder:
// the representative window (claimed/running first, then updated_at), the
// fairness window, and the source-heads window. Any claim order over the
// same inputs converges to the same graph, so an ordering regression is
// invisible to graph truth — these pins are the only thing that catches it.
func TestH2ClaimOrderByUntouched(t *testing.T) {
	t.Parallel()

	// The query embeds comments that name historical shapes (e.g.
	// "ORDER BY ... LIMIT 1"), so strip line comments before pinning: only
	// live clauses may satisfy the bound.
	uncommented := regexp.MustCompile(`(?m)--.*$`).ReplaceAllString(claimReducerWorkBatchQuery, "")
	for _, want := range []string{
		// Representative window: the #4137 conflict-key representative.
		"ORDER BY claimed_running_first ASC, is_search_doc ASC, updated_at ASC, work_item_id ASC",
		// Fairness window: every priority-0 candidate sorts ahead.
		"ORDER BY is_search_doc ASC, updated_at ASC, work_item_id ASC",
	} {
		if !strings.Contains(uncommented, want) {
			t.Fatalf("batch claim query lost its ordering %q:\n%s", want, claimReducerWorkBatchQuery)
		}
	}
}

// TestH2ClaimFencesUntouched pins the two fences H2 must not move. The
// representative fence admits exactly one row per conflict key; the
// live-lease fence drops representatives another live claim owns. The fan-out
// state write stays fenced on the pass's own item (owner, live lease,
// attempt), so a lost lease fails instead of writing another worker's row.
func TestH2ClaimFencesUntouched(t *testing.T) {
	t.Parallel()

	fence := reducerClaimLiveFenceKeysCTE()
	for _, want := range []string{
		"live_fence_keys AS MATERIALIZED",
		"status IN ('claimed', 'running')",
		"claim_until > $1",
	} {
		if !strings.Contains(fence, want) {
			t.Fatalf("live-lease fence lost %q:\n%s", want, fence)
		}
	}
	reps := reducerClaimBatchRepresentativeCTEs()
	for _, want := range []string{
		"reps.same_rn = 1",
		"live_fence_keys",
		"NOT EXISTS",
	} {
		if !strings.Contains(reps, want) {
			t.Fatalf("representative fence lost %q", want)
		}
	}
	state := recordSupplyChainImpactFanOutStateQuery
	for _, want := range []string{
		"lease_owner = $3",
		"claim_until > clock_timestamp()",
		"last_attempt_at = $4",
		"stage = 'reducer'",
	} {
		if !strings.Contains(state, want) {
			t.Fatalf("fan-out state write lost its lease fence %q", want)
		}
	}
}

// TestH2HolderGuardBounded pins the 10s per-statement guard H2 must keep on
// every holder statement: the bound itself, and the wiring that makes the
// timeout the FIRST statement of the read transaction, before the readiness
// check and the holder read, so a regressed plan can pin a connection and a
// snapshot for at most 10s on ANY arm of the split.
func TestH2HolderGuardBounded(t *testing.T) {
	t.Parallel()

	if supplyChainImpactFanOutReadTimeout != 10*time.Second {
		t.Fatalf("holder read timeout = %v, want 10s (the per-statement guard H2 must keep)", supplyChainImpactFanOutReadTimeout)
	}
	if !strings.Contains(setSupplyChainImpactFanOutReadTimeoutQuery, "statement_timeout") {
		t.Fatalf("guard query does not set statement_timeout:\n%s", setSupplyChainImpactFanOutReadTimeoutQuery)
	}

	fake := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{},                           // statement_timeout
		{rows: [][]any{{true}}},      // supports readiness
		{rows: [][]any{{"scope-a"}}}, // holder read
	}}
	store := SupplyChainImpactFanOutStore{
		Beginner: massProbeBeginner{massProbeTx{fake}},
		DB:       fake,
	}
	got, err := store.FindHolderScopes(context.Background(), "own", []string{"CVE-2026-1"})
	if err != nil {
		t.Fatalf("FindHolderScopes() error = %v", err)
	}
	if len(got) != 1 || got[0] != "scope-a" {
		t.Fatalf("FindHolderScopes() = %v, want [scope-a]", got)
	}
	if len(fake.execs) == 0 || fake.execs[0].query != setHolderKeyRangeSnapshotQuery {
		t.Fatalf("first exec = %+v, want the read-only snapshot %q", fake.execs, setHolderKeyRangeSnapshotQuery)
	}
	if len(fake.queries) != 3 {
		t.Fatalf("holder read ran %d statements, want 3 (timeout, readiness, holder)", len(fake.queries))
	}
	if !strings.Contains(fake.queries[0].query, "statement_timeout") {
		t.Fatalf("first statement = %q, want the statement timeout", fake.queries[0].query)
	}
	if len(fake.queries[0].args) != 1 || fake.queries[0].args[0] != "10000ms" {
		t.Fatalf("guard args = %v, want [10000ms]", fake.queries[0].args)
	}
	if fake.queries[2].query != FindSupplyChainImpactHoldersQuery {
		t.Fatal("holder read is not the shipped constant: the guard test must run the statement it guards")
	}
}
