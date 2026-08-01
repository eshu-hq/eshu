// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// quietFleetBarrierState is a minimal, thread-safe, in-memory model of the
// deferred_maintenance_barriers / deferred_maintenance_barrier_arrivals
// tables, scoped to the #5852 P1 follow-up quiet-restart regression: real
// goroutines, one per shard, drive collector.Service.Run concurrently exactly
// as separate ingester processes would, so the once-latch behavior
// (startupMaintenanceEscapeUsed in collector.Service.Run) is proven against
// genuine concurrent arrival at the real join-only barrier logic
// (ensureDeferredMaintenanceBarrierEpoch), not a single-goroutine simulation
// that could never reach the leader path. epochOpens records every epoch this
// state ever opened, in insertion order, for the "exactly one, ever" assertion.
type quietFleetBarrierState struct {
	mu          sync.Mutex
	stateLock   sync.Mutex // models pg_advisory_xact_lock(deferredMaintenanceBarrierStateLockKey)
	epoch       int64
	shardCount  int
	completedAt sql.NullTime
	arrived     map[int64]map[int]bool
	epochOpens  []int64
}

func newQuietFleetBarrierState() *quietFleetBarrierState {
	return &quietFleetBarrierState{arrived: make(map[int64]map[int]bool)}
}

// quietFleetDB is the top-level ExecQueryer/Beginner a shard's IngestionStore
// talks to outside the barrier-arrival transaction: catalog load, active
// generation snapshot, and reopen-candidate listings. The quiet-restart
// scenario has no committed corpus, so every one of those reads
// deterministically returns zero rows — matching real Postgres against a
// truly empty fact table — which lets the leader's real
// RunDeferredRelationshipMaintenance short-circuit through backfill and
// reopen without any hand-crafted per-query fixture.
type quietFleetDB struct {
	state *quietFleetBarrierState
}

func (d *quietFleetDB) Begin(context.Context) (Transaction, error) {
	return &quietFleetTx{state: d.state}, nil
}

func (d *quietFleetDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (d *quietFleetDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	switch {
	case strings.Contains(query, "SELECT completed_at") && strings.Contains(query, "FROM deferred_maintenance_barriers"):
		epoch, _ := args[1].(int64)
		d.state.mu.Lock()
		defer d.state.mu.Unlock()
		if epoch != d.state.epoch {
			return &queueFakeRows{}, nil
		}
		return &queueFakeRows{rows: [][]any{{d.state.completedAt}}}, nil
	case strings.Contains(query, "FROM deferred_maintenance_barrier_arrivals") && strings.Contains(query, "ORDER BY shard_index"):
		epoch, _ := args[1].(int64)
		d.state.mu.Lock()
		defer d.state.mu.Unlock()
		rows := make([][]any, 0, len(d.state.arrived[epoch]))
		for shardIndex := range d.state.arrived[epoch] {
			rows = append(rows, []any{shardIndex})
		}
		return &queueFakeRows{rows: rows}, nil
	default:
		// Every other read (repository catalog, fact loads, active
		// generation snapshots, reopen-candidate listings) sees a genuinely
		// empty corpus: no shard in this scenario ever committed anything.
		return &queueFakeRows{}, nil
	}
}

// quietFleetTx backs every transaction a shard's IngestionStore opens: the
// barrier-arrival transaction (state lock, epoch lookup, epoch insert,
// arrival insert), the leader's maintenance batch/reopen transactions (which
// see zero rows and so do no writes), and the completion transaction.
// Dispatch is by query/exec text, matching the pattern the rest of this
// package's fakes use, so no exact call-count or call-order fixture is
// needed for the corpus-empty reads.
type quietFleetTx struct {
	state         *quietFleetBarrierState
	heldStateLock bool
}

func (tx *quietFleetTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "pg_advisory_xact_lock($1)"):
		tx.state.stateLock.Lock()
		tx.heldStateLock = true
		return fakeResult{}, nil
	case strings.Contains(query, "INSERT INTO deferred_maintenance_barriers"):
		epoch, _ := args[1].(int64)
		shardCount, _ := args[2].(int)
		tx.state.mu.Lock()
		tx.state.epoch = epoch
		tx.state.shardCount = shardCount
		tx.state.completedAt = sql.NullTime{}
		tx.state.arrived[epoch] = make(map[int]bool)
		tx.state.epochOpens = append(tx.state.epochOpens, epoch)
		tx.state.mu.Unlock()
		return fakeResult{}, nil
	case strings.Contains(query, "INSERT INTO deferred_maintenance_barrier_arrivals"):
		epoch, _ := args[1].(int64)
		shardIndex, _ := args[2].(int)
		tx.state.mu.Lock()
		if tx.state.arrived[epoch] == nil {
			tx.state.arrived[epoch] = make(map[int]bool)
		}
		tx.state.arrived[epoch][shardIndex] = true
		tx.state.mu.Unlock()
		return fakeResult{}, nil
	case strings.Contains(query, "completed_at = $4"):
		epoch, _ := args[1].(int64)
		now, _ := args[3].(time.Time)
		tx.state.mu.Lock()
		if epoch == tx.state.epoch {
			tx.state.completedAt = sql.NullTime{Time: now, Valid: true}
		}
		tx.state.mu.Unlock()
		return fakeResult{}, nil
	default:
		// Reopen candidate lists are empty, so no reopen UPDATE ever runs;
		// treat any other exec as a harmless no-op write.
		return fakeResult{}, nil
	}
}

