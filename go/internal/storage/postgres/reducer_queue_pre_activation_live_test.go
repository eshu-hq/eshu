// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Real-queue proof for #6686: reducer work enqueued for a newer generation
// that has not activated yet must not be acked `succeeded` without its handler
// running.
//
// Everything on the queue side is production code against real PostgreSQL:
// ReducerQueue Enqueue/Claim (default claim mode)/Ack/Fail, Runtime.Execute
// with the production NewGenerationFreshnessCheck, and ProjectorQueue.Ack
// activating the generation. The acknowledgement mirrors
// Service.executeWithTelemetry: an Execute error goes to Fail, a result goes
// to Ack. Test-only pieces are the scope/generation rows, the generation's
// claimed projector row, and a handler stub that counts calls.
//
// The TestReducerContentionGate prefix puts this test in the contention gate's
// -run filter (.github/workflows/reducer-contention-gate.yml), which runs it
// against a real PostgreSQL service in CI.

const (
	preActivationProofScope      = "pre-activation-6686"
	preActivationProofLeaseOwner = "pre-activation-proof-projector"
)

// TestReducerContentionGatePreActivationGenerationRunsAfterActivation walks
// the #6686 window end to end: G1 active, G2 pending with its projector
// claimed; a reducer intent for G2 is enqueued and claimed before the
// projector's Ack. The claim must defer (retrying, handler not run), and after
// ProjectorQueue.Ack activates G2 the next claim must run the handler exactly
// once and ack it succeeded.
func TestReducerContentionGatePreActivationGenerationRunsAfterActivation(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	db := openCrossScopeReadinessProofDB(t, ctx, dsn)

	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Millisecond)
	seedPreActivationScope(t, ctx, db, base)

	calls := 0
	env := newPreActivationProofEnv(t, db, &calls)

	if _, err := env.queue.Enqueue(ctx, []runtime.ReducerIntent{{
		ScopeID:      preActivationProofScope,
		GenerationID: "gen-2",
		Domain:       reducer.DomainWorkloadMaterialization,
		EntityKey:    "workload:6686",
		Reason:       "pre-activation proof",
		FactID:       "fact-6686",
		SourceSystem: "git",
	}}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	// Claimed inside the activation window: G1 still active, G2 pending.
	if claimed := env.claimExecuteSettle(t, ctx); !claimed {
		t.Fatal("pre-activation Claim() claimed = false, want the G2 row (default claim mode has no projector gate)")
	}
	status, failureClass, attempts := readPreActivationWorkItem(t, ctx, db, "gen-2")
	if status == "succeeded" || calls != 0 {
		t.Fatalf(
			"pre-activation: status = %q, handler calls = %d; want a deferral (not succeeded, 0 calls): "+
				"a not-yet-active newer generation acked succeeded is never re-driven (#6686)",
			status, calls,
		)
	}
	if status != "retrying" || failureClass != reducercontract.GenerationActivationNotReadyFailureClass || attempts != 1 {
		t.Fatalf(
			"pre-activation: status=%q failure_class=%q attempt_count=%d, want retrying/generation_activation_not_ready/1",
			status, failureClass, attempts,
		)
	}

	// A non-counting class: re-claiming across more cycles than MaxAttempts
	// must neither dead-letter the row nor erode its attempt budget.
	for cycle := 0; cycle < 4; cycle++ {
		env.advance(time.Minute)
		if claimed := env.claimExecuteSettle(t, ctx); !claimed {
			t.Fatalf("defer cycle %d: Claim() claimed = false, want the deferred row back", cycle)
		}
		status, _, attempts = readPreActivationWorkItem(t, ctx, db, "gen-2")
		if status != "retrying" || attempts != 1 || calls != 0 {
			t.Fatalf("defer cycle %d: status=%q attempt_count=%d calls=%d, want retrying/1/0", cycle, status, attempts, calls)
		}
	}

	// The projector finishes G2 and activates it through production Ack.
	projectorQueue := NewProjectorQueue(SQLDB{DB: db}, preActivationProofLeaseOwner, time.Minute)
	if err := projectorQueue.Ack(ctx, projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: preActivationProofScope},
		Generation:   scope.ScopeGeneration{GenerationID: "gen-2", ScopeID: preActivationProofScope},
		AttemptCount: 1,
	}, runtime.Result{}); err != nil {
		t.Fatalf("ProjectorQueue.Ack(gen-2) error = %v", err)
	}

	env.advance(time.Minute)
	if claimed := env.claimExecuteSettle(t, ctx); !claimed {
		t.Fatal("post-activation Claim() claimed = false, want the deferred G2 row back")
	}
	if calls != 1 {
		t.Fatalf("post-activation handler calls = %d, want exactly 1", calls)
	}
	if status, _, _ := readPreActivationWorkItem(t, ctx, db, "gen-2"); status != "succeeded" {
		t.Fatalf("post-activation status = %q, want succeeded", status)
	}
	env.advance(time.Minute)
	if claimed := env.claimExecuteSettle(t, ctx); claimed {
		t.Fatal("drained queue Claim() claimed = true, want nothing left")
	}
	if calls != 1 {
		t.Fatalf("final handler calls = %d, want exactly 1", calls)
	}
}

