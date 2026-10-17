// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8261 poison recovery (RecoverDeadLettersQuery) lock order over 400,000 analyzed background rows on an en_US.utf8 database: the held-middle-row race against the scoped and stage-only recovery replays completes for production-shaped work ids (codeowners_ownership against code_taint_evidence and code_value_flow_refresh, which en_US and C order differently), mixed-case and lower-case ids (main: 40P01 for the first two), 150 rounds of poison plus both replays over production-shaped ids with a 2 ms per-row trigger sleep and random start delays give 0 x 40P01 with every row re-driven once (main: 5 x 40P01), and the EvalPlanQual case drops a row claimed or whose recovery budget was spent before the lock step; isolated schemas; about 70-90 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

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
	// suffixes follow "reducer_<scope>_<generation>_" in the three
	// dead-lettered reducer work ids (domain, then entity key); held is the
	// one the third session keeps locked.
	suffixes []string
	held     string
}

// poisonLockOrderCases are id shapes whose default-collation (en_US.utf8)
// order and C order disagree, and one where they agree.
var poisonLockOrderCases = []poisonLockOrderCase{
	// Production shape: reducer domain names and hex entity keys. en_US
	// ignores '_' at the first level, so codeowners_ownership sorts before
	// code_taint_evidence and code_value_flow_refresh; C sorts it after them.
	// Poison waits on the held codeowners row holding nothing; the replay takes
	// the other two and waits on it; the release closes the cycle.
	{name: "production domain names", suffixes: []string{
		"codeowners_ownership_3f9a1c", "code_taint_evidence_8b20e4", "code_value_flow_refresh_c41d77",
	}, held: "codeowners_ownership_3f9a1c"},
	// Upper-case entity key. C: Nginx, api, mysql. en_US: api, mysql, Nginx.
	{name: "mixed-case keys", suffixes: []string{
		"workload_identity_Nginx", "workload_identity_api", "workload_identity_mysql",
	}, held: "workload_identity_mysql"},
	// Both collations: api, mysql, nginx.
	{name: "lower-case keys", suffixes: []string{
		"workload_identity_api", "workload_identity_mysql", "workload_identity_nginx",
	}, held: "workload_identity_mysql"},
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
				ids := f.seedPoisonRows(t, ctx, tc.suffixes)
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

// poisonWorkItemID is the reducer work id of a poison-fixture row:
// reducer_<scope>_<generation>_<suffix>, the reducerWorkItemID layout.
func poisonWorkItemID(suffix string) string {
	return "reducer_pz_pz-g_" + suffix
}

// seedPoisonRows replaces the poison scope's rows with one dead-lettered
// reducer row per id suffix and returns their ids.
func (f *lockOrderFixture) seedPoisonRows(t *testing.T, ctx context.Context, suffixes []string) []string {
	t.Helper()
	lockOrderExec(t, ctx, f.base, `DELETE FROM fact_work_items WHERE scope_id = 'pz'`)
	ids := make([]string, 0, len(suffixes))
	for i, suffix := range suffixes {
		id := poisonWorkItemID(suffix)
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
			ids := f.seedPoisonRows(t, ctx, poisonLockOrderCases[0].suffixes)
			target := poisonWorkItemID("code_taint_evidence_8b20e4") // first in C order
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

// poisonStressSuffixes are 18 production-shaped id suffixes: three reducer
// domains in turn with hex entity keys. C order puts every
// code_taint_evidence and code_value_flow_refresh row before every
// codeowners_ownership row; en_US.utf8 puts codeowners_ownership first, so
// the two orders share no leading row.
func poisonStressSuffixes() []string {
	domains := []string{"codeowners_ownership", "code_taint_evidence", "code_value_flow_refresh"}
	suffixes := make([]string, 0, 18)
	for i := 0; i < 18; i++ {
		suffixes = append(suffixes, fmt.Sprintf("%s_%06x", domains[i%len(domains)], 0x3f9a1c+i*0x1111))
	}
	return suffixes
}

// poisonRecoveryLocker runs production poison recovery and reports one entry
// per re-driven row.
func poisonRecoveryLocker(ctx context.Context, db *sql.DB) ([]string, error) {
	result, err := poison.NewStore(SQLDB{DB: db}).RecoverDeadLetters(ctx, poison.Policy{}, time.Now())
	return result.RecoveredScopeIDs, err
}

// TestPoisonRecoveryConcurrentWithReplaysNeverDeadlockLive runs poison
// recovery, a scoped and a stage-only recovery replay concurrently over 18
// production-shaped dead letters for lockOrderRounds rounds (#8261). Every round
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
	ids := f.seedPoisonRows(t, ctx, poisonStressSuffixes())
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
