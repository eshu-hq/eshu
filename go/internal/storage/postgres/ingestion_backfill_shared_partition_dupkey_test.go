// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Shared-partition duplicate-conflict-key proof for the deferred-backfill
// dup-key bug found by a live Ifá gate run.
//
// The original defect: writeDeferredBackfillBatch (ingestion_backfill.go)
// looped per repository and appended one reducer.GraphProjectionPhaseState and
// one scopeGenerationPartition memo candidate per repo.
// graph_projection_phase_state's conflict target (scope_id,
// acceptance_unit_id, source_run_id, generation_id, keyspace, phase) and
// deferred_backfill_partition_memo's conflict target (scope_id, generation_id)
// are both functions of (ScopeID, GenerationID) alone -- the per-repo
// phase/memo rows built from repoGeneration carry AcceptanceUnitID = ScopeID
// and SourceRunID = GenerationID, and a constant Keyspace/Phase. When N
// repositories in one batch shared a (scope, generation) partition (the
// ingestion commit path accepts multi-repo scopes; production git sync just
// happens to commit one repo per scope), the batch upsert carried N
// byte-identical conflict keys in one INSERT ... ON CONFLICT DO UPDATE and
// Postgres rejected the whole batch transaction with SQLSTATE 21000 ("ON
// CONFLICT DO UPDATE command cannot affect row a second time"). The rollback
// meant NO repository in the batch got its readiness published, including
// well-formed ones sharing the batch with no other bug of their own.
//
// Publication has since moved out of the batch into the per-partition fan-in
// (publishDeferredBackfillPartitions), which iterates partitions and therefore
// cannot construct a duplicate conflict key at all. These tests keep the
// original guarantee pinned at its new home: they drive the real batched entry
// point end to end and assert that repositories sharing a (scope, generation)
// produce exactly ONE phase row and ONE memo row for that partition, while
// repositories in DISTINCT partitions still get one row each -- the collapse
// must not over-reach.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestDeferredBackfillPublishesOneRowPerPartitionHermetic is the
// DSN-independent sibling of the live proofs below. The live tests are the real
// end-to-end proof (they alone run against Postgres conflict-key semantics),
// but they self-skip whenever ESHU_POSTGRES_DSN is unset, and CI is not
// guaranteed to set it -- a DSN-gated-only regression test is a guard that
// exists on paper and never runs in that case. This test needs no database: it
// drives the real batched entry point against the package's existing
// concurrencyProbeDB fake (ingestion_backfill_concurrency_test.go), which
// records every ExecContext call verbatim instead of enforcing Postgres
// conflict-key semantics, and asserts on the CONSTRUCTED row count across the
// publication upserts -- the exact quantity the partition keying controls -- so
// it has teeth without needing a live ON CONFLICT rejection to prove it.
//
// The repositories are deliberately spread across several batches, because
// after the fan-in change that is the shape the guarantee has to survive: a
// partition's rows now come from a step that runs after every batch, not from
// whichever batch happened to see the partition first.
func TestDeferredBackfillPublishesOneRowPerPartitionHermetic(t *testing.T) {
	t.Parallel()

	// repo-shared-a and repo-shared-b share one (scope, generation) partition
	// (scope-shared, gen-shared) -- the multi-repo scope the ingestion commit
	// path accepts and the original dup-key defect choked on. repo-other and
	// repo-solo own their own partitions, so the collapse must not reach them.
	// 4 repos, 3 distinct partitions.
	activeGen := [][]any{
		{"repo-shared-a", "scope-shared", "gen-shared"},
		{"repo-shared-b", "scope-shared", "gen-shared"},
		{"repo-other", "scope-other", "gen-other"},
		{"repo-solo", "scope-solo", "gen-solo"},
	}
	db := &concurrencyProbeDB{activeGenRows: activeGen}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	// One repository per batch, so repo-shared-a and repo-shared-b reach the
	// fan-in from two SEPARATE committed batches.
	store.maintenanceBatchSize = 1
	store.maintenanceWorkers = 1

	published, err := store.writeDeferredBackfillInBatches(
		context.Background(),
		map[string][]relationships.EvidenceFact{},
		nil,
		"fingerprint-hermetic",
		nil,
	)
	if err != nil {
		t.Fatalf("writeDeferredBackfillInBatches() error = %v, want nil", err)
	}
	if published != 3 {
		t.Fatalf("writeDeferredBackfillInBatches() published = %d, want 3 (one per distinct (scope, generation) partition, not one per repo and not one per scope)", published)
	}

	phaseRows := publishedUpsertRowCount(t, db.allEvidence, upsertGraphProjectionPhaseStateBatchPrefix, graphProjectionPhaseColumnsPerRow, 3)
	if phaseRows != 3 {
		t.Fatalf("graph_projection_phase_state upserts carried %d row(s) in total, want 3", phaseRows)
	}
	memoRows := publishedUpsertRowCount(t, db.allEvidence, upsertDeferredBackfillPartitionMemoBatchPrefix, deferredBackfillPartitionMemoColumnsPerRow, 3)
	if memoRows != 3 {
		t.Fatalf("deferred_backfill_partition_memo upserts carried %d row(s) in total, want 3", memoRows)
	}
}

