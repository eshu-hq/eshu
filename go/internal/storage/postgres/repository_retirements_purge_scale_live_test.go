// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// seedPurgeScaleScope writes one scope with generationCount superseded
// generations and factsPerGeneration file facts each, in multi-row INSERT
// batches. Everything is terminal (superseded/completed) so the fence
// terminal gate passes and the measurement isolates purge cost, not drain
// waiting.
func seedPurgeScaleScope(t *testing.T, ctx context.Context, database SQLDB, partition, scopeID string, generationCount, factsPerGeneration int) {
	t.Helper()
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		 observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1, 'repository', 'git', $2, 'git_collector', $3, NOW(), NOW(), 'active', NULL, '{}'::jsonb)`,
		scopeID, "key-"+scopeID, partition); err != nil {
		t.Fatalf("seed scope %s: %v", scopeID, err)
	}
	const batch = 500
	for start := 0; start < generationCount; start += batch {
		end := start + batch
		if end > generationCount {
			end = generationCount
		}
		var values []string
		var args []any
		for i := start; i < end; i++ {
			values = append(values, fmt.Sprintf("($%d, $%d, 'push', NOW(), NOW(), 'superseded', '{}'::jsonb)", len(args)+1, len(args)+2))
			args = append(args, fmt.Sprintf("%s-gen-%05d", scopeID, i), scopeID)
		}
		if _, err := database.DB.ExecContext(ctx, `INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
			VALUES `+strings.Join(values, ","), args...); err != nil {
			t.Fatalf("seed generations %s [%d:%d]: %v", scopeID, start, end, err)
		}
	}
	// Facts batch by row count, not generation count: 500 rows per
	// statement stays far under the 65,535 extended-protocol parameter
	// limit at any scale.
	const factBatch = 500
	var values []string
	var args []any
	flushFacts := func() {
		if len(values) == 0 {
			return
		}
		if _, err := database.DB.ExecContext(ctx, `INSERT INTO fact_records
			(fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
			 source_fact_key, observed_at, ingested_at, payload)
			VALUES `+strings.Join(values, ","), args...); err != nil {
			t.Fatalf("seed facts %s (%d rows): %v", scopeID, len(values), err)
		}
		values = nil
		args = nil
	}
	for i := 0; i < generationCount; i++ {
		genID := fmt.Sprintf("%s-gen-%05d", scopeID, i)
		for f := 0; f < factsPerGeneration; f++ {
			values = append(values, fmt.Sprintf("($%d, $%d, $%d, 'file', $%d, 'git', $%d, NOW(), NOW(), '{}'::jsonb)",
				len(args)+1, len(args)+2, len(args)+3, len(args)+4, len(args)+5))
			args = append(args,
				fmt.Sprintf("%s-fact-%05d-%04d", scopeID, i, f),
				scopeID, genID,
				fmt.Sprintf("%s-%05d-%04d", scopeID, i, f),
				fmt.Sprintf("src-%05d-%04d", i, f))
			if len(values) >= factBatch {
				flushFacts()
			}
		}
	}
	flushFacts()
	if _, err := database.DB.ExecContext(ctx, `INSERT INTO fact_work_items
		(work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
		VALUES ($1, $2, $3, 'reducer', 'd1', 'superseded', NOW(), NOW())`,
		"scale-w-"+scopeID, scopeID, fmt.Sprintf("%s-gen-%05d", scopeID, 0)); err != nil {
		t.Fatalf("seed work row %s: %v", scopeID, err)
	}
}

// purgeScaleResult is one measured purge at one scale.
type purgeScaleResult struct {
	generations int
	facts       int
	fenceWall   time.Duration
	purgeWall   time.Duration
}

// runPurgeScale fences and purges one repository and reports the walls.
// The caller supplies long-lived contexts: seeding and purging at scale
// exceed the shared 90-second proof deadline by design.
func runPurgeScale(t *testing.T, ctx context.Context, database SQLDB, partition string, scopes []string, retirementID string) purgeScaleResult {
	t.Helper()
	result := purgeScaleResult{}
	for _, scopeID := range scopes {
		var gens, facts int64
		if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM scope_generations WHERE scope_id = $1`, scopeID).Scan(&gens); err != nil {
			t.Fatalf("count generations: %v", err)
		}
		if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM fact_records WHERE scope_id = $1`, scopeID).Scan(&facts); err != nil {
			t.Fatalf("count facts: %v", err)
		}
		result.generations += int(gens)
		result.facts += int(facts)
	}
	store := NewRetirementStore(database)
	if _, err := store.InsertPending(ctx, RetirementRequest{
		RetirementID: retirementID, RepoID: partition, ScopeID: scopes[0],
		ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
		IdempotencyKeyHash: "scale-key-" + retirementID,
	}); err != nil {
		t.Fatalf("InsertPending: %v", err)
	}
	fencer := NewRetirementFencer(database, database)
	fenceStart := time.Now()
	fenceResult, err := fencer.FenceRepository(ctx, FenceRequest{
		Retirements:        []FenceRetirement{{RetirementID: retirementID, RepoID: partition, ScopeID: scopes[0]}},
		ReasonCode:         RetirementReasonOperatorRetired,
		ActorClass:         "operator",
		IdempotencyKeyHash: "scale-key-" + retirementID,
	})
	if err != nil {
		t.Fatalf("FenceRepository: %v", err)
	}
	result.fenceWall = time.Since(fenceStart)
	if len(fenceResult.ScopeIDs) != len(scopes) {
		t.Fatalf("fenced scopes = %v, want %v", fenceResult.ScopeIDs, scopes)
	}
	purger := NewRetirementPurger(database, database)
	purgeStart := time.Now()
	purgeResult, err := purger.PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{partition}})
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	result.purgeWall = time.Since(purgeStart)
	if len(purgeResult.Completed) != 1 || purgeResult.Completed[0] != partition {
		t.Fatalf("purge result = %+v, want [%s] completed", purgeResult, partition)
	}
	var state, phase, ledger string
	if err := database.DB.QueryRowContext(ctx, `SELECT state, phase, rows_deleted::text
		FROM repository_retirements WHERE retirement_id = $1`, retirementID).Scan(&state, &phase, &ledger); err != nil {
		t.Fatalf("tombstone readback: %v", err)
	}
	if state != string(RetirementStateComplete) || phase != string(RetirementPhaseDone) {
		t.Fatalf("tombstone = %s/%s, want complete/done", state, phase)
	}
	t.Logf("tombstone ledger: %s", ledger)
	for _, scopeID := range scopes {
		var remaining int64
		if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM scope_generations WHERE scope_id = $1`, scopeID).Scan(&remaining); err != nil {
			t.Fatalf("remaining generations: %v", err)
		}
		if remaining != 0 {
			t.Fatalf("scope %s still holds %d generations after purge", scopeID, remaining)
		}
		if err := database.DB.QueryRowContext(ctx, `SELECT count(*) FROM fact_records WHERE scope_id = $1`, scopeID).Scan(&remaining); err != nil {
			t.Fatalf("remaining facts: %v", err)
		}
		if remaining != 0 {
			t.Fatalf("scope %s still holds %d facts after purge", scopeID, remaining)
		}
	}
	return result
}

