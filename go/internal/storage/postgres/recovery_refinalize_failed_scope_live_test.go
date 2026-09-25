// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Real-Postgres proofs for #7116: an all-scopes disaster-recovery rebuild used
// to select only scopes with status = 'active' and a non-null
// active_generation_id. A scope whose latest generation failed carries
// status = 'failed' and no active generation, so the rebuild skipped it without
// saying so and, after a graph-backend swap, that repository was simply absent
// from the new graph.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run RefinalizeFailedScope -count=1

// refinalizeFailedScope seeds the shape observed on ops-qa: a failed scope with
// no active generation, an older superseded generation, and a newest generation
// that failed. It returns the scope and both generation ids.
func refinalizeFailedScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	suffix string,
) (scopeID, failedGeneration, supersededGeneration string) {
	t.Helper()

	scopeID = "refinalize-failed-scope-" + suffix
	failedGeneration = "refinalize-failed-latest-" + suffix
	supersededGeneration = "refinalize-failed-older-" + suffix
	now := time.Now().UTC()

	seedRefinalizeFailedScopeRow(t, ctx, db, scopeID, "failed", now)
	seedFailedScopeGeneration(t, ctx, db, scopeID, supersededGeneration, "superseded", now.Add(-2*time.Hour))
	seedFailedScopeGeneration(t, ctx, db, scopeID, failedGeneration, "failed", now.Add(-1*time.Hour))
	return scopeID, failedGeneration, supersededGeneration
}

// seedRefinalizeFailedScopeRow inserts a scope row with no active generation in the given
// status and registers cleanup for everything hanging off the scope.
func seedRefinalizeFailedScopeRow(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, status string,
	now time.Time,
) {
	t.Helper()

	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, $3, NULL, '{}'::jsonb)`,
		scopeID, now, status,
	); err != nil {
		t.Fatalf("seed ingestion_scopes %s: %v", scopeID, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, statement := range []string{
			`DELETE FROM relationship_generations WHERE scope = $1`,
			`DELETE FROM graph_projection_phase_state WHERE scope_id = $1`,
			`DELETE FROM shared_projection_intents WHERE scope_id = $1`,
			`DELETE FROM fact_work_items WHERE scope_id = $1`,
			`DELETE FROM scope_generations WHERE scope_id = $1`,
			`DELETE FROM ingestion_scopes WHERE scope_id = $1`,
		} {
			_, _ = db.ExecContext(cleanupCtx, statement, scopeID)
		}
	})
}

// seedFailedScopeGeneration inserts one generation with an explicit ingest time so a
// test controls which generation is newest.
func seedFailedScopeGeneration(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, generationID, status string,
	ingestedAt time.Time,
) {
	t.Helper()

	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
		VALUES ($1, $2, 'manual', $4, $4, $3)`,
		generationID, scopeID, status, ingestedAt,
	); err != nil {
		t.Fatalf("seed scope_generations %s: %v", generationID, err)
	}
}

// refinalizeResultNamesScope reports whether ids names the scope.
func refinalizeResultNamesScope(ids []string, scopeID string) bool {
	for _, id := range ids {
		if id == scopeID {
			return true
		}
	}
	return false
}

