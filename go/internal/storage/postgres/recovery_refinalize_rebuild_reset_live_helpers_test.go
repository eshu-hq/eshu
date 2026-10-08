// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"hash/crc32"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
)

// Real-Postgres proofs for the #4594 disaster-recovery rebuild.
//
// A rebuild-from-facts throws the graph away, keeps Postgres, and replays every
// active generation through the projector. Before this change it rebuilt only
// the source-local part of the graph, because three pieces of Postgres state
// that survive a graph wipe told the pipeline the work was already done:
//
//   - succeeded reducer work items, which a re-projection cannot re-open because
//     the enqueue uses ON CONFLICT (work_item_id) DO NOTHING;
//   - shared projection intents with completed_at set, which the partition
//     workers skip and the upsert deliberately never reopens;
//   - graph projection phase rows, which claim canonical nodes are committed for
//     a graph that no longer has any.
//
// These tests pin the reset to exactly the generations being refinalized. The
// two guard proofs at the bottom pin the opposite: that ordinary enqueue and
// ordinary shared-intent upsert still refuse to reset anything.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run RefinalizeRebuildReset -count=1

// refinalizeRebuildResetLiveDB opens the DSN-gated database on a schema of its
// own with the bootstrap applied, skipping when no DSN is configured. Schema
// setup and the proof get separate deadlines so one-time DDL cannot spend the
// proof's budget.
//
// The proofs claim, ack, replay and reopen work, and each of those acts on every
// matching row in the schema. On the shared schema they picked up rows earlier
// tests and earlier runs left behind, so whether a proof passed depended on how
// much history the database held (#7489).
func refinalizeRebuildResetLiveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	return openIsolatedLiveDB(t, "refinalize_reset",
		"set ESHU_POSTGRES_DSN to run the #4594 refinalize rebuild-reset proofs")
}

// refinalizeResetScope seeds one scope plus two generations: the active one a
// refinalize covers, and a retired one that must survive untouched. Returning
// both lets a test assert the affected-set predicate by generation, not only by
// scope, which is the difference between "rebuilds the current graph" and
// "re-drives retired history nobody asked for".
func refinalizeResetScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	suffix string,
) (scopeID, activeGeneration, retiredGeneration string) {
	t.Helper()

	scopeID = "refinalize-reset-scope-" + suffix
	activeGeneration = "refinalize-reset-active-" + suffix
	retiredGeneration = "refinalize-reset-retired-" + suffix
	now := time.Now().UTC()

	// The scope row lands first: scope_generations carries an FK to it.
	seedRefinalizeResetScopeRow(t, ctx, db, scopeID, activeGeneration, now)

	// Only one generation per scope may be 'active' (scope_generations_active_scope_idx),
	// which is also what makes the retired one a genuine out-of-set control: the
	// refinalize predicate selects ingestion_scopes.active_generation_id, so a
	// superseded generation is exactly the row a rebuild must not re-drive.
	for generationID, status := range map[string]string{
		activeGeneration:  "active",
		retiredGeneration: "superseded",
	} {
		if _, err := db.ExecContext(
			ctx, `
			INSERT INTO scope_generations
			  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
			VALUES ($1, $2, 'manual', $4, $4, $3, $4)
			ON CONFLICT (generation_id) DO NOTHING`,
			generationID, scopeID, status, now,
		); err != nil {
			t.Fatalf("seed scope_generations %s: %v", generationID, err)
		}
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, statement := range []string{
			`DELETE FROM graph_projection_phase_state WHERE scope_id = $1`,
			`DELETE FROM shared_projection_intents WHERE scope_id = $1`,
			`DELETE FROM shared_projection_acceptance WHERE scope_id = $1`,
			`DELETE FROM fact_work_items WHERE scope_id = $1`,
			`DELETE FROM scope_generations WHERE scope_id = $1`,
			`DELETE FROM ingestion_scopes WHERE scope_id = $1`,
		} {
			_, _ = db.ExecContext(cleanupCtx, statement, scopeID)
		}
	})

	return scopeID, activeGeneration, retiredGeneration
}

