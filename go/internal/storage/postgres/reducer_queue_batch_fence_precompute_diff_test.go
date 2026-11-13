// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestClaimBatchFencePrecomputeShape is the query-shape gate for #10630 step
// 2: the batch claim must compute the live-lease conflict-key fence once per
// claim as a materialized set and read it in `candidate` through an
// anti-join, instead of evaluating the correlated per-row NOT EXISTS fence
// (one fact_work_items index probe per representative, ~5.2k/claim on the
// live backlog, 95%+ of claim time per probe B) for every representative
// row. Everything from the lock tail onward stays byte-identical (proved
// set- and order-identical by
// TestClaimBatchFencePrecomputeMatchesPreChangeCandidateSetAndOrder, and
// race-safe by TestClaimBatchFencePrecomputeSharedKeyStorm).
func TestClaimBatchFencePrecomputeShape(t *testing.T) {
	t.Parallel()

	query := claimReducerWorkBatchQuery
	for _, want := range []string{
		"live_fence_keys AS MATERIALIZED (",
		"FROM live_fence_keys",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing precomputed-fence construct %q", want)
		}
	}
	// The per-row correlated conflict-key fence this rewrite exists to
	// remove must not come back. The ban is scoped to the candidate fence
	// alias: the sweep and the lock tail join fact_work_items under their
	// own aliases (stale, lock_target, inflight is the fence's).
	if strings.Contains(query, "FROM fact_work_items AS inflight") {
		t.Fatal("batch claim query still evaluates the per-row correlated live-lease fence " +
			"(FROM fact_work_items AS inflight)")
	}
}

// TestClaimBatchFencePrecomputeComposesFromShippedSource pins the drift
// guard: the candidate anti-join must read the fence set from the same Go
// generator as the shipped claim, so a future fence-predicate edit lands in
// both instead of silently diverging admission from the single source.
func TestClaimBatchFencePrecomputeComposesFromShippedSource(t *testing.T) {
	t.Parallel()

	precompute := reducerClaimLiveFenceKeysCTE()
	if !strings.Contains(claimReducerWorkBatchQuery, precompute) {
		t.Fatal("batch claim query does not contain reducerClaimLiveFenceKeysCTE() verbatim: " +
			"the precomputed fence must generate from the same Go source as the shipped query")
	}
	chain := reducerClaimBatchCandidateChainCTEs()
	if !strings.Contains(claimReducerWorkBatchQuery, chain) {
		t.Fatal("shipped batch claim does not contain reducerClaimBatchCandidateChainCTEs() verbatim: " +
			"the funnel chain must keep generating from the same Go source as the claim")
	}
	if !strings.Contains(chain, precompute) {
		t.Fatal("candidate chain does not contain the precomputed fence CTE verbatim: " +
			"the funnel counts query replays this chain, so the fence fix must flow into both")
	}
}