// TestRefinalizeFailedScopeAllScopesEnqueuesNewestFailedGeneration is the #7116
// failing-first regression. The all-scopes rebuild must re-enqueue a failed
// scope through its newest failed generation, must not touch the superseded
// history, and must re-arm the never-active generation's dedup state exactly as
// it does for an active one. Then the re-projection has to be able to finish:
// the projector ack must turn the failed scope back into an active one.
func TestRefinalizeFailedScopeAllScopesEnqueuesNewestFailedGeneration(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)

	activeScope, activeGeneration, _ := refinalizeResetScope(t, ctx, database, suffix)
	failedScope, failedGeneration, supersededGeneration := refinalizeFailedScope(t, ctx, database, suffix)

	// Dedup state that survived the graph wipe for the failed generation: a
	// succeeded reducer item, a completed shared intent, a readiness phase row,
	// and an active relationship generation. None may survive the rebuild or the
	// re-projection stays deduplicated. This exercises all four reset statements
	// against a generation that was never active.
	reducerWork := seedRefinalizeResetReducerWork(t, ctx, database, failedScope, failedGeneration, "failed-entity", "succeeded")
	seedActiveRelationshipGeneration(t, ctx, database, failedGeneration, failedScope)
	completed := time.Now().UTC()
	intentID := "refinalize-failed-intent-" + suffix
	if err := NewSharedIntentStore(SQLDB{DB: database}).UpsertIntents(ctx, []reducer.SharedProjectionIntentRow{{
		IntentID:         intentID,
		ProjectionDomain: reducer.DomainCodeCalls,
		PartitionKey:     "caller->callee",
		ScopeID:          failedScope,
		AcceptanceUnitID: "unit-" + suffix,
		RepositoryID:     "repo-" + suffix,
		SourceRunID:      failedGeneration,
		GenerationID:     failedGeneration,
		Payload:          map[string]any{"action": "write"},
		CreatedAt:        completed,
		CompletedAt:      &completed,
	}}); err != nil {
		t.Fatalf("seed shared intent: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO graph_projection_phase_state
		  (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase, committed_at, updated_at)
		VALUES ($1, $2, $3, $3, 'code_entities_uid', 'canonical_nodes_committed', $4, $4)`,
		failedScope, "unit-"+suffix, failedGeneration, completed,
	); err != nil {
		t.Fatalf("seed graph_projection_phase_state: %v", err)
	}

	store := NewRecoveryStore(SQLDB{DB: database})
	result, err := store.RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{AllScopes: true}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RefinalizeScopeProjections(AllScopes) error = %v, want nil", err)
	}

	if !refinalizeResultNamesScope(result.ScopeIDs, activeScope) {
		t.Fatalf("all-scopes rebuild dropped the active scope %q; result.ScopeIDs = %v", activeScope, result.ScopeIDs)
	}
	if !refinalizeResultNamesScope(result.ScopeIDs, failedScope) {
		t.Fatalf("all-scopes rebuild skipped failed scope %q (latest generation failed, no active generation); "+
			"after a graph-backend rebuild that repository is absent from the graph; result.ScopeIDs = %v",
			failedScope, result.ScopeIDs)
	}

	if got := refinalizeResetWorkItemStatus(t, ctx, database, "refinalize_"+activeScope+"_"+activeGeneration); got != "pending" {
		t.Fatalf("active scope projector work item = %q, want pending", got)
	}
	if got := refinalizeResetWorkItemStatus(t, ctx, database, "refinalize_"+failedScope+"_"+failedGeneration); got != "pending" {
		t.Fatalf("failed scope projector work item for its newest failed generation = %q, want pending", got)
	}
	if got := refinalizeResetWorkItemStatus(t, ctx, database, "refinalize_"+failedScope+"_"+supersededGeneration); got != "" {
		t.Fatalf("rebuild re-drove superseded generation history: work item status %q, want none", got)
	}

	if got := refinalizeResetWorkItemStatus(t, ctx, database, reducerWork); got != "" {
		t.Fatalf("succeeded reducer work for the failed generation survived as %q; the re-projection would stay deduplicated", got)
	}
	if got := relationshipGenerationStatus(t, ctx, database, failedGeneration); got != "superseded" {
		t.Fatalf("relationship generation of the never-active failed generation = %q, want superseded", got)
	}
	if refinalizeResetIntentCompletedAt(t, ctx, database, intentID) != nil {
		t.Fatal("shared intent of the failed generation is still completed; its edges would never drain again")
	}
	if got := refinalizeResetPhaseRowCount(t, ctx, database, failedScope, failedGeneration); got != 0 {
		t.Fatalf("readiness phase rows for the failed generation = %d, want 0", got)
	}

	// The re-projection must be able to finish: claim the recovered work and ack
	// it exactly as the projector does on success.
	queue := NewProjectorQueue(SQLDB{DB: database}, "refinalize-failed-scope-test", time.Minute)
	acked := false
	for attempt := 0; attempt < 10 && !acked; attempt++ {
		work, found, claimErr := queue.Claim(ctx)
		if claimErr != nil {
			t.Fatalf("Claim() error = %v", claimErr)
		}
		if !found {
			break
		}
		if ackErr := queue.Ack(ctx, work, runtime.Result{}); ackErr != nil {
			t.Fatalf("Ack(%s/%s) error = %v", work.Scope.ScopeID, work.Generation.GenerationID, ackErr)
		}
		acked = work.Scope.ScopeID == failedScope
	}
	if !acked {
		t.Fatalf("the recovered projector work for failed scope %q was never claimable", failedScope)
	}

	var scopeStatus, activeID, generationStatus string
	if err := database.QueryRowContext(ctx,
		`SELECT status, COALESCE(active_generation_id, '') FROM ingestion_scopes WHERE scope_id = $1`, failedScope,
	).Scan(&scopeStatus, &activeID); err != nil {
		t.Fatalf("read failed scope after ack: %v", err)
	}
	if scopeStatus != "active" || activeID != failedGeneration {
		t.Fatalf("after a successful re-projection the scope is status=%q active_generation_id=%q, want active/%q",
			scopeStatus, activeID, failedGeneration)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT status FROM scope_generations WHERE generation_id = $1`, failedGeneration,
	).Scan(&generationStatus); err != nil {
		t.Fatalf("read failed generation after ack: %v", err)
	}
	if generationStatus != "active" {
		t.Fatalf("failed generation status after ack = %q, want active", generationStatus)
	}
}

