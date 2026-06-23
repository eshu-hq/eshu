package postgres

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestGenerationLivenessIntegration exercises RecoverWedgedGenerations and
// CountActiveGenerationsByAge against a real Postgres instance seeded with
// scope_generations and fact_work_items fixtures. Set
// ESHU_GENERATION_LIVENESS_PROOF_DSN to a Postgres DSN to run the suite;
// the test is skipped when the env var is absent so the normal unit gate is
// unaffected.
//
// Each subtest provisions its own isolated schema so that a RecoverWedgedGenerations
// sweep in one subtest cannot mutate rows observed by another. This prevents the
// orphaned-supersede CTE from pre-incrementing liveness_recovery_attempts on
// gen-wedged before the WedgedReDrive subtest runs (which would exhaust the
// MaxRecoverAttempts=1 budget and cause Recovered=0).
//
// Scenarios covered:
//
//  1. Wedged re-drive: a generation that has aged past the activation deadline
//     and has outstanding shared_projection_intents is re-enqueued exactly once
//     with liveness_recovery_attempts = 1. A second sweep past the budget ceiling
//     (MaxRecoverAttempts = 1) does NOT re-enqueue — the budget gate and LEAST
//     cap prevent a poison scope from looping.
//
//  2. Orphaned supersede: an older active generation for a scope that already has
//     a newer active generation is retired to 'superseded'; the newest generation
//     is untouched. A repeat sweep is a no-op (idempotent).
//
//  3. No-op safety: a fresh active generation within the activation deadline is
//     never re-driven. A scope whose pending generation is newer than the active
//     one is also left alone (the projector supersede path owns that case).
//
//  4. CountActiveGenerationsByAge returns correct fresh/aging/stuck counts. The
//     stuck bucket requires an outstanding shared_projection_intents row; it does
//     not filter for newer siblings, so gen-pending-active is also stuck. The
//     aging bucket contains aged-but-drained generations (gen-aging, gen-orphaned-*).
func TestGenerationLivenessIntegration(t *testing.T) {
	dsn := os.Getenv("ESHU_GENERATION_LIVENESS_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_GENERATION_LIVENESS_PROOF_DSN to run the generation liveness integration proof")
	}

	// Policy: 30-minute activation deadline, budget of 1 re-drive attempt.
	policy := GenerationLivenessPolicy{
		ActivationDeadline: 30 * time.Minute,
		MaxRecoverAttempts: 1,
		BatchLimit:         100,
	}
	now := time.Now().UTC()

	// ---------------------------------------------------------------------------
	// Scenario 4: CountActiveGenerationsByAge before any recovery sweep.
	// Uses the full fixture so all bucket counts are exercised.
	// ---------------------------------------------------------------------------
	t.Run("CountActiveGenerationsByAge", func(t *testing.T) {
		db := openLivenessProofDB(t, dsn)
		provisionLivenessSchema(t, db, generationLivenessProofSeedSQL)
		store := NewGenerationLivenessStore(SQLDB{DB: db})
		ctx := context.Background()

		counts, err := store.CountActiveGenerationsByAge(ctx, policy, now)
		if err != nil {
			t.Fatalf("CountActiveGenerationsByAge() error = %v", err)
		}
		// Stuck bucket: generations past the full deadline with outstanding
		// shared_projection_intents (completed_at IS NULL).
		//   gen-wedged: activated 2h ago, outstanding intent → stuck.
		//   gen-pending-active: activated 2h ago, outstanding intent → stuck.
		//     (CountActiveGenerationsByAge does not filter for newer siblings —
		//      only RecoverWedgedGenerations excludes them via NOT EXISTS.)
		if got, want := counts["stuck"], int64(2); got != want {
			t.Fatalf("stuck count = %d, want %d", got, want)
		}
		// Fresh bucket: generations with no activated_at or activated within half
		// the activation deadline (15 min).
		//   gen-fresh: activated 5 min ago → fresh.
		if got, want := counts["fresh"], int64(1); got != want {
			t.Fatalf("fresh count = %d, want %d", got, want)
		}
		// Aging bucket: generations past half the deadline but not stuck.
		//   gen-aging: activated 20 min ago, all intents completed → aging.
		//   gen-orphaned-old: activated 3h ago, no intents → aging.
		//   gen-orphaned-new: activated 1h ago, no intents → aging.
		if got, want := counts["aging"], int64(3); got != want {
			t.Fatalf("aging count = %d, want %d", got, want)
		}
	})

	// ---------------------------------------------------------------------------
	// Scenario 2: orphaned supersede.
	// Isolated schema: only scope-orphaned is seeded so the wedged re-drive CTE
	// finds nothing to recover and cannot create a gen-wedged work item here.
	// scope-orphaned has two active generations: gen-orphaned-old and
	// gen-orphaned-new. The older one must be retired; the newer one must be
	// untouched. A second sweep is a no-op.
	// ---------------------------------------------------------------------------
	t.Run("OrphanedSupersede", func(t *testing.T) {
		db := openLivenessProofDB(t, dsn)
		provisionLivenessSchema(t, db, generationLivenessOrphanedOnlySeedSQL)
		store := NewGenerationLivenessStore(SQLDB{DB: db})
		ctx := context.Background()

		result, err := store.RecoverWedgedGenerations(ctx, policy, now)
		if err != nil {
			t.Fatalf("RecoverWedgedGenerations() error = %v", err)
		}
		// Exactly one orphaned active generation retired.
		if got, want := result.Superseded, 1; got != want {
			t.Fatalf("Superseded = %d, want %d", got, want)
		}
		if len(result.SupersededScopeIDs) != 1 || result.SupersededScopeIDs[0] != "scope-orphaned" {
			t.Fatalf("SupersededScopeIDs = %v, want [scope-orphaned]", result.SupersededScopeIDs)
		}

		// Verify gen-orphaned-old is now superseded in DB.
		var orphanedOldStatus string
		if err := db.QueryRowContext(ctx,
			"SELECT status FROM scope_generations WHERE generation_id = 'gen-orphaned-old'",
		).Scan(&orphanedOldStatus); err != nil {
			t.Fatalf("query gen-orphaned-old status: %v", err)
		}
		if orphanedOldStatus != "superseded" {
			t.Fatalf("gen-orphaned-old status = %q, want 'superseded'", orphanedOldStatus)
		}

		// Verify gen-orphaned-new is still active.
		var orphanedNewStatus string
		if err := db.QueryRowContext(ctx,
			"SELECT status FROM scope_generations WHERE generation_id = 'gen-orphaned-new'",
		).Scan(&orphanedNewStatus); err != nil {
			t.Fatalf("query gen-orphaned-new status: %v", err)
		}
		if orphanedNewStatus != "active" {
			t.Fatalf("gen-orphaned-new status = %q, want 'active'", orphanedNewStatus)
		}

		// Second sweep is idempotent: no more orphaned actives for scope-orphaned.
		result2, err := store.RecoverWedgedGenerations(ctx, policy, now)
		if err != nil {
			t.Fatalf("second RecoverWedgedGenerations() error = %v", err)
		}
		if result2.Superseded != 0 {
			t.Fatalf("second sweep Superseded = %d, want 0 (idempotent)", result2.Superseded)
		}
	})

	// ---------------------------------------------------------------------------
	// Scenario 1: wedged re-drive.
	// Isolated schema: only scope-wedged is seeded so liveness_recovery_attempts
	// starts at zero. No prior orphaned-supersede sweep can pre-increment the
	// counter before this subtest runs.
	// gen-wedged: active for scope-wedged, activated 2h ago, has an outstanding
	// shared_projection_intents row (completed_at IS NULL), no newer generation
	// for the scope. First sweep must re-enqueue exactly one projector work item
	// with liveness_recovery_attempts = 1.
	// ---------------------------------------------------------------------------
	t.Run("WedgedReDrive", func(t *testing.T) {
		db := openLivenessProofDB(t, dsn)
		provisionLivenessSchema(t, db, generationLivenessWedgedOnlySeedSQL)
		store := NewGenerationLivenessStore(SQLDB{DB: db})
		ctx := context.Background()

		result, err := store.RecoverWedgedGenerations(ctx, policy, now)
		if err != nil {
			t.Fatalf("RecoverWedgedGenerations() error = %v", err)
		}
		if got, want := result.Recovered, 1; got != want {
			t.Fatalf("Recovered = %d, want %d", got, want)
		}
		if len(result.RecoveredScopeIDs) != 1 || result.RecoveredScopeIDs[0] != "scope-wedged" {
			t.Fatalf("RecoveredScopeIDs = %v, want [scope-wedged]", result.RecoveredScopeIDs)
		}

		// Verify the projector work item exists and has liveness_recovery_attempts = 1.
		var attempts int
		if err := db.QueryRowContext(ctx, `
			SELECT (payload ->> 'liveness_recovery_attempts')::int
			FROM fact_work_items
			WHERE scope_id = 'scope-wedged'
			  AND generation_id = 'gen-wedged'
			  AND stage = 'projector'
		`).Scan(&attempts); err != nil {
			t.Fatalf("query liveness_recovery_attempts: %v", err)
		}
		if attempts != 1 {
			t.Fatalf("liveness_recovery_attempts = %d, want 1", attempts)
		}

		// Second sweep: budget ceiling is MaxRecoverAttempts = 1, so the work item
		// already has liveness_recovery_attempts = 1 which equals the ceiling. The
		// wedged CTE must exclude it and return zero recovered.
		result2, err := store.RecoverWedgedGenerations(ctx, policy, now)
		if err != nil {
			t.Fatalf("second RecoverWedgedGenerations() error = %v", err)
		}
		if result2.Recovered != 0 {
			t.Fatalf("second sweep Recovered = %d, want 0 (budget exhausted)", result2.Recovered)
		}

		// The work item counter must NOT exceed the ceiling.
		var attemptsAfter int
		if err := db.QueryRowContext(ctx, `
			SELECT (payload ->> 'liveness_recovery_attempts')::int
			FROM fact_work_items
			WHERE scope_id = 'scope-wedged'
			  AND generation_id = 'gen-wedged'
			  AND stage = 'projector'
		`).Scan(&attemptsAfter); err != nil {
			t.Fatalf("query liveness_recovery_attempts after second sweep: %v", err)
		}
		if attemptsAfter != 1 {
			t.Fatalf("liveness_recovery_attempts after second sweep = %d, want 1 (LEAST cap)", attemptsAfter)
		}
	})

	// ---------------------------------------------------------------------------
	// Scenario 3: no-op safety.
	// Uses the full fixture. No recovery sweep has run on this schema, so
	// scope-fresh and scope-pending-newer are pristine candidates to verify
	// they are correctly excluded.
	// ---------------------------------------------------------------------------
	t.Run("NoOpSafety", func(t *testing.T) {
		db := openLivenessProofDB(t, dsn)
		provisionLivenessSchema(t, db, generationLivenessProofSeedSQL)
		store := NewGenerationLivenessStore(SQLDB{DB: db})
		ctx := context.Background()

		// Run a sweep. scope-fresh and scope-pending-newer must not be touched.
		// scope-orphaned will be superseded and scope-wedged recovered, but this
		// subtest only asserts on the safe scopes.
		result, err := store.RecoverWedgedGenerations(ctx, policy, now)
		if err != nil {
			t.Fatalf("RecoverWedgedGenerations() error = %v", err)
		}
		if result.Recovered != 1 {
			t.Fatalf("no-op sweep Recovered = %d, want 1 (only scope-wedged)", result.Recovered)
		}
		if result.Superseded != 1 {
			t.Fatalf("no-op sweep Superseded = %d, want 1 (only scope-orphaned)", result.Superseded)
		}

		// Confirm scope-fresh generation status unchanged.
		var freshStatus string
		if err := db.QueryRowContext(ctx,
			"SELECT status FROM scope_generations WHERE generation_id = 'gen-fresh'",
		).Scan(&freshStatus); err != nil {
			t.Fatalf("query gen-fresh status: %v", err)
		}
		if freshStatus != "active" {
			t.Fatalf("gen-fresh status = %q, want 'active'", freshStatus)
		}

		// Confirm scope-pending-newer active generation unchanged.
		var pendingStatus string
		if err := db.QueryRowContext(ctx,
			"SELECT status FROM scope_generations WHERE generation_id = 'gen-pending-active'",
		).Scan(&pendingStatus); err != nil {
			t.Fatalf("query gen-pending-active status: %v", err)
		}
		if pendingStatus != "active" {
			t.Fatalf("gen-pending-active status = %q, want 'active'", pendingStatus)
		}

		// Confirm no projector work item created for scope-fresh or scope-pending-newer.
		var noOpCount int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM fact_work_items
			WHERE scope_id IN ('scope-fresh', 'scope-pending-newer')
			  AND stage = 'projector'
		`).Scan(&noOpCount); err != nil {
			t.Fatalf("count no-op work items: %v", err)
		}
		if noOpCount != 0 {
			t.Fatalf("projector work items for safe scopes = %d, want 0", noOpCount)
		}
	})
}
