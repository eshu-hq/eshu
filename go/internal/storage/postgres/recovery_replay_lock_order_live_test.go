// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8205 150 rounds of three concurrent recovery replays (scoped plus failure class, stage-only, bounded per scope in reverse) over 400,000 analyzed background rows, with an EXPLAIN check that the scoped and stage-only replays reach their rows through different indexes and never seq-scan fact_work_items; requires 0 x 40P01 and every row replayed exactly once (main: 145 of 150 rounds deadlocked); isolated schema from ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN; about 17-19 s locally including a 10 s seed; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// lockOrderRounds is the issue's acceptance bar: at least 150 rounds of the
// three concurrent replays with zero 40P01.
const lockOrderRounds = 150

// TestRecoveryReplayConcurrentReplaysNeverDeadlockLive is the #8205
// regression. Each round resets the 18 target rows to dead_letter and starts
// three replays on independent connections at once:
//
//   - an unbounded replay scoped to the six scopes and retry_exhausted;
//   - an unbounded stage-only replay;
//   - a bounded (Limit 3) replay of each scope in reverse scope order.
//
// The two unbounded replays wait on row locks. Before the fix each locked
// rows in its own index-scan order, and the plan assertion below proves the
// planner gives them different indexes, so overlapping replays formed a lock
// cycle and Postgres aborted one with 40P01 (77 to 149 of 150 rounds measured
// on the diagnosis). The round must also account for every row exactly once:
// the three replays together report each target row once, and none is left
// terminal.
func TestRecoveryReplayConcurrentReplaysNeverDeadlockLive(t *testing.T) {
	f := openLockOrderFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	assertReplayPlansDiverge(t, ctx, f)

	conns := []*sql.DB{f.session(t), f.session(t), f.session(t)}
	reversed := slices.Clone(f.scopes)
	slices.Reverse(reversed)
	replays := []func(RecoveryStore) ([]string, error){
		func(s RecoveryStore) ([]string, error) {
			result, err := s.ReplayFailedWorkItems(ctx, f.scopedFilter(), time.Now())
			return result.WorkItemIDs, err
		},
		func(s RecoveryStore) ([]string, error) {
			result, err := s.ReplayFailedWorkItems(ctx, stageOnlyFilter(), time.Now())
			return result.WorkItemIDs, err
		},
		func(s RecoveryStore) ([]string, error) {
			var ids []string
			for _, scope := range reversed {
				result, err := s.ReplayFailedWorkItems(ctx, recovery.ReplayFilter{
					Stage: recovery.StageReducer, ScopeIDs: []string{scope}, Limit: lockOrderRowsPerScope,
				}, time.Now())
				if err != nil {
					return ids, err
				}
				ids = append(ids, result.WorkItemIDs...)
			}
			return ids, nil
		},
	}

	var deadlocks [3]int
	var accountingFailures int
	started := time.Now()
	for round := 0; round < lockOrderRounds; round++ {
		f.reset(t, ctx)
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			start   = make(chan struct{})
			counted = map[string]int{}
		)
		for i, replay := range replays {
			wg.Add(1)
			go func(i int, replay func(RecoveryStore) ([]string, error)) {
				defer wg.Done()
				<-start
				ids, err := replay(NewRecoveryStore(SQLDB{DB: conns[i]}))
				mu.Lock()
				defer mu.Unlock()
				for _, id := range ids {
					counted[id]++
				}
				switch {
				case isDeadlock(err):
					deadlocks[i]++
				case err != nil:
					t.Errorf("round %d replay %d: %v", round, i, err)
				}
			}(i, replay)
		}
		close(start)
		wg.Wait()

		if problem := lockOrderAccounting(f, counted, f.statusCounts(t, ctx)); problem != "" {
			accountingFailures++
			if accountingFailures <= 5 {
				t.Logf("round %d accounting: %s", round, problem)
			}
		}
	}
	t.Logf("%d rounds in %s: 40P01 scoped=%d stage-only=%d bounded=%d; rounds failing exact accounting=%d",
		lockOrderRounds, time.Since(started).Round(time.Millisecond),
		deadlocks[0], deadlocks[1], deadlocks[2], accountingFailures)
	if total := deadlocks[0] + deadlocks[1] + deadlocks[2]; total != 0 {
		t.Errorf("concurrent replays deadlocked %d times in %d rounds, want 0", total, lockOrderRounds)
	}
	if accountingFailures != 0 {
		t.Errorf("%d of %d rounds did not replay every target row exactly once", accountingFailures, lockOrderRounds)
	}
}

// lockOrderAccounting returns "" when every target row was reported replayed
// exactly once and all rows are now pending, else a description.
func lockOrderAccounting(f *lockOrderFixture, counted map[string]int, statuses map[string]int) string {
	var problems []string
	for _, id := range f.ids {
		if counted[id] != 1 {
			problems = append(problems, fmt.Sprintf("%s reported %d times", id, counted[id]))
		}
	}
	if len(counted) != len(f.ids) {
		problems = append(problems, fmt.Sprintf("%d distinct rows reported, want %d", len(counted), len(f.ids)))
	}
	if statuses["pending"] != lockOrderTargetRows {
		problems = append(problems, fmt.Sprintf("statuses %v, want all %d pending", statuses, lockOrderTargetRows))
	}
	return strings.Join(problems, "; ")
}

// assertReplayPlansDiverge proves the regression is not vacuous: the scoped
// and stage-only replays the production builder renders scan fact_work_items
// through different indexes, so without an explicit lock order their row
// locks come in opposite orders. It also requires no sequential scan of
// fact_work_items in either plan.
func assertReplayPlansDiverge(t *testing.T, ctx context.Context, f *lockOrderFixture) {
	t.Helper()
	now := time.Now()
	scopedSQL, scopedArgs := buildReplayFailedWorkItemsQuery(f.scopedFilter(), now)
	stageSQL, stageArgs := buildReplayFailedWorkItemsQuery(stageOnlyFilter(), now)
	scoped := factWorkItemsScans(t, ctx, f.base, scopedSQL, scopedArgs...)
	stage := factWorkItemsScans(t, ctx, f.base, stageSQL, stageArgs...)
	t.Logf("scoped replay scans fact_work_items via %v; stage-only via %v", scoped, stage)
	for _, scan := range append(slices.Clone(scoped), stage...) {
		if strings.HasPrefix(scan, "Seq Scan") {
			t.Fatalf("replay plan has a sequential scan of fact_work_items: scoped %v, stage-only %v", scoped, stage)
		}
	}
	filterScans := func(scans []string) []string {
		var out []string
		for _, scan := range scans {
			if !strings.HasSuffix(scan, ":fact_work_items_pkey") {
				out = append(out, scan)
			}
		}
		return out
	}
	scopedFilterScans, stageFilterScans := filterScans(scoped), filterScans(stage)
	if len(scopedFilterScans) == 0 || len(stageFilterScans) == 0 || slices.Equal(scopedFilterScans, stageFilterScans) {
		t.Fatalf("scoped and stage-only replays must reach their rows through different indexes "+
			"(else the deadlock regression is vacuous): scoped %v, stage-only %v", scoped, stage)
	}
}
