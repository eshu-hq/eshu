// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// measureConcurrentExclusiveLockWaitDuringWork opens a shared-barrier-holding
// transaction on repoKey via work (which must acquire the barrier, do its
// work, and release it when done), and concurrently measures how long a
// separate exclusive-lock maintenance transaction on the SAME repoKey waits to
// acquire its lock. It returns the maintenance side's observed wait.
func measureConcurrentExclusiveLockWaitDuringWork(
	t *testing.T,
	ctx context.Context,
	dsn string,
	repoKey string,
	work func() error,
) time.Duration {
	t.Helper()

	workStarted := make(chan struct{})
	workDone := make(chan error, 1)
	go func() {
		close(workStarted)
		workDone <- work()
	}()
	<-workStarted
	// Give the work goroutine a head start so it has almost certainly already
	// acquired its shared barrier before the maintenance side requests the
	// exclusive lock on the same repository key.
	time.Sleep(20 * time.Millisecond)

	maintenanceDB := openIngestionTxLockSplitProofDB(t, dsn)
	maintenanceStart := time.Now()
	tx, err := maintenanceDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin maintenance tx: %v", err)
	}
	if _, err := tx.ExecContext(ctx, deferredMaintenancePartitionedExclusiveLockSQL, deferredMaintenanceLockNamespace, repoKey); err != nil {
		t.Fatalf("acquire maintenance exclusive lock: %v", err)
	}
	maintenanceWait := time.Since(maintenanceStart)
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit maintenance tx: %v", err)
	}

	if err := <-workDone; err != nil {
		t.Fatalf("work() error = %v, want nil", err)
	}
	return maintenanceWait
}

// runLockedBackfillWindow reproduces the PRE-FIX locked critical section:
// acquire the repo's shared barrier, then run
// backfillRelationshipEvidenceForNewRepositories before releasing it — exactly
// what commitScopeGeneration used to do when the in-TX backfill call still
// existed. newRepoAlias must be a repo id/alias the seeded anchor-matching
// corpus (seedAnchorMatchingCorpus) references, so the anchor-scoped fact load
// and DiscoverEvidence pass have real matching work to do.
func runLockedBackfillWindow(ctx context.Context, adapter SQLDB, repoKey, newRepoAlias string) error {
	tx, err := adapter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin locked backfill window: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := acquireDeferredMaintenanceRepoSharedLock(ctx, tx, repoKey); err != nil {
		return fmt.Errorf("acquire shared barrier: %w", err)
	}

	relationshipStore := NewRelationshipStore(tx)
	knownRepoIDs := map[string]struct{}{"repo-already-known": {}}
	// The "new" repository id must match the catalog fact
	// seedAnchorMatchingCorpus inserted (repo_id = newRepoAlias), which is what
	// backfillRelationshipAnchorTerms actually derives its LIKE ANY anchors
	// from. repoKey is only the deferred-maintenance lock partition key here,
	// matching production where a scope's PartitionKey and a fact's repo_id
	// are independent identifiers.
	currentGenerationRepoIDs := map[string]struct{}{newRepoAlias: {}}
	if err := backfillRelationshipEvidenceForNewRepositories(
		ctx, tx, relationshipStore, "gen-locked-backfill-window", knownRepoIDs, currentGenerationRepoIDs,
	); err != nil {
		return fmt.Errorf("backfill inside locked window: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit locked backfill window: %w", err)
	}
	committed = true
	return nil
}

// runLockedCommitWindowWithoutBackfill reproduces the POST-FIX locked critical
// section: acquire the repo's shared barrier and release it without running
// any backfill-shaped work, matching the shape commitScopeGeneration takes
// today now that the in-TX backfill call is gone.
func runLockedCommitWindowWithoutBackfill(ctx context.Context, adapter SQLDB, repoKey string) error {
	tx, err := adapter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin locked commit window: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := acquireDeferredMaintenanceRepoSharedLock(ctx, tx, repoKey); err != nil {
		return fmt.Errorf("acquire shared barrier: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit locked commit window: %w", err)
	}
	committed = true
	return nil
}

