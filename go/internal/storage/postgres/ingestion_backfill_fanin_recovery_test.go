// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Recovery and correctness proofs for the deferred backfill's fan-in
// publication step: what survives when the generation moves under it, when the
// publication itself fails, and when the process dies in the window between the
// last evidence batch and publication.
//
// The property under test in all three is the same asymmetry the fan-in is
// built around. A memo row without complete evidence is durable wrong state.
// Evidence without a memo row is recoverable: the partition is a gate miss, the
// next pass reloads it, and the re-upsert of already-committed evidence is a
// no-op because relationship_evidence_facts is content-addressed.

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// advanceFanInProofGeneration activates a NEW generation for the shared proof
// scope, modelling an ingester committing a fresh snapshot while a deferred
// maintenance pass is mid-flight.
func advanceFanInProofGeneration(t *testing.T, ctx context.Context, db *sql.DB, generationID string, ingestedAt time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
		generationID, fanInProofScopeID, ingestedAt); err != nil {
		t.Fatalf("seed advanced generation %q: %v", generationID, err)
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2",
		generationID, fanInProofScopeID); err != nil {
		t.Fatalf("activate advanced generation %q: %v", generationID, err)
	}
}

// TestDeferredBackfillFanInSkipsPartitionWhoseGenerationAdvanced pins the
// under-lock generation re-read. The batches commit evidence against
// gen-shared, then a new generation activates before the fan-in opens its
// transaction. Publishing then would mark a superseded generation
// backward-evidence-committed and memoize a partition the next pass must
// reload, so the fan-in must publish nothing -- and must not treat it as an
// error, because nothing went wrong.
func TestDeferredBackfillFanInSkipsPartitionWhoseGenerationAdvanced(t *testing.T) {
	ctx := context.Background()
	db := openFanInProofSchema(t, ctx, fanInProofDSN(t), 4)

	base := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	seedFanInProofSharedPartition(t, ctx, db, 2, base)

	adapter := SQLDB{DB: db}
	// Two repositories at one per batch: ordinals 1 and 2 are the evidence
	// batches, ordinal 3 is the first fan-in transaction.
	hooked := &failOnNthBeginner{inner: adapter}
	hooked.beforeBegin = func(ordinal int) {
		if ordinal != 3 {
			return
		}
		advanceFanInProofGeneration(t, ctx, db, "gen-advanced", base.Add(time.Hour))
	}

	store := NewIngestionStore(hooked)
	store.Now = func() time.Time { return base }
	store.maintenanceBatchSize = 1
	store.maintenanceWorkers = 1

	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil (an advanced generation is a skip, not a failure)", err)
	}

	if got := countFanInProofPhaseRows(t, ctx, db); got != 0 {
		t.Fatalf("graph_projection_phase_state rows for the superseded generation = %d, want 0", got)
	}
	if got := countFanInProofMemoRows(t, ctx, db); got != 0 {
		t.Fatalf("deferred_backfill_partition_memo rows for the superseded generation = %d, want 0", got)
	}
	var advancedRows int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM graph_projection_phase_state WHERE generation_id = $1", "gen-advanced",
	).Scan(&advancedRows); err != nil {
		t.Fatalf("count readiness rows for the advanced generation: %v", err)
	}
	if advancedRows != 0 {
		t.Fatalf("published %d readiness row(s) for gen-advanced, want 0; this pass never loaded its facts", advancedRows)
	}
}

