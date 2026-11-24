// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	completionstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/scope/completion"
)

// cicdGate is the fixture of the #10972 PR-4 gate proofs: a settled fleet of
// supply_chain_impact findings items (the completion consumers) and one
// claimed ci_cd_run_correlation item whose ack decides the event.
type cicdGate struct {
	t     *testing.T
	ctx   context.Context
	db    *sql.DB
	queue ReducerQueue
	scope string
	gen   string
	item  string
}

const cicdGateOwner = "cicd-gate-owner"

func newCICDGate(t *testing.T, name string) *cicdGate {
	t.Helper()
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)
	g := &cicdGate{
		t: t, ctx: ctx, db: db, queue: NewReducerQueue(SQLDB{DB: db}, cicdGateOwner, time.Minute),
		scope: "repository:10972-gate-ci-" + name, gen: "generation:10972-gate-ci-" + name, item: "reducer_10972_gate_ci_" + name,
	}
	for i := 0; i < cicdBoundFindings; i++ {
		scope := fmt.Sprintf("repository:10972-gate-%s-%02d", name, i)
		gen := "generation:" + scope
		seedContainerImageIdentityAckScope(t, ctx, db, scope)
		seedContainerImageIdentityAckGeneration(t, ctx, db, scope, gen)
		if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, gen); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'supply_chain_impact', 'succeeded', 'intent', $1, '{}'::jsonb, now() - interval '1 hour', now() - interval '1 hour')`,
			fmt.Sprintf("reducer_10972_gate_findings_%s_%02d", name, i), scope, gen); err != nil {
			t.Fatal(err)
		}
	}
	seedContainerImageIdentityAckScope(t, ctx, db, g.scope)
	seedContainerImageIdentityAckGeneration(t, ctx, db, g.scope, g.gen)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'ci_cd_run_correlation', 'pending', 'intent', $1, '{}'::jsonb, now() - interval '1 hour', now() - interval '1 hour')`,
		g.item, g.scope, g.gen); err != nil {
		t.Fatal(err)
	}
	return g
}

// claim takes the ci_cd item the way a claim does: lease, a strictly newer
// last_attempt_at, attempt count plus one. It returns the claimed intent.
func (g *cicdGate) claim() reducercontract.Intent {
	g.t.Helper()
	var claimedAt time.Time
	var attempts int
	if err := g.db.QueryRowContext(g.ctx, `
UPDATE fact_work_items SET status = 'claimed', lease_owner = $2, claim_until = clock_timestamp() + interval '1 minute',
    attempt_count = attempt_count + 1,
    last_attempt_at = date_trunc('microseconds', GREATEST(clock_timestamp(), COALESCE(last_attempt_at, clock_timestamp()) + interval '1 millisecond'))
WHERE work_item_id = $1 RETURNING last_attempt_at, attempt_count`, g.item, cicdGateOwner).Scan(&claimedAt, &attempts); err != nil {
		g.t.Fatalf("claim ci_cd item: %v", err)
	}
	claimedAt = claimedAt.UTC()
	return reducercontract.Intent{
		IntentID: g.item, ScopeID: g.scope, GenerationID: g.gen, Domain: reducer.DomainCICDRunCorrelation,
		AttemptCount: attempts, ClaimedAt: &claimedAt, SourceSystem: "github_actions",
	}
}

