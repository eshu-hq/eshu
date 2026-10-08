// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Lane-level proofs for #7673: a refinalize must clear the acceptance rows
// that would otherwise let the repo_dependency lane project edges for a
// retired generation.
//
// The storage proof (recovery_refinalize_rebuild_reset_core_test.go) shows the
// rows are gone; these show the lane consequence. Every drain below runs the
// real RepoDependencyProjectionRunner over live Postgres with the production
// gate, the production intent store, lease, acceptance-unit gate, readiness
// prefetch and queue-as-replayer; only the graph edge writer is a recorder.
// A zero-write result is therefore meaningful only alongside a control that
// drains in the same window, which each test carries: without it a dead lane
// and a fenced lane look identical.

// TestRefinalizeRetiredGenerationLaneWritesNoEdgesForBypassRun is the issue's
// regression: retire a generation through refinalize plus activation, run the
// lane, assert no edges for the retired generation. The intent rides a
// code-import source run, which bypasses the relationship-generation fence, so
// on unfixed code the surviving acceptance row grants authority and the lane
// writes (RED); with the fix the unit is never selected (GREEN).
//
// Phase A drains pre-refinalize to prove the harness writes; phase B is the
// regression with an in-window control scope proving the lane is live and
// selective; phase C re-advances acceptance to G2 through the production
// acceptance writer and proves the lane drains again, so the fix cannot strand
// a rebuilt scope.
func TestRefinalizeRetiredGenerationLaneWritesNoEdgesForBypassRun(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, database, suffix)
	repoID := "repository:7673-lane-" + suffix
	sourceRunID := "code_import_repo_dependency:" + scopeID
	now := time.Now().UTC()

	seedRefinalizeResetAcceptance(t, ctx, database, scopeID, repoID, sourceRunID, activeGeneration, now)
	dependsOn := func(intentID, generationID string) reducer.SharedProjectionIntentRow {
		return reducer.SharedProjectionIntentRow{
			IntentID: intentID, ProjectionDomain: reducer.DomainRepoDependency,
			PartitionKey: "depends_on:" + repoID + "->repository:7673-target-" + suffix,
			ScopeID:      scopeID, AcceptanceUnitID: repoID, RepositoryID: repoID,
			SourceRunID: sourceRunID, GenerationID: generationID, CreatedAt: now,
			Payload: map[string]any{
				"repo_id":           repoID,
				"target_repo_id":    "repository:7673-target-" + suffix,
				"relationship_type": "DEPENDS_ON",
				"evidence_source":   reducer.CrossRepoEvidenceSource,
			},
		}
	}
	intentA := "lane-7673-a-" + suffix
	seedRefinalizeResetRepoDependencyIntent(t, ctx, database, dependsOn(intentA, activeGeneration))

	queue := NewReducerQueue(SQLDB{DB: database}, "lane-7673-"+suffix, time.Minute)
	queue.Now = func() time.Time {
		var now time.Time
		if err := database.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			t.Errorf("read Postgres clock: %v", err)
			return time.Now().UTC()
		}
		return now.UTC()
	}
	writer := &causalFenceEdgeWriter{}
	runner := refinalizeResetLaneRunner(database, queue, writer, suffix)

	// Phase A: the harness drains before the refinalize.
	stop := startCausalFenceRunner(ctx, t, runner)
	waitForCausalFence(t, ctx, func() bool { return sharedIntentCompleted(t, ctx, database, intentA) })
	stop()
	if got := writer.writeCount(); got != 1 {
		t.Fatalf("pre-refinalize write count = %d, want 1: the lane must drain before the harness can prove it stops", got)
	}

	// Phase B: refinalize, roll the scope to G2, and run the lane with a
	// control scope draining in the same window.
	intentB := "lane-7673-b-" + suffix
	seedRefinalizeResetRepoDependencyIntent(t, ctx, database, dependsOn(intentB, activeGeneration))
	store := NewRecoveryStore(SQLDB{DB: database})
	if _, err := store.RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{
		ScopeIDs: []string{scopeID},
	}, time.Now().UTC()); err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil", err)
	}
	nextGeneration := "refinalize-reset-next-" + suffix
	activateRefinalizeResetGeneration(t, ctx, database, scopeID, activeGeneration, nextGeneration)

	controlScope, controlActive, _ := refinalizeResetScope(t, ctx, database, suffix+"-control")
	controlRepo := "repository:7673-control-" + suffix
	controlRun := "code_import_repo_dependency:" + controlScope
	seedRefinalizeResetAcceptance(t, ctx, database, controlScope, controlRepo, controlRun, controlActive, now)
	controlIntent := "lane-7673-control-" + suffix
	seedRefinalizeResetRepoDependencyIntent(t, ctx, database, reducer.SharedProjectionIntentRow{
		IntentID: controlIntent, ProjectionDomain: reducer.DomainRepoDependency,
		PartitionKey: "depends_on:" + controlRepo + "->repository:7673-target-" + suffix,
		ScopeID:      controlScope, AcceptanceUnitID: controlRepo, RepositoryID: controlRepo,
		SourceRunID: controlRun, GenerationID: controlActive, CreatedAt: now,
		Payload: map[string]any{
			"repo_id":           controlRepo,
			"target_repo_id":    "repository:7673-target-" + suffix,
			"relationship_type": "DEPENDS_ON",
			"evidence_source":   reducer.CrossRepoEvidenceSource,
		},
	})

	writesBefore, retractsBefore := writer.writeCount(), writer.retractCount()
	stop = startCausalFenceRunner(ctx, t, runner)
	waitForCausalFence(t, ctx, func() bool { return sharedIntentCompleted(t, ctx, database, controlIntent) })
	select {
	case <-time.After(time.Second):
	case <-ctx.Done():
		t.Fatal("context expired during the post-refinalize settle window")
	}
	stop()
	if !sharedIntentCompleted(t, ctx, database, controlIntent) {
		t.Fatal("control intent did not drain: the lane was not live during the regression window")
	}
	// Exactly one write: the control scope's. Any more means the lane also
	// projected the retired generation (on unfixed code the delta is 2).
	if got := writer.writeCount() - writesBefore; got != 1 {
		t.Fatalf("post-refinalize write count delta = %d, want 1 (control only): the lane projected edges for retired generation %q", got, activeGeneration)
	}
	if got := writer.retractCount() - retractsBefore; got != 0 {
		t.Fatalf("post-refinalize retract count delta = %d, want 0", got)
	}
	if sharedIntentCompleted(t, ctx, database, intentB) {
		t.Fatal("retired-generation intent completed: the lane must not even select a unit with no acceptance")
	}

	// Phase C: the re-projection re-advances acceptance to G2 and the lane
	// drains again.
	intentC := "lane-7673-c-" + suffix
	if err := NewSharedIntentAcceptanceWriter(SQLDB{DB: database}).UpsertIntents(ctx, []reducer.SharedProjectionIntentRow{
		dependsOn(intentC, nextGeneration),
	}); err != nil {
		t.Fatalf("commit G2 intent with acceptance: %v", err)
	}
	if got, ok := refinalizeResetAcceptedGeneration(t, ctx, database, scopeID, repoID, sourceRunID); !ok || got != nextGeneration {
		t.Fatalf("re-advanced acceptance = (%q, %v), want (%q, true)", got, ok, nextGeneration)
	}
	writesBeforeReAdvance := writer.writeCount()
	stop = startCausalFenceRunner(ctx, t, runner)
	waitForCausalFence(t, ctx, func() bool { return sharedIntentCompleted(t, ctx, database, intentC) })
	stop()
	if got := writer.writeCount() - writesBeforeReAdvance; got < 1 {
		t.Fatalf("post-re-advance write count delta = %d, want >= 1: clearing acceptance must not strand the rebuilt scope", got)
	}
}