// TestDeferredBackfillFanInFailureLeavesEvidenceRecoverable proves the tolerated
// direction. Every evidence batch commits, the fan-in transaction fails, and the
// result is committed evidence with no memo and no readiness. A clean rerun then
// converges: it publishes exactly one readiness row and one memo row, and adds
// ZERO evidence rows, because re-upserting content-addressed evidence is a
// no-op.
func TestDeferredBackfillFanInFailureLeavesEvidenceRecoverable(t *testing.T) {
	ctx := context.Background()
	db := openFanInProofSchema(t, ctx, fanInProofDSN(t), 4)

	base := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	seedFanInProofSharedPartition(t, ctx, db, 2, base)

	adapter := SQLDB{DB: db}
	fingerprint := fanInProofCatalogFingerprint(t, ctx, adapter)

	// Ordinals 1 and 2 are the evidence batches; ordinal 3 is the fan-in.
	failing := &failOnNthBeginner{inner: adapter, failOn: 3}
	store := NewIngestionStore(failing)
	store.Now = func() time.Time { return base }
	store.maintenanceBatchSize = 1
	store.maintenanceWorkers = 1

	err := store.BackfillAllRelationshipEvidence(ctx, nil, nil)
	if err == nil {
		t.Fatal("BackfillAllRelationshipEvidence() error = nil, want the injected fan-in failure")
	}
	if !errors.Is(err, errFanInProofBatchFailed) {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want the injected fan-in failure", err)
	}

	evidenceAfterFailure := countEvidenceRows(t, ctx, db)
	if evidenceAfterFailure == 0 {
		t.Fatal("no evidence rows survived the failed pass; this test cannot prove the recoverable direction")
	}
	if got := countFanInProofMemoRows(t, ctx, db); got != 0 {
		t.Fatalf("deferred_backfill_partition_memo rows after a failed fan-in = %d, want 0", got)
	}
	if got := countFanInProofPhaseRows(t, ctx, db); got != 0 {
		t.Fatalf("graph_projection_phase_state rows after a failed fan-in = %d, want 0", got)
	}
	assertFanInProofGateLoads(t, ctx, adapter, fingerprint)

	assertFanInProofRerunConverges(t, ctx, db, base, evidenceAfterFailure)
}

// TestDeferredBackfillCrashBetweenBatchesAndFanInConverges covers the window the
// in-process failure paths do not reach: every evidence batch committed and the
// process then died, so publication was never ATTEMPTED at all. The test runs
// exactly the work such a process would have finished -- the real batch phase,
// stopping before the fan-in -- rather than injecting a fault into a fan-in that
// did run.
//
// The durable state is identical to a failed fan-in (a transaction that never
// commits writes nothing), but it is worth proving separately: the claim being
// made is that an ABSENT publication step, not merely a failed one, leaves the
// partition reloadable. A crash window that re-created the memo skip would
// defeat the whole redesign.
func TestDeferredBackfillCrashBetweenBatchesAndFanInConverges(t *testing.T) {
	ctx := context.Background()
	db := openFanInProofSchema(t, ctx, fanInProofDSN(t), 4)

	base := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	seedFanInProofSharedPartition(t, ctx, db, 3, base)

	adapter := SQLDB{DB: db}
	fingerprint := fanInProofCatalogFingerprint(t, ctx, adapter)

	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }

	contributions := runFanInProofEvidencePhaseOnly(t, ctx, store, adapter)
	if len(contributions) == 0 {
		t.Fatal("the evidence phase contributed no partitions; the fixture is not exercising the batch path")
	}

	evidenceAfterCrash := countEvidenceRows(t, ctx, db)
	if evidenceAfterCrash == 0 {
		t.Fatal("the evidence phase committed no evidence rows; this test cannot prove the crash window")
	}
	if got := countFanInProofMemoRows(t, ctx, db); got != 0 {
		t.Fatalf("deferred_backfill_partition_memo rows after a crash before publication = %d, want 0", got)
	}
	if got := countFanInProofPhaseRows(t, ctx, db); got != 0 {
		t.Fatalf("graph_projection_phase_state rows after a crash before publication = %d, want 0", got)
	}
	assertFanInProofGateLoads(t, ctx, adapter, fingerprint)

	assertFanInProofRerunConverges(t, ctx, db, base, evidenceAfterCrash)
}