// write runs the real writer, in its transaction, with the real owed
// recorder, and returns the result the handler would hand the queue.
func (g *cicdGate) write(intent reducercontract.Intent, decisions []cicdrun.CICDRunCorrelationDecision) reducercontract.Result {
	g.t.Helper()
	writer := cicdrun.PostgresCICDRunCorrelationWriter{
		DB: SQLDB{DB: g.db}, Beginner: SQLDB{DB: g.db}, CompletionOwed: CICDCompletionOwedMarker{LeaseOwner: cicdGateOwner},
	}
	written, err := writer.WriteCICDRunCorrelations(g.ctx, cicdrun.CICDRunCorrelationWrite{
		IntentID: intent.IntentID, ScopeID: intent.ScopeID, GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem, Cause: "pass " + intent.ClaimedAt.String(), Decisions: decisions,
		ClaimedAt: *intent.ClaimedAt,
	})
	if err != nil {
		g.t.Fatalf("write ci_cd decisions: %v", err)
	}
	if written.CompletionChanged == nil {
		g.t.Fatal("writer with a beginner returned no changed verdict")
	}
	changed := 0.0
	if *written.CompletionChanged {
		changed = 1
	}
	return reducercontract.Result{
		IntentID: intent.IntentID, Domain: reducer.DomainCICDRunCorrelation, Status: reducercontract.ResultStatusSucceeded,
		CanonicalWrites: written.CanonicalWrites, SubSignals: map[string]float64{cicdrun.CompletionChangedSignal: changed},
	}
}

func (g *cicdGate) ack(intent reducercontract.Intent, result reducercontract.Result) {
	g.t.Helper()
	if err := g.queue.Ack(g.ctx, intent, result); err != nil {
		g.t.Fatalf("ack ci_cd pass: %v", err)
	}
}

// fanOutRePends runs the real completion store once (if an event is pending),
// returns how many settled findings items it re-pended, and settles them again
// so the next step starts from the same state.
func (g *cicdGate) fanOutRePends() int {
	g.t.Helper()
	store := completionstore.NewCrossScopeCompletionStore(SQLDB{DB: g.db})
	store.Now = func() time.Time { return time.Now().UTC().Add(5 * time.Second) }
	if lease, ok, err := store.Claim(g.ctx, cicdGateOwner, time.Minute); err != nil {
		g.t.Fatalf("claim completion event: %v", err)
	} else if ok {
		if _, err := store.Fanout(g.ctx, lease, 500); err != nil {
			g.t.Fatalf("fan out completion event: %v", err)
		}
	}
	var repended int
	if err := g.db.QueryRowContext(g.ctx, `SELECT count(*) FROM fact_work_items
WHERE domain = 'supply_chain_impact' AND work_item_id LIKE 'reducer_10972_gate_findings_%' AND status <> 'succeeded'`).Scan(&repended); err != nil {
		g.t.Fatal(err)
	}
	if _, err := g.db.ExecContext(g.ctx, `UPDATE fact_work_items SET status = 'succeeded', reopened_at = NULL
WHERE work_item_id LIKE 'reducer_10972_gate_findings_%'`); err != nil {
		g.t.Fatal(err)
	}
	return repended
}

func (g *cicdGate) owed() bool {
	g.t.Helper()
	var owed bool
	if err := g.db.QueryRowContext(g.ctx, `SELECT COALESCE(payload->>$2, '') = 'true' FROM fact_work_items WHERE work_item_id = $1`,
		g.item, cicdrun.PayloadKeyCompletionOwed).Scan(&owed); err != nil {
		g.t.Fatal(err)
	}
	return owed
}

func gateDecision(outcome cicdrun.CICDRunCorrelationOutcome, environment string) cicdrun.CICDRunCorrelationDecision {
	return cicdrun.CICDRunCorrelationDecision{
		Provider: "github_actions", RunID: "run-10972", RunAttempt: "1",
		RepositoryID: "repository:r_10972_gate", CommitSHA: "abc123", Environment: environment,
		ArtifactDigest: "sha256:10972", ImageRef: "registry.example.com/gate:1", Outcome: outcome,
		CanonicalWrites: 1, EvidenceFactIDs: []string{"fact:10972:gate:" + environment},
	}
}

