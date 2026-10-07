// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// claimLoop runs two workers that claim and Ack reducer work through the
// production ReducerQueue, then re-open each acked row so the backlog stays
// at its live fraction.
type claimLoop struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu        sync.Mutex
	latencies []time.Duration
	errs      []error
	since     time.Time
}

// claimPhase summarizes the claims since the previous phase call.
type claimPhase struct {
	count int
	rate  float64
	p95   time.Duration
}

func startClaimLoop(ctx context.Context, t *testing.T, database *sql.DB) *claimLoop {
	t.Helper()
	loopCtx, cancel := context.WithCancel(ctx)
	loop := &claimLoop{cancel: cancel, since: time.Now()}
	t.Cleanup(loop.stop)
	for worker := 0; worker < 2; worker++ {
		queue := postgres.NewReducerQueue(postgres.SQLDB{DB: database}, fmt.Sprintf("claim-worker-%d", worker), time.Minute)
		loop.wg.Add(1)
		go func() {
			defer loop.wg.Done()
			for loopCtx.Err() == nil {
				start := time.Now()
				err := claimAndAck(loopCtx, database, queue)
				if loopCtx.Err() != nil {
					return
				}
				loop.mu.Lock()
				if errors.Is(err, errNoWork) {
					loop.mu.Unlock()
					time.Sleep(10 * time.Millisecond)
					continue
				}
				if err != nil {
					loop.errs = append(loop.errs, err)
				} else {
					loop.latencies = append(loop.latencies, time.Since(start))
				}
				loop.mu.Unlock()
			}
		}()
	}
	return loop
}

var errNoWork = errors.New("no claimable work")

