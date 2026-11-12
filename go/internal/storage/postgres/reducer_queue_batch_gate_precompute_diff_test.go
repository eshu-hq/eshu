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

// TestClaimBatchGatePrecomputeMatchesPreChangeCandidateSetAndOrder is the C-1
// differential proof for #10565 step 1: the precomputed-gate candidate SELECT
// (derived from the shipped query at test time) must return the IDENTICAL
// work_item_id set, in the IDENTICAL order, as the pre-change per-row-gate
// candidate SELECT (frozen in testdata), across the $5/$6/$7 bypass/cap
// matrix and two $1 clock positions.
//
// The fixture exercises every gate the rewrite precomputes:
//
//   - projector drain fence ($5): a scope with live projector work.
//   - semantic caps ($6/$7): pending semantic rows under a live-inflight cap,
//     with the live leases expiring between the two clock positions (so the
//     late clock also covers the empty semantic-inflight set).
//   - readiness gate: a readiness-gated domain row with its phase committed
//     (claimable) and one without (blocked).
//   - conflict fence + expired holder: an expired-holder key shared with an
//     older pending sibling, where the holder lease is live at the early
//     clock and expired at the late clock.
//   - clock edges: a pending row whose visible_at and a claimed row whose
//     claim_until fall between the two $1 positions.
//
// Skipped unless a DSN is provided, like the other claim differentials.
func TestClaimBatchGatePrecomputeMatchesPreChangeCandidateSetAndOrder(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the gate-precompute differential proof")
	}

	oldQuery := frozenPreChangeCandidate(t)
	for _, want := range []string{"FROM fact_work_items AS projector_work", "FROM fact_work_items AS semantic_next"} {
		if !strings.Contains(oldQuery, want) {
			t.Fatalf("frozen pre-change candidate lost its per-row gate %q; it no longer represents the pre-#10565 query", want)
		}
	}
	newQuery, ok := deriveReadOnlyCandidateSelect(claimReducerWorkBatchQuery)
	if !ok {
		t.Fatal("could not derive read-only candidate SELECT from the shipped batch claim query")
	}
	for _, gone := range []string{"FROM fact_work_items AS projector_work", "FROM fact_work_items AS semantic_next"} {
		if strings.Contains(newQuery, gone) {
			t.Fatalf("derived candidate still has per-row gate %q; the rewrite is not in the shipped query", gone)
		}
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)
	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	seedGatePrecomputeFixture(t, ctx, db, now)

	clockA := now.Add(2 * time.Hour)
	clockB := now.Add(4 * time.Hour)

	type cell struct {
		name   string
		clock  time.Time
		filter []string
		drain  bool
		projN  int
		semCap int
	}
	var cells []cell
	for _, tc := range []struct {
		name  string
		clock time.Time
	}{
		{"clockA", clockA},
		{"clockB", clockB},
	} {
		for _, drain := range []bool{false, true} {
			for _, projN := range []int{0, 2} {
				for _, semCap := range []int{0, 3} {
					cells = append(cells, cell{
						name:   fmt.Sprintf("%s/drain=%v/proj=%d/sem=%d", tc.name, drain, projN, semCap),
						clock:  tc.clock,
						drain:  drain,
						projN:  projN,
						semCap: semCap,
					})
				}
			}
		}
	}
	cells = append(cells, cell{
		name:   "clockA/domain-filter",
		clock:  clockA,
		filter: []string{"supply_chain_impact"},
		drain:  true,
		projN:  2,
		semCap: 3,
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
				c.drain,                // $5 require projector drain
				c.projN,                // $6 expected source-local projectors
				c.semCap,               // $7 semantic entity claim limit
				1000,                   // $8 batch limit (covers the whole fixture)
			}
			oldIDs := queryCandidateWorkItemIDs(t, ctx, db, oldQuery, args)
			newIDs := queryCandidateWorkItemIDs(t, ctx, db, newQuery, args)
			onlyInOld := diffStringSlices(oldIDs, newIDs)
			onlyInNew := diffStringSlices(newIDs, oldIDs)
			if len(onlyInOld) != 0 || len(onlyInNew) != 0 {
				t.Fatalf(
					"gate-precompute candidate set diverges from pre-change set (want 0/0): "+
						"only in OLD = %v, only in NEW = %v\nOLD order = %v\nNEW order = %v",
					onlyInOld, onlyInNew, oldIDs, newIDs,
				)
			}
			if len(oldIDs) != len(newIDs) || fmt.Sprint(oldIDs) != fmt.Sprint(newIDs) {
				t.Fatalf("gate-precompute candidate ORDER diverges from pre-change order:\nOLD = %v\nNEW = %v", oldIDs, newIDs)
			}
			if len(oldIDs) > 0 {
				nonEmpty++
			}
		})
	}
	if nonEmpty == 0 {
		t.Fatal("fixture produced zero candidates in every matrix cell; the differential is vacuous")
	}
	t.Logf("gate-precompute differential green in %d cells (%d non-empty)", len(cells), nonEmpty)
}

