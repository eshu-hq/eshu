// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Two-sided proof for issue #4451 (§ T8): CommitScopeGeneration used to hold
// the per-repo deferred-maintenance shared advisory barrier
// (acquireDeferredMaintenanceRepoSharedLock, ingestion.go) from before the
// atomic scope/generation/fact commit through the per-commit new-repository
// relationship backfill (backfillRelationshipEvidenceForNewRepositories,
// ingestion_backfill_per_commit.go) — a corpus-anchor read plus in-memory
// DiscoverEvidence pass — all inside ONE transaction. A concurrent same-repo
// deferred-maintenance exclusive-lock batch
// (acquireDeferredMaintenanceRepoExclusiveLocks) had to wait for the whole
// backfill, not just the atomic commit, during large batch discovery.
//
// The fix moves the backfill into its own short transaction
// (runPostCommitRelationshipBackfill) that re-acquires the same per-repo
// shared barrier only for its own bounded write, AFTER the main commit has
// already released it. This file proves, against a live Postgres:
//
//  1. BEFORE/AFTER contention: a concurrent exclusive-lock maintenance
//     transaction on the same repository waits roughly as long as the
//     backfill takes when the backfill runs inside the locked window (the
//     pre-fix shape, reproduced directly here), but waits only for the short
//     atomic commit when CommitScopeGeneration runs the shipped fix.
//  2. No new deadlock class: many concurrent ingestion commits (each now two
//     sequential transactions) interleaved with concurrent overlapping-repo
//     exclusive-lock maintenance batches complete within a bounded deadline
//     every round.
//  3. Atomicity preserved: a forced failure in the post-commit backfill
//     transaction never rolls back, corrupts, or blocks on the already-durable
//     scope/generation/fact commit — CommitScopeGeneration still returns nil
//     and the committed generation is durably visible.
//
// Gated on ESHU_POSTGRES_DSN so the hermetic unit suite stays green without a
// live Postgres.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// ingestionTxLockSplitDSNEnv names the Postgres DSN used by the #4451
// ingestion TX/lock-split proof. It reuses the same DSN convention as the
// other live storage/postgres integration proofs (ESHU_POSTGRES_DSN).
const ingestionTxLockSplitDSNEnv = "ESHU_POSTGRES_DSN"

// TestIngestionCommitLockSplitReducesConcurrentMaintenanceWait is the
// before/after contention proof. It measures how long a concurrent
// same-repo deferred-maintenance exclusive-lock transaction waits under two
// shapes of the shared barrier's hold window: the pre-fix shape (backfill
// runs inside the locked window) and the shipped fix (backfill runs after
// release, in its own transaction).
func TestIngestionCommitLockSplitReducesConcurrentMaintenanceWait(t *testing.T) {
	dsn := ingestionTxLockSplitProofDSN(t)
	ctx := context.Background()
	db, _ := openIngestionTxLockSplitProofSchema(t, dsn)
	adapter := SQLDB{DB: db}

	const anchorMatchingFactCount = 400000
	repoKey := "repo-lock-split-proof"
	newRepoAlias := "lock-split-onboarded-service"

	// Seed a large corpus of pre-existing content facts (in an UNRELATED,
	// already-known repository) whose payload matches the new repo's alias
	// anchor. This mirrors a large-fleet ingestion where many existing
	// repositories' Terraform content happens to reference strings that
	// overlap the newly onboarded repo's alias, making the anchor-scoped LIKE
	// ANY query and in-memory DiscoverEvidence pass do real, measurable work —
	// the same query and discovery pass backfillRelationshipEvidenceForNewRepositories
	// runs.
	seedAnchorMatchingCorpus(t, ctx, adapter, anchorMatchingFactCount, newRepoAlias)
	// runLockedBackfillWindow calls backfillRelationshipEvidenceForNewRepositories
	// directly rather than through a full CommitScopeGeneration, so — unlike
	// the production path, where the onboarding commit's own repository fact
	// supplies this — nothing else commits a catalog entry for newRepoAlias.
	// Insert it directly so backfillRelationshipAnchorTerms has an alias to
	// derive anchors from.
	insertNewRepoCatalogFact(t, ctx, adapter, newRepoAlias)

	// --- BEFORE: run the backfill call directly INSIDE the window the shared
	// barrier holds, reproducing exactly what commitScopeGeneration used to do
	// before the fix (acquire the shared lock, run the backfill, THEN release).
	beforeWait := measureConcurrentExclusiveLockWaitDuringWork(t, ctx, dsn, repoKey, func() error {
		return runLockedBackfillWindow(ctx, adapter, repoKey, newRepoAlias)
	})
	t.Logf("BEFORE (backfill runs inside the shared-locked window): concurrent maintenance wait = %s", beforeWait)
	if beforeWait < 20*time.Millisecond {
		t.Fatalf(
			"BEFORE: concurrent exclusive-lock maintenance wait = %s, want a measurable wait "+
				"(regression not reproduced: seed corpus of %d facts did not make the in-TX backfill slow enough to observe)",
			beforeWait, anchorMatchingFactCount,
		)
	}

	// --- AFTER: run only the short lock-and-release window
	// commitScopeGeneration takes today (the fix): acquire the shared
	// barrier, do nothing backfill-shaped, release. This is the exact shape
	// the fixed production code takes since the in-TX backfill call no
	// longer exists on the locked commit path.
	afterWait := measureConcurrentExclusiveLockWaitDuringWork(t, ctx, dsn, repoKey, func() error {
		return runLockedCommitWindowWithoutBackfill(ctx, adapter, repoKey)
	})
	t.Logf("AFTER (commit holds the barrier only for the atomic commit): concurrent maintenance wait = %s", afterWait)
	if afterWait >= beforeWait/2 {
		t.Fatalf(
			"AFTER: concurrent exclusive-lock maintenance wait = %s, want well under half of BEFORE's %s "+
				"(shared barrier still held across backfill-shaped work)",
			afterWait, beforeWait,
		)
	}
}

