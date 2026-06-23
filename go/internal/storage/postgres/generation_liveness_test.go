package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestRecoverWedgedActiveGenerationsQueryContract pins the durable re-drive
// contract for wedged active generations: it must target only active
// generations whose activation aged past the deadline, must not touch a scope
// that already has a newer same-scope generation (the projector supersede path
// owns that case), must re-enqueue projector work idempotently, and must bound
// the re-drive by attempt count so a poison scope cannot loop forever.
func TestRecoverWedgedActiveGenerationsQueryContract(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"INSERT INTO fact_work_items",
		"'projector'",
		"generation.status = 'active'",
		"generation.activated_at < $1",
		"ON CONFLICT (work_item_id) DO UPDATE",
		"liveness_recovery_attempts",
		"NOT EXISTS",
		"newer.scope_id = generation.scope_id",
		"LIMIT $3",
		// A generation is wedged only if it has real outstanding downstream work,
		// not merely because it aged. Healthy quiet scopes stay active+projected
		// with all shared_projection_intents completed and must NOT be re-driven.
		"shared_projection_intents",
		"completed_at IS NULL",
	} {
		if !strings.Contains(recoverWedgedActiveGenerationsQuery, want) {
			t.Fatalf("recover wedged query missing %q:\n%s", want, recoverWedgedActiveGenerationsQuery)
		}
	}
	// A wedged active must never be reset to attempt_count = 0; that would erase
	// the bounded re-drive budget and let a poison scope loop forever.
	if strings.Contains(recoverWedgedActiveGenerationsQuery, "attempt_count = 0") {
		t.Fatalf("recover wedged query resets the re-drive budget:\n%s", recoverWedgedActiveGenerationsQuery)
	}
	// The re-drive budget must be capped at the max-attempts ceiling so the
	// durable counter can never exceed the budget on repeated re-enqueue.
	if !strings.Contains(recoverWedgedActiveGenerationsQuery, "LEAST(COALESCE((fact_work_items.payload ->> 'liveness_recovery_attempts')::int, 0) + 1, $2)") {
		t.Fatalf("recover wedged query does not cap the re-drive budget at the ceiling:\n%s", recoverWedgedActiveGenerationsQuery)
	}
}

// TestSupersedeOrphanedActiveGenerationsQueryContract pins the auto-supersede
// contract: when a newer same-scope generation is already authoritative, the
// stale older active generations for that scope must be superseded, never the
// newest one, and the write must be idempotent on already-superseded rows.
func TestSupersedeOrphanedActiveGenerationsQueryContract(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"UPDATE scope_generations",
		"status = 'superseded'",
		"superseded_at = $1",
		"stale.status = 'active'",
		"newer.scope_id = stale.scope_id",
		"newer.ingested_at > stale.ingested_at",
	} {
		if !strings.Contains(supersedeOrphanedActiveGenerationsQuery, want) {
			t.Fatalf("supersede orphaned query missing %q:\n%s", want, supersedeOrphanedActiveGenerationsQuery)
		}
	}
}

func TestGenerationLivenessStoreRecoverWedgedGenerations(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// supersede orphaned actives returns superseded generation ids.
			{rows: [][]any{{"scope-1", "gen-old"}}},
			// recover wedged returns re-enqueued (scope, generation) pairs.
			{rows: [][]any{{"scope-2", "gen-wedged-a"}, {"scope-3", "gen-wedged-b"}}},
		},
	}

	store := NewGenerationLivenessStore(db)
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	result, err := store.RecoverWedgedGenerations(context.Background(), GenerationLivenessPolicy{
		ActivationDeadline: 30 * time.Minute,
		MaxRecoverAttempts: 5,
		BatchLimit:         100,
	}, now)
	if err != nil {
		t.Fatalf("RecoverWedgedGenerations() error = %v, want nil", err)
	}
	if got, want := result.Superseded, 1; got != want {
		t.Fatalf("result.Superseded = %d, want %d", got, want)
	}
	if got, want := result.Recovered, 2; got != want {
		t.Fatalf("result.Recovered = %d, want %d", got, want)
	}
	if len(db.queries) != 2 {
		t.Fatalf("query count = %d, want 2", len(db.queries))
	}
	// The deadline argument must be now minus the activation deadline.
	deadlineArg, ok := db.queries[1].args[0].(time.Time)
	if !ok {
		t.Fatalf("recover query arg[0] = %T, want time.Time", db.queries[1].args[0])
	}
	if want := now.Add(-30 * time.Minute); !deadlineArg.Equal(want) {
		t.Fatalf("recover deadline = %v, want %v", deadlineArg, want)
	}
}