// TestRefinalizeFailedScopeExplicitScopeIDsIncludesFailedScope pins the decision
// that an operator who names a failed scope gets it rebuilt. The named-scope and
// all-scopes paths share one selection, so "recover repo X" cannot answer
// differently from "recover everything" for the same scope.
func TestRefinalizeFailedScopeExplicitScopeIDsIncludesFailedScope(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)

	failedScope, failedGeneration, _ := refinalizeFailedScope(t, ctx, database, suffix)

	store := NewRecoveryStore(SQLDB{DB: database})
	result, err := store.RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{
		ScopeIDs: []string{failedScope},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RefinalizeScopeProjections(explicit failed scope) error = %v, want nil", err)
	}

	if got, want := result.Enqueued, 1; got != want {
		t.Fatalf("result.Enqueued = %d, want %d: a named failed scope was skipped", got, want)
	}
	if got := refinalizeResetWorkItemStatus(t, ctx, database, "refinalize_"+failedScope+"_"+failedGeneration); got != "pending" {
		t.Fatalf("projector work item for the named failed scope = %q, want pending", got)
	}
}

// TestRefinalizeFailedScopeReportsSkippedScopesByReason is the visibility half
// of #7116. Every scope a refinalize considers and does not re-enqueue must show
// up in the result under a closed reason, or a partial rebuild looks identical
// to a complete one. A named scope's counts are exact because the request bounds
// the set, so this test names one scope per outcome.
func TestRefinalizeFailedScopeReportsSkippedScopesByReason(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	now := time.Now().UTC()

	activeScope, _, _ := refinalizeResetScope(t, ctx, database, suffix)
	recoverable, _, _ := refinalizeFailedScope(t, ctx, database, suffix)

	// Failed, and every generation is superseded: nothing to re-project.
	allSuperseded := "refinalize-failed-all-superseded-" + suffix
	seedRefinalizeFailedScopeRow(t, ctx, database, allSuperseded, "failed", now)
	seedFailedScopeGeneration(t, ctx, database, allSuperseded, "gen-all-superseded-"+suffix, "superseded", now.Add(-time.Hour))

	// Failed, but a newer generation arrived after the failure and is pending:
	// it has its own projector work, so a refinalize must not double-queue it.
	newerPending := "refinalize-failed-newer-pending-" + suffix
	seedRefinalizeFailedScopeRow(t, ctx, database, newerPending, "failed", now)
	seedFailedScopeGeneration(t, ctx, database, newerPending, "gen-older-failed-"+suffix, "failed", now.Add(-2*time.Hour))
	seedFailedScopeGeneration(t, ctx, database, newerPending, "gen-newer-pending-"+suffix, "pending", now.Add(-time.Hour))

	// Never activated: first generation still pending, scope status pending.
	neverActivated := "refinalize-failed-never-activated-" + suffix
	seedRefinalizeFailedScopeRow(t, ctx, database, neverActivated, "pending", now)
	seedFailedScopeGeneration(t, ctx, database, neverActivated, "gen-first-pending-"+suffix, "pending", now.Add(-time.Hour))

	unknown := "refinalize-failed-no-such-scope-" + suffix

	store := NewRecoveryStore(SQLDB{DB: database})
	result, err := store.RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{
		ScopeIDs: []string{activeScope, recoverable, allSuperseded, newerPending, neverActivated, unknown},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil", err)
	}

	if got, want := result.Enqueued, 2; got != want {
		t.Fatalf("result.Enqueued = %d, want %d (the active scope and the recoverable failed scope)", got, want)
	}
	wantSkipped := map[string]string{
		recovery.SkipReasonNoRecoverableGeneration:   allSuperseded,
		recovery.SkipReasonNewestGenerationNotFailed: newerPending,
		recovery.SkipReasonNoActiveGeneration:        neverActivated,
		recovery.SkipReasonUnknownScope:              unknown,
	}
	if got, want := result.Skipped.Total(), len(wantSkipped); got != want {
		t.Fatalf("result.Skipped.Total() = %d, want %d; ByReason = %v", got, want, result.Skipped.ByReason)
	}
	for reason, scopeID := range wantSkipped {
		if got := result.Skipped.ByReason[reason]; got != 1 {
			t.Fatalf("result.Skipped.ByReason[%q] = %d, want 1; ByReason = %v", reason, got, result.Skipped.ByReason)
		}
		if !refinalizeResultNamesScope(result.Skipped.Samples[reason], scopeID) {
			t.Fatalf("result.Skipped.Samples[%q] = %v, want it to name %q", reason, result.Skipped.Samples[reason], scopeID)
		}
	}
}

