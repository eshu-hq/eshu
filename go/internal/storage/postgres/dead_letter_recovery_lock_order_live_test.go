// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/poison"
)

// poisonLockOrderCase is one id shape for the poison recovery repro.
type poisonLockOrderCase struct {
	name string
	// keys are the entity-key suffixes of the three dead-lettered reducer
	// rows; held is the one the third session keeps locked.
	keys []string
	held string
}

// poisonLockOrderCases pairs an id shape whose default-collation order and
// C order disagree (an upper-case entity key) with one where they agree.
var poisonLockOrderCases = []poisonLockOrderCase{
	// C: Nginx, api, mysql. en_US: api, mysql, Nginx. Held mysql: poison holds
	// api and waits on mysql, the replay takes Nginx and waits on api.
	{name: "mixed-case keys", keys: []string{"Nginx", "api", "mysql"}, held: "mysql"},
	// Both collations: api, mysql, nginx.
	{name: "lower-case keys", keys: []string{"api", "mysql", "nginx"}, held: "mysql"},
}

// TestPoisonRecoveryHeldRowRaceLive races poison recovery
// (RecoverDeadLettersQuery) against the ordered recovery replays (#8261). The
// poison candidate CTE orders by work_item_id in the database's default
// collation and the UPDATE locks in that order, while every replay locks in
// work_item_id COLLATE "C" order (#8205). Both lock the dead letters of a
// scope's newest generation. Poison starts first and the replay second; the
// held middle row makes the outcome depend only on the two orders. Poison
// and the replays together must re-drive each row exactly once.
func TestPoisonRecoveryHeldRowRaceLive(t *testing.T) {
	f := openUnorderedLockerFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	// Keep the background projector dead-letter tail out of the poison batch.
	lockOrderExec(t, ctx, f.base, `
UPDATE fact_work_items SET payload = payload || '{"poison_recovery_attempts": 99}'::jsonb
WHERE scope_id LIKE 'bg-%' AND status = 'dead_letter'`)
	f.seedLockerScope(t, ctx, "pz", "pz-g")
	f.seedLockerGeneration(t, ctx, "pz", "pz-g", "active", 0)

	replays := []struct {
		name   string
		filter recovery.ReplayFilter
	}{
		{name: "scoped replay", filter: recovery.ReplayFilter{Stage: recovery.StageReducer, ScopeIDs: []string{"pz"}}},
		{name: "stage-only replay", filter: recovery.ReplayFilter{Stage: recovery.StageReducer}},
	}
	for _, tc := range poisonLockOrderCases {
		for _, replay := range replays {
			replayName, filter := replay.name, replay.filter
			t.Run(tc.name+"/"+replayName, func(t *testing.T) {
				ids := f.seedPoisonRows(t, ctx, tc.keys)
				f.analyzeLockerTables(t, ctx)
				held := poisonWorkItemID(tc.held)
				recovered := 0
				_, replayed := f.heldRowPair(t, ctx, held, "poison recovery", replayName,
					func(ctx context.Context, db *sql.DB) ([]string, error) {
						result, err := poison.NewStore(SQLDB{DB: db}).RecoverDeadLetters(ctx, poison.Policy{}, time.Now())
						recovered = result.Recovered
						return nil, err
					},
					recoveryReplayLocker(filter))
				statuses := f.workStatuses(t, ctx, ids)
				for _, id := range ids {
					if statuses[id] != "pending" {
						t.Errorf("%s ended %q, want pending", id, statuses[id])
					}
				}
				if got := recovered + len(replayed.ids); got != len(ids) {
					t.Errorf("poison re-drove %d and the replay %d (%s): %d in all, want %d",
						recovered, len(replayed.ids), joinIDs(replayed.ids), got, len(ids))
				}
			})
		}
	}
}

// poisonWorkItemID is the reducer work id of a poison-fixture row.
func poisonWorkItemID(key string) string {
	return "reducer_pz_pz-g_workload_identity_" + key
}

// seedPoisonRows replaces the poison scope's rows with one dead-lettered
// reducer row per key and returns their ids.
func (f *lockOrderFixture) seedPoisonRows(t *testing.T, ctx context.Context, keys []string) []string {
	t.Helper()
	lockOrderExec(t, ctx, f.base, `DELETE FROM fact_work_items WHERE scope_id = 'pz'`)
	ids := make([]string, 0, len(keys))
	for i, key := range keys {
		id := poisonWorkItemID(key)
		ids = append(ids, id)
		f.seedLockerWork(t, ctx, id, "pz", "pz-g", "reducer", "dead_letter", time.Duration(i)*time.Second)
	}
	return ids
}

// analyzeLockerTables refreshes planner statistics after a reseed.
func (f *lockOrderFixture) analyzeLockerTables(t *testing.T, ctx context.Context) {
	t.Helper()
	for _, table := range []string{"fact_work_items", "scope_generations", "ingestion_scopes"} {
		lockOrderExec(t, ctx, f.base, fmt.Sprintf("ANALYZE %s", table))
	}
}