// TestBypassedAcceptanceStillDrainsSupersededGenerationWithoutRefinalize pins
// the golden path the fix must not break: during the rollover window the
// bypass serves a superseded generation (acceptance advances with the
// reducer's intent commits, which postdate the projector ack that activates
// the next generation), so without any refinalize the lane still drains the
// retired generation. This passes before and after the fix; it fails if the
// bypass ever gains a scope-active check.
func TestBypassedAcceptanceStillDrainsSupersededGenerationWithoutRefinalize(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, database, suffix)
	repoID := "repository:7673-guard-" + suffix
	sourceRunID := "code_import_repo_dependency:" + scopeID
	now := time.Now().UTC()

	seedRefinalizeResetAcceptance(t, ctx, database, scopeID, repoID, sourceRunID, activeGeneration, now)
	intentID := "lane-7673-guard-" + suffix
	seedRefinalizeResetRepoDependencyIntent(t, ctx, database, reducer.SharedProjectionIntentRow{
		IntentID: intentID, ProjectionDomain: reducer.DomainRepoDependency,
		PartitionKey: "depends_on:" + repoID + "->repository:7673-target-" + suffix,
		ScopeID:      scopeID, AcceptanceUnitID: repoID, RepositoryID: repoID,
		SourceRunID: sourceRunID, GenerationID: activeGeneration, CreatedAt: now,
		Payload: map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    "repository:7673-target-" + suffix,
			"relationship_type": "DEPENDS_ON",
			"evidence_source":   reducer.CrossRepoEvidenceSource,
		},
	})
	activateRefinalizeResetGeneration(t, ctx, database, scopeID, activeGeneration, "refinalize-reset-next-"+suffix)

	queue := NewReducerQueue(SQLDB{DB: database}, "lane-7673-guard-"+suffix, time.Minute)
	writer := &causalFenceEdgeWriter{}
	runner := refinalizeResetLaneRunner(database, queue, writer, suffix+"-guard")
	stop := startCausalFenceRunner(ctx, t, runner)
	// The intent may never complete: the post-write workload replay refuses a
	// superseded generation (#7670), so the cycle errors after the write. The
	// guard is the write itself, which is what the bypass grants.
	waitForCausalFence(t, ctx, func() bool { return writer.writeCount() >= 1 })
	stop()
	if got := writer.writeCount(); got < 1 {
		t.Fatalf("bypassed write count = %d, want >= 1: the rollover window must keep draining the superseded generation", got)
	}
}

