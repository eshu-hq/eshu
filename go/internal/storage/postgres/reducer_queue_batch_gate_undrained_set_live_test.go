// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason eshu-hq/eshu#11380 the batch-claim gate set and the checkpoint family read name the same undrained generations: both list the checkpoint lasts and the orphaned older window of an incomplete year while excluding ordinary rows, terminal windows, completed years, and other runs, so a re-split of either definition fails; disposable PostgreSQL through postgresproof with the production batch claim query and the shared set
//eshu:readiness:env ESHU_REDUCER_GATE_PRIORITY_PROOF_DISPOSABLE ESHU_REDUCER_GATE_PRIORITY_PROOF_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
)

// TestClaimGateAndCheckpointNameTheSameUndrainedSetLive is the
// eshu-hq/eshu#11380 mutant-killer: it runs the shipped gate CTE standalone
// and reads the shared family set through the checkpoint store on one
// fixture, then asserts both name exactly the same generations. Either side
// re-splitting its definition (dropping the older-window arm, dropping the
// family read, or narrowing a fence on one side only) breaks the equality.
func TestClaimGateAndCheckpointNameTheSameUndrainedSetLive(t *testing.T) {
	t.Parallel()

	db := openGatePriorityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	now := time.Date(2026, time.November, 19, 22, 0, 0, 0, time.UTC)
	const (
		runID       = "test-gate-shared-set"
		scopeOrd    = "vuln-intel://test-gate-shared-set/ordinary"
		genOrd      = "gen:gate-shared-set-ord"
		scopeOld    = "vuln-intel://nvd/_baseline/test-gate-shared-set/2026/1767225600000000"
		genOld      = "vulnerability_intelligence:test-gate-shared-set:old"
		scopeNew    = "vuln-intel://nvd/_baseline/test-gate-shared-set/2026/1781691075297000"
		genNew      = "vulnerability_intelligence:test-gate-shared-set:new"
		scopeDon    = "vuln-intel://nvd/_baseline/test-gate-shared-set/2026/1781691300187000"
		genDon      = "vulnerability_intelligence:test-gate-shared-set:done"
		scope25     = "vuln-intel://nvd/_baseline/test-gate-shared-set/2025/1735689600000000"
		gen25       = "vulnerability_intelligence:test-gate-shared-set:y2025"
		scope25O    = "vuln-intel://nvd/_baseline/test-gate-shared-set/2025/1733011200000000"
		gen25O      = "vulnerability_intelligence:shared-set:y2025-old"
		scopeOth    = "vuln-intel://nvd/_baseline/test-gate-shared-set-other/2026/1767225600000000"
		genOth      = "vulnerability_intelligence:test-gate-shared-set:other"
		scopeOthOld = "vuln-intel://nvd/_baseline/test-gate-shared-set-other/2026/1735689600000000"
		genOthOld   = "vulnerability_intelligence:test-gate-shared-set:other-old"
	)
	// Incomplete year: the checkpoint moved to the newer window while the
	// older window's reducer never ran.
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_shared_ord", scopeOrd, genOrd, now.Add(-4*time.Hour), "pending", nil)
	seedGatePriorityCheckpoint(t, ctx, db, runID, 2026, false, scopeNew, genNew, now.Add(-30*time.Minute))
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_shared_old", scopeOld, genOld, now.Add(-2*time.Hour), "pending", nil)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_shared_new", scopeNew, genNew, now.Add(-time.Hour), "pending", nil)
	// A terminal window of the same family never joins either set.
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_shared_done", scopeDon, genDon, now.Add(-3*time.Hour), "succeeded", nil)
	// A completed year with a live older window drops out of both sets.
	seedGatePriorityCheckpoint(t, ctx, db, runID, 2025, true, scope25, gen25, now.Add(-time.Hour))
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_shared_25old", scope25O, gen25O, now.Add(-2*time.Hour), "pending", nil)
	// Another run's last chunk is waited on by its own year (the gate
	// covers every incomplete year's last), but its window never enters
	// this run's family set.
	seedGatePriorityCheckpoint(t, ctx, db, "test-gate-shared-set-other", 2026, false, scopeOth, genOth, now.Add(-time.Hour))
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_shared_other", scopeOth, genOth, now.Add(-2*time.Hour), "pending", nil)
	// The other run's own older window joins the gate (every run's
	// incomplete family) but never this run's family set.
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_shared_other_old", scopeOthOld, genOthOld, now.Add(-90*time.Minute), "pending", nil)

	gate := undrainedGateGenerations(t, ctx, db)
	family, err := vulnerabilitystore.NewNVDBaselineCheckpointStore(SQLDB{DB: db}).
		ReadNVDBaselineUndrainedGenerations(ctx, runID)
	if err != nil {
		t.Fatalf("ReadNVDBaselineUndrainedGenerations() error = %v", err)
	}
	familyGens := map[string]bool{}
	for _, windows := range family {
		for _, window := range windows {
			familyGens[window.GenerationID] = true
		}
	}
	wantFamily := map[string]bool{genOld: true}
	if len(familyGens) != len(wantFamily) {
		t.Fatalf("family generations = %v, want %v", keysOf(familyGens), keysOf(wantFamily))
	}
	for gen := range wantFamily {
		if !familyGens[gen] {
			t.Fatalf("family generations = %v, want %v", keysOf(familyGens), keysOf(wantFamily))
		}
	}
	// The gate set is the checkpoint lasts of incomplete years plus the
	// family set; both sides must agree on the union. The other run's
	// last chunk joins through its own year's checkpoint arm.
	wantGate := map[string]bool{genNew: true, genOld: true, genOth: true, genOthOld: true}
	if len(gate) != len(wantGate) {
		t.Fatalf("gate generations = %v, want %v", keysOf(gate), keysOf(wantGate))
	}
	for gen := range wantGate {
		if !gate[gen] {
			t.Fatalf("gate generations = %v, want %v", keysOf(gate), keysOf(wantGate))
		}
	}
	for gen := range familyGens {
		if !gate[gen] {
			t.Fatalf("family generation %s is missing from the gate set: the two definitions drifted", gen)
		}
	}
}

// undrainedGateGenerations runs the shipped gate_waited_generations CTE
// standalone, sliced verbatim from the batch claim query the way the
// rejoin guard derives its candidate SELECT: the slice is a byte-prefix of
// production, never a hand copy, so it cannot drift from the shipped
// definition.
func undrainedGateGenerations(t *testing.T, ctx context.Context, db *sql.DB) map[string]bool {
	t.Helper()
	body := cteBody(claimReducerWorkBatchQuery,
		"gate_waited_generations AS MATERIALIZED (", "),\nlocked_search_doc_reserved AS MATERIALIZED (")
	if body == "" {
		t.Fatal("could not slice the gate set CTE from the shipped batch claim")
	}
	// The slice ends before the CTE's closing paren (the end marker
	// opens on it), so close it here before the outer SELECT.
	rows, err := db.QueryContext(ctx, "WITH "+body+") SELECT waited_generation_id FROM gate_waited_generations ORDER BY 1")
	if err != nil {
		t.Fatalf("run shipped gate set standalone: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]bool{}
	for rows.Next() {
		var generation string
		if err := rows.Scan(&generation); err != nil {
			t.Fatalf("scan gate generation: %v", err)
		}
		got[generation] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate gate generations: %v", err)
	}
	return got
}