// TestDeferredBackfillWithholdsPublicationForSupersededGenerationHermetic pins
// the GenerationID axis of the partition key. A partition key that carried only
// ScopeID would treat a batch's (scope-shared, gen-stale) contribution as the
// same partition as (scope-shared, gen-shared) and publish it. The real key
// carries the generation, and the fan-in's under-lock re-read then finds that
// the scope's active generation is gen-shared, not gen-stale, and withholds
// publication for the superseded partition while still publishing the live one.
//
// Note the shape this test can and cannot model: a scope has exactly ONE active
// generation, so "the same scope active at two generations at once" is not
// representable in Postgres. The reachable form of the axis is a batch that
// observed a generation which has since been superseded, which is what the fake
// reproduces by answering the fan-in's per-scope lookup with gen-shared.
func TestDeferredBackfillWithholdsPublicationForSupersededGenerationHermetic(t *testing.T) {
	t.Parallel()

	// The fake answers loadActiveGenerationForScope from the FIRST row matching
	// the scope, so scope-shared resolves to gen-shared: repo-stale's batch
	// contribution under gen-stale is a superseded partition.
	activeGen := [][]any{
		{"repo-live", "scope-shared", "gen-shared"},
		{"repo-stale", "scope-shared", "gen-stale"},
	}
	db := &concurrencyProbeDB{activeGenRows: activeGen}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	store.maintenanceBatchSize = 1
	store.maintenanceWorkers = 1

	published, err := store.writeDeferredBackfillInBatches(
		context.Background(),
		map[string][]relationships.EvidenceFact{},
		nil,
		"fingerprint-superseded",
		nil,
	)
	if err != nil {
		t.Fatalf("writeDeferredBackfillInBatches() error = %v, want nil (a superseded generation is a skip, not a failure)", err)
	}
	if published != 1 {
		t.Fatalf("writeDeferredBackfillInBatches() published = %d, want 1 (only the live generation)", published)
	}

	phaseRows := publishedUpsertRowCount(t, db.allEvidence, upsertGraphProjectionPhaseStateBatchPrefix, graphProjectionPhaseColumnsPerRow, 1)
	if phaseRows != 1 {
		t.Fatalf("graph_projection_phase_state upserts carried %d row(s), want 1", phaseRows)
	}
	memoRows := publishedUpsertRowCount(t, db.allEvidence, upsertDeferredBackfillPartitionMemoBatchPrefix, deferredBackfillPartitionMemoColumnsPerRow, 1)
	if memoRows != 1 {
		t.Fatalf("deferred_backfill_partition_memo upserts carried %d row(s), want 1; a memo for the superseded generation would suppress its reload", memoRows)
	}
}