func (tx *quietFleetTx) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	switch {
	case strings.Contains(query, "FROM deferred_maintenance_barriers") && strings.Contains(query, "FOR UPDATE"):
		tx.state.mu.Lock()
		defer tx.state.mu.Unlock()
		if tx.state.epoch == 0 {
			return &queueFakeRows{}, nil
		}
		return &queueFakeRows{rows: [][]any{{tx.state.epoch, tx.state.shardCount, tx.state.completedAt}}}, nil
	case strings.Contains(query, "COUNT(*)") && strings.Contains(query, "FROM deferred_maintenance_barrier_arrivals"):
		tx.state.mu.Lock()
		count := len(tx.state.arrived[tx.state.epoch])
		tx.state.mu.Unlock()
		return &queueFakeRows{rows: [][]any{{count}}}, nil
	default:
		return &queueFakeRows{}, nil
	}
}

func (tx *quietFleetTx) Commit() error {
	if tx.heldStateLock {
		tx.state.stateLock.Unlock()
		tx.heldStateLock = false
	}
	return nil
}

func (tx *quietFleetTx) Rollback() error {
	if tx.heldStateLock {
		tx.state.stateLock.Unlock()
		tx.heldStateLock = false
	}
	return nil
}

// quietFleetIdleSource never yields a generation to commit. Its Next reports
// every poll as an idle empty batch and cancels the shard's run once it has
// been polled idlePollsPerShard times, mirroring a real shard that owns no
// repositories: source discovery always comes back empty.
type quietFleetIdleSource struct {
	pollsRemaining int
	cancel         context.CancelFunc
}

func (s *quietFleetIdleSource) Next(context.Context) (collector.CollectedGeneration, bool, error) {
	s.pollsRemaining--
	if s.pollsRemaining <= 0 {
		s.cancel()
	}
	return collector.CollectedGeneration{}, false, nil
}

// quietFleetNoopCommitter is never called: quietFleetIdleSource never yields a
// generation. It exists only because collector.Service.Run requires a
// non-nil Committer.
type quietFleetNoopCommitter struct{}