// runFanInProofEvidencePhaseOnly drives the real evidence-batch half of the pass
// and returns before publication, reproducing the state a process leaves behind
// when it dies after its last batch commit. Everything it calls is the
// production path writeDeferredBackfillInBatches walks; only the fan-in call at
// the end is omitted.
func runFanInProofEvidencePhaseOnly(
	t *testing.T,
	ctx context.Context,
	store IngestionStore,
	adapter ExecQueryer,
) map[scopeGenerationPartition][]string {
	t.Helper()

	catalog, _, err := loadRepositoryCatalog(ctx, adapter)
	if err != nil {
		t.Fatalf("loadRepositoryCatalog() error = %v, want nil", err)
	}
	activeFacts, snapshotGenerations, _, err := store.loadDeferredAnchorScopedRelationshipFacts(ctx, adapter, catalog, nil)
	if err != nil {
		t.Fatalf("loadDeferredAnchorScopedRelationshipFacts() error = %v, want nil", err)
	}
	evidenceBySourceRepo := make(map[string][]relationships.EvidenceFact)
	for _, fact := range relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(activeFacts, catalog)) {
		if strings.TrimSpace(fact.SourceRepoID) == "" || strings.TrimSpace(fact.TargetRepoID) == "" {
			continue
		}
		evidenceBySourceRepo[fact.SourceRepoID] = append(evidenceBySourceRepo[fact.SourceRepoID], fact)
	}

	repoGenerations, err := loadActiveRepositoryGenerations(ctx, adapter)
	if err != nil {
		t.Fatalf("loadActiveRepositoryGenerations() error = %v, want nil", err)
	}
	repoIDs := make([]string, 0, len(repoGenerations))
	for repoID := range repoGenerations {
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)

	bounds := make([][2]int, 0, len(repoIDs))
	for i := range repoIDs {
		bounds = append(bounds, [2]int{i, i + 1})
	}

	contributions, err := store.runDeferredBackfillBatches(
		ctx, repoIDs, bounds, 1, evidenceBySourceRepo, snapshotGenerations, nil,
	)
	if err != nil {
		t.Fatalf("runDeferredBackfillBatches() error = %v, want nil", err)
	}
	return contributions
}

// assertFanInProofRerunConverges runs one clean pass over the same fixture and
// pins convergence: exactly one readiness row and one memo row for the shared
// partition, and no new evidence rows, since the previous pass already
// committed them content-addressed.
func assertFanInProofRerunConverges(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	base time.Time,
	evidenceBefore int,
) {
	t.Helper()

	recovery := NewIngestionStore(SQLDB{DB: db})
	recovery.Now = func() time.Time { return base.Add(time.Minute) }
	recovery.maintenanceBatchSize = 1
	recovery.maintenanceWorkers = 1
	if err := recovery.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("recovery BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	if got := countFanInProofPhaseRows(t, ctx, db); got != 1 {
		t.Fatalf("graph_projection_phase_state rows after recovery = %d, want exactly 1", got)
	}
	if got := countFanInProofMemoRows(t, ctx, db); got != 1 {
		t.Fatalf("deferred_backfill_partition_memo rows after recovery = %d, want exactly 1", got)
	}
	if got := countEvidenceRows(t, ctx, db); got != evidenceBefore {
		t.Fatalf(
			"recovery pass changed the evidence row count from %d to %d, want no change; re-upserting content-addressed evidence must be a no-op",
			evidenceBefore, got,
		)
	}
}

