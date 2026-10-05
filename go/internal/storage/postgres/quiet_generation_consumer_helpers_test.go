// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
)

// wholeMaintenanceControlArm is the LABELLED CONTROL ARM of #7584 ruling D1:
// the activation maintenance port backed by the whole native deferred
// relationship maintenance. It exists only in test code. Ruling D2 forbids it
// as the shipped callback (it is corpus work per activation); the targeted
// callback that replaces it is decided by the D3 parity and cost proof.
type wholeMaintenanceControlArm struct {
	store IngestionStore
	calls *atomic.Int32
}

func (c wholeMaintenanceControlArm) MaintainActivation(ctx context.Context, _ maintenance.ActivationObligation) error {
	c.calls.Add(1)
	return c.store.RunDeferredRelationshipMaintenance(ctx, nil, nil)
}

// startQuietActivationConsumer runs the production activation obligation
// runner against the proof schema with the control-arm callback, and stops
// it when the test ends. It returns the control-arm call counter.
func startQuietActivationConsumer(t *testing.T, ctx context.Context, database *sql.DB, store IngestionStore) *atomic.Int32 {
	t.Helper()
	calls := &atomic.Int32{}
	runner := &maintenance.ActivationObligationRunner{
		Store:      activation.RunnerStore{Store: activation.NewStore(SQLDB{DB: database})},
		Maintainer: wholeMaintenanceControlArm{store: store, calls: calls},
		Config: maintenance.ActivationObligationRunnerConfig{
			Owner: "quiet-activation-consumer", Lease: time.Minute,
			PollInterval: 10 * time.Millisecond,
		},
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- runner.Run(runCtx) }()
	t.Cleanup(func() {
		stop()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("activation obligation runner: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("activation obligation runner did not stop after cancellation")
		}
	})
	return calls
}

// seedQuietWaitingDeploymentMapping inserts the row shape the reducer's
// real Fail leaves for a deployment_mapping handler that found the
// backward-evidence phase missing (proven on the full schema by
// TestActivationObligationConsumerOrderingLive): retrying, the not-ready
// class, no lease, visible an hour from now.
func seedQuietWaitingDeploymentMapping(t *testing.T, ctx context.Context, database *sql.DB,
	scopeID, generationID string, now time.Time,
) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_work_items
    (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
     visible_at, next_attempt_at, failure_class, failure_message, payload, created_at, updated_at)
VALUES ('quiet-new-deployment-mapping', $1, $2, 'reducer', 'deployment_mapping', 'retrying', 1,
     clock_timestamp() + interval '1 hour', clock_timestamp() + interval '1 hour',
     'cross_repo_backward_evidence_not_ready', 'backward evidence not ready', '{}'::jsonb, $3, $3)`,
		scopeID, generationID, now); err != nil {
		t.Fatalf("seed waiting deployment_mapping row: %v", err)
	}
}

// awaitQuietObligationCompleted waits for the exact obligation to complete,
// then asserts the exact wake: the waiting row is visible now and still
// retrying with its class, attempt count and no lease.
func awaitQuietObligationCompleted(t *testing.T, ctx context.Context, database *sql.DB,
	scopeID, generationID string,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var state string
	for {
		if err := database.QueryRowContext(ctx, `SELECT state FROM activation_obligations
WHERE scope_id = $1 AND generation_id = $2`, scopeID, generationID).Scan(&state); err != nil {
			t.Fatalf("read activation obligation: %v", err)
		}
		if state == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("activation obligation for %q is %q, want completed", generationID, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var status, class string
	var attempts int
	var visible, leased bool
	if err := database.QueryRowContext(ctx, `SELECT status, failure_class, attempt_count,
    visible_at <= clock_timestamp(), lease_owner IS NOT NULL OR claim_until IS NOT NULL
FROM fact_work_items WHERE work_item_id = 'quiet-new-deployment-mapping'`).Scan(
		&status, &class, &attempts, &visible, &leased); err != nil {
		t.Fatalf("read woken deployment_mapping row: %v", err)
	}
	if status != "retrying" || class != "cross_repo_backward_evidence_not_ready" ||
		attempts != 1 || !visible || leased {
		t.Fatalf("woken row = status %q class %q attempts %d visible %t leased %t, "+
			"want retrying, not-ready class, 1 attempt, visible now, no lease",
			status, class, attempts, visible, leased)
	}
}