// TestClaimBatchFencePrecomputeMatchesPreChangeCandidateSetAndOrder is the
// C-1 differential proof for #10630 step 2: the fenced candidate SELECT
// (derived from the shipped query at test time) must return the IDENTICAL
// work_item_id set, in the IDENTICAL order, as the pre-change correlated
// candidate SELECT (frozen in testdata), across the fence-relevant matrix:
// live vs expired vs absent holder leases, NULL conflict_key scope fallback,
// cross-domain same-key isolation, and two $1 clock positions.
//
// Skipped unless a DSN is provided, like the other claim differentials.
func TestClaimBatchFencePrecomputeMatchesPreChangeCandidateSetAndOrder(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the fence-precompute differential proof")
	}

	oldQuery := frozenPreFenceCandidate(t)
	if !strings.Contains(oldQuery, "FROM fact_work_items AS inflight") {
		t.Fatal("frozen pre-change candidate lost its correlated fence; " +
			"it no longer represents the pre-#10630-step-2 query")
	}
	newQuery, ok := deriveReadOnlyCandidateSelect(claimReducerWorkBatchQuery)
	if !ok {
		t.Fatal("could not derive read-only candidate SELECT from the shipped batch claim query")
	}
	if strings.Contains(newQuery, "FROM fact_work_items AS inflight") {
		t.Fatal("derived candidate still has the correlated fence; the rewrite is not in the shipped query")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)
	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	seedFencePrecomputeFixture(t, ctx, db, now)

	clockA := now.Add(2 * time.Hour)
	clockB := now.Add(4 * time.Hour)

	type cell struct {
		name   string
		clock  time.Time
		filter []string
	}
	var cells []cell
	for _, tc := range []struct {
		name  string
		clock time.Time
	}{
		{"clockA", clockA},
		{"clockB", clockB},
	} {
		cells = append(cells, cell{name: tc.name, clock: tc.clock})
	}
	cells = append(cells, cell{
		name:   "clockA/domain-filter",
		clock:  clockA,
		filter: []string{"supply_chain_impact"},
	})

	nonEmpty := 0
	for _, c := range cells {
		c := c
		t.Run(c.name, func(t *testing.T) {
			args := []any{
				c.clock,                // $1 p_now
				c.filter,               // $2 domain filter (nil => no restriction)
				"n/a",                  // $3 lease owner (unused by read-only candidate select)
				c.clock.Add(time.Hour), // $4 claim_until (unused by read-only candidate select)
				false,                  // $5 require projector drain
				0,                      // $6 expected source-local projectors
				0,                      // $7 semantic entity claim limit
				1000,                   // $8 batch limit (covers the whole fixture)
			}
			oldIDs := queryCandidateWorkItemIDs(t, ctx, db, oldQuery, args)
			newIDs := queryCandidateWorkItemIDs(t, ctx, db, newQuery, args)
			onlyInOld := diffStringSlices(oldIDs, newIDs)
			onlyInNew := diffStringSlices(newIDs, oldIDs)
			if len(onlyInOld) != 0 || len(onlyInNew) != 0 {
				t.Fatalf(
					"fence-precompute candidate set diverges from pre-change set (want 0/0): "+
						"only in OLD = %v, only in NEW = %v\nOLD order = %v\nNEW order = %v",
					onlyInOld, onlyInNew, oldIDs, newIDs,
				)
			}
			if len(oldIDs) != len(newIDs) || fmt.Sprint(oldIDs) != fmt.Sprint(newIDs) {
				t.Fatalf("fence-precompute candidate ORDER diverges from pre-change order:\nOLD = %v\nNEW = %v", oldIDs, newIDs)
			}
			if len(oldIDs) > 0 {
				nonEmpty++
			}
		})
	}
	if nonEmpty == 0 {
		t.Fatal("fixture produced zero candidates in every matrix cell; the differential is vacuous")
	}
	t.Logf("fence-precompute differential green in %d cells (%d non-empty)", len(cells), nonEmpty)

	// Fence non-vacuity: a live-held row is never itself admitted (base
	// pre-filters live leases), so at clockA the live-holder key admits
	// NOBODY (holder leased, sibling fenced); at clockB the first
	// holder's lease has expired so the holder reclaims its key as the
	// representative. The expired-holder key admits its holder at both
	// clocks. The NULL-key rows ride the scope_id fallback the same way,
	// and the cross-domain same-key sibling is never fenced by another
	// domain's lease.
	deciding := []struct {
		name    string
		clock   time.Time
		want    []string
		refused []string
	}{
		{
			name:  "live-holder-fences-sibling",
			clock: clockA,
			want: []string{
				"fence-expired-holder",
				"fence-plain-00", "fence-plain-01", "fence-plain-02", "fence-plain-03",
				"fence-xdom-sibling",
			},
			refused: []string{
				"fence-holder-live",     // leased: base excludes live rows
				"fence-older-sibling",   // same key as the live holder
				"fence-null-holder",     // leased: base excludes live rows
				"fence-null-sibling",    // same scope as the live NULL-key holder
				"fence-xdom-holder",     // leased: base excludes live rows
				"fence-expired-sibling", // holder is the representative
			},
		},
		{
			name:  "expired-holder-releases-sibling",
			clock: clockB,
			want: []string{
				"fence-holder-live", // lease expired: reclaims its key as representative
				"fence-expired-holder",
				"fence-null-holder", // lease expired: reclaims its scope as representative
				"fence-xdom-holder", // lease expired: sole row on its key
				"fence-plain-00", "fence-plain-01", "fence-plain-02", "fence-plain-03",
				"fence-xdom-sibling",
			},
			refused: []string{
				"fence-older-sibling",   // holder reclaimed the representative slot
				"fence-null-sibling",    // holder reclaimed the representative slot
				"fence-expired-sibling", // holder is the representative
			},
		},
	}
	for _, d := range deciding {
		t.Run(d.name, func(t *testing.T) {
			args := []any{
				d.clock,                // $1 p_now
				nil,                    // $2 domain filter
				"n/a",                  // $3 lease owner (unused by read-only candidate select)
				d.clock.Add(time.Hour), // $4 claim_until (unused by read-only candidate select)
				false,                  // $5 require projector drain
				0,                      // $6 expected source-local projectors
				0,                      // $7 semantic entity claim limit
				1000,                   // $8 batch limit (covers the whole fixture)
			}
			newIDs := queryCandidateWorkItemIDs(t, ctx, db, newQuery, args)
			got := map[string]bool{}
			for _, id := range newIDs {
				got[id] = true
			}
			for _, want := range d.want {
				if !got[want] {
					t.Errorf("deciding cell %s admits %v, want %s admitted", d.name, newIDs, want)
				}
			}
			for _, refused := range d.refused {
				if got[refused] {
					t.Errorf("deciding cell %s admits %s, want it fenced out", d.name, refused)
				}
			}
		})
	}
}

