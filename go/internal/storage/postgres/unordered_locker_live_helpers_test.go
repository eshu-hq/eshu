// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// Shared fixture for the #8261 repros: the multi-row fact_work_items lockers
// that #8205 left unordered (poison recovery, the retention cascade delete
// and projector Ack's obsolete-generation supersede) raced against the
// ordered lockers (the recovery replays and the reducer claim sweep, both
// work_item_id COLLATE "C"). Each repro seeds one small scope on top of the
// 400,000-row background of the #8205 fixture, so the planner sees
// production-shaped statistics, and drives the pair through
// raceThroughHeldRow: the outcome then depends only on the two lock orders.

// openUnorderedLockerFixture bootstraps an isolated schema with the #8205
// background rows (no #8205 target rows). It skips without a proof DSN.
func openUnorderedLockerFixture(t *testing.T) *lockOrderFixture {
	t.Helper()
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8261 unordered-locker repros")
	}
	ctx, cancel := context.WithTimeout(context.Background(), lockOrderSeedTimeout)
	defer cancel()
	base, schema := openReducerFairnessDBWithSchema(t, ctx, dsn)
	f := &lockOrderFixture{dsn: dsn, schema: schema, base: base, at: time.Now().UTC().Add(-time.Hour).Truncate(time.Second)}
	started := time.Now()
	f.seedBackground(t, ctx)
	var collation string
	if err := base.QueryRowContext(ctx, `SELECT datcollate FROM pg_database WHERE datname = current_database()`).Scan(&collation); err != nil {
		t.Fatalf("read database collation: %v", err)
	}
	t.Logf("seeded background in %s; database collation %s", time.Since(started).Round(time.Millisecond), collation)
	return f
}

// seedLockerScope inserts one scope pointing at active; the test adds the
// generation rows.
func (f *lockOrderFixture) seedLockerScope(t *testing.T, ctx context.Context, scope, active string) {
	t.Helper()
	lockOrderExec(t, ctx, f.base, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, NULL, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`, scope, f.at, active)
}

// seedLockerGeneration inserts one generation of scope. ingestedOffset orders
// generations within the scope (larger is newer).
func (f *lockOrderFixture) seedLockerGeneration(
	t *testing.T, ctx context.Context, scope, generation, status string, ingestedOffset time.Duration,
) {
	t.Helper()
	ingested := f.at.Add(ingestedOffset)
	var activated, superseded any
	switch status {
	case "active":
		activated = ingested
	case "superseded":
		superseded = f.at.Add(-30 * 24 * time.Hour)
	}
	lockOrderExec(t, ctx, f.base, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload)
VALUES ($1, $2, 'snapshot', 'x', $3, $3, $4, $5, $6, '{}'::jsonb)`,
		generation, scope, ingested, status, activated, superseded)
}