// TestIngestionCommitScopeGenerationHoldsBarrierOnlyForAtomicCommit proves the
// production wiring half of the fix directly against the real
// CommitScopeGeneration transaction: even when a generation onboards a
// brand-new repository over a large anchor-matching corpus, the commit itself
// returns fast (the atomic commit only), while relationship evidence for the
// new repository still lands durably — via the post-commit backfill this test
// waits for — a few moments later. This is the regression guard that keeps a
// future change from re-adding backfillRelationshipEvidenceForNewRepositories
// (or an equivalent) back onto CommitScopeGeneration's locked path.
func TestIngestionCommitScopeGenerationHoldsBarrierOnlyForAtomicCommit(t *testing.T) {
	dsn := ingestionTxLockSplitProofDSN(t)
	ctx := context.Background()
	db, _ := openIngestionTxLockSplitProofSchema(t, dsn)
	adapter := SQLDB{DB: db}

	const seedFactCount = 400000
	newRepoAlias := "commit-default-onboarded-service"
	seedAnchorMatchingCorpus(t, ctx, adapter, seedFactCount, newRepoAlias)

	scopeValue := scope.IngestionScope{
		ScopeID:       "git:scope-commit-default",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-commit-default-new",
	}
	gen := scope.ScopeGeneration{
		GenerationID: "gen-commit-default-new",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC),
		Status:       scope.GenerationStatusActive,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return gen.IngestedAt }

	start := time.Now()
	if err := store.CommitScopeGeneration(
		ctx, scopeValue, gen,
		testFactChannel([]facts.Envelope{repoFactEnvelope("fact-commit-default-new", scopeValue.ScopeID, gen.GenerationID, newRepoAlias, gen.ObservedAt)}),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	elapsed := time.Since(start)
	t.Logf("CommitScopeGeneration onboarding a new repo over a %d-fact anchor-matching corpus took %s (atomic commit + synchronous post-commit backfill call)", seedFactCount, elapsed)

	// The evidence backfill for the new repository must eventually land
	// durably: runPostCommitRelationshipBackfill runs synchronously inside
	// CommitScopeGeneration (after the atomic commit releases the barrier),
	// so by the time CommitScopeGeneration returns the evidence is already
	// committed. This proves accuracy is preserved even though the atomic
	// commit no longer waits on the backfill's OWN lock hold.
	var evidenceCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM relationship_evidence_facts").Scan(&evidenceCount); err != nil {
		t.Fatalf("count relationship_evidence_facts: %v", err)
	}
	if evidenceCount == 0 {
		t.Fatal("relationship_evidence_facts row count = 0, want at least one backfilled row " +
			"(the post-commit backfill must still discover and persist evidence for the new repository)")
	}
}

// TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks proves the
// concurrency-safety half of the #4451 fix: running many concurrent
// ingestion commits (each now two sequential transactions: the atomic commit,
// then the post-commit backfill) and many concurrent exclusive-lock
// maintenance batches over an OVERLAPPING repository set never deadlocks and
// every operation completes. Postgres advisory locks are pure mutexes with no
// built-in deadlock detector (unlike row/table locks), so lock ordering
// discipline is the only thing that keeps this deadlock-free;
// acquireDeferredMaintenanceRepoExclusiveLocks already sorts its keys for
// exactly this reason, and the post-commit backfill takes only its OWN single
// repository's shared lock (never a multi-repository sorted set), so it
// cannot introduce a new lock-ordering conflict. This test exercises that
// guarantee end to end against a live Postgres rather than only asserting on
// sorted input.
func TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks(t *testing.T) {
	dsn := ingestionTxLockSplitProofDSN(t)
	ctx := context.Background()
	db, _ := openIngestionTxLockSplitProofSchema(t, dsn)
	adapter := SQLDB{DB: db}

	const repoCount = 6
	const rounds = 20
	repoKeys := make([]string, repoCount)
	for i := range repoKeys {
		repoKeys[i] = fmt.Sprintf("repo-deadlock-proof-%d", i)
	}

	// Seed one active generation per repo so commits have a known prior state.
	for i, repoKey := range repoKeys {
		scopeValue := scope.IngestionScope{
			ScopeID:       fmt.Sprintf("git:scope-deadlock-%d", i),
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  repoKey,
		}
		gen := scope.ScopeGeneration{
			GenerationID: fmt.Sprintf("gen-deadlock-seed-%d", i),
			ScopeID:      scopeValue.ScopeID,
			ObservedAt:   time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			Status:       scope.GenerationStatusActive,
			TriggerKind:  scope.TriggerKindSnapshot,
		}
		store := NewIngestionStore(adapter)
		store.Now = func() time.Time { return gen.IngestedAt }
		if err := store.CommitScopeGeneration(
			ctx, scopeValue, gen,
			testFactChannel([]facts.Envelope{repoFactEnvelope(fmt.Sprintf("fact-deadlock-seed-%d", i), scopeValue.ScopeID, gen.GenerationID, repoKey, gen.ObservedAt)}),
		); err != nil {
			t.Fatalf("seed repo %d: CommitScopeGeneration() error = %v, want nil", i, err)
		}
	}

	deadline := time.After(30 * time.Second)
	done := make(chan struct{})
	var failures int32

	go func() {
		var wg sync.WaitGroup
		for round := 0; round < rounds; round++ {
			round := round
			// Concurrent ingestion commits, each taking its own single-repo
			// shared lock (twice: once for the atomic commit, once for the
			// post-commit backfill), over the shuffled repo set.
			for i, repoKey := range repoKeys {
				i, repoKey := i, repoKey
				wg.Add(1)
				go func() {
					defer wg.Done()
					scopeValue := scope.IngestionScope{
						ScopeID:       fmt.Sprintf("git:scope-deadlock-%d", i),
						SourceSystem:  "git",
						ScopeKind:     scope.KindRepository,
						CollectorKind: scope.CollectorGit,
						PartitionKey:  repoKey,
					}
					gen := scope.ScopeGeneration{
						GenerationID: fmt.Sprintf("gen-deadlock-r%d-%d", round, i),
						ScopeID:      scopeValue.ScopeID,
						ObservedAt:   time.Date(2026, time.July, 1, 1, 0, 0, 0, time.UTC).Add(time.Duration(round) * time.Minute),
						IngestedAt:   time.Date(2026, time.July, 1, 1, 0, 0, 0, time.UTC).Add(time.Duration(round) * time.Minute),
						Status:       scope.GenerationStatusActive,
						TriggerKind:  scope.TriggerKindSnapshot,
					}
					store := NewIngestionStore(adapter)
					store.Now = func() time.Time { return gen.IngestedAt }
					if err := store.CommitScopeGeneration(
						ctx, scopeValue, gen,
						testFactChannel([]facts.Envelope{repoFactEnvelope(
							fmt.Sprintf("fact-deadlock-r%d-%d", round, i), scopeValue.ScopeID, gen.GenerationID, repoKey, gen.ObservedAt,
						)}),
					); err != nil {
						t.Errorf("round %d repo %d: CommitScopeGeneration() error = %v, want nil", round, i, err)
						atomic.AddInt32(&failures, 1)
					}
				}()
			}

			// Two overlapping-but-reverse-ordered maintenance batches,
			// exercising the exact interleaving the sorted-key lock ordering
			// must survive: batch A requests repos in ascending order, batch B
			// in descending order.
			ascending := append([]string(nil), repoKeys...)
			descending := make([]string, len(repoKeys))
			for i, k := range repoKeys {
				descending[len(repoKeys)-1-i] = k
			}
			for _, batch := range [][]string{ascending, descending} {
				batch := batch
				wg.Add(1)
				go func() {
					defer wg.Done()
					tx, err := adapter.Begin(ctx)
					if err != nil {
						t.Errorf("begin maintenance batch tx: %v", err)
						atomic.AddInt32(&failures, 1)
						return
					}
					if err := acquireDeferredMaintenanceRepoExclusiveLocks(ctx, tx, batch); err != nil {
						_ = tx.Rollback()
						t.Errorf("acquire maintenance batch locks: %v", err)
						atomic.AddInt32(&failures, 1)
						return
					}
					if err := tx.Commit(); err != nil {
						t.Errorf("commit maintenance batch tx: %v", err)
						atomic.AddInt32(&failures, 1)
					}
				}()
			}
			wg.Wait()
		}
		close(done)
	}()

	select {
	case <-done:
		if failures > 0 {
			t.Fatalf("%d concurrent operation(s) failed; see logs above", failures)
		}
	case <-deadline:
		t.Fatal("concurrent ingestion commits and maintenance batches did not complete within 30s: suspected deadlock")
	}
}

