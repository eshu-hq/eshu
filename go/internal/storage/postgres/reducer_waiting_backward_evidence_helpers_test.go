// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// Helpers for the #11104 backward-evidence wake proofs. The waiter is a real
// deployment_mapping row deferred by the real cross-repo handler gate (the
// production readiness prefetch over graph_projection_phase_state), and the
// provider is the real deferred-backfill fan-in entry
// (backfill.Store.PublishDeferredBackfillPartitions) built through the root
// seam, so the proofs exercise the shipped park, publish, wake, and audit SQL.

// backwardEvidenceEmptyLoader returns no evidence, so a handler that passed
// the readiness gate takes the empty-evidence path and succeeds.
type backwardEvidenceEmptyLoader struct{}

func (backwardEvidenceEmptyLoader) ListEvidenceFacts(context.Context, string) ([]relationships.EvidenceFact, error) {
	return nil, nil
}

// backwardEvidenceIntentSink accepts retract intents without writing them.
type backwardEvidenceIntentSink struct{}

func (backwardEvidenceIntentSink) UpsertIntents(context.Context, []sharedintent.Row) error {
	return nil
}

// backwardEvidenceHandler is the production cross-repo handler gate wired to
// the durable readiness table.
func backwardEvidenceHandler(db *sql.DB) *crossrepo.CrossRepoRelationshipHandler {
	return &crossrepo.CrossRepoRelationshipHandler{
		EvidenceLoader:    backwardEvidenceEmptyLoader{},
		IntentWriter:      backwardEvidenceIntentSink{},
		ReadinessPrefetch: NewGraphProjectionReadinessPrefetch(SQLDB{DB: db}),
	}
}

// seedBackwardEvidenceScope seeds one active repository scope generation with
// a pending deployment_mapping waiter and, when sibling is set, a sibling
// reducer row that succeeded before the waiter parks (the CI order: the
// audit's terminal-progress proxy then never fires).
func seedBackwardEvidenceScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, genID, waiterID, siblingID string, at time.Time) {
	t.Helper()
	seedOwnGateScope(t, ctx, db, scopeID, genID, at.Add(-time.Hour))
	addOwnGateItem(t, ctx, db, waiterID, scopeID, genID, reducer.DomainDeploymentMapping, at.Add(-time.Minute))
	if siblingID == "" {
		return
	}
	addOwnGateItem(t, ctx, db, siblingID, scopeID, genID, reducer.DomainCodeImportRepoEdge, at.Add(-2*time.Minute))
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items SET status = 'succeeded', updated_at = $2 WHERE work_item_id = $1`,
		siblingID, at.Add(-30*time.Second)); err != nil {
		t.Fatalf("mark sibling %s succeeded: %v", siblingID, err)
	}
}

// claimGatedBackwardEvidence claims the deployment_mapping waiter and runs
// the real handler gate, which must refuse with the backward-evidence error.
func claimGatedBackwardEvidence(t *testing.T, ctx context.Context, db *sql.DB, clock *time.Time) (ReducerQueue, reducercontract.Intent, error) {
	t.Helper()
	queue := newOwnGateQueue(db, clock, reducer.DomainDeploymentMapping)
	intent, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim deployment_mapping waiter: ok=%v err=%v", ok, err)
	}
	_, gateErr := backwardEvidenceHandler(db).Resolve(ctx, intent.ScopeID, intent.GenerationID)
	var notReady crossrepo.BackwardEvidenceNotReadyError
	if !errors.As(gateErr, &notReady) {
		t.Fatalf("handler gate err = %v, want BackwardEvidenceNotReadyError", gateErr)
	}
	return queue, intent, gateErr
}

// parkBackwardEvidenceWaiter claims, gates, and fails the waiter into the
// waiting set-aside.
func parkBackwardEvidenceWaiter(t *testing.T, ctx context.Context, db *sql.DB, clock *time.Time) {
	t.Helper()
	queue, intent, gateErr := claimGatedBackwardEvidence(t, ctx, db, clock)
	if err := queue.Fail(ctx, intent, gateErr); err != nil {
		t.Fatalf("fail waiter into wait: %v", err)
	}
	state := readWaitingRow(t, ctx, db, intent.IntentID)
	if state.status != "retrying" || state.waitRows != 1 {
		t.Fatalf("waiter not parked: status=%s waitRows=%d", state.status, state.waitRows)
	}
}

// backwardEvidenceFanIn is the root-built deferred-backfill store, the same
// construction every production fan-in uses.
func backwardEvidenceFanIn(db *sql.DB) backfill.Store {
	return NewIngestionStore(SQLDB{DB: db}).backfillStore()
}

// publishBackwardEvidence runs the real fan-in entry for one partition.
func publishBackwardEvidence(t *testing.T, ctx context.Context, store backfill.Store, scopeID, genID string) {
	t.Helper()
	published, err := store.PublishDeferredBackfillPartitions(ctx,
		map[backfill.ScopeGenerationPartition][]string{
			{ScopeID: scopeID, GenerationID: genID}: {"repository:" + scopeID},
		}, nil, "", 1, nil)
	if err != nil {
		t.Fatalf("publish backward evidence %s/%s: %v", scopeID, genID, err)
	}
	if published != 1 {
		t.Fatalf("published = %d, want 1", published)
	}
}

// assertBackwardEvidenceReopened asserts the woken shape: claimable now, still
// retrying in its non-counting class, attempt count frozen, wait row gone.
func assertBackwardEvidenceReopened(t *testing.T, ctx context.Context, db *sql.DB, id string, wantAttempts int) {
	t.Helper()
	state := readWaitingRow(t, ctx, db, id)
	if state.status != "retrying" {
		t.Fatalf("woken status = %q, want retrying", state.status)
	}
	if state.attempts != wantAttempts {
		t.Fatalf("woken attempt_count = %d, want %d (a wake never counts)", state.attempts, wantAttempts)
	}
	if !state.visibleAt.Valid || state.visibleAt.Time.After(time.Now().Add(time.Second)) {
		t.Fatalf("woken visible_at = %v, want <= now (parked rows sit on the 10-minute backstop)", state.visibleAt)
	}
	if state.waitRows != 0 {
		t.Fatalf("wait rows after wake = %d, want 0", state.waitRows)
	}
}

// assertBackwardEvidenceParked asserts the waiter is still parked on the
// backstop with its wait row.
func assertBackwardEvidenceParked(t *testing.T, ctx context.Context, db *sql.DB, id string) {
	t.Helper()
	state := readWaitingRow(t, ctx, db, id)
	if state.status != "retrying" || state.waitRows != 1 {
		t.Fatalf("waiter status=%s waitRows=%d, want retrying with its wait row", state.status, state.waitRows)
	}
	if !state.visibleAt.Valid || state.visibleAt.Time.Before(time.Now().Add(5*time.Minute)) {
		t.Fatalf("waiter visible_at = %v, want the 10-minute backstop (not woken)", state.visibleAt)
	}
}
