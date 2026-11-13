// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10020 disposable PostgreSQL 18 proof that five fact inserts started while a chunk predrain batch holds the over-limit generation's row lock all commit instead of deadlocking or being lost: the batch's FOR NO KEY UPDATE does not conflict with a concurrent fact insert's foreign-key check, and the exact final counts prove the inserts landed in no drained page and the generation still converges to a whole prune; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the hard-ceiling proof does. Replaces the #8206 under-lock chunk wait proof, whose under-lock page drain no longer exists.
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// insertCommitBound caps how long the predrain hook waits for the concurrent
// inserts to commit. A lock-strength regression (the batch taking FOR UPDATE
// instead of FOR NO KEY UPDATE) blocks the inserts on the held row lock, so
// the hook — and with it the batch — waits out the bound and fails instead
// of deadlocking silently.
const insertCommitBound = 30 * time.Second

// TestGenerationRetentionChunkPredrainConcurrentInsertLive proves a fact
// insert into a predraining generation commits while the batch holds that
// generation's row lock. The hook fires after the first batch locks the
// chunk target and before it pages facts; it returns only once all five
// tagged inserts have committed, so every insert provably committed under
// the held lock. The inserts sort after the oldest-first page (their
// observed_at is newest), so no insert lands in a drained page, and the
// generation still fits after the drain and prunes whole in the same pass.
func TestGenerationRetentionChunkPredrainConcurrentInsertLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scopeID, generationID = "chunk-insertwait", "chunk-insertwait-g0"
	testfixtures.SeedScope(t, ctx, database, scopeID)
	testfixtures.SeedSupersededGeneration(t, ctx, database, scopeID, generationID, now.Add(-10*24*time.Hour))
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT 'iw-' || lpad(n::text, 5, '0'), $1, $2, 'metric', 'iwk' || n, 'git', 'iwk' || n, now(), now(), '{}'::jsonb
FROM generate_series(1, 1500) AS n`, scopeID, generationID); err != nil {
		t.Fatalf("seed insert-wait facts: %v", err)
	}

	tag := fmt.Sprintf("/* eshu-10020-predrain-insert-wait %d */", time.Now().UnixNano())
	insertSQL := tag + `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'metric', $4, 'git', $4, now(), now(), '{}'::jsonb)`

	const inserts = 5
	var wg sync.WaitGroup
	errs := make(chan error, inserts)
	hookCalls := 0
	hookErr := make(chan error, 1)
	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	store.beforeChunkPredrainPage = func() {
		hookCalls++
		if hookCalls != 1 {
			return
		}
		for i := range inserts {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := database.ExecContext(ctx, insertSQL,
					fmt.Sprintf("iw-new-%d", i), scopeID, generationID, fmt.Sprintf("iwnewk-%d", i))
				errs <- err
			}(i)
		}
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(insertCommitBound):
			hookErr <- fmt.Errorf("timed out waiting for %d concurrent inserts to commit under the predrain lock", inserts)
		}
	}

	result, err := store.PruneSupersededGenerations(ctx, chunkDrainPolicy())
	wg.Wait()
	close(errs)
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	select {
	case err := <-hookErr:
		t.Fatalf("predrain hook: %v", err)
	default:
	}
	if hookCalls != 2 {
		t.Fatalf("beforeChunkPredrainPage ran %d times, want 2 (one 1000-fact page and one 505-fact page)", hookCalls)
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent insert: %v", err)
		}
	}
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1 (the drained generation prunes whole)", result.GenerationsPruned)
	}
	// One 1000-fact page plus one 505-fact page drained, five inserts
	// committed between them: every row is accounted for, so no insert was
	// lost to the predrain and no deadlock stalled either side.
	if got := result.RowsPruned["fact_records"]; got != 1505 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 1505 (1500 seeded + 5 concurrent)", got)
	}
	if facts := countScopeGenerationFacts(t, ctx, database, scopeID, generationID); facts != 0 {
		t.Fatalf("facts after the prune = %d, want 0", facts)
	}
}