// publishedUpsertRowCount sums the rows carried by every recorded ExecContext
// call whose query starts with prefix (len(args) / columnsPerRow each), and
// pins the number of calls at wantCalls. Both halves matter: the row total is
// the dedupe assertion, and the call count pins that publication happens once
// per partition rather than once per repository or once per batch, so a
// regression that publishes the right rows from the wrong place still fails.
func publishedUpsertRowCount(t *testing.T, calls []fakeExecCall, prefix string, columnsPerRow, wantCalls int) int {
	t.Helper()
	var matched []fakeExecCall
	for _, call := range calls {
		if strings.HasPrefix(call.query, prefix) {
			matched = append(matched, call)
		}
	}
	if len(matched) != wantCalls {
		t.Fatalf("recorded %d ExecContext call(s) matching prefix %q, want exactly %d", len(matched), prefix, wantCalls)
	}
	total := 0
	for _, call := range matched {
		if columnsPerRow <= 0 || len(call.args)%columnsPerRow != 0 {
			t.Fatalf("upsert args len = %d not divisible by columnsPerRow = %d", len(call.args), columnsPerRow)
		}
		total += len(call.args) / columnsPerRow
	}
	return total
}

// sharedPartitionDupKeyProofDSN reads the throwaway live-Postgres DSN this
// proof runs against, following the same ESHU_POSTGRES_DSN convention as the
// sibling derived-evidence fencing proof
// (ingestion_derived_evidence_fencing_proof_test.go).
func sharedPartitionDupKeyProofDSN() string {
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

func TestDeferredBackfillSharedScopeGenerationPublishesOneRowPerPartition(t *testing.T) {
	dsn := sharedPartitionDupKeyProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the deferred-backfill shared-partition dedupe proof")
	}

	ctx := context.Background()
	db := openSharedPartitionDupKeySchema(t, ctx, dsn)
	store := NewIngestionStore(SQLDB{DB: db})
	store.SkipRelationshipBackfill = true
	now := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	// repo-alpha and repo-beta committed under the SAME scope+generation in one
	// CommitScopeGeneration call -- the multi-repo-per-scope shape the ingestion
	// commit path accepts today.
	commitSharedPartitionRepositories(t, ctx, store, "scope-shared", "gen-shared", []string{"repo-alpha", "repo-beta"}, now)

	repoGenerations, err := loadActiveRepositoryGenerations(ctx, SQLDB{DB: db})
	if err != nil {
		t.Fatalf("loadActiveRepositoryGenerations() error = %v, want nil", err)
	}
	for _, repoID := range []string{"repo-alpha", "repo-beta"} {
		if _, ok := repoGenerations[repoID]; !ok {
			t.Fatalf("%s missing from active repository generations: %+v", repoID, repoGenerations)
		}
	}

	// One repository per batch, so the shared partition is assembled from two
	// separate committed batches before the fan-in publishes it once.
	store.maintenanceBatchSize = 1
	store.maintenanceWorkers = 1

	published, err := store.writeDeferredBackfillInBatches(
		ctx,
		map[string][]relationships.EvidenceFact{},
		nil,
		"catalog-fingerprint-shared",
		nil,
	)
	if err != nil {
		var sqlState interface{ SQLState() string }
		if errors.As(err, &sqlState) {
			t.Fatalf("writeDeferredBackfillInBatches() error = %v (SQLSTATE %s), want nil", err, sqlState.SQLState())
		}
		t.Fatalf("writeDeferredBackfillInBatches() error = %v, want nil", err)
	}
	if published != 1 {
		t.Fatalf(
			"writeDeferredBackfillInBatches() published = %d readiness rows, want 1 (one per shared partition, not one per repo)",
			published,
		)
	}

	assertPhaseStateRowCount(t, ctx, db, "scope-shared", "gen-shared", 1)
	assertPartitionMemoRowCount(t, ctx, db, "scope-shared", "gen-shared", 1)
}

