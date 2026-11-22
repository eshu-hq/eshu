// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

// Poison-liveness sweep helpers for the #11217 proofs. They extend the #11182
// replay fixture (replay_live_fixture_test.go) with the fourth dead_letter to
// pending writer that carries the replay retry allowance: the reducer's
// bounded poison-liveness auto-retry sweep (pgpoison.Store.RecoverDeadLetters
// over poison.RecoverDeadLettersQuery). The proofs live beside the fixture
// because the concurrency proofs race the sweep against all three replay
// writers, and the admin replay writer is reachable from this package.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	pgpoison "github.com/eshu-hq/eshu/go/internal/storage/postgres/poison"
)

// sweepWith runs one poison-liveness sweep over database with the per-row
// budget maxRecover and the production default batch, and returns how many
// rows it moved.
func sweepWith(ctx context.Context, database db.ExecQueryer, maxRecover int) (int, error) {
	result, err := pgpoison.NewStore(database).RecoverDeadLetters(ctx, pgpoison.Policy{
		MaxRecoverAttempts: maxRecover,
		BatchLimit:         pgpoison.DefaultBatchLimit,
	}, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return result.Recovered, nil
}

// sweep runs one sweep in its own statement with budget maxRecover.
func (p *replayProof) sweep(maxRecover int) int {
	p.t.Helper()
	moved, err := sweepWith(p.ctx, pgstatus.SQLDB{DB: p.db}, maxRecover)
	if err != nil {
		p.t.Fatalf("poison sweep: %v", err)
	}
	return moved
}

// poisonRecoveries reads the sweep's per-row budget counter
// (payload.poison_recovery_attempts) of generationID's projector row; 0 when
// the sweep never moved it.
func (p *replayProof) poisonRecoveries(generationID string) int {
	p.t.Helper()
	var recoveries int
	if err := p.db.QueryRowContext(p.ctx, `
SELECT COALESCE((payload ->> 'poison_recovery_attempts')::int, 0)
FROM fact_work_items
WHERE stage = 'projector' AND generation_id = $1 AND work_item_id = 'projector_' || scope_id || '_' || generation_id`,
		generationID).Scan(&recoveries); err != nil {
		p.t.Fatalf("read poison recoveries %s: %v", generationID, err)
	}
	return recoveries
}

// requireSwept checks the sweep gave generationID's row a new row's
// allowance exactly as a replay does (pending, attempt_count kept, baseline
// equal to it, admission waits 0, reopened_at stamped) and that the payload
// budget counter reads recoveries.
func (p *replayProof) requireSwept(generationID string, attempts, recoveries int) {
	p.t.Helper()
	p.requireReplayed(generationID, attempts)
	if got := p.poisonRecoveries(generationID); got != recoveries {
		p.t.Fatalf("poison_recovery_attempts = %d, want %d", got, recoveries)
	}
}

// requireRecovered checks only that the sweep moved generationID's row back
// to pending with reopened_at stamped and the payload budget counter at
// recoveries, so a proof can show the retry behavior before it checks the
// counters.
func (p *replayProof) requireRecovered(generationID string, recoveries int) {
	p.t.Helper()
	if row := p.work(generationID); row.status != "pending" || !row.reopened {
		p.t.Fatalf("after sweep: work = %+v, want pending with reopened_at set", row)
	}
	if got := p.poisonRecoveries(generationID); got != recoveries {
		p.t.Fatalf("poison_recovery_attempts = %d, want %d", got, recoveries)
	}
}

// requireSweptBaseline checks the baseline the sweep wrote is still the row's
// replay_attempt_baseline: no later claim or failure moves it.
func (p *replayProof) requireSweptBaseline(generationID string, baseline int) {
	p.t.Helper()
	if row := p.work(generationID); row.baseline != baseline {
		p.t.Fatalf("work = %+v, want replay_attempt_baseline %d from the sweep", row, baseline)
	}
}

// begin opens a transaction the test commits itself, so a writer run inside
// it holds its row locks until the test lets it go.
func (p *replayProof) begin() *sql.Tx {
	p.t.Helper()
	tx, err := p.db.BeginTx(p.ctx, nil)
	if err != nil {
		p.t.Fatalf("begin: %v", err)
	}
	p.t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

// commitTx commits tx or fails the test.
func (p *replayProof) commitTx(tx *sql.Tx) {
	p.t.Helper()
	if err := tx.Commit(); err != nil {
		p.t.Fatalf("commit: %v", err)
	}
}

// recoveryReplayWith runs the recovery replay of the proof scope's projector
// rows over database, bounded (the SKIP LOCKED drain) when bounded is true,
// and returns how many rows it moved.
func (p *replayProof) recoveryReplayWith(database db.ExecQueryer, bounded bool) (int, error) {
	filter := recovery.ReplayFilter{Stage: recovery.StageProjector, ScopeIDs: []string{p.scope.ScopeID}}
	if bounded {
		filter.Limit = 10
	}
	result, err := pgstatus.NewRecoveryStore(database).ReplayFailedWorkItems(p.ctx, filter, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return result.Replayed, nil
}

// adminReplayWriter is the admin replay site as a goroutine-safe writer: it
// posts POST /api/v0/admin/replay with a fresh idempotency key and returns
// replayed_count, reporting failures as errors instead of calling t.Fatal.
func (p *replayProof) adminReplayWriter(workItemID string) func() (int, error) {
	key := fmt.Sprintf("11217-%d", p.keys.Add(1))
	return func() (int, error) {
		w := testutil.PostJSON(p.mux, "/api/v0/admin/replay", map[string]any{
			"work_item_ids":   []string{workItemID},
			"reason":          "#11217 proof: replay racing the poison-liveness sweep",
			"idempotency_key": key,
			"operator_note":   "#11217 proof",
		})
		if w.Code != http.StatusOK {
			return 0, fmt.Errorf("admin replay status = %d; body: %s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			return 0, fmt.Errorf("decode admin replay: %w", err)
		}
		count, _ := body["replayed_count"].(float64)
		return int(count), nil
	}
}

// waitForLockWaiters blocks until at least n backends of this database wait
// on a heavyweight lock, so a racing writer is known to be queued behind the
// lock holder before the test releases it.
func (p *replayProof) waitForLockWaiters(n int) {
	p.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var waiting int
		if err := p.db.QueryRowContext(p.ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			p.t.Fatalf("read lock waiters: %v", err)
		}
		if waiting >= n {
			return
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("lock waiters = %d after 30s, want %d", waiting, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// asyncCount is the outcome of a writer run on its own goroutine.
type asyncCount struct {
	moved int
	err   error
}

// async runs writer on a goroutine and returns the channel its outcome
// arrives on. The writer must not call t.Fatal.
func async(writer func() (int, error)) <-chan asyncCount {
	done := make(chan asyncCount, 1)
	go func() {
		moved, err := writer()
		done <- asyncCount{moved: moved, err: err}
	}()
	return done
}

// await reads an async outcome or fails the test after 30 seconds.
func (p *replayProof) await(name string, done <-chan asyncCount) int {
	p.t.Helper()
	select {
	case got := <-done:
		if got.err != nil {
			p.t.Fatalf("%s: %v", name, got.err)
		}
		return got.moved
	case <-time.After(30 * time.Second):
		p.t.Fatalf("%s did not finish within 30s", name)
		return 0
	}
}