// TestRefinalizeFailedScopeAllScopesReportsSkippedScopes proves the all-scopes
// path fills the same report. The shared test database may hold other scopes, so
// the counts are lower bounds here; the named-scope test above pins them exactly.
func TestRefinalizeFailedScopeAllScopesReportsSkippedScopes(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	now := time.Now().UTC()

	allSuperseded := "refinalize-failed-all-scopes-superseded-" + suffix
	seedRefinalizeFailedScopeRow(t, ctx, database, allSuperseded, "failed", now)
	seedFailedScopeGeneration(t, ctx, database, allSuperseded, "gen-all-scopes-superseded-"+suffix, "superseded", now.Add(-time.Hour))

	store := NewRecoveryStore(SQLDB{DB: database})
	result, err := store.RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{AllScopes: true}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RefinalizeScopeProjections(AllScopes) error = %v, want nil", err)
	}
	if result.Skipped.ByReason[recovery.SkipReasonNoRecoverableGeneration] < 1 {
		t.Fatalf("all-scopes rebuild did not report the failed scope with no recoverable generation: Skipped = %+v", result.Skipped)
	}
	if !refinalizeResultNamesScope(result.Skipped.Samples[recovery.SkipReasonNoRecoverableGeneration], allSuperseded) &&
		result.Skipped.ByReason[recovery.SkipReasonNoRecoverableGeneration] <= recovery.SkippedScopeSampleLimit {
		t.Fatalf("all-scopes report sample %v does not name %q", result.Skipped.Samples, allSuperseded)
	}
	if refinalizeResultNamesScope(result.ScopeIDs, allSuperseded) {
		t.Fatalf("all-scopes rebuild enqueued %q, which has no recoverable generation", allSuperseded)
	}
}