// TestRefinalizeClearsResolverAcceptanceSoLaneLosesAuthority covers the RUNS_ON
// side at the lane's authority decision: resolver source runs take the fenced
// path, so after a refinalize plus the resolution re-activation the lane
// selects the unit only if the acceptance row survived. Pre-refinalize the
// filter yields the intent; post-refinalize it yields nothing on fixed code
// (RED on base, where the row survives).
//
// This stays at FilterAuthoritativeIntents — the exact predicate both unit
// selection and per-unit filtering resolve through — rather than driving a
// full RUNS_ON drain, because on current main the fenced workload replay
// refuses a superseded generation and quarantines before any write (#7670),
// so a full-drain RUNS_ON test cannot go RED until that fix lands. The storage
// proof shows the resolver rows are deleted; this shows the lane consequence.
func TestRefinalizeClearsResolverAcceptanceSoLaneLosesAuthority(t *testing.T) {
	database, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, activeGeneration, _ := refinalizeResetScope(t, ctx, database, suffix)
	repoID := "repository:7673-runs-on-" + suffix
	sourceRunID := "repo_dependency:" + scopeID
	now := time.Now().UTC()

	seedRefinalizeResetAcceptance(t, ctx, database, scopeID, repoID, sourceRunID, activeGeneration, now)
	seedActiveRelationshipGeneration(t, ctx, database, activeGeneration, scopeID)
	intentID := "lane-7673-runson-" + suffix
	seedRefinalizeResetRepoDependencyIntent(t, ctx, database, reducer.SharedProjectionIntentRow{
		IntentID: intentID, ProjectionDomain: reducer.DomainRepoDependency,
		PartitionKey: "runs_on:" + repoID + "->platform:kubernetes:test", ScopeID: scopeID,
		AcceptanceUnitID: repoID, RepositoryID: repoID, SourceRunID: sourceRunID,
		GenerationID: activeGeneration, CreatedAt: now,
		Payload: map[string]any{
			"repo_id":           repoID,
			"platform_id":       "platform:kubernetes:test",
			"relationship_type": "RUNS_ON",
			"evidence_source":   reducer.CrossRepoEvidenceSource,
		},
	})

	lookup := refinalizeResetGatedAcceptedGen(database)
	readRows := func() []reducer.SharedProjectionIntentRow {
		rows, err := NewSharedIntentStore(SQLDB{DB: database}).ListAcceptanceUnitDomainIntents(
			ctx, repoID, reducer.DomainRepoDependency, 100)
		if err != nil {
			t.Fatalf("list acceptance unit intents: %v", err)
		}
		return rows
	}
	if active, _ := reducer.FilterAuthoritativeIntents(readRows(), lookup); len(active) != 1 {
		t.Fatalf("pre-refinalize authoritative intents = %d, want 1: the fenced unit must hold authority while accepted and active", len(active))
	}

	store := NewRecoveryStore(SQLDB{DB: database})
	if _, err := store.RefinalizeScopeProjections(ctx, recovery.RefinalizeFilter{
		ScopeIDs: []string{scopeID},
	}, time.Now().UTC()); err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil", err)
	}
	activateRefinalizeResetGeneration(t, ctx, database, scopeID, activeGeneration, "refinalize-reset-next-"+suffix)
	// Resolution re-activates the relationship generation from the preserved
	// facts after the refinalize retires it; without that step the fence
	// would defer on both arms and the test could not tell the fix apart.
	if _, err := database.ExecContext(ctx,
		`UPDATE relationship_generations SET status = 'active' WHERE generation_id = $1`, activeGeneration); err != nil {
		t.Fatalf("re-activate relationship generation %s: %v", activeGeneration, err)
	}

	active, stale := reducer.FilterAuthoritativeIntents(readRows(), lookup)
	if len(active) != 0 || len(stale) != 0 {
		t.Fatalf("post-refinalize authoritative = %d active + %d stale, want 0 + 0: "+
			"with no acceptance row the unit must not be selected", len(active), len(stale))
	}
}
