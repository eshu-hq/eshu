// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks is the
// concurrency-safety half of the #4451 lock-split proof (see the package
// overview in ingestion_tx_lock_split_integration_test.go): running many
// concurrent ingestion commits interleaved with concurrent overlapping-repo
// exclusive-lock maintenance batches never deadlocks and every operation
// completes within a bounded deadline. Postgres advisory locks are pure
// mutexes with no built-in deadlock detector (unlike row/table locks), so
// lock ordering discipline is the only thing that keeps this deadlock-free;
// acquireDeferredMaintenanceRepoExclusiveLocks already sorts its keys for
// exactly this reason, and the post-commit backfill takes only its OWN single
// repository's shared lock (never a multi-repository sorted set), so it
// cannot introduce a new lock-ordering conflict.
//
// Split into its own file (500-line cap) from
// ingestion_tx_lock_split_integration_test.go, which keeps the DSN helper,
// the other three proofs in this file's family, and the shared package
// overview comment.

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

// beginCountingDB wraps SQLDB and counts every Begin call through an atomic
// counter, so a concurrency proof can assert transactions were genuinely
// opened rather than only that the test finished without error. It exists
// because runPostCommitRelationshipBackfill's hasNewRepo gate
// (ingestion_backfill_per_commit.go) makes it easy to build proof input that
// never flips the gate: TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks
// originally reused each repoKey as the "repository" fact's own repo_id on
// every round, but the seed loop had already committed that exact repo_id as
// a known catalog entry, so hasNewRepo was false on every single round, the
// post-commit backfill transaction never opened, and the test held the
// shared barrier once per commit while its own doc comment claimed to
// exercise both transactions (PR #5883 review). A begin count is a
// deterministic, non-flaky witness that a *sql.Tx was actually opened, unlike
// sampling pg_stat_activity/pg_locks for a moment that may or may not land
// inside the second transaction's short window.
type beginCountingDB struct {
	SQLDB
	begins *int64
}

func (d beginCountingDB) Begin(ctx context.Context) (Transaction, error) {
	atomic.AddInt64(d.begins, 1)
	return d.SQLDB.Begin(ctx)
}

// TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks exercises the
// deadlock-freedom guarantee end to end against a live Postgres rather than
// only asserting on sorted input. Each commit is genuinely two sequential
// transactions: this test commits a never-before-seen repository alias every
// round specifically so runPostCommitRelationshipBackfill's hasNewRepo gate
// (ingestion_backfill_per_commit.go) is true and its transaction actually
// opens, and asserts the resulting Begin() count per repo slot
// (beginCountingDB) as positive proof — not only that the round loop finished
// without error, which a single-transaction shape would also do.
func TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks(t *testing.T) {
	dsn := ingestionTxLockSplitProofDSN(t)
	ctx := context.Background()
	db, schemaName := openIngestionTxLockSplitProofSchema(t, dsn)
	adapter := SQLDB{DB: db}

	const repoCount = 6
	const rounds = 20
	repoKeys := make([]string, repoCount)
	for i := range repoKeys {
		repoKeys[i] = fmt.Sprintf("repo-deadlock-proof-%d", i)
	}

	// Seed one generation per repo so commits have a known prior state. Status
	// is Pending, matching every real production caller of
	// CommitScopeGeneration (e.g. the git collector's Source, which always
	// submits GenerationStatusPending — see git_source_processing.go):
	// commitScopeGeneration never supersedes a scope's prior active generation
	// itself, only the projector queue's activate/supersede queries do
	// (activateProjectorGenerationQuery / supersedeProjectorActiveGenerationQuery
	// in projector_queue_sql.go), and this test never runs the projector.
	// Seeding (or committing below) as Active writes straight into
	// ingestion_scopes.active_generation_id (activeGenerationID/
	// activeTimestamp in ingestion.go gate on Status == Active) without ever
	// superseding the previous active row, which deterministically violates
	// scope_generations_active_scope_idx on the scope's second commit — a
	// second, latent test-setup defect that the #4451 connection-pinning fix
	// exposed by letting every commit finally reach the INSERT instead of
	// failing earlier on a stray connection with no search_path.
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
			Status:       scope.GenerationStatusPending,
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

	// Each concurrent participant below (one per repo per round for ingestion
	// commits, plus two per round for the ascending/descending maintenance
	// batches) gets its own single-connection handle bound to schemaName,
	// rather than sharing the schema-owner `adapter` above. Postgres advisory
	// locks are session-scoped, and a *sql.DB capped at one connection
	// serializes every goroutine that shares it onto that one physical
	// connection/session — so sharing `adapter` here would make it
	// impossible for two "concurrent" transactions to ever actually contend
	// for the same advisory lock, and this deadlock/lock-ordering proof would
	// pass vacuously without exercising anything. Handles are opened once, up
	// front, on the main test goroutine (t.Fatalf/t.Fatal inside
	// openIngestionTxLockSplitProofClaimerDB is documented unsafe to call
	// from a spawned goroutine) and reused across rounds: each round's
	// wg.Wait() makes rounds strictly sequential even though the
	// repoCount+2 participants within a single round run concurrently, so
	// reusing per-slot handles across rounds does not reintroduce sharing
	// within a round.
	// commitAdapters counts Begin calls per repo slot (commitBeginCounts) so
	// the assertion after the concurrent phase below can prove the
	// post-commit backfill transaction genuinely opened, not just that the
	// test passed. See beginCountingDB and the wantBeginsPerRepo check below.
	commitBeginCounts := make([]int64, repoCount)
	commitAdapters := make([]beginCountingDB, repoCount)
	for i := range commitAdapters {
		commitAdapters[i] = beginCountingDB{
			SQLDB:  SQLDB{DB: openIngestionTxLockSplitProofClaimerDB(t, ctx, dsn, schemaName)},
			begins: &commitBeginCounts[i],
		}
	}
	maintenanceAdapters := [2]SQLDB{
		{DB: openIngestionTxLockSplitProofClaimerDB(t, ctx, dsn, schemaName)},
		{DB: openIngestionTxLockSplitProofClaimerDB(t, ctx, dsn, schemaName)},
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
					// Status is Pending, same as the seed loop above: repeated
					// same-scope commits across rounds must not each claim
					// Active, or they deterministically collide on
					// scope_generations_active_scope_idx since nothing in this
					// test runs the projector queue's supersede step.
					gen := scope.ScopeGeneration{
						GenerationID: fmt.Sprintf("gen-deadlock-r%d-%d", round, i),
						ScopeID:      scopeValue.ScopeID,
						ObservedAt:   time.Date(2026, time.July, 1, 1, 0, 0, 0, time.UTC).Add(time.Duration(round) * time.Minute),
						IngestedAt:   time.Date(2026, time.July, 1, 1, 0, 0, 0, time.UTC).Add(time.Duration(round) * time.Minute),
						Status:       scope.GenerationStatusPending,
						TriggerKind:  scope.TriggerKindSnapshot,
					}
					// The committed "repository" fact's repo_id is a fresh
					// alias unique to this (repo slot, round) pair — never
					// repoKey itself, and never reused across rounds. repoKey
					// stays fixed as scopeValue.PartitionKey (the deferred-
					// maintenance lock key, deferredMaintenanceRepoLockKey),
					// so the lock-ordering contention this test exists to
					// prove is unchanged; only the catalog identity onboarded
					// this round is new. hasNewRepo
					// (ingestion_backfill_per_commit.go) compares this
					// generation's repository facts against knownRepoIDs, a
					// cold per-commit catalog load (NewIngestionStore below
					// always starts a fresh, empty repositoryCatalogCache), so
					// an alias that was never committed before is guaranteed
					// unknown and hasNewRepo is true on every round — unlike
					// the prior version of this test, which reused repoKey
					// itself (already registered by the seed loop above) and
					// so never opened the post-commit backfill transaction at
					// all. See the wantBeginsPerRepo assertion below for the
					// positive proof.
					onboardedRepoAlias := fmt.Sprintf("%s-onboard-r%d", repoKey, round)
					store := NewIngestionStore(commitAdapters[i])
					store.Now = func() time.Time { return gen.IngestedAt }
					if err := store.CommitScopeGeneration(
						ctx, scopeValue, gen,
						testFactChannel([]facts.Envelope{repoFactEnvelope(
							fmt.Sprintf("fact-deadlock-r%d-%d", round, i), scopeValue.ScopeID, gen.GenerationID, onboardedRepoAlias, gen.ObservedAt,
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
			for bi, batch := range [][]string{ascending, descending} {
				bi, batch := bi, batch
				wg.Add(1)
				go func() {
					defer wg.Done()
					tx, err := maintenanceAdapters[bi].Begin(ctx)
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

	// Positive proof the split path was genuinely exercised, not merely that
	// the test above passed: every round commits a repository fact for a
	// never-before-seen alias (onboardedRepoAlias above), so hasNewRepo
	// (ingestion_backfill_per_commit.go) must be true and
	// runPostCommitRelationshipBackfill must open its own transaction on top
	// of CommitScopeGeneration's atomic-commit transaction, every round, for
	// every repo slot. beginCountingDB counts every Begin() the repo slot's
	// single connection issues, so a repo slot that only ever opened the
	// atomic-commit transaction (the defect PR #5883 review found: hasNewRepo
	// was always false because every round reused repoKey, which the seed
	// loop had already registered as known) shows exactly `rounds` begins
	// instead of `2*rounds`, and this fails loudly instead of the test
	// silently passing on a single-transaction shape.
	const wantBeginsPerRepo = int64(rounds) * 2
	for i := range commitBeginCounts {
		if got := atomic.LoadInt64(&commitBeginCounts[i]); got != wantBeginsPerRepo {
			t.Fatalf(
				"repo slot %d: commit connection opened %d transaction(s) across %d rounds, want %d "+
					"(2 per round: the atomic CommitScopeGeneration transaction plus "+
					"runPostCommitRelationshipBackfill's own transaction) — the post-commit "+
					"backfill transaction is not opening, so this test is not exercising the "+
					"second lock acquisition it claims to prove (hasNewRepo may be false again)",
				i, got, rounds, wantBeginsPerRepo,
			)
		}
	}
}