// TestReducerContentionGateOlderGenerationSupersessionStaysTerminal keeps the
// true-supersession path terminal. An intent for a generation that sorts
// before the active one is superseded by the claim itself (the
// superseded_stale_reducer_generations CTE), so the handler never runs and no
// retry is scheduled. The production freshness check, read against the same
// rows, must agree: older, superseded, and unknown generations report
// (false, nil) -- terminal -- and only the newer pending generation reports a
// retryable not-yet-active error.
func TestReducerContentionGateOlderGenerationSupersessionStaysTerminal(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	db := openCrossScopeReadinessProofDB(t, ctx, dsn)

	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Millisecond)
	seedPreActivationScope(t, ctx, db, base)

	calls := 0
	env := newPreActivationProofEnv(t, db, &calls)
	// gen-0 is superseded; gen-old-pending is pending but sorts BEFORE the
	// active gen-1, so it can never activate ahead of it. Both stay terminal.
	for _, generationID := range []string{"gen-0", "gen-old-pending"} {
		if _, err := env.queue.Enqueue(ctx, []runtime.ReducerIntent{{
			ScopeID:      preActivationProofScope,
			GenerationID: generationID,
			Domain:       reducer.DomainWorkloadMaterialization,
			EntityKey:    "workload:6686:" + generationID,
			Reason:       "older generation proof",
			FactID:       "fact-6686-" + generationID,
			SourceSystem: "git",
		}}); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", generationID, err)
		}
		if claimed := env.claimExecuteSettle(t, ctx); claimed {
			t.Fatalf("Claim(%s) claimed = true, want the claim-time supersession to retire it", generationID)
		}
		if status, _, _ := readPreActivationWorkItem(t, ctx, db, generationID); status != "superseded" {
			t.Fatalf("%s: status = %q, want superseded (true supersession stays terminal)", generationID, status)
		}
	}
	if calls != 0 {
		t.Fatalf("handler calls = %d, want 0 for superseded generations", calls)
	}

	check := NewGenerationFreshnessCheck(SQLDB{DB: db})
	for _, tc := range []struct {
		generationID  string
		wantCurrent   bool
		wantRetryable bool
	}{
		{generationID: "gen-1", wantCurrent: true},
		{generationID: "gen-2", wantRetryable: true},
		{generationID: "gen-0"},
		{generationID: "gen-old-pending"},
		{generationID: "gen-missing"},
	} {
		current, err := check(ctx, preActivationProofScope, tc.generationID)
		if current != tc.wantCurrent || reducer.IsRetryable(err) != tc.wantRetryable || (err != nil) != tc.wantRetryable {
			t.Fatalf("check(%s) = (%v, %v), want current=%v retryable=%v",
				tc.generationID, current, err, tc.wantCurrent, tc.wantRetryable)
		}
	}
	if current, err := check(ctx, "scope-unknown-6686", "gen-2"); !current || err != nil {
		t.Fatalf("check(unknown scope) = (%v, %v), want (true, nil)", current, err)
	}
}

// preActivationProofEnv bundles the production queue and runtime under test.
type preActivationProofEnv struct {
	queue   ReducerQueue
	runtime *reducer.Runtime
	clock   *time.Time
}