// TestPoisonRecoveryDropsRowChangedBeforeItsLockLive is the EvalPlanQual case
// of poison recovery's ordered lock step (#8261). Session X changes the first
// row in lock order and holds it uncommitted; poison recovery takes its
// snapshot while the row still reads as an eligible dead letter and waits on
// it. X then commits either a claim (status claimed) or a spent recovery
// budget (another sweep re-drove the row and it dead-lettered again). Poison
// recovery must drop the row and re-drive only the other two.
func TestPoisonRecoveryDropsRowChangedBeforeItsLockLive(t *testing.T) {
	f := openUnorderedLockerFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	lockOrderExec(t, ctx, f.base, `
UPDATE fact_work_items SET payload = payload || '{"poison_recovery_attempts": 99}'::jsonb
WHERE scope_id LIKE 'bg-%' AND status = 'dead_letter'`)
	f.seedLockerScope(t, ctx, "pz", "pz-g")
	f.seedLockerGeneration(t, ctx, "pz", "pz-g", "active", 0)
	for _, tc := range []struct {
		name, change, wantStatus string
	}{
		{name: "claimed", change: `SET status = 'claimed', lease_owner = 'epq-worker'`, wantStatus: "claimed"},
		{name: "budget spent", change: `SET payload = payload || '{"poison_recovery_attempts": 1}'::jsonb`, wantStatus: "dead_letter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := f.seedPoisonRows(t, ctx, poisonLockOrderCases[0].keys)
			target := poisonWorkItemID("Nginx") // first in C order
			x := f.session(t)
			tx, err := x.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin X: %v", err)
			}
			defer func() { _ = tx.Rollback() }()
			if _, err := tx.ExecContext(ctx, `UPDATE fact_work_items `+tc.change+` WHERE work_item_id = $1`, target); err != nil {
				t.Fatalf("X change: %v", err)
			}
			y := f.session(t)
			pidY := lockOrderBackendPID(t, ctx, y)
			recovered := 0
			run := startLocker(ctx, y, func(ctx context.Context, db *sql.DB) ([]string, error) {
				result, err := poison.NewStore(SQLDB{DB: db}).RecoverDeadLetters(ctx, poison.Policy{}, time.Now())
				recovered = result.Recovered
				return nil, err
			})
			waitBlocked(t, ctx, f.base, pidY)
			if err := tx.Commit(); err != nil {
				t.Fatalf("X commit: %v", err)
			}
			if result := run.wait(); result.err != nil {
				t.Fatalf("poison recovery: %v", result.err)
			}
			if recovered != len(ids)-1 {
				t.Errorf("poison recovery re-drove %d rows, want %d", recovered, len(ids)-1)
			}
			statuses := f.workStatuses(t, ctx, ids)
			for _, id := range ids {
				want := "pending"
				if id == target {
					want = tc.wantStatus
				}
				if statuses[id] != want {
					t.Errorf("%s ended %q, want %q", id, statuses[id], want)
				}
			}
		})
	}
}

// poisonStressKeys are 18 entity keys, half "Z.." and half "a..": C order
// puts every "Z" key first, while a case-insensitive default collation puts
// every "a" key first, so the two orders share no leading row.
func poisonStressKeys() []string {
	keys := make([]string, 0, 18)
	for i := 0; i < 18; i++ {
		prefix := "a"
		if i%2 == 0 {
			prefix = "Z"
		}
		keys = append(keys, fmt.Sprintf("%s%02d", prefix, i))
	}
	return keys
}

// poisonRecoveryLocker runs production poison recovery and reports one entry
// per re-driven row.
func poisonRecoveryLocker(ctx context.Context, db *sql.DB) ([]string, error) {
	result, err := poison.NewStore(SQLDB{DB: db}).RecoverDeadLetters(ctx, poison.Policy{}, time.Now())
	return result.RecoveredScopeIDs, err
}

// TestPoisonRecoveryConcurrentWithReplaysNeverDeadlockLive runs poison
// recovery, a scoped and a stage-only recovery replay concurrently over 18
// mixed-case dead letters for lockOrderRounds rounds (#8261). Every round
// must end with each row re-driven exactly once and pending.
func TestPoisonRecoveryConcurrentWithReplaysNeverDeadlockLive(t *testing.T) {
	f := openUnorderedLockerFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	lockOrderExec(t, ctx, f.base, `
UPDATE fact_work_items SET payload = payload || '{"poison_recovery_attempts": 99}'::jsonb
WHERE scope_id LIKE 'bg-%' AND status = 'dead_letter'`)
	f.seedLockerScope(t, ctx, "pz", "pz-g")
	f.seedLockerGeneration(t, ctx, "pz", "pz-g", "active", 0)
	keys := poisonStressKeys()
	ids := f.seedPoisonRows(t, ctx, keys)
	f.analyzeLockerTables(t, ctx)
	f.installSleepTrigger(t, ctx)
	reset := func() {
		lockOrderExec(t, ctx, f.base, `
UPDATE fact_work_items SET status = 'dead_letter', payload = jsonb_build_object('source_system', 'git'),
    lease_owner = NULL, claim_until = NULL, failure_class = $1
WHERE scope_id = 'pz'`, lockOrderClass)
	}
	check := func(results []lockOrderReplayResult) string {
		total := 0
		for _, result := range results {
			total += len(result.ids)
		}
		statuses := f.workStatuses(t, ctx, ids)
		pending := 0
		for _, status := range statuses {
			if status == "pending" {
				pending++
			}
		}
		if total != len(ids) || pending != len(ids) {
			return fmt.Sprintf("%d rows re-driven in all and %d pending, want %d and %d", total, pending, len(ids), len(ids))
		}
		return ""
	}
	f.stressLockers(t, ctx, reset, check, []namedLocker{
		{name: "poison", locker: poisonRecoveryLocker},
		{name: "scoped", locker: recoveryReplayLocker(recovery.ReplayFilter{Stage: recovery.StageReducer, ScopeIDs: []string{"pz"}})},
		{name: "stage-only", locker: recoveryReplayLocker(recovery.ReplayFilter{Stage: recovery.StageReducer})},
	})
}