// TestDeferredBackfillDistinctScopesPublishOneRowEach guards against
// over-collapsing the dedupe fix: two repositories in two DISTINCT
// scope+generation partitions (the ordinary one-repo-per-scope shape) must
// still publish one phase row and one memo row EACH, not merge onto a single
// row.
func TestDeferredBackfillDistinctScopesPublishOneRowEach(t *testing.T) {
	dsn := sharedPartitionDupKeyProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the deferred-backfill shared-partition dedupe proof")
	}

	ctx := context.Background()
	db := openSharedPartitionDupKeySchema(t, ctx, dsn)
	store := NewIngestionStore(SQLDB{DB: db})
	store.SkipRelationshipBackfill = true
	now := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	commitSharedPartitionRepositories(t, ctx, store, "scope-gamma", "gen-gamma", []string{"repo-gamma"}, now)
	commitSharedPartitionRepositories(t, ctx, store, "scope-delta", "gen-delta", []string{"repo-delta"}, now.Add(time.Minute))

	store.maintenanceBatchSize = 1
	store.maintenanceWorkers = 1

	published, err := store.writeDeferredBackfillInBatches(
		ctx,
		map[string][]relationships.EvidenceFact{},
		nil,
		"catalog-fingerprint-distinct",
		nil,
	)
	if err != nil {
		t.Fatalf("writeDeferredBackfillInBatches() error = %v, want nil", err)
	}
	if published != 2 {
		t.Fatalf("writeDeferredBackfillInBatches() published = %d readiness rows, want 2 (one per distinct partition)", published)
	}

	assertPhaseStateRowCount(t, ctx, db, "scope-gamma", "gen-gamma", 1)
	assertPartitionMemoRowCount(t, ctx, db, "scope-gamma", "gen-gamma", 1)
	assertPhaseStateRowCount(t, ctx, db, "scope-delta", "gen-delta", 1)
	assertPartitionMemoRowCount(t, ctx, db, "scope-delta", "gen-delta", 1)
}

// commitSharedPartitionRepositories onboards one or more "repository" facts
// under the same scope+generation through the real CommitScopeGeneration path,
// so loadActiveRepositoryGenerations resolves each repoID to that shared
// partition exactly as production git sync would for a multi-repo scope.
func commitSharedPartitionRepositories(
	t *testing.T,
	ctx context.Context,
	store IngestionStore,
	scopeID, generationID string,
	repoIDs []string,
	observedAt time.Time,
) {
	t.Helper()
	envelopes := make([]facts.Envelope, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		factID := "fact-" + generationID + "-" + repoID
		envelopes = append(envelopes, facts.Envelope{
			FactID:        factID,
			ScopeID:       scopeID,
			GenerationID:  generationID,
			FactKind:      "repository",
			StableFactKey: "repository:" + scopeID + ":" + repoID,
			ObservedAt:    observedAt,
			Payload:       map[string]any{"repo_id": repoID, "name": repoID},
			SourceRef:     facts.Ref{SourceSystem: "git", FactKey: factID},
		})
	}
	if err := store.CommitScopeGeneration(
		ctx,
		sharedPartitionDupKeyScope(scopeID),
		sharedPartitionDupKeyGeneration(scopeID, generationID, observedAt),
		testFactChannel(envelopes),
	); err != nil {
		t.Fatalf("commit shared-partition repositories %v: CommitScopeGeneration() error = %v, want nil", repoIDs, err)
	}
}

func sharedPartitionDupKeyScope(scopeID string) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  scopeID,
	}
}

func sharedPartitionDupKeyGeneration(scopeID, generationID string, observedAt time.Time) scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: generationID,
		ScopeID:      scopeID,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		Status:       scope.GenerationStatusActive,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

func assertPhaseStateRowCount(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM graph_projection_phase_state WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID,
	).Scan(&got); err != nil {
		t.Fatalf("count graph_projection_phase_state rows: %v", err)
	}
	if got != want {
		t.Fatalf("graph_projection_phase_state rows for %s/%s = %d, want %d", scopeID, generationID, got, want)
	}
}

func assertPartitionMemoRowCount(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM deferred_backfill_partition_memo WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID,
	).Scan(&got); err != nil {
		t.Fatalf("count deferred_backfill_partition_memo rows: %v", err)
	}
	if got != want {
		t.Fatalf("deferred_backfill_partition_memo rows for %s/%s = %d, want %d", scopeID, generationID, got, want)
	}
}

// openSharedPartitionDupKeySchema creates an isolated throwaway schema and
// applies the full bootstrap DDL, mirroring openDerivedEvidenceFencingSchema:
// this proof exercises the real commit transaction (ingestion scopes/
// generations, fact_records, fact_work_items) plus the real deferred-backfill
// batch transaction (graph_projection_phase_state,
// deferred_backfill_partition_memo).
func openSharedPartitionDupKeySchema(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	schemaName := fmt.Sprintf("shared_partition_dupkey_%d", time.Now().UnixNano())

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create shared-partition dupkey schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply full bootstrap: %v", err)
	}
	return db
}