// seedAnchorMatchingCorpus inserts one already-onboarded repository, a
// catalog (repository-kind) fact for the newly onboarded repository aliased
// newRepoAlias, and factCount content facts (in the already-onboarded
// repository's scope) whose payload contains newRepoAlias, using direct SQL
// against the bootstrap schema (not CommitScopeGeneration, which is the code
// path under test elsewhere in this file and must not be exercised twice for
// the same corpus). The new repository's own catalog entry is required:
// backfillRelationshipAnchorTerms derives its LIKE ANY anchors from the
// onboarding repository's catalog aliases, so without it the anchor-scoped
// query has nothing to search for and short-circuits before touching the
// content corpus at all. This gives the anchor-scoped backfill query
// (listOnboardedRepoScopedRelationshipFactRecordsQuery) and DiscoverEvidence a
// realistic amount of matching work when a generation onboarding
// newRepoAlias triggers the new-repo backfill.
func seedAnchorMatchingCorpus(t *testing.T, ctx context.Context, adapter SQLDB, factCount int, newRepoAlias string) {
	t.Helper()

	knownScopeID := "git:scope-already-known"
	knownRepoID := "repo-already-known"
	knownGenID := "gen-already-known"
	observedAt := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	knownScopeValue := scope.IngestionScope{
		ScopeID:       knownScopeID,
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  knownRepoID,
	}
	knownGen := scope.ScopeGeneration{
		GenerationID: knownGenID,
		ScopeID:      knownScopeID,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		Status:       scope.GenerationStatusActive,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	if err := upsertIngestionScope(ctx, adapter, knownScopeValue, knownGen); err != nil {
		t.Fatalf("seed anchor-matching corpus: upsertIngestionScope() error = %v, want nil", err)
	}
	if err := upsertScopeGeneration(ctx, adapter, knownGen); err != nil {
		t.Fatalf("seed anchor-matching corpus: upsertScopeGeneration() error = %v, want nil", err)
	}

	insertFact := func(factID, factKind, payloadJSON string) {
		if _, err := adapter.ExecContext(
			ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, $4, $5, 'git', $5, $6, $6, $7::jsonb)`,
			factID, knownScopeID, knownGenID, factKind, factID, observedAt, payloadJSON,
		); err != nil {
			t.Fatalf("seed anchor-matching corpus: insert fact %q: %v", factID, err)
		}
	}
	insertFact("fact-already-known-repo", "repository", fmt.Sprintf(`{"repo_id":%q,"name":%q}`, knownRepoID, knownRepoID))

	// Batch the bulk corpus as one multi-row INSERT via unnest($n) arrays
	// instead of factCount round trips: at factCount in the hundreds of
	// thousands (the scale needed to make the full-scan `lower(payload::text)
	// LIKE ANY($1)` anchor predicate genuinely slow, matching a large-fleet
	// ingestion), one-row-at-a-time inserts would dominate the seed's own wall
	// clock and make the test far slower than the backfill work it measures.
	factIDs := make([]string, factCount)
	stableKeys := make([]string, factCount)
	sourceKeys := make([]string, factCount)
	payloads := make([]string, factCount)
	observedAts := make([]time.Time, factCount)
	for i := 0; i < factCount; i++ {
		factID := fmt.Sprintf("fact-anchor-corpus-%d", i)
		factIDs[i] = factID
		stableKeys[i] = factID
		sourceKeys[i] = factID
		observedAts[i] = observedAt
		payloads[i] = fmt.Sprintf(
			`{"repo_id":%q,"artifact_type":"terraform","relative_path":"modules/mod-%d/main.tf","content":"module_source = \"%s\"\nunrelated_index = %d"}`,
			knownRepoID, i, newRepoAlias, i,
		)
	}
	if _, err := adapter.ExecContext(
		ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT fact_id, $2, $3, 'content', stable_fact_key, 'git', source_fact_key, observed_at, observed_at, payload::jsonb
FROM unnest($1::text[], $4::text[], $5::text[], $6::timestamptz[], $7::text[])
  AS t(fact_id, stable_fact_key, source_fact_key, observed_at, payload)`,
		factIDs, knownScopeID, knownGenID, stableKeys, sourceKeys, observedAts, payloads,
	); err != nil {
		t.Fatalf("seed anchor-matching corpus: batch insert %d facts: %v", factCount, err)
	}
}

// insertNewRepoCatalogFact commits a standalone repository-kind fact aliased
// repoAlias, in its own scope/generation, using direct SQL. Only callers that
// exercise backfillRelationshipEvidenceForNewRepositories directly (bypassing
// CommitScopeGeneration, which would otherwise supply this fact itself as part
// of the onboarding commit) need this: repositoryCatalogEntryFromMap reads
// "repo_id" for the catalog entry's RepoID, giving
// backfillRelationshipAnchorTerms an alias to derive its LIKE ANY anchors
// from.
func insertNewRepoCatalogFact(t *testing.T, ctx context.Context, adapter SQLDB, repoAlias string) {
	t.Helper()

	scopeID := "git:scope-" + repoAlias
	genID := "gen-" + repoAlias
	observedAt := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	scopeValue := scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  repoAlias,
	}
	gen := scope.ScopeGeneration{
		GenerationID: genID,
		ScopeID:      scopeID,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		Status:       scope.GenerationStatusActive,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	if err := upsertIngestionScope(ctx, adapter, scopeValue, gen); err != nil {
		t.Fatalf("insert new-repo catalog fact: upsertIngestionScope() error = %v, want nil", err)
	}
	if err := upsertScopeGeneration(ctx, adapter, gen); err != nil {
		t.Fatalf("insert new-repo catalog fact: upsertScopeGeneration() error = %v, want nil", err)
	}
	factID := "fact-catalog-" + repoAlias
	if _, err := adapter.ExecContext(
		ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)`,
		factID, scopeID, genID, observedAt, fmt.Sprintf(`{"repo_id":%q,"name":%q}`, repoAlias, repoAlias),
	); err != nil {
		t.Fatalf("insert new-repo catalog fact: %v", err)
	}
}

func repoFactEnvelope(factID, scopeID, generationID, repoID string, observedAt time.Time) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      "repository",
		StableFactKey: "repository:" + repoID,
		ObservedAt:    observedAt,
		Payload:       map[string]any{"repo_id": repoID, "name": repoID},
		SourceRef: facts.Ref{
			SourceSystem: "git",
			FactKey:      factID,
		},
	}
}

func ingestionTxLockSplitProofDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(ingestionTxLockSplitDSNEnv))
	if dsn == "" {
		t.Skipf("set %s to run the #4451 ingestion TX/lock-split live Postgres proof", ingestionTxLockSplitDSNEnv)
	}
	return dsn
}

func openIngestionTxLockSplitProofDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	return db
}

// openIngestionTxLockSplitProofSchema opens a dedicated *sql.DB pinned to a
// throwaway schema with the bootstrap DDL applied, and registers a t.Cleanup
// to drop the schema unconditionally (even on t.Fatal) so a leftover
// pg_trgm/gin_trgm_ops install in a stray schema from a crashed run can never
// shadow the extension's normal public-schema location for a later run.
func openIngestionTxLockSplitProofSchema(t *testing.T, dsn string) (*sql.DB, string) {
	t.Helper()
	db := openIngestionTxLockSplitProofDB(t, dsn)
	schemaName := fmt.Sprintf("ingestion_tx_lock_split_proof_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	return db, schemaName
}