// claimAndAck claims one row, Acks it with the same queue (the lease owner
// must match), and re-opens it.
func claimAndAck(ctx context.Context, database *sql.DB, queue postgres.ReducerQueue) error {
	intent, ok, err := queue.Claim(ctx)
	if err != nil {
		return fmt.Errorf("claim: %w", err)
	}
	if !ok {
		return errNoWork
	}
	if err := queue.Ack(ctx, intent, reducer.Result{}); err != nil {
		return fmt.Errorf("ack %s: %w", intent.IntentID, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE fact_work_items
		SET status = 'pending', lease_owner = NULL, claim_until = NULL, updated_at = now()
		WHERE work_item_id = $1`, intent.IntentID); err != nil {
		return fmt.Errorf("re-open %s: %w", intent.IntentID, err)
	}
	return nil
}

// phase returns the claims since the previous call and starts a new phase.
func (l *claimLoop) phase() claimPhase {
	l.mu.Lock()
	defer l.mu.Unlock()
	latencies := slices.Clone(l.latencies)
	elapsed := time.Since(l.since)
	l.latencies, l.since = nil, time.Now()
	out := claimPhase{count: len(latencies), rate: float64(len(latencies)) / elapsed.Seconds()}
	if len(latencies) > 0 {
		slices.Sort(latencies)
		out.p95 = latencies[(len(latencies)*95+99)/100-1]
	}
	return out
}

func (l *claimLoop) stop() {
	l.cancel()
	l.wg.Wait()
}

func (l *claimLoop) errorCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.errs)
}

func (l *claimLoop) firstError() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.errs) == 0 {
		return nil
	}
	return l.errs[0]
}

// lockSampler counts, every 100 ms, ungranted locks held or caused by a
// writer backend (identified by its application_name).
type lockSampler struct {
	done        chan struct{}
	samples     int
	writerWaits int
}

func startLockSampler(ctx context.Context, t *testing.T, database *sql.DB) *lockSampler {
	t.Helper()
	sampler := &lockSampler{done: make(chan struct{})}
	go func() {
		defer close(sampler.done)
		for ctx.Err() == nil {
			waits, err := writerLockWaits(ctx, database)
			if err == nil {
				sampler.samples++
				sampler.writerWaits += waits
			} else if ctx.Err() == nil {
				t.Errorf("lock sample: %v", err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	return sampler
}

func (s *lockSampler) wait() { <-s.done }

// writerLockWaits counts ungranted locks whose waiter or blocker is a writer
// backend, identified by its application_name.
func writerLockWaits(ctx context.Context, database *sql.DB) (int, error) {
	var waits int
	err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM pg_locks AS waiting
JOIN pg_stat_activity AS waiter ON waiter.pid = waiting.pid
WHERE NOT waiting.granted
  AND (waiter.application_name = $1
       OR EXISTS (SELECT 1 FROM pg_stat_activity AS blocker
                  WHERE blocker.pid = ANY (pg_blocking_pids(waiting.pid))
                    AND blocker.application_name = $1))`, writerAppName).Scan(&waits)
	return waits, err
}

// assertSamplerSeesAWriterWait seeds a lock wait caused by a writer-pool
// backend and fails unless writerLockWaits counts it, so a zero count in the
// contention run is evidence and not a blind probe.
func assertSamplerSeesAWriterWait(ctx context.Context, t *testing.T, database, writerPool *sql.DB) {
	t.Helper()
	holder, err := writerPool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin writer-pool holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(ctx, `LOCK TABLE status_summary_snapshots IN EXCLUSIVE MODE`); err != nil {
		t.Fatalf("writer-pool holder lock: %v", err)
	}
	waiterDone := make(chan error, 1)
	go func() {
		waiter, err := database.BeginTx(ctx, nil)
		if err != nil {
			waiterDone <- err
			return
		}
		defer func() { _ = waiter.Rollback() }()
		_, err = waiter.ExecContext(ctx, `LOCK TABLE status_summary_snapshots IN EXCLUSIVE MODE`)
		waiterDone <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		waits, err := writerLockWaits(ctx, database)
		if err != nil {
			t.Fatalf("seeded sample: %v", err)
		}
		if waits > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the lock sampler did not see a wait caused by a writer backend; sessions:\n%s", sessionDump(ctx, database))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := holder.Rollback(); err != nil {
		t.Fatalf("release writer-pool holder: %v", err)
	}
	if err := <-waiterDone; err != nil {
		t.Fatalf("seeded waiter: %v", err)
	}
}

// asOfReader polls the stored row every 250 ms and checks that its as_of
// never moves backwards.
type asOfReader struct {
	done     chan struct{}
	reads    int
	monotone bool
	maxAge   time.Duration
}

func startAsOfReader(ctx context.Context, t *testing.T, database *sql.DB) *asOfReader {
	t.Helper()
	reader := &asOfReader{done: make(chan struct{}), monotone: true}
	go func() {
		defer close(reader.done)
		var last time.Time
		for ctx.Err() == nil {
			var (
				asOf time.Time
				age  float64
			)
			err := database.QueryRowContext(ctx, `SELECT as_of, EXTRACT(EPOCH FROM now() - as_of)
				FROM status_summary_snapshots WHERE model_key = $1`, store.ModelActiveWorkSummary).Scan(&asOf, &age)
			switch {
			case errors.Is(err, sql.ErrNoRows):
			case err != nil:
				if ctx.Err() == nil {
					t.Errorf("read stored as_of: %v", err)
				}
			default:
				reader.reads++
				if asOf.Before(last) {
					reader.monotone = false
				}
				last = asOf
				reader.maxAge = max(reader.maxAge, time.Duration(age*float64(time.Second)))
			}
			time.Sleep(250 * time.Millisecond)
		}
	}()
	return reader
}

func (r *asOfReader) wait() { <-r.done }

// sessionDump lists this database's sessions for a failure message.
func sessionDump(ctx context.Context, database *sql.DB) string {
	rows, err := database.QueryContext(ctx, `
SELECT pid, application_name, state, coalesce(wait_event_type, ''), pg_blocking_pids(pid)::text, left(query, 80)
FROM pg_stat_activity WHERE datname = current_database()`)
	if err != nil {
		return err.Error()
	}
	defer func() { _ = rows.Close() }()
	var out strings.Builder
	for rows.Next() {
		var (
			pid                               int
			app, state, wait, blockers, query string
		)
		if err := rows.Scan(&pid, &app, &state, &wait, &blockers, &query); err != nil {
			return err.Error()
		}
		fmt.Fprintf(&out, "%d app=%q state=%s wait=%s blockers=%s %s\n", pid, app, state, wait, blockers, query)
	}
	return out.String()
}
