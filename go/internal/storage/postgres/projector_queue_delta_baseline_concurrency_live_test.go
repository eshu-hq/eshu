// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
)

// TestProjectorDeltaBaselineConcurrentAcks races same-scope Acks of deltas
// with mixed baselines: five diffed from A and three diffed from D1. Every Ack
// runs on its own connection behind a start barrier. At most one delta may
// activate per baseline, exactly one generation ends active, and no Ack may
// deadlock (40P01). Run with -race and -count to repeat.
func TestProjectorDeltaBaselineConcurrentAcks(t *testing.T) {
	dsn := proofDSN(t)
	gens := []fenceGen{genActiveA}
	var ids []string
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("gen-d%d", i)
		gens = append(gens, fenceGen{id: id, commit: fmt.Sprintf("D%d", i), status: "pending", isDelta: true, baseline: "A", minutesAgo: 20 - i})
		ids = append(ids, id)
	}
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("gen-e%d", i)
		gens = append(gens, fenceGen{id: id, commit: fmt.Sprintf("E%d", i), status: "pending", isDelta: true, baseline: "D1", minutesAgo: 10 - i})
		ids = append(ids, id)
	}
	database := provisionFenceProof(t, dsn, "gen-a", gens, ids...)

	start := make(chan struct{})
	results := make([]error, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		peer := searchPathPeer(t, dsn, database)
		queue := NewProjectorQueue(SQLDB{DB: peer}, "proof-worker", time.Minute)
		queue.AckScopeLockTimeout = 4 * time.Second
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = queue.Ack(context.Background(), fenceWork(id), runtime.Result{})
		}()
	}
	close(start)
	wg.Wait()

	activated := 0
	for i, err := range results {
		switch {
		case err == nil:
			activated++
		case projector.IsDeltaBaselineRefusal(err):
		default:
			t.Fatalf("Ack(%s) = %v, want nil or a delta-baseline refusal", ids[i], err)
		}
		if err != nil && strings.Contains(err.Error(), "40P01") {
			t.Fatalf("Ack(%s) deadlocked: %v", ids[i], err)
		}
	}
	rows, err := database.QueryContext(context.Background(), `
SELECT delta_baseline_commit_sha, count(*)
FROM scope_generations
WHERE scope_id = $1 AND is_delta AND activated_at IS NOT NULL
GROUP BY delta_baseline_commit_sha`, fenceProofScope)
	if err != nil {
		t.Fatalf("count activations: %v", err)
	}
	defer func() { _ = rows.Close() }()
	total := 0
	for rows.Next() {
		var baseline string
		var count int
		if err := rows.Scan(&baseline, &count); err != nil {
			t.Fatalf("scan activations: %v", err)
		}
		if count > 1 {
			t.Fatalf("%d deltas diffed from %s activated, want at most 1", count, baseline)
		}
		total += count
	}
	if total != activated || activated < 1 || activated > 2 {
		t.Fatalf("activations: rows %d, successful Acks %d; want equal and 1 or 2", total, activated)
	}
	var active int
	if err := database.QueryRowContext(context.Background(),
		"SELECT count(*) FROM scope_generations WHERE scope_id = $1 AND status = 'active'", fenceProofScope,
	).Scan(&active); err != nil || active != 1 {
		t.Fatalf("active generations = %d (%v), want 1", active, err)
	}
}

// TestProjectorDeltaBaselineAckWaitsOutIngestionCommit is the lock-order proof
// for the fenced Ack, modelled on the ingestion lock-order live test. An
// ingestion transaction holds the scope row and the delta's generation row.
// Ack waits on the scope row, then runs the fence read and either activates or
// refuses (rolling back and marking) once ingestion commits. Neither case may
// deadlock or time out.
func TestProjectorDeltaBaselineAckWaitsOutIngestionCommit(t *testing.T) {
	dsn := proofDSN(t)
	for _, tc := range []struct {
		name, pointer string
		active        fenceGen
		wantRefused   bool
	}{
		{name: "matched", pointer: "gen-a", active: genActiveA},
		{name: "refused", pointer: "gen-b", active: genActiveB, wantRefused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := provisionFenceProof(t, dsn, tc.pointer, []fenceGen{tc.active, pendingDelta("A")}, "gen-d")
			ingest := searchPathPeer(t, dsn, database)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			ingestTx, err := ingest.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin ingest: %v", err)
			}
			defer func() { _ = ingestTx.Rollback() }()
			for _, q := range []string{
				"UPDATE ingestion_scopes SET observed_at = observed_at WHERE scope_id = '" + fenceProofScope + "'",
				"UPDATE scope_generations SET observed_at = observed_at WHERE generation_id = 'gen-d'",
			} {
				if _, err := ingestTx.ExecContext(ctx, q); err != nil {
					t.Fatalf("ingest lock: %v", err)
				}
			}
			var ackPID int
			if err := database.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&ackPID); err != nil {
				t.Fatalf("ack pid: %v", err)
			}
			queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
			queue.AckScopeLockTimeout = 4 * time.Second
			done := make(chan error, 1)
			go func() { done <- queue.Ack(ctx, fenceWork("gen-d"), runtime.Result{}) }()
			waitForBackendLockWait(ctx, t, ingestTx, ackPID)
			if err := ingestTx.Commit(); err != nil {
				t.Fatalf("commit ingest: %v", err)
			}
			err = <-done
			if tc.wantRefused != projector.IsDeltaBaselineRefusal(err) || (!tc.wantRefused && err != nil) {
				t.Fatalf("Ack after ingest commit = %v, want refused=%v", err, tc.wantRefused)
			}
			if errors.Is(err, context.DeadlineExceeded) || (err != nil && strings.Contains(err.Error(), "40P01")) {
				t.Fatalf("Ack deadlocked or timed out: %v", err)
			}
		})
	}
}

// waitForBackendLockWait blocks until backend pid waits on a lock.
func waitForBackendLockWait(ctx context.Context, t *testing.T, tx *sql.Tx, pid int) {
	t.Helper()
	for {
		var waitType sql.NullString
		if err := tx.QueryRowContext(ctx, "SELECT wait_event_type FROM pg_stat_activity WHERE pid = $1", pid).Scan(&waitType); err != nil {
			t.Fatalf("inspect wait: %v", err)
		}
		if waitType.String == "Lock" {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("Ack never waited on the ingestion transaction")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