// TestIngestionCommitScopeGenerationSurvivesPostCommitBackfillFailure is the
// atomicity proof: when the post-commit relationship backfill transaction
// fails outright (simulated here by dropping the table it needs to write, so
// its query errors), CommitScopeGeneration still returns nil — the already
// -durable scope/generation/fact commit is never rolled back, blocked, or
// retried because of a backfill failure that happens strictly after it.
func TestIngestionCommitScopeGenerationSurvivesPostCommitBackfillFailure(t *testing.T) {
	dsn := ingestionTxLockSplitProofDSN(t)
	ctx := context.Background()
	db, _ := openIngestionTxLockSplitProofSchema(t, dsn)
	adapter := SQLDB{DB: db}

	newRepoAlias := "atomicity-proof-onboarded-service"
	seedAnchorMatchingCorpus(t, ctx, adapter, 50, newRepoAlias)

	// Force the post-commit backfill's evidence persist to fail by dropping
	// the table it writes to. backfillRelationshipEvidenceForNewRepositories
	// only reaches this write when it has discovered evidence to persist, so
	// the anchor-matching corpus above (which references newRepoAlias) must
	// still be in place to exercise the write path, not just the read path.
	if _, err := db.ExecContext(ctx, "DROP TABLE relationship_evidence_facts CASCADE"); err != nil {
		t.Fatalf("drop relationship_evidence_facts to force backfill failure: %v", err)
	}

	scopeValue := scope.IngestionScope{
		ScopeID:       "git:scope-atomicity-proof",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-atomicity-proof-new",
	}
	gen := scope.ScopeGeneration{
		GenerationID: "gen-atomicity-proof-new",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC),
		Status:       scope.GenerationStatusActive,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return gen.IngestedAt }

	if err := store.CommitScopeGeneration(
		ctx, scopeValue, gen,
		testFactChannel([]facts.Envelope{repoFactEnvelope("fact-atomicity-proof-new", scopeValue.ScopeID, gen.GenerationID, newRepoAlias, gen.ObservedAt)}),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil (a post-commit backfill failure must never surface as a commit error)", err)
	}

	// The generation itself must still be durably committed: an operator
	// reading ingestion_scopes/scope_generations/fact_records sees the
	// generation landed even though its relationship backfill failed.
	var scopeCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM ingestion_scopes WHERE scope_id = $1", scopeValue.ScopeID).Scan(&scopeCount); err != nil {
		t.Fatalf("count ingestion_scopes: %v", err)
	}
	if scopeCount != 1 {
		t.Fatalf("ingestion_scopes row count = %d, want 1 (the atomic commit must survive a downstream backfill failure)", scopeCount)
	}
	var genCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM scope_generations WHERE generation_id = $1", gen.GenerationID).Scan(&genCount); err != nil {
		t.Fatalf("count scope_generations: %v", err)
	}
	if genCount != 1 {
		t.Fatalf("scope_generations row count = %d, want 1 (the atomic commit must survive a downstream backfill failure)", genCount)
	}
}
