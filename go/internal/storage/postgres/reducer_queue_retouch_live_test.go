// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10411 disposable PostgreSQL proof that a same-generation retouch advances a pending row's cycle start and marks a claimed row for replay so its ACK returns it to pending instead of succeeding against pre-commit state; seeded rows carry a unique prefix with cleanup on both paths; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestReducerQueueRetouchLive proves the #10411 two-touch contract over
// real PostgreSQL: retouching a pending intent advances reopened_at (the
// claim read's CycleStartedAt), and retouching a claimed intent sets the
// replay bit so the ACK trigger returns the row to pending for a rerun.
func TestReducerQueueRetouchLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the retouch live proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	scope := "test-retouch-10411-" + suffix
	gen := scope + "-gen"
	for _, seed := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload) VALUES ($1,'vulnerability_intelligence','synthetic',$1,'synthetic',$1,NOW(),NOW(),'active','{}'::jsonb) ON CONFLICT (scope_id) DO NOTHING`, []any{scope}},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload) VALUES ($1,$2,'synthetic',NOW(),NOW(),'active',NOW(),'{}'::jsonb) ON CONFLICT (generation_id) DO NOTHING`, []any{gen, scope}},
	} {
		if _, err := db.ExecContext(ctx, seed.q, seed.args...); err != nil {
			t.Fatalf("seed scope/generation: %v", err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for _, q := range []string{
			`DELETE FROM fact_work_items WHERE scope_id = $1`,
			`DELETE FROM scope_generations WHERE scope_id = $1`,
			`DELETE FROM ingestion_scopes WHERE scope_id = $1`,
		} {
			if _, err := db.ExecContext(cleanupCtx, q, scope); err != nil {
				t.Errorf("cleanup %s: %v", q, err)
			}
		}
	})

	queue := NewReducerQueue(SQLDB{DB: db}, "retouch-proof-"+suffix, time.Minute)
	queue.ClaimDomains = []reducercontract.Domain{reducercontract.DomainAdvisoryGroupProjection}
	intent := runtime.ReducerIntent{
		ScopeID:      scope,
		GenerationID: gen,
		Domain:       reducercontract.DomainAdvisoryGroupProjection,
		EntityKey:    "advisory_group:CVE-10411-RETOUCH",
		Reason:       advisoryGroupProjectionCause,
		SourceSystem: "synthetic",
		Payload:      map[string]any{"group_id": "CVE-10411-RETOUCH"},
	}
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{intent}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	workItemID := reducerWorkItemID(intent)

	readRow := func() (status string, reopenedAt sql.NullTime, replay bool) {
		t.Helper()
		if err := db.QueryRowContext(ctx,
			`SELECT status, reopened_at, cross_scope_replay_required FROM fact_work_items WHERE work_item_id = $1`,
			workItemID).Scan(&status, &reopenedAt, &replay); err != nil {
			t.Fatalf("read work item: %v", err)
		}
		return status, reopenedAt, replay
	}

	before := time.Now().UTC()
	if ok, err := queue.RetouchWorkItem(ctx, workItemID); err != nil || !ok {
		t.Fatalf("retouch pending: ok=%v err=%v", ok, err)
	}
	status, reopenedAt, replay := readRow()
	if status != "pending" || !reopenedAt.Valid || reopenedAt.Time.Before(before.Add(-time.Second)) || replay {
		t.Fatalf("pending retouch: status=%s reopened_at=%v replay=%v, want pending with advanced cycle start", status, reopenedAt, replay)
	}

	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if claimed.CycleStartedAt.Before(before.Add(-time.Second)) {
		t.Fatalf("claimed CycleStartedAt = %v, want at or after the retouch", claimed.CycleStartedAt)
	}
	if ok, err := queue.RetouchWorkItem(ctx, workItemID); err != nil || !ok {
		t.Fatalf("retouch claimed: ok=%v err=%v", ok, err)
	}
	status, _, replay = readRow()
	if status != "claimed" && status != "running" {
		t.Fatalf("claimed retouch changed status to %s, want the lease kept", status)
	}
	if !replay {
		t.Fatal("claimed retouch did not set cross_scope_replay_required: the ACK would succeed against pre-commit state")
	}
	if err := queue.Ack(ctx, claimed, reducercontract.Result{}); err != nil {
		t.Fatalf("ack: %v", err)
	}
	status, _, replay = readRow()
	if status != "pending" || replay {
		t.Fatalf("after ACK: status=%s replay=%v, want pending with the bit consumed for one rerun", status, replay)
	}
}