// seedLockerWork inserts one work row. updatedOffset sets updated_at and
// visible_at relative to the fixture clock.
func (f *lockOrderFixture) seedLockerWork(
	t *testing.T, ctx context.Context, id, scope, generation, stage, status string, updatedOffset time.Duration,
) {
	t.Helper()
	domain := "workload_identity"
	if stage == "projector" {
		domain = "source_local"
	}
	lockOrderExec(t, ctx, f.base, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, failure_class, payload, created_at, updated_at, visible_at)
VALUES ($1, $2, $3, $4, $5, 'scope', $1, $6, 3, $7,
    jsonb_build_object('source_system', 'git'), $8, $8, $8)`,
		id, scope, generation, stage, domain, status, lockOrderClass, f.at.Add(updatedOffset))
}

// workStatuses reads the status of each id.
func (f *lockOrderFixture) workStatuses(t *testing.T, ctx context.Context, ids []string) map[string]string {
	t.Helper()
	rows, err := f.base.QueryContext(ctx, `SELECT work_item_id, status FROM fact_work_items WHERE work_item_id = ANY($1)`, ids)
	if err != nil {
		t.Fatalf("read statuses: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("scan status: %v", err)
		}
		out[id] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate statuses: %v", err)
	}
	return out
}

// startLocker runs fn on its own goroutine as a lockOrderReplay.
func startLocker(ctx context.Context, db *sql.DB, fn func(context.Context, *sql.DB) ([]string, error)) lockOrderReplay {
	r := lockOrderReplay{done: make(chan lockOrderReplayResult, 1)}
	go func() {
		ids, err := fn(ctx, db)
		r.done <- lockOrderReplayResult{ids: ids, err: err}
	}()
	return r
}

// lockerVerdict names the outcome of one held-row race for the test log.
func lockerVerdict(a, b lockOrderReplayResult) string {
	switch {
	case isDeadlock(a.err) || isDeadlock(b.err):
		return "40P01"
	case a.err != nil || b.err != nil:
		return "error"
	default:
		return "one-way"
	}
}

// heldRowPair races a (started first) against b through held and fails the
// test on any error, naming a 40P01 explicitly.
func (f *lockOrderFixture) heldRowPair(
	t *testing.T, ctx context.Context, held, nameA, nameB string,
	a, b func(context.Context, *sql.DB) ([]string, error),
) (lockOrderReplayResult, lockOrderReplayResult) {
	t.Helper()
	resultA, resultB := f.raceThroughHeldRow(t, ctx, held,
		func(ctx context.Context, db *sql.DB) lockOrderReplay { return startLocker(ctx, db, a) },
		func(ctx context.Context, db *sql.DB) lockOrderReplay { return startLocker(ctx, db, b) })
	t.Logf("%s then %s through %s: %s", nameA, nameB, held, lockerVerdict(resultA, resultB))
	for name, result := range map[string]lockOrderReplayResult{nameA: resultA, nameB: resultB} {
		if result.err != nil {
			t.Errorf("%s: %v (40P01=%t)", name, result.err, isDeadlock(result.err))
		}
	}
	return resultA, resultB
}

// recoveryReplayLocker runs the production recovery replay for filter.
func recoveryReplayLocker(filter recovery.ReplayFilter) func(context.Context, *sql.DB) ([]string, error) {
	return func(ctx context.Context, db *sql.DB) ([]string, error) {
		result, err := NewRecoveryStore(SQLDB{DB: db}).ReplayFailedWorkItems(ctx, filter, time.Now())
		return result.WorkItemIDs, err
	}
}

// claimSweepLocker runs one reducer claim, and with it the claim-time
// supersede sweep (supersedeInactiveReducerGenerationsCTE).
func claimSweepLocker(ctx context.Context, db *sql.DB) ([]string, error) {
	clock := time.Now().UTC()
	_, _, err := newOwnGateQueue(db, &clock, "workload_identity").Claim(ctx)
	return nil, err
}

// joinIDs renders ids for a log line.
func joinIDs(ids []string) string { return strings.Join(ids, ",") }

// namedLocker is one statement of a concurrent stress round.
type namedLocker struct {
	name   string
	locker func(context.Context, *sql.DB) ([]string, error)
}

// lockerStressSleepMS is the per-row trigger sleep of the stress rounds. It
// widens the window between two row locks of one statement (a BEFORE ROW
// trigger fires with the row lock held) without changing any lock order;
// deadlock_timeout stays at its 1 s default so a cycle resolves quickly.
const lockerStressSleepMS = "2"

// lockerStressJitterMS bounds each locker's random start delay in a stress
// round, so a statement can start while another is part way through its rows
// instead of the fastest lock step always winning outright.
const lockerStressJitterMS = 160

// stressLockers runs every locker concurrently, each on its own session, for
// lockOrderRounds rounds, each after a random start delay below
// lockerStressJitterMS. reset reseeds the rows before each round; check
// returns "" when a round's outcome accounts for every row. Each session
// sleeps lockerStressSleepMS per updated row in the test trigger, which the
// caller installs. It logs the 40P01 count per locker and fails the test on
// any deadlock, any other error, or any round failing the accounting.
func (f *lockOrderFixture) stressLockers(
	t *testing.T, ctx context.Context, reset func(), check func([]lockOrderReplayResult) string, lockers []namedLocker,
) {
	t.Helper()
	conns := make([]*sql.DB, len(lockers))
	for i := range lockers {
		conns[i] = f.session(t)
		lockOrderExec(t, ctx, conns[i], "SET "+lockOrderSleepGUC+" = '"+lockerStressSleepMS+"'")
	}
	deadlocks := make([]int, len(lockers))
	accountingFailures := 0
	started := time.Now()
	for round := 0; round < lockOrderRounds; round++ {
		reset()
		results := make([]lockOrderReplayResult, len(lockers))
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i, l := range lockers {
			wg.Add(1)
			go func(i int, l namedLocker) {
				defer wg.Done()
				<-start
				time.Sleep(time.Duration(rand.IntN(lockerStressJitterMS)) * time.Millisecond)
				ids, err := l.locker(ctx, conns[i])
				results[i] = lockOrderReplayResult{ids: ids, err: err}
			}(i, l)
		}
		close(start)
		wg.Wait()
		for i, result := range results {
			switch {
			case isDeadlock(result.err):
				deadlocks[i]++
			case result.err != nil:
				t.Errorf("round %d %s: %v", round, lockers[i].name, result.err)
			}
		}
		if problem := check(results); problem != "" {
			accountingFailures++
			if accountingFailures <= 3 {
				t.Logf("round %d accounting: %s", round, problem)
			}
		}
	}
	parts := make([]string, len(lockers))
	total := 0
	for i, l := range lockers {
		parts[i] = fmt.Sprintf("%s=%d", l.name, deadlocks[i])
		total += deadlocks[i]
	}
	t.Logf("%d rounds in %s: 40P01 %s; rounds failing accounting=%d",
		lockOrderRounds, time.Since(started).Round(time.Millisecond), strings.Join(parts, " "), accountingFailures)
	if total != 0 {
		t.Errorf("concurrent lockers deadlocked %d times in %d rounds, want 0", total, lockOrderRounds)
	}
	if accountingFailures != 0 {
		t.Errorf("%d of %d rounds failed the row accounting", accountingFailures, lockOrderRounds)
	}
}