// TestCICDCompletionGateFollowsTheWritersVerdictLive drives the real writer
// and the real ack through one item's life (#10972 PR-4): the first success
// (no baseline) emits and re-pends all 30 settled findings items; an identical
// rewrite with only per-pass fields different (intent, cause, evidence ids)
// emits nothing; a changed environment emits; an outcome downgrade from exact
// to rejected (the writer's form of a retraction: it never tombstones) emits.
// Every emitting ack clears the owed marker.
//
// Expected: RED before the ack gate (the first ack leaves the owed marker, and
// the unchanged step re-pends 30), GREEN after.
func TestCICDCompletionGateFollowsTheWritersVerdictLive(t *testing.T) {
	g := newCICDGate(t, "verdict")
	steps := []struct {
		name     string
		decision cicdrun.CICDRunCorrelationDecision
		want     int
	}{
		{"first success", gateDecision(cicdrun.CICDRunCorrelationExact, "prod"), cicdBoundFindings},
		{"identical rewrite", gateDecision(cicdrun.CICDRunCorrelationExact, "prod"), 0},
		{"environment changed", gateDecision(cicdrun.CICDRunCorrelationExact, "staging"), cicdBoundFindings},
		{"outcome downgraded", gateDecision(cicdrun.CICDRunCorrelationRejected, "staging"), cicdBoundFindings},
		{"identical rewrite again", gateDecision(cicdrun.CICDRunCorrelationRejected, "staging"), 0},
	}
	for _, step := range steps {
		intent := g.claim()
		decision := step.decision
		decision.EvidenceFactIDs = []string{"fact:10972:gate:" + intent.ClaimedAt.String()} // a per-pass field
		g.ack(intent, g.write(intent, []cicdrun.CICDRunCorrelationDecision{decision}))
		if g.owed() {
			t.Fatalf("%s: owed marker still set after the ack", step.name)
		}
		if got := g.fanOutRePends(); got != step.want {
			t.Fatalf("%s: re-pended %d settled findings items, want %d", step.name, got, step.want)
		}
	}
}

// TestCICDCompletionEmitsForAPassThatCommittedAndDiedBeforeItsAckLive is the
// hard edge: an attempt writes a changed row (the marker is set in its writer
// transaction) and dies before its ack. The retry's own rewrite is identical,
// so its verdict is unchanged, but the marker makes its ack emit, and the ack
// clears it.
//
// Expected: RED before the ack gate (the ack never clears the marker), GREEN
// after.
func TestCICDCompletionEmitsForAPassThatCommittedAndDiedBeforeItsAckLive(t *testing.T) {
	g := newCICDGate(t, "crash")
	decision := []cicdrun.CICDRunCorrelationDecision{gateDecision(cicdrun.CICDRunCorrelationExact, "prod")}
	died := g.claim()
	if result := g.write(died, decision); result.SubSignals[cicdrun.CompletionChangedSignal] != 1 {
		t.Fatalf("first attempt verdict = %v, want changed", result.SubSignals)
	}
	if !g.owed() {
		t.Fatal("the changed write did not record the owed marker")
	}
	// The attempt dies: no ack. The lease expires and a new claim retries.
	retry := g.claim()
	result := g.write(retry, decision)
	if result.SubSignals[cicdrun.CompletionChangedSignal] != 0 {
		t.Fatalf("retry verdict = %v, want unchanged (its own rewrite is identical)", result.SubSignals)
	}
	g.ack(retry, result)
	if g.owed() {
		t.Fatal("the emitting ack left the owed marker")
	}
	if got := g.fanOutRePends(); got != cicdBoundFindings {
		t.Fatalf("retry ack re-pended %d, want %d: the committed change must still reach the consumers", got, cicdBoundFindings)
	}
}

// TestCICDCompletionChangedSuccessRePendsEverySettledItemLive is the twin of
// the unchanged-success bound: a paired result with the changed verdict
// emits through the single ack and re-pends every settled findings item.
//
// Expected: GREEN before and after the gate.
func TestCICDCompletionChangedSuccessRePendsEverySettledItemLive(t *testing.T) {
	g := newCICDGate(t, "changed")
	intent := g.claim()
	g.ack(intent, reducercontract.Result{
		IntentID: g.item, Domain: reducer.DomainCICDRunCorrelation, Status: reducercontract.ResultStatusSucceeded,
		SubSignals: map[string]float64{cicdrun.CompletionChangedSignal: 1},
	})
	if got := g.fanOutRePends(); got != cicdBoundFindings {
		t.Fatalf("changed success re-pended %d, want %d", got, cicdBoundFindings)
	}
}