// seedFencePrecomputeFixture seeds one disposable database with a row for
// every fence shape the #10630 step-2 rewrite must preserve. now is the
// fixture epoch; the differential runs at now+2h (clockA) and now+4h
// (clockB). Leases straddling those clocks decide fence membership:
//
//   - fence-holder-key: live holder (lease to now+3h) plus an older pending
//     sibling. At clockA the holder is live so the sibling is fenced and
//     the holder is the representative; at clockB both leases expired so
//     the older sibling becomes the representative.
//   - fence-expired-key: holder whose lease already expired plus a pending
//     sibling — the holder stays the representative at both clocks.
//   - fence-null-key: NULL conflict_key rows on one scope (scope_id
//     fallback), live holder plus pending sibling — same fence behavior
//     through the COALESCE path.
//   - fence-xdom-key: same conflict_key string under two conflict_domains,
//     one holding a live lease — the other domain's pending row is never
//     fenced (the fence partitions by domain).
func seedFencePrecomputeFixture(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
	t.Helper()
	seedGateScope(t, ctx, db, "scope-fence-diff", "gen-fair", now)

	insert := func(item reducerFairnessWorkItem) {
		t.Helper()
		insertReducerFairnessWorkItem(t, ctx, db, item)
	}

	for i := 0; i < 4; i++ {
		insert(reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("fence-plain-%02d", i), scopeID: "scope-fence-diff", generationID: "gen-fair",
			domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: fmt.Sprintf("fence-plain-key-%02d", i),
			updatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}

	// Live-holder key: lease to now+3h is live at clockA, expired at clockB.
	insert(reducerFairnessWorkItem{
		workItemID: "fence-older-sibling", scopeID: "scope-fence-diff", generationID: "gen-fair",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: "fence-holder-key",
		updatedAt: now.Add(-time.Hour),
	})
	insert(reducerFairnessWorkItem{
		workItemID: "fence-holder-live", scopeID: "scope-fence-diff", generationID: "gen-fair",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: "fence-holder-key",
		updatedAt: now.Add(-30 * time.Minute),
	})
	mustExecGateArgs(t, ctx, db, `UPDATE fact_work_items SET status='claimed', lease_owner='fence-holder', claim_until=$1 WHERE work_item_id='fence-holder-live'`, now.Add(3*time.Hour))

	// Expired-holder key: lease to now+1h is expired at both clocks, so
	// the holder (claimed_running_first=0) stays the representative.
	insert(reducerFairnessWorkItem{
		workItemID: "fence-expired-sibling", scopeID: "scope-fence-diff", generationID: "gen-fair",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: "fence-expired-key",
		updatedAt: now.Add(-time.Hour),
	})
	insert(reducerFairnessWorkItem{
		workItemID: "fence-expired-holder", scopeID: "scope-fence-diff", generationID: "gen-fair",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: "fence-expired-key",
		updatedAt: now.Add(-30 * time.Minute),
	})
	mustExecGateArgs(t, ctx, db, `UPDATE fact_work_items SET status='claimed', lease_owner='fence-expired', claim_until=$1 WHERE work_item_id='fence-expired-holder'`, now.Add(time.Hour))

	// NULL conflict_key rows: the fence falls back to scope_id, so the
	// holder's live lease fences every other NULL-key row on the scope.
	// A dedicated sibling scope (with its own generation: one generation
	// id backs at most one scope's active pointer) keeps the blast radius
	// visible: rows on other scopes are unaffected.
	seedGateScope(t, ctx, db, "scope-fence-null", "gen-null", now)
	mustExecGateArgs(t, ctx, db, `INSERT INTO fact_work_items (
	    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
	    conflict_key, status, attempt_count, payload, created_at, updated_at
	) VALUES
	    ('fence-null-sibling', 'scope-fence-null', 'gen-null', 'reducer', 'supply_chain_impact', 'scope',
	        NULL, 'pending', 0, jsonb_build_object('entity_key', 'fence-null-sibling'), $1, $1),
	    ('fence-null-holder', 'scope-fence-null', 'gen-null', 'reducer', 'supply_chain_impact', 'scope',
	        NULL, 'claimed', 0, jsonb_build_object('entity_key', 'fence-null-holder'), $1, $1)`, now)
	mustExecGateArgs(t, ctx, db, `UPDATE fact_work_items SET lease_owner='fence-null', claim_until=$1 WHERE work_item_id='fence-null-holder'`, now.Add(3*time.Hour))

	// Cross-domain same key: the live lease is under conflict_domain
	// 'scope' while the pending sibling is under 'domain'; the fence
	// partitions by domain so the sibling is never fenced.
	mustExecGateArgs(t, ctx, db, `INSERT INTO fact_work_items (
	    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
	    conflict_key, status, attempt_count, payload, created_at, updated_at
	) VALUES
	    ('fence-xdom-sibling', 'scope-fence-diff', 'gen-fair', 'reducer', 'supply_chain_impact', 'domain',
	        'fence-xdom-key', 'pending', 0, jsonb_build_object('entity_key', 'fence-xdom-sibling'), $1, $1),
	    ('fence-xdom-holder', 'scope-fence-diff', 'gen-fair', 'reducer', 'supply_chain_impact', 'scope',
	        'fence-xdom-key', 'claimed', 0, jsonb_build_object('entity_key', 'fence-xdom-holder'), $1, $1)`, now)
	mustExecGateArgs(t, ctx, db, `UPDATE fact_work_items SET lease_owner='fence-xdom', claim_until=$1 WHERE work_item_id='fence-xdom-holder'`, now.Add(3*time.Hour))
}

// frozenPreFenceCandidate reads the machine-derived pre-change candidate
// SELECT frozen in testdata (see that file's header for provenance).
func frozenPreFenceCandidate(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	oldBytes, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "testdata", "claim_candidate_pre_fence_precompute.sql"))
	if err != nil {
		t.Fatalf("read frozen pre-change candidate: %v", err)
	}
	if len(oldBytes) == 0 {
		t.Fatal("frozen pre-change candidate is empty")
	}
	return string(oldBytes)
}