// seedGatePrecomputeFixture seeds one disposable database with a row for
// every gate the #10565 step-1 rewrite precomputes. now is the fixture epoch;
// the differential runs at now+2h (clockA) and now+4h (clockB), and the lease
// and visibility edges below fall strictly between those clocks.
func seedGatePrecomputeFixture(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
	t.Helper()
	seedGateScope(t, ctx, db, "scope-gate-diff", "gen-fair", now)
	seedGateScope(t, ctx, db, "scope-gate-proj", "gen-proj", now)

	insert := func(item reducerFairnessWorkItem) {
		t.Helper()
		insertReducerFairnessWorkItem(t, ctx, db, item)
	}

	// Plain pending rows on distinct conflict keys (baseline representatives).
	for i := 0; i < 4; i++ {
		insert(reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("gate-plain-%02d", i), scopeID: "scope-gate-diff", generationID: "gen-fair",
			domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: fmt.Sprintf("gate-plain-key-%02d", i),
			updatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}

	// Expired-holder key: the holder lease (claim_until now+3h) is live at
	// clockA and expired at clockB; the older pending sibling must never jump
	// the fence while the holder is live, and the holder is the
	// representative once its lease expires.
	insert(reducerFairnessWorkItem{
		workItemID: "gate-older-pending", scopeID: "scope-gate-diff", generationID: "gen-fair",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: "gate-holder-key",
		updatedAt: now.Add(-time.Hour),
	})
	insert(reducerFairnessWorkItem{
		workItemID: "gate-holder", scopeID: "scope-gate-diff", generationID: "gen-fair",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: "gate-holder-key",
		updatedAt: now.Add(-30 * time.Minute),
	})
	mustExecGateArgs(t, ctx, db, `UPDATE fact_work_items SET status='claimed', lease_owner='gate-holder', claim_until=$1 WHERE work_item_id='gate-holder'`, now.Add(3*time.Hour))

	// Projector-fenced scope: live projector work on scope-gate-proj fences
	// that scope's reducer rows while $5 is on.
	mustExecGateArgs(t, ctx, db, `INSERT INTO fact_work_items (
	    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
	    conflict_key, status, attempt_count, payload, created_at, updated_at
	) VALUES ('gate-projector-live', 'scope-gate-proj', 'gen-proj', 'projector', 'source_local', 'scope',
	    'scope-gate-proj', 'pending', 0,
	    jsonb_build_object('entity_key', 'gate-projector-live'), $1, $1)`, now)
	for i := 0; i < 2; i++ {
		insert(reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("gate-proj-fenced-%02d", i), scopeID: "scope-gate-proj", generationID: "gen-proj",
			domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: fmt.Sprintf("gate-proj-key-%02d", i),
			updatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}
	// Succeeded source-local projector rows feeding the $6 count.
	for i := 0; i < 2; i++ {
		mustExecGateArgs(t, ctx, db, `INSERT INTO fact_work_items (
		    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
		    conflict_key, status, attempt_count, payload, created_at, updated_at
		) VALUES ($1, 'scope-gate-diff', 'gen-fair', 'projector', 'source_local', 'scope',
		    'scope-gate-diff', 'succeeded', 1,
		    jsonb_build_object('entity_key', $1::text), $2, $2)`, fmt.Sprintf("gate-projector-done-%02d", i), now)
	}

	// Semantic rows: four pending plus two live-inflight leases that expire
	// at now+3h (live at clockA, gone at clockB — the empty-set edge).
	for i := 0; i < 4; i++ {
		insert(reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("gate-sem-%02d", i), scopeID: "scope-gate-diff", generationID: "gen-fair",
			domain: "semantic_entity_materialization", conflictDomain: reducerConflictDomainScope, conflictKey: fmt.Sprintf("gate-sem-key-%02d", i),
			updatedAt: now.Add(time.Duration(20+i) * time.Minute),
		})
	}
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("gate-sem-live-%02d", i)
		insert(reducerFairnessWorkItem{
			workItemID: id, scopeID: "scope-gate-diff", generationID: "gen-fair",
			domain: "semantic_entity_materialization", conflictDomain: reducerConflictDomainScope, conflictKey: fmt.Sprintf("gate-sem-live-key-%02d", i),
			updatedAt: now.Add(-2 * time.Hour),
		})
		mustExecGateArgs(t, ctx, db, `UPDATE fact_work_items SET status='claimed', lease_owner='gate-sem', claim_until=$2 WHERE work_item_id=$1`, id, now.Add(3*time.Hour))
	}

	// Readiness-gated rows: gate-ready has its canonical phase committed,
	// gate-blocked does not.
	insert(reducerFairnessWorkItem{
		workItemID: "gate-ready", scopeID: "scope-gate-diff", generationID: "gen-fair",
		domain: "aws_relationship_materialization", conflictDomain: reducerConflictDomainScope, conflictKey: "gate-ready-key",
		updatedAt: now.Add(5 * time.Minute),
	})
	insert(reducerFairnessWorkItem{
		workItemID: "gate-blocked", scopeID: "scope-gate-diff", generationID: "gen-fair",
		domain: "aws_relationship_materialization", conflictDomain: reducerConflictDomainScope, conflictKey: "gate-blocked-key",
		updatedAt: now.Add(6 * time.Minute),
	})
	mustExecGateArgs(t, ctx, db, `INSERT INTO graph_projection_phase_state (
	    scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase, committed_at, updated_at
	) VALUES ('scope-gate-diff', 'gate-ready', 'gen-fair', 'gen-fair', 'cloud_resource_uid', 'canonical_nodes_committed', $1, $1)`, now)

	// Search-document rows keep the is_search_doc ordering path exercised.
	for i := 0; i < 2; i++ {
		insert(reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("gate-search-%02d", i), scopeID: "scope-gate-diff", generationID: "gen-fair",
			domain: "eshu_search_document", conflictDomain: reducerConflictDomainScope, conflictKey: fmt.Sprintf("gate-search-key-%02d", i),
			updatedAt: now.Add(time.Duration(40+i) * time.Minute),
		})
	}

	// Clock edges between clockA (now+2h) and clockB (now+4h): a pending row
	// visible only at the late clock, and a claimed row live only at the
	// early clock.
	insert(reducerFairnessWorkItem{
		workItemID: "gate-visible-late", scopeID: "scope-gate-diff", generationID: "gen-fair",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: "gate-visible-late-key",
		updatedAt: now,
	})
	mustExecGateArgs(t, ctx, db, `UPDATE fact_work_items SET visible_at=$1 WHERE work_item_id='gate-visible-late'`, now.Add(3*time.Hour))
	insert(reducerFairnessWorkItem{
		workItemID: "gate-lease-edge", scopeID: "scope-gate-diff", generationID: "gen-fair",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope, conflictKey: "gate-lease-edge-key",
		updatedAt: now,
	})
	mustExecGateArgs(t, ctx, db, `UPDATE fact_work_items SET status='claimed', lease_owner='gate-edge', claim_until=$1 WHERE work_item_id='gate-lease-edge'`, now.Add(3*time.Hour))
}

func seedGateScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string, now time.Time) {
	t.Helper()
	mustExecGateArgs(t, ctx, db, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'cloud', 'aws', $1, NULL, 'aws', $1, $2, $2, 'active', $3, '{}'::jsonb)`,
		scopeID, now, generationID)
	mustExecGateArgs(t, ctx, db, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ($1, $2, 'snapshot', 'fair', $3, $3, 'active', $3, NULL, '{}'::jsonb)`,
		generationID, scopeID, now)
}

// frozenPreChangeCandidate reads the machine-derived pre-#10565 candidate
// SELECT frozen in testdata (see that file's header for provenance).
func frozenPreChangeCandidate(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	oldBytes, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "testdata", "claim_candidate_pre_gate_precompute.sql"))
	if err != nil {
		t.Fatalf("read frozen pre-change candidate: %v", err)
	}
	if len(oldBytes) == 0 {
		t.Fatal("frozen pre-change candidate is empty")
	}
	return string(oldBytes)
}

func mustExecGateArgs(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("seed gate-precompute fixture: %v\nquery:\n%s\nargs: %#v", err, query, args)
	}
}