func newPreActivationProofEnv(t *testing.T, db *sql.DB, calls *int) preActivationProofEnv {
	t.Helper()
	registry := reducer.NewRegistry()
	if err := registry.Register(reducer.DomainDefinition{
		Domain:        reducer.DomainWorkloadMaterialization,
		Summary:       "pre-activation proof handler",
		Ownership:     reducer.OwnershipShape{CrossSource: true, CrossScope: true, CanonicalWrite: true},
		TruthContract: testReducerTruthContract("workload"),
		Handler: reducer.HandlerFunc(func(_ context.Context, intent reducer.Intent) (reducer.Result, error) {
			*calls++
			return reducer.Result{IntentID: intent.IntentID, Domain: intent.Domain, Status: reducer.ResultStatusSucceeded}, nil
		}),
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	rt, err := reducer.NewRuntime(registry)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	rt.GenerationCheck = NewGenerationFreshnessCheck(SQLDB{DB: db})

	clock := time.Now().UTC()
	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "pre-activation-proof-reducer",
		LeaseDuration: time.Minute,
		RetryDelay:    time.Second,
		MaxAttempts:   2,
		ClaimDomains:  []reducer.Domain{reducer.DomainWorkloadMaterialization},
	}
	env := preActivationProofEnv{queue: queue, runtime: rt, clock: &clock}
	env.queue.Now = func() time.Time { return *env.clock }
	return env
}

func (e preActivationProofEnv) advance(d time.Duration) { *e.clock = e.clock.Add(d) }

// claimExecuteSettle claims one intent, executes it through the production
// runtime, and settles it exactly as Service.executeWithTelemetry does: an
// Execute error goes to Fail, a result (succeeded or superseded) goes to Ack.
func (e preActivationProofEnv) claimExecuteSettle(t *testing.T, ctx context.Context) bool {
	t.Helper()
	intent, claimed, err := e.queue.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !claimed {
		return false
	}
	result, execErr := e.runtime.Execute(ctx, intent)
	if execErr != nil {
		if err := e.queue.Fail(ctx, intent, execErr); err != nil {
			t.Fatalf("Fail() error = %v", err)
		}
		return true
	}
	if err := e.queue.Ack(ctx, intent, result); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	return true
}

// seedPreActivationScope writes gen-0 (superseded), gen-old-pending (pending
// but ingested before the active generation), gen-1 (active), and gen-2
// (pending, newest) plus gen-2's claimed projector row.
func seedPreActivationScope(t *testing.T, ctx context.Context, db *sql.DB, base time.Time) {
	t.Helper()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	exec(`
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id, payload
) VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', 'gen-1', '{}'::jsonb)`,
		preActivationProofScope, base)
	exec(`
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload
) VALUES
    ('gen-0', $1, 'snapshot', $2, $2, 'superseded', $2, '{}'::jsonb),
    ('gen-old-pending', $1, 'snapshot', $3, $3, 'pending', NULL, '{}'::jsonb),
    ('gen-1', $1, 'snapshot', $4, $4, 'active', $4, '{}'::jsonb),
    ('gen-2', $1, 'snapshot', $5, $5, 'pending', NULL, '{}'::jsonb)`,
		preActivationProofScope, base, base.Add(time.Minute), base.Add(2*time.Minute), base.Add(3*time.Minute))
	exec(`
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload, created_at, updated_at
) VALUES ('projector-gen-2', $1, 'gen-2', 'projector', 'source_local', 'running',
          1, $2, $3, $4, '{}'::jsonb, $4, $4)`,
		preActivationProofScope, preActivationProofLeaseOwner, base.Add(time.Hour), base.Add(3*time.Minute))
}

func readPreActivationWorkItem(t *testing.T, ctx context.Context, db *sql.DB, generationID string) (string, string, int) {
	t.Helper()
	var (
		status       string
		failureClass sql.NullString
		attempts     int
	)
	if err := db.QueryRowContext(ctx, `
SELECT status, failure_class, attempt_count
FROM fact_work_items
WHERE stage = 'reducer' AND scope_id = $1 AND generation_id = $2`,
		preActivationProofScope, generationID).Scan(&status, &failureClass, &attempts); err != nil {
		t.Fatalf("read reducer work item for %s: %v", generationID, err)
	}
	return status, failureClass.String, attempts
}