// TestCICDCompletionBatchAckGatesLive covers the batch ack variant
// (ReducerQueue.AckBatch, ackCICDRunCorrelationReducerWorkBatchQuery): a
// paired unchanged result adds no event, a paired changed result emits, and an
// item with no paired result emits (fail open).
//
// Expected: RED before the gate on the unchanged case, GREEN after.
func TestCICDCompletionBatchAckGatesLive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results func(reducercontract.Intent) []reducercontract.Result
		want    int
	}{
		{"paired unchanged", func(i reducercontract.Intent) []reducercontract.Result {
			return []reducercontract.Result{{
				IntentID: i.IntentID, Domain: i.Domain, Status: reducercontract.ResultStatusSucceeded,
				SubSignals: map[string]float64{cicdrun.CompletionChangedSignal: 0},
			}}
		}, 0},
		{"paired changed", func(i reducercontract.Intent) []reducercontract.Result {
			return []reducercontract.Result{{
				IntentID: i.IntentID, Domain: i.Domain, Status: reducercontract.ResultStatusSucceeded,
				SubSignals: map[string]float64{cicdrun.CompletionChangedSignal: 1},
			}}
		}, cicdBoundFindings},
		{"missing paired result", func(reducercontract.Intent) []reducercontract.Result { return nil }, cicdBoundFindings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newCICDGate(t, "batch")
			intent := g.claim()
			if err := g.queue.AckBatch(g.ctx, []reducercontract.Intent{intent}, tc.results(intent)); err != nil {
				t.Fatalf("batch ack: %v", err)
			}
			if got := g.fanOutRePends(); got != tc.want {
				t.Fatalf("batch ack re-pended %d, want %d", got, tc.want)
			}
		})
	}
}

// TestCICDCompletionSingleAckWithoutAVerdictEmitsLive: a single-ack result
// that carries no verdict (an unwired writer, or a future caller) emits.
//
// Expected: GREEN before and after the gate.
func TestCICDCompletionSingleAckWithoutAVerdictEmitsLive(t *testing.T) {
	g := newCICDGate(t, "noverdict")
	intent := g.claim()
	g.ack(intent, reducercontract.Result{IntentID: g.item, Domain: reducer.DomainCICDRunCorrelation, Status: reducercontract.ResultStatusSucceeded})
	if got := g.fanOutRePends(); got != cicdBoundFindings {
		t.Fatalf("ack without a verdict re-pended %d, want %d", got, cicdBoundFindings)
	}
}

// TestCICDCompletionOldReducerAckStillEmitsThroughTheTriggerLive is the
// rolling-upgrade path: an old reducer acks with a plain status update that
// does not bump cross_scope_completion_ack_epoch, so the migration 093 row
// trigger (fact_work_items_cross_scope_completion) emits the event.
//
// Expected: GREEN before and after the gate (the gate does not touch it).
func TestCICDCompletionOldReducerAckStillEmitsThroughTheTriggerLive(t *testing.T) {
	g := newCICDGate(t, "old")
	g.claim()
	if _, err := g.db.ExecContext(g.ctx, `UPDATE fact_work_items SET status = 'succeeded', lease_owner = NULL, claim_until = NULL
WHERE work_item_id = $1`, g.item); err != nil {
		t.Fatal(err)
	}
	if got := g.fanOutRePends(); got != cicdBoundFindings {
		t.Fatalf("old-reducer ack re-pended %d, want %d through the 093 trigger", got, cicdBoundFindings)
	}
}