// TestFanInActiveGenerationMatchesCorpusLoader is the anti-drift differential
// for activeScopeGenerationQuery. The fan-in resolves a scope's active
// generation with a single-scope LIMIT 1 lookup instead of the corpus-wide
// loadActiveRepositoryGenerations, because running the corpus-wide query once
// per partition would mean one full scan per partition per pass. The two must
// agree, so this test asserts they do on the shapes where they could diverge:
// a scope whose latest generation is NOT its only one, both with
// ingestion_scopes.active_generation_id set and with it left NULL so the
// COALESCE fallback to the newest generation applies.
func TestFanInActiveGenerationMatchesCorpusLoader(t *testing.T) {
	ctx := context.Background()
	db := openFanInProofSchema(t, ctx, fanInProofDSN(t), 2)

	base := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)

	type scopeFixture struct {
		scopeID       string
		repoID        string
		generationIDs []string // ascending ingested_at
		activate      string   // "" leaves active_generation_id NULL
		wantActive    string
	}
	fixtures := []scopeFixture{
		{
			// Explicit activation that is NOT the newest row: proves the query
			// honours active_generation_id rather than always taking the latest.
			scopeID:       "git:scope-pinned",
			repoID:        "repo-pinned",
			generationIDs: []string{"gen-pinned-1", "gen-pinned-2", "gen-pinned-3"},
			activate:      "gen-pinned-2",
			wantActive:    "gen-pinned-2",
		},
		{
			// No activation: the COALESCE fallback must pick the newest by
			// (ingested_at DESC, generation_id DESC).
			scopeID:       "git:scope-fallback",
			repoID:        "repo-fallback",
			generationIDs: []string{"gen-fallback-1", "gen-fallback-2"},
			wantActive:    "gen-fallback-2",
		},
		{
			scopeID:       "git:scope-single",
			repoID:        "repo-single",
			generationIDs: []string{"gen-single-1"},
			wantActive:    "gen-single-1",
		},
	}

	for _, fixture := range fixtures {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, NULL)", fixture.scopeID); err != nil {
			t.Fatalf("seed scope %q: %v", fixture.scopeID, err)
		}
		for i, generationID := range fixture.generationIDs {
			ingestedAt := base.Add(time.Duration(i) * time.Hour)
			if _, err := db.ExecContext(ctx,
				"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
				generationID, fixture.scopeID, ingestedAt); err != nil {
				t.Fatalf("seed generation %q: %v", generationID, err)
			}
			// A repository fact under EVERY generation, so the corpus-wide
			// loader can resolve the repo whichever generation ends up active.
			if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)`,
				"repo-fact-"+generationID, fixture.scopeID, generationID, ingestedAt,
				`{"repo_id":"`+fixture.repoID+`","name":"`+fixture.repoID+`"}`); err != nil {
				t.Fatalf("seed repository fact under %q: %v", generationID, err)
			}
		}
		if fixture.activate != "" {
			if _, err := db.ExecContext(ctx,
				"UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2",
				fixture.activate, fixture.scopeID); err != nil {
				t.Fatalf("activate %q: %v", fixture.activate, err)
			}
		}
	}

	adapter := SQLDB{DB: db}
	corpus, err := loadActiveRepositoryGenerations(ctx, adapter)
	if err != nil {
		t.Fatalf("loadActiveRepositoryGenerations() error = %v, want nil", err)
	}

	for _, fixture := range fixtures {
		t.Run(fixture.scopeID, func(t *testing.T) {
			scoped, err := loadActiveGenerationForScope(ctx, adapter, fixture.scopeID)
			if err != nil {
				t.Fatalf("loadActiveGenerationForScope(%q) error = %v, want nil", fixture.scopeID, err)
			}
			if scoped != fixture.wantActive {
				t.Fatalf("loadActiveGenerationForScope(%q) = %q, want %q", fixture.scopeID, scoped, fixture.wantActive)
			}
			identity, ok := corpus[fixture.repoID]
			if !ok {
				t.Fatalf("corpus-wide loader has no entry for %q; the differential has nothing to compare", fixture.repoID)
			}
			if identity.GenerationID != scoped {
				t.Fatalf(
					"single-scope lookup returned %q but the corpus-wide loadActiveRepositoryGenerations returned %q for scope %q; the two definitions of 'active generation' have drifted",
					scoped, identity.GenerationID, fixture.scopeID,
				)
			}
		})
	}

	// A scope with no generation row at all resolves to the empty string, which
	// the fan-in treats as "nothing to publish" rather than as an error.
	missing, err := loadActiveGenerationForScope(ctx, adapter, "git:scope-absent")
	if err != nil {
		t.Fatalf("loadActiveGenerationForScope() on an unknown scope error = %v, want nil", err)
	}
	if missing != "" {
		t.Fatalf("loadActiveGenerationForScope() on an unknown scope = %q, want the empty string", missing)
	}
}