// seedRefinalizeResetScopeRow inserts or re-points the scope row so
// active_generation_id names the generation the refinalize is expected to cover.
func seedRefinalizeResetScopeRow(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, activeGeneration string,
	now time.Time,
) {
	t.Helper()

	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, 'active', $3::text, '{}'::jsonb)
		ON CONFLICT (scope_id) DO UPDATE SET active_generation_id = EXCLUDED.active_generation_id`,
		scopeID, now, activeGeneration,
	); err != nil {
		t.Fatalf("seed ingestion_scopes %s: %v", scopeID, err)
	}
}

// seedRefinalizeResetReducerWork inserts one reducer work item in the requested status,
// through the production enqueue so the row carries exactly the columns the
// projector writes, then moves it to its terminal status.
func seedRefinalizeResetReducerWork(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, generationID, entityKey, status string,
) string {
	t.Helper()

	queue := NewReducerQueue(SQLDB{DB: db}, "refinalize-reset-test", time.Minute)
	intent := runtime.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainCodeCallMaterialization,
		EntityKey:    entityKey,
		Reason:       "refinalize rebuild reset proof",
		FactID:       "fact-" + entityKey,
		SourceSystem: "git",
	}
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{intent}); err != nil {
		t.Fatalf("seed reducer work item %s: %v", entityKey, err)
	}

	workItemID := reducerWorkItemID(intent)
	if status != "pending" {
		if _, err := db.ExecContext(
			ctx,
			`UPDATE fact_work_items SET status = $2 WHERE work_item_id = $1`,
			workItemID, status,
		); err != nil {
			t.Fatalf("set reducer work item %s to %s: %v", entityKey, status, err)
		}
	}
	return workItemID
}

// seedRefinalizeResetAcceptance inserts one shared projection acceptance row
// pointing at the given generation. The generation must exist in
// scope_generations: the table carries an FK to it.
func seedRefinalizeResetAcceptance(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, acceptanceUnitID, sourceRunID, generationID string,
	now time.Time,
) {
	t.Helper()

	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO shared_projection_acceptance
		  (scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		ON CONFLICT (scope_id, acceptance_unit_id, source_run_id)
		DO UPDATE SET generation_id = EXCLUDED.generation_id, updated_at = EXCLUDED.updated_at`,
		scopeID, acceptanceUnitID, sourceRunID, generationID, now,
	); err != nil {
		t.Fatalf("seed shared_projection_acceptance %s/%s/%s: %v", scopeID, acceptanceUnitID, sourceRunID, err)
	}
}

// refinalizeResetAcceptedGeneration reads one acceptance row's generation,
// reporting absence as ok=false so a test can tell "cleared" apart from
// "still pointing at a generation".
func refinalizeResetAcceptedGeneration(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, acceptanceUnitID, sourceRunID string,
) (string, bool) {
	t.Helper()

	generation, ok, err := NewSharedProjectionAcceptanceStore(SQLDB{DB: db}).Lookup(ctx, scopeID, acceptanceUnitID, sourceRunID)
	if err != nil {
		t.Fatalf("Lookup acceptance %s/%s/%s: %v", scopeID, acceptanceUnitID, sourceRunID, err)
	}
	return generation, ok
}

// refinalizeResetWorkItemStatus reads one work item's status, reporting absence as the empty
// string so a test can tell "deleted" apart from "still here in some state".
func refinalizeResetWorkItemStatus(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) string {
	t.Helper()

	var status string
	err := db.QueryRowContext(
		ctx,
		`SELECT status FROM fact_work_items WHERE work_item_id = $1`, workItemID,
	).Scan(&status)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("read work item %s: %v", workItemID, err)
	}
	return status
}

// seedRefinalizeResetRepoDependencyIntent inserts one pending repo_dependency
// shared intent through the production upsert, without touching acceptance:
// unlike the acceptance writer, the plain store never commits an acceptance
// row, so the test controls the (intent, acceptance) pair exactly. The caller
// owns the acceptance row via seedRefinalizeResetAcceptance.
func seedRefinalizeResetRepoDependencyIntent(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	row reducer.SharedProjectionIntentRow,
) {
	t.Helper()

	if err := NewSharedIntentStore(SQLDB{DB: db}).UpsertIntents(ctx, []reducer.SharedProjectionIntentRow{row}); err != nil {
		t.Fatalf("seed repo_dependency intent %s: %v", row.IntentID, err)
	}
}