func (quietFleetNoopCommitter) CommitScopeGeneration(context.Context, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error {
	return fmt.Errorf("quiet-restart fleet test: no generation should ever be committed")
}

// TestIngestionStoreShardDrainBarrierQuietRestartOpensExactlyOneEpochAcrossManyIdlePolls
// is the P1 follow-up regression for #5852: N real collector.Service.Run
// instances, one goroutine per shard, none of which ever commits a
// generation, driven against the real join-only barrier
// (ensureDeferredMaintenanceBarrierEpoch) and the real once-latch
// (startupMaintenanceEscapeUsed in collector.Service.Run). Each shard runs
// exactly one Service.Run call for the life of the test (no process restart
// is simulated); within that single call each shard's own once-latch
// reports hasCommitted=true on its own first-ever escape call, so all three
// shards independently believe they may open an epoch — proving the
// property this test exists for: across many idle polls per shard within
// that one quiet-restart process lifetime, the join-only barrier logic still
// lets only ONE of those calls actually insert a new epoch; every other
// arrival — including the other two shards' own "I may open" calls — joins
// the epoch already open instead of inserting another. That is the fix for
// the join-only-only design (this branch's prior state), which gated the
// escape on a permanently-false HasCommitted and opened zero epochs ever,
// silently dropping origin/main's one-time startup maintenance pass.
func TestIngestionStoreShardDrainBarrierQuietRestartOpensExactlyOneEpochAcrossManyIdlePolls(t *testing.T) {
	t.Parallel()

	const shardCount = 3
	const idlePollsPerShard = 25
	// safetyDeadline bounds each shard's run so a broken once-latch mutation
	// (every escape call reporting hasCommitted=true) cannot hang this test
	// forever: waitDeferredMaintenanceBarrierCompletion's wait is
	// deliberately unbounded by design (see that function's doc comment), so
	// a shard stuck waiting on an epoch that never reaches full arrival
	// because other shards kept opening newer ones needs an external bound.
	// The fixed code finishes in a fraction of a second; this deadline is a
	// safety net, not the property under test.
	const safetyDeadline = 5 * time.Second

	state := newQuietFleetBarrierState()
	db := &quietFleetDB{state: state}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return time.Date(2026, time.July, 27, 9, 0, 0, 0, time.UTC) }

	var wg sync.WaitGroup
	runErrs := make([]error, shardCount)
	for shardIndex := 0; shardIndex < shardCount; shardIndex++ {
		wg.Add(1)
		go func(shardIndex int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), safetyDeadline)
			defer cancel()
			service := collector.Service{
				Source:                 &quietFleetIdleSource{pollsRemaining: idlePollsPerShard, cancel: cancel},
				Committer:              quietFleetNoopCommitter{},
				PollInterval:           time.Millisecond,
				AfterEmptyBatchDrained: true,
				AfterBatchDrained: func(ctx context.Context, hasCommitted bool) error {
					return store.RunDeferredRelationshipMaintenanceAfterShardDrain(
						ctx,
						DeferredMaintenanceBarrierConfig{
							ShardCount:   shardCount,
							ShardIndex:   shardIndex,
							HasCommitted: hasCommitted,
						},
						nil,
						nil,
					)
				},
			}
			if err := service.Run(ctx); err != nil {
				runErrs[shardIndex] = fmt.Errorf("shard=%d: %w", shardIndex, err)
			}
		}(shardIndex)
	}
	wg.Wait()

	// Check the epoch-open count before any per-shard run error: under a
	// broken once-latch, the diagnostic signal is HOW MANY epochs opened, not
	// merely that a shard's run errored (which follows from the same
	// mutation once a stuck wait crosses safetyDeadline).
	state.mu.Lock()
	epochOpens := append([]int64(nil), state.epochOpens...)
	state.mu.Unlock()
	if got := len(epochOpens); got != 1 {
		t.Fatalf("epoch opens across %d shards x %d idle polls each = %d (epochs %v), want exactly 1 (the once-latch startup pass; every later escape call across the whole fleet must stay join-only)",
			shardCount, idlePollsPerShard, got, epochOpens)
	}

	for shardIndex, err := range runErrs {
		if err != nil {
			t.Fatalf("collector.Service.Run(shard=%d) error = %v, want nil", shardIndex, err)
		}
	}
}
