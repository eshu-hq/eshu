// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestReducerServiceClosesLeaseExpiryStartupWindowAgainstPostgres proves the
// #4447 fix's runtime effect against a real Postgres fact_work_items row, not
// just the in-memory Service.Run unit test. Set
// ESHU_REDUCER_HEARTBEAT_PROOF_DSN to run it; it is skipped otherwise,
// matching the sibling live proofs in this package.
//
// The proof seeds one claimed row with a short claim_until (simulating a
// short-lease claim just made) and then drives reducer.Service.Run with a
// real ReducerQueue.Heartbeat as the Heartbeater, an executor that blocks
// past the row's original claim_until, and a HeartbeatInterval long enough
// that the periodic ticker cannot possibly fire during the test. A
// concurrent "reclaim sweep" query -- modeling another worker's lease
// recovery, matching the shape of the production expired-lease reclaim added
// in #4464 -- polls the row's claim_until against wall-clock time.
//
//   - Unpatched (no immediate pre-heartbeat): claim_until stays at its
//     original short expiry until the periodic ticker eventually fires. The
//     reclaim sweep observes claim_until in the past and would reclaim the
//     row while the original worker is still executing.
//   - Patched: the immediate pre-heartbeat extends claim_until before
//     Executor.Execute runs, so the reclaim sweep always observes a live
//     claim_until and never reclaims.
//
// This test asserts the patched behavior; it is a regression guard, not a
// two-sided harness, because the in-memory
// TestServiceRunPreHeartbeatsImmediatelyOnClaim in
// internal/reducer/service_heartbeat_test.go already proves the unpatched
// code times out (no heartbeat observed before a 1h ticker interval). Here
// the same fix is proven end-to-end against the real UPDATE ... WHERE
// lease_owner = $4 AND status IN ('claimed','running') heartbeat query.
func TestReducerServiceClosesLeaseExpiryStartupWindowAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ESHU_REDUCER_HEARTBEAT_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_HEARTBEAT_PROOF_DSN to run the reducer lease heartbeat startup-window proof")
	}

	// The heartbeat, sweep, and Service.Run goroutines below all issue
	// concurrent queries against this pool, so search_path cannot be set
	// per-session with a bare SET statement -- a pooled connection picked up
	// by a later query would not see it. Instead, create the proof schema on
	// a throwaway bootstrap connection, then open the real pool with the
	// schema baked into the DSN's search_path option so every pooled
	// connection resolves the unqualified table name consistently.
	bootstrapDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open bootstrap connection: %v", err)
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("reducer_heartbeat_proof_%d", time.Now().UnixNano())
	if _, err := bootstrapDB.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		_ = bootstrapDB.Close()
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = bootstrapDB.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
		_ = bootstrapDB.Close()
	})

	scopedDSN := dsn + "?search_path=" + schemaName
	if strings.Contains(dsn, "?") {
		scopedDSN = dsn + "&search_path=" + schemaName
	}
	db, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open scoped connection pool: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(4)

	const minimalFactWorkItemsSchemaSQL = `
CREATE TABLE fact_work_items (
    work_item_id    TEXT PRIMARY KEY,
    scope_id        TEXT NOT NULL,
    generation_id   TEXT NOT NULL,
    stage           TEXT NOT NULL,
    domain          TEXT NOT NULL,
    conflict_domain TEXT NOT NULL DEFAULT 'scope',
    conflict_key    TEXT NULL,
    status          TEXT NOT NULL,
    attempt_count   INTEGER NOT NULL DEFAULT 0,
    lease_owner     TEXT NULL,
    claim_until     TIMESTAMPTZ NULL,
    visible_at      TIMESTAMPTZ NULL,
    last_attempt_at TIMESTAMPTZ NULL,
    next_attempt_at TIMESTAMPTZ NULL,
    failure_class   TEXT NULL,
    failure_message TEXT NULL,
    failure_details TEXT NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);
`
	if _, err := db.ExecContext(ctx, minimalFactWorkItemsSchemaSQL); err != nil {
		t.Fatalf("create proof table: %v", err)
	}

	const leaseOwner = "worker-startup-window-proof"
	const workItemID = "reducer_startup_window_proof"
	now := time.Now().UTC()
	// A short initial claim_until models the just-claimed lease that the
	// stalled worker must extend before it expires.
	shortClaimUntil := now.Add(150 * time.Millisecond)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO fact_work_items (
			work_item_id, scope_id, generation_id, stage, domain, status,
			lease_owner, claim_until, payload, created_at, updated_at
		) VALUES ($1, 'scope-1', 'gen-1', 'reducer', 'semantic_entity_materialization', 'claimed',
			$2, $3, '{}'::jsonb, $4, $4)
	`, workItemID, leaseOwner, shortClaimUntil, now); err != nil {
		t.Fatalf("seed claimed work item: %v", err)
	}

	queue := NewReducerQueue(SQLDB{DB: db}, leaseOwner, 30*time.Second)
	intent := reducer.Intent{
		IntentID:     workItemID,
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducer.DomainSemanticEntityMaterialization,
	}

	// Reclaim sweep: models another worker's lease-expiry recovery (#4464)
	// checking whether this row's claim_until has passed. Runs concurrently
	// while the "worker" below is executing, polling until either it
	// observes an expired lease (unpatched failure) or the worker finishes
	// (patched success).
	reclaimObserved := make(chan bool, 1)
	sweepDone := make(chan struct{})
	go func() {
		defer close(sweepDone)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-sweepDone:
				return
			case <-ticker.C:
				var claimUntil sql.NullTime
				if err := db.QueryRowContext(ctx, `
					SELECT claim_until FROM fact_work_items WHERE work_item_id = $1
				`, workItemID).Scan(&claimUntil); err != nil {
					continue
				}
				if claimUntil.Valid && claimUntil.Time.Before(time.Now().UTC()) {
					select {
					case reclaimObserved <- true:
					default:
					}
					return
				}
			}
		}
	}()

	release := make(chan struct{})
	executor := &blockingProofExecutor{release: release}
	sink := &proofWorkSink{db: db, workItemID: workItemID, leaseOwner: leaseOwner}
	sweepStopper := make(chan struct{})
	defer close(sweepStopper)

	service := reducer.Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &singleIntentSource{intent: intent},
		Executor:          executor,
		WorkSink:          sink,
		Heartbeater:       queue,
		HeartbeatInterval: time.Hour, // the ticker must not fire during this test
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}

	// Give the immediate pre-heartbeat (or, unpatched, nothing) a moment to
	// land before releasing the executor and checking the row.
	runDone := make(chan error, 1)
	go func() { runDone <- service.Run(ctx) }()

	select {
	case reclaimed := <-reclaimObserved:
		close(release)
		<-runDone
		if reclaimed {
			t.Fatal("reclaim sweep observed an expired claim_until while the worker was still executing: the lease-expiry startup window is not closed")
		}
	case <-time.After(3 * time.Second):
		close(release)
		if err := <-runDone; err != nil {
			t.Fatalf("Service.Run() error = %v", err)
		}
	}
	sweepDone <- struct{}{}

	var claimUntil sql.NullTime
	var status string
	if err := db.QueryRowContext(ctx, `
		SELECT status, claim_until FROM fact_work_items WHERE work_item_id = $1
	`, workItemID).Scan(&status, &claimUntil); err != nil {
		t.Fatalf("query final row state: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", status)
	}
}

type blockingProofExecutor struct {
	release <-chan struct{}
}

func (b *blockingProofExecutor) Execute(ctx context.Context, intent reducer.Intent) (reducer.Result, error) {
	select {
	case <-b.release:
		return reducer.Result{IntentID: intent.IntentID, Domain: intent.Domain, Status: reducer.ResultStatusSucceeded}, nil
	case <-ctx.Done():
		return reducer.Result{}, ctx.Err()
	}
}

type singleIntentSource struct {
	intent reducer.Intent
	served bool
}

func (s *singleIntentSource) Claim(context.Context) (reducer.Intent, bool, error) {
	if s.served {
		return reducer.Intent{}, false, nil
	}
	s.served = true
	return s.intent, true, nil
}

type proofWorkSink struct {
	db         *sql.DB
	workItemID string
	leaseOwner string
}

func (p *proofWorkSink) Ack(ctx context.Context, _ reducer.Intent, _ reducer.Result) error {
	_, err := p.db.ExecContext(ctx, `
		UPDATE fact_work_items
		SET status = 'succeeded', lease_owner = NULL, claim_until = NULL, updated_at = $1
		WHERE work_item_id = $2 AND lease_owner = $3
	`, time.Now().UTC(), p.workItemID, p.leaseOwner)
	return err
}

func (p *proofWorkSink) Fail(ctx context.Context, _ reducer.Intent, execErr error) error {
	_, err := p.db.ExecContext(ctx, `
		UPDATE fact_work_items
		SET status = 'dead_letter', failure_message = $1, updated_at = $2
		WHERE work_item_id = $3
	`, execErr.Error(), time.Now().UTC(), p.workItemID)
	return err
}