// activateRefinalizeResetGeneration rolls the scope forward: the old active
// generation is superseded and newGenerationID becomes active, mirroring what a
// projector ack does when the next generation commits. The old generation must
// drop out of 'active' before the new one lands: only one generation per scope
// may be active (scope_generations_active_scope_idx).
func activateRefinalizeResetGeneration(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, oldGenerationID, newGenerationID string,
) {
	t.Helper()

	now := time.Now().UTC()
	if _, err := db.ExecContext(
		ctx,
		`UPDATE scope_generations SET status = 'superseded' WHERE generation_id = $1`,
		oldGenerationID,
	); err != nil {
		t.Fatalf("supersede generation %s: %v", oldGenerationID, err)
	}
	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		VALUES ($1, $2, 'manual', $3, $3, 'active', $3)
		ON CONFLICT (generation_id) DO NOTHING`,
		newGenerationID, scopeID, now,
	); err != nil {
		t.Fatalf("seed scope_generations %s: %v", newGenerationID, err)
	}
	seedRefinalizeResetScopeRow(t, ctx, db, scopeID, newGenerationID, now)
}

// refinalizeResetGatedAcceptedGen builds the production repo_dependency
// authority lookup: the raw acceptance-store lookup fenced by the production
// relationship-generation active check through
// maintenance.GateAcceptedGenerationOnActive, exactly as go/cmd/reducer wires
// the lane. Bypassed source runs (code-import, package-consumption) skip the
// fence; resolver runs ("repo_dependency[:<scope>]") must clear it.
func refinalizeResetGatedAcceptedGen(database *sql.DB) reducer.AcceptedGenerationLookup {
	return maintenance.GateAcceptedGenerationOnActive(
		NewAcceptedGenerationLookup(SQLDB{DB: database}),
		NewRelationshipGenerationActiveLookup(NewRelationshipStore(SQLDB{DB: database})),
		nil,
	)
}

// refinalizeResetLaneRunner builds a repo_dependency lane over live Postgres,
// mirroring causalFenceRunner except for the accepted-generation lookup: where
// the fence proof uses the raw store lookup, the #7673 lane proofs use the
// production gate (maintenance.GateAcceptedGenerationOnActive over the
// production lookup and the production relationship-generation active check),
// which is the authority under test. AcceptedGenPrefetch stays nil so both
// selection and filtering resolve through that one gate. The edge writer is the
// caller's recorder; everything else is production.
func refinalizeResetLaneRunner(
	database *sql.DB,
	queue ReducerQueue,
	writer reducer.SharedProjectionEdgeWriter,
	suffix string,
) *reducer.RepoDependencyProjectionRunner {
	const partitionCount = 1_000_000_000
	partitionID := int(crc32.ChecksumIEEE([]byte(suffix)) % partitionCount)
	store := NewSharedIntentStore(SQLDB{DB: database})
	return &reducer.RepoDependencyProjectionRunner{
		IntentReader:                    store,
		LeaseManager:                    store,
		AcceptanceUnitGate:              NewRepoDependencyAcceptanceUnitGate(SQLDB{DB: database}),
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: queue,
		WorkloadReadinessPrefetch:       NewGraphProjectionReadinessPrefetch(SQLDB{DB: database}),
		AcceptedGen:                     refinalizeResetGatedAcceptedGen(database),
		Config: reducer.RepoDependencyProjectionRunnerConfig{
			LeaseOwner:            "refinalize-reset-lane-" + suffix,
			PollInterval:          time.Millisecond,
			LeaseTTL:              35 * time.Second,
			CycleTimeout:          2 * time.Second,
			GraphQuiescenceBudget: time.Millisecond,
			BatchLimit:            100,
			PartitionID:           partitionID,
			PartitionCount:        partitionCount,
		},
	}
}