func TestGenerationLivenessStoreRecoverWedgedGenerationsRequiresDB(t *testing.T) {
	t.Parallel()

	store := NewGenerationLivenessStore(nil)
	_, err := store.RecoverWedgedGenerations(context.Background(), GenerationLivenessPolicy{}, time.Now())
	if err == nil {
		t.Fatal("RecoverWedgedGenerations() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("error = %q, want 'database is required'", err.Error())
	}
}

func TestGenerationLivenessStoreRecoverWedgedGenerationsPropagatesError(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{err: errors.New("connection refused")},
		},
	}

	store := NewGenerationLivenessStore(db)
	_, err := store.RecoverWedgedGenerations(context.Background(), GenerationLivenessPolicy{
		ActivationDeadline: time.Minute,
		BatchLimit:         10,
	}, time.Now())
	if err == nil {
		t.Fatal("RecoverWedgedGenerations() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "supersede orphaned active generations") {
		t.Fatalf("error = %q, want supersede context", err.Error())
	}
}

func TestGenerationLivenessStoreCountActiveByAge(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"fresh", int64(900)},
				{"aging", int64(70)},
				{"stuck", int64(12)},
			}},
		},
	}

	store := NewGenerationLivenessStore(db)
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	counts, err := store.CountActiveGenerationsByAge(context.Background(), GenerationLivenessPolicy{
		ActivationDeadline: 30 * time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("CountActiveGenerationsByAge() error = %v, want nil", err)
	}
	if got, want := counts["stuck"], int64(12); got != want {
		t.Fatalf("counts[stuck] = %d, want %d", got, want)
	}
	if got, want := counts["fresh"], int64(900); got != want {
		t.Fatalf("counts[fresh] = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "scope_generations") {
		t.Fatalf("count query missing scope_generations:\n%s", db.queries[0].query)
	}
	// The stuck bucket is the wedged-generation alarm and must require actual
	// downstream blockage (outstanding shared_projection_intents), so a healthy
	// quiet aged scope is counted aging/fresh, never stuck.
	if !strings.Contains(db.queries[0].query, "shared_projection_intents") {
		t.Fatalf("count query stuck bucket missing downstream-blockage gate:\n%s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "completed_at IS NULL") {
		t.Fatalf("count query stuck bucket missing completed_at IS NULL gate:\n%s", db.queries[0].query)
	}
}

// TestRecoverWedgedActiveGenerationsExcludesHealthyQuietScopes pins the
// false-positive guard: an aged active generation that is projected and quiet
// (all shared_projection_intents completed, no outstanding work) must NOT be
// re-driven. The query's downstream-blockage subquery is the gate; here we prove
// the store passes the deadline arg and emits the gate so a healthy quiet scope
// produces no re-enqueue rows.
func TestRecoverWedgedActiveGenerationsExcludesHealthyQuietScopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// supersede orphaned actives: none.
			{rows: [][]any{}},
			// recover wedged: a healthy quiet scope returns no rows because the
			// downstream-blockage gate excludes it.
			{rows: [][]any{}},
		},
	}

	store := NewGenerationLivenessStore(db)
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	result, err := store.RecoverWedgedGenerations(context.Background(), GenerationLivenessPolicy{
		ActivationDeadline: 30 * time.Minute,
		MaxRecoverAttempts: 5,
		BatchLimit:         100,
	}, now)
	if err != nil {
		t.Fatalf("RecoverWedgedGenerations() error = %v, want nil", err)
	}
	if got, want := result.Recovered, 0; got != want {
		t.Fatalf("result.Recovered = %d, want %d (healthy quiet scope must not be re-driven)", got, want)
	}
	// The recover query must carry the downstream-blockage gate so the exclusion
	// is enforced in SQL, not just by the empty fixture.
	if !strings.Contains(db.queries[1].query, "shared_projection_intents") ||
		!strings.Contains(db.queries[1].query, "completed_at IS NULL") {
		t.Fatalf("recover query missing downstream-blockage gate:\n%s", db.queries[1].query)
	}
}

// TestRecoverWedgedActiveGenerationsRecoversBlockedScopes proves the positive
// case: an aged active generation with outstanding downstream work (uncompleted
// shared_projection_intents) is re-driven.
func TestRecoverWedgedActiveGenerationsRecoversBlockedScopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// supersede orphaned actives: none.
			{rows: [][]any{}},
			// recover wedged: one blocked scope is re-driven.
			{rows: [][]any{{"scope-wedged", "gen-wedged"}}},
		},
	}

	store := NewGenerationLivenessStore(db)
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	result, err := store.RecoverWedgedGenerations(context.Background(), GenerationLivenessPolicy{
		ActivationDeadline: 30 * time.Minute,
		MaxRecoverAttempts: 5,
		BatchLimit:         100,
	}, now)
	if err != nil {
		t.Fatalf("RecoverWedgedGenerations() error = %v, want nil", err)
	}
	if got, want := result.Recovered, 1; got != want {
		t.Fatalf("result.Recovered = %d, want %d (blocked scope must be re-driven)", got, want)
	}
	if len(result.RecoveredScopeIDs) != 1 || result.RecoveredScopeIDs[0] != "scope-wedged" {
		t.Fatalf("result.RecoveredScopeIDs = %v, want [scope-wedged]", result.RecoveredScopeIDs)
	}
}