// TestRetirementPurgeScaleMeasurementLive is the #8659 AC8 measurement:
// the one-transaction purge at the ADR-2248 reference batch (100
// generations) and at the observed retirement scale (3,280 generations,
// ~328k facts). It asserts completion and exact removal, and logs both
// walls so the batching decision rests on measured scaling, not on the
// shape of the code. The verdict lives in the #8659 evidence note, not in
// a wall-time assertion: walls are environment-dependent, removal is not.
func TestRetirementPurgeScaleMeasurementLive(t *testing.T) {
	database, _ := openIsolatedLiveDB(t, "retire_scale",
		"set ESHU_POSTGRES_DSN to run the retirement purge scale measurement")
	sqlDB := SQLDB{DB: database}
	seedCtx, seedCancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer seedCancel()
	opCtx, opCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer opCancel()

	seedPurgeScaleScope(t, seedCtx, sqlDB, "PARTITION:scale-small", "scale-small-main", 100, 100)
	small := runPurgeScale(t, opCtx, sqlDB, "PARTITION:scale-small", []string{"scale-small-main"}, "rr_scale_small")
	t.Logf("small: generations=%d facts=%d fence=%s purge=%s (%.1fms/gen)",
		small.generations, small.facts, small.fenceWall, small.purgeWall,
		float64(small.purgeWall.Milliseconds())/float64(small.generations))

	seedPurgeScaleScope(t, seedCtx, sqlDB, "PARTITION:scale-large", "scale-large-main", 1640, 100)
	seedPurgeScaleScope(t, seedCtx, sqlDB, "PARTITION:scale-large", "scale-large-ref", 1640, 100)
	large := runPurgeScale(t, opCtx, sqlDB, "PARTITION:scale-large",
		[]string{"scale-large-main", "scale-large-ref"}, "rr_scale_large")
	t.Logf("large: generations=%d facts=%d fence=%s purge=%s (%.1fms/gen)",
		large.generations, large.facts, large.fenceWall, large.purgeWall,
		float64(large.purgeWall.Milliseconds())/float64(large.generations))
	t.Logf("scaling: %.1fx generations, %.1fx purge wall",
		float64(large.generations)/float64(small.generations),
		float64(large.purgeWall)/float64(small.purgeWall))
}