// TestRefinalizeFailedScopeBindsTheGenerationSetItRead is the concurrency proof
// for the failed-scope arm of the selection (#7116). It drives the interleaving
// the one-read design exists to survive: a failed scope is read, then an
// ingester recovers it on its own by activating a newer generation before the
// refinalize's later statements run. Every statement must keep binding the
// failed generation the read returned, so the newly active generation's dedup
// state is neither reset nor deleted by a re-derived, different selection.
func TestRefinalizeFailedScopeBindsTheGenerationSetItRead(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)

	failedScope, failedGeneration, _ := refinalizeFailedScope(t, ctx, database, suffix)
	newerGeneration := "refinalize-failed-recovered-" + suffix
	// Ingested before the failed generation so the failed one stays the newest
	// non-superseded generation the read selects; the activation below is what
	// the race switches, not the selection.
	seedFailedScopeGeneration(t, ctx, database, failedScope, newerGeneration, "pending", time.Now().UTC().Add(-90*time.Minute))

	failedWork := seedRefinalizeResetReducerWork(t, ctx, database, failedScope, failedGeneration, "read-generation-entity", "succeeded")
	newerWork := seedRefinalizeResetReducerWork(t, ctx, database, failedScope, newerGeneration, "activated-generation-entity", "succeeded")

	raceDB := &refinalizeActivationRaceDB{
		SQLDB: SQLDB{DB: database},
		activate: func() {
			if _, err := database.ExecContext(ctx, `
				UPDATE ingestion_scopes SET status = 'active', active_generation_id = $2 WHERE scope_id = $1`,
				failedScope, newerGeneration,
			); err != nil {
				t.Errorf("recover the failed scope mid-refinalize: %v", err)
			}
		},
	}

	result, err := NewRecoveryStore(raceDB).RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{
		ScopeIDs: []string{failedScope},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil", err)
	}
	if !raceDB.fired() {
		t.Fatal("the concurrent recovery never ran, so this test proved nothing about the race")
	}

	if got := refinalizeResetWorkItemStatus(t, ctx, database, failedWork); got != "" {
		t.Fatalf("succeeded reducer work of the generation this refinalize enqueued survived as %q", got)
	}
	if got := refinalizeResetWorkItemStatus(t, ctx, database, newerWork); got != "succeeded" {
		t.Fatalf("reducer work of a generation activated mid-transaction = %q, want it untouched (succeeded)", got)
	}
	if got, want := result.ReducerWorkDeleted, 1; got != want {
		t.Fatalf("result.ReducerWorkDeleted = %d, want %d", got, want)
	}
	if got := refinalizeResetWorkItemStatus(t, ctx, database, "refinalize_"+failedScope+"_"+failedGeneration); got != "pending" {
		t.Fatalf("projector work item for the generation the read returned = %q, want pending", got)
	}
	if got := refinalizeResetWorkItemStatus(t, ctx, database, "refinalize_"+failedScope+"_"+newerGeneration); got != "" {
		t.Fatalf("a projector work item exists for the generation activated mid-transaction (%q); the enqueue re-derived its set", got)
	}
}

// TestRefinalizeFailedScopeConvergesAcrossTwoCalls proves a failed-scope
// rebuild is re-runnable, like the active-scope one: the runbook's answer to an
// interrupted rebuild is to run the same command again.
func TestRefinalizeFailedScopeConvergesAcrossTwoCalls(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	failedScope, failedGeneration, _ := refinalizeFailedScope(t, ctx, database, suffix)
	seedRefinalizeResetReducerWork(t, ctx, database, failedScope, failedGeneration, "converge-failed-entity", "succeeded")

	store := NewRecoveryStore(SQLDB{DB: database})
	filter := recovery.RefinalizeFilter{ScopeIDs: []string{failedScope}}
	first, err := store.RefinalizeScopeProjections(ctx, filter, time.Now().UTC())
	if err != nil {
		t.Fatalf("first RefinalizeScopeProjections() error = %v", err)
	}
	second, err := store.RefinalizeScopeProjections(ctx, filter, time.Now().UTC())
	if err != nil {
		t.Fatalf("second RefinalizeScopeProjections() error = %v", err)
	}
	if first.ReducerWorkDeleted != 1 || second.ReducerWorkDeleted != 0 {
		t.Fatalf("ReducerWorkDeleted first/second = %d/%d, want 1/0", first.ReducerWorkDeleted, second.ReducerWorkDeleted)
	}
	if second.Enqueued != 1 {
		t.Fatalf("second call Enqueued = %d, want 1 (the projector row is reset to pending again)", second.Enqueued)
	}
	if got := refinalizeResetWorkItemStatus(t, ctx, database, "refinalize_"+failedScope+"_"+failedGeneration); got != "pending" {
		t.Fatalf("projector work item after two refinalizes = %q, want a single pending row", got)
	}
}
