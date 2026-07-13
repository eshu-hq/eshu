// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
		"reducer_work.stage = 'reducer'",
		"reducer_work.status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter')",
		"projector_work.stage = 'projector'",
		"projector_work.domain = 'source_local'",
		// The in-flight guard excludes queued rows unconditionally and
		// claimed/running rows only while their lease is still live; see
		// TestRecoverWedgedActiveGenerationsQueryReclaimsExpiredProjectorLease
		// for the #4464 Bug 2 orphaned-lease reclaim contract.
		"projector_work.status IN ('pending', 'retrying')",
		"projector_work.claim_until > $4",
		"existing.domain = 'source_local'",
		"intent.projection_domain = 'repo_dependency'",
		"intent.source_run_id = 'repo_dependency'",
		"starts_with(intent.source_run_id, 'repo_dependency:')",
	} {
		if !strings.Contains(recoverWedgedActiveGenerationsQuery, want) {
			t.Fatalf("recover wedged query missing %q:\n%s", want, recoverWedgedActiveGenerationsQuery)
		}
	}
	if strings.Contains(recoverWedgedActiveGenerationsQuery, "projector_work.status IN ('pending', 'claimed', 'running', 'retrying', 'succeeded')") {
		t.Fatalf("recover wedged query must not exclude succeeded source-local projector rows:\n%s", recoverWedgedActiveGenerationsQuery)
	}
	if strings.Contains(recoverWedgedActiveGenerationsQuery, "graph_projection_phase_state") ||
		strings.Contains(recoverWedgedActiveGenerationsQuery, "backward_evidence_committed'") {
		t.Fatalf("recover wedged query must not treat shared-resolver readiness as a source-local wedge:\n%s", recoverWedgedActiveGenerationsQuery)
	}
	if strings.Contains(recoverWedgedActiveGenerationsQuery, "LIKE 'repo_dependency:%'") {
		t.Fatalf("recover wedged query must not use an unescaped underscore as a LIKE wildcard:\n%s", recoverWedgedActiveGenerationsQuery)
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

// TestRecoverWedgedActiveGenerationsQueryReclaimsExpiredProjectorLease pins
// the #4464 Bug 2 fix: a source-local projector work item stuck in
// 'claimed'/'running' with an EXPIRED lease (claim_until in the past) must
// NOT block the wedged re-drive. Before this fix, the in-flight guard
// excluded any 'claimed'/'running' row regardless of lease expiry, so a
// one-shot claimer (bootstrap-index) that died holding a claim left the row
// permanently invisible to this liveness sweep — the only other consumer
// that could ever transition the row out of 'claimed'/'running' is a fresh
// Claim() call, and nothing in the runtime topology issues one once the sole
// source_local claimer process has exited (ingester depends_on
// bootstrap-index: condition: service_completed_successfully, so it never
// starts after a bootstrap-index crash). A live (unexpired) lease must still
// block re-drive, since that row is genuinely being worked right now.
func TestRecoverWedgedActiveGenerationsQueryReclaimsExpiredProjectorLease(t *testing.T) {
	t.Parallel()

	if !strings.Contains(recoverWedgedActiveGenerationsQuery, "projector_work.claim_until > $4") {
		t.Fatalf(
			"recover wedged query must gate the claimed/running exclusion on an unexpired lease (projector_work.claim_until > $4):\n%s",
			recoverWedgedActiveGenerationsQuery,
		)
	}
	// The exclusion for claimed/running rows must be conditioned on the lease
	// still being live, not on status alone — a bare
	// status IN ('claimed', 'running') check without a claim_until comparison
	// would re-introduce the orphaned-lease wedge.
	if strings.Contains(recoverWedgedActiveGenerationsQuery, "projector_work.status IN ('pending', 'claimed', 'running', 'retrying')") {
		t.Fatalf(
			"recover wedged query still excludes claimed/running rows unconditionally on status alone (must also require an unexpired lease):\n%s",
			recoverWedgedActiveGenerationsQuery,
		)
	}
	// pending/retrying rows have no active lease owner but are still
	// legitimately queued for a live claimer, so they must remain excluded
	// unconditionally.
	if !strings.Contains(recoverWedgedActiveGenerationsQuery, "projector_work.status IN ('pending', 'retrying')") {
		t.Fatalf(
			"recover wedged query must still unconditionally exclude pending/retrying source-local projector rows:\n%s",
			recoverWedgedActiveGenerationsQuery,
		)
	}
}

// TestRecoverWedgedActiveGenerationsQueryReVerifiesLeaseAtWriteTime pins a
// TOCTOU fix on top of TestRecoverWedgedActiveGenerationsQueryReclaimsExpiredProjectorLease:
// the wedged CTE reads the projector row's lease state from a snapshot taken
// before the re_enqueued INSERT ... ON CONFLICT DO UPDATE executes. Between
// that snapshot and the UPDATE, a live worker's Heartbeat can extend
// claim_until (or a fresh Claim() can move status to claimed/running),
// making the row genuinely in-flight again. Without a WHERE guard on the
// ON CONFLICT DO UPDATE that re-checks the same in-flight condition at write
// time, the UPDATE would unconditionally clobber that concurrently-renewed
// claim back to lease_owner=NULL/claim_until=NULL — reintroducing, at the
// liveness-sweep layer, the same "one actor's write silently cancels
// another's in-flight work" defect class this issue is about, just via a
// write race instead of a shared context.
func TestRecoverWedgedActiveGenerationsQueryReVerifiesLeaseAtWriteTime(t *testing.T) {
	t.Parallel()

	if !strings.Contains(recoverWedgedActiveGenerationsQuery, "ON CONFLICT (work_item_id) DO UPDATE") {
		t.Fatalf("recover wedged query missing the expected upsert clause:\n%s", recoverWedgedActiveGenerationsQuery)
	}
	// The WHERE guard must sit after the ON CONFLICT DO UPDATE's SET clause
	// (i.e. as the conflict-action WHERE, which Postgres evaluates per
	// candidate row under the row's own lock, atomically with the write) and
	// must re-express the same in-flight condition as the read-side gate, so
	// a row that became genuinely in-flight between the SELECT snapshot and
	// this UPDATE is left untouched instead of being clobbered.
	setIdx := strings.Index(recoverWedgedActiveGenerationsQuery, "ON CONFLICT (work_item_id) DO UPDATE")
	if setIdx < 0 {
		t.Fatal("could not locate ON CONFLICT DO UPDATE clause")
	}
	conflictAction := recoverWedgedActiveGenerationsQuery[setIdx:]
	if !strings.Contains(conflictAction, "WHERE NOT (") {
		t.Fatalf(
			"recover wedged query's ON CONFLICT DO UPDATE is missing a write-time re-verification WHERE guard (TOCTOU: a concurrent Heartbeat/Claim between the wedged CTE snapshot and this UPDATE must not be clobbered):\n%s",
			conflictAction,
		)
	}
	if !strings.Contains(conflictAction, "fact_work_items.status IN ('pending', 'retrying')") ||
		!strings.Contains(conflictAction, "fact_work_items.status IN ('claimed', 'running')") ||
		!strings.Contains(conflictAction, "fact_work_items.claim_until > $4") {
		t.Fatalf(
			"recover wedged query's write-time guard must mirror the read-side in-flight condition exactly (status IN pending/retrying, or claimed/running with a live lease):\n%s",
			conflictAction,
		)
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
	if !strings.Contains(db.queries[0].query, "reducer_work.stage = 'reducer'") {
		t.Fatalf("count query stuck bucket missing reducer-work backlog exclusion:\n%s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "projector_work.stage = 'projector'") ||
		!strings.Contains(db.queries[0].query, "projector_work.status IN ('pending', 'retrying')") ||
		!strings.Contains(db.queries[0].query, "projector_work.claim_until > $3") {
		t.Fatalf("count query stuck bucket missing source-local in-flight projector gate (queued rows excluded unconditionally, claimed/running only while the lease is live):\n%s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "intent.projection_domain = 'repo_dependency'") ||
		!strings.Contains(db.queries[0].query, "intent.source_run_id = 'repo_dependency'") ||
		!strings.Contains(db.queries[0].query, "starts_with(intent.source_run_id, 'repo_dependency:')") {
		t.Fatalf("count query stuck bucket missing exact shared-resolver exclusion:\n%s", db.queries[0].query)
	}
	if strings.Contains(db.queries[0].query, "graph_projection_phase_state") ||
		strings.Contains(db.queries[0].query, "backward_evidence_committed'") {
		t.Fatalf("count query must not treat shared-resolver readiness as a source-local wedge:\n%s", db.queries[0].query)
	}
	if strings.Contains(db.queries[0].query, "LIKE 'repo_dependency:%'") {
		t.Fatalf("count query must not use an unescaped underscore as a LIKE wildcard:\n%s", db.queries[0].query)
	}
	if strings.Contains(db.queries[0].query, "projector_work.status IN ('pending', 'claimed', 'running', 'retrying', 'succeeded')") {
		t.Fatalf("count query stuck bucket must not suppress succeeded source-local projector rows:\n%s", db.queries[0].query)
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
