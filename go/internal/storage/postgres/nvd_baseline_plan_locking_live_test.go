// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10828 disposable PostgreSQL proof of the NVD baseline plan row's locking: two coordinators of different instances that share one baseline run and name the same years in opposite order neither deadlock nor retry, because the writer takes row locks in one order, and an admission that records plan rows holds the same single advisory lock as one that does not plus only RowExclusiveLock on the plan table and its primary key; isolated bootstrap schema with migration 312
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// planLockProbeTx reads the admission backend's own granted locks just before
// the commit, while the transaction still holds everything it took.
type planLockProbeTx struct {
	db.Transaction
	ctx   context.Context
	t     *testing.T
	locks *[]string
}

func (p planLockProbeTx) Commit() error {
	rows, err := p.QueryContext(p.ctx, `
SELECT l.locktype || ' ' || COALESCE(c.relname, '') || ' ' || l.mode
FROM pg_locks AS l
LEFT JOIN pg_class AS c ON c.oid = l.relation
WHERE l.pid = pg_backend_pid()
  AND l.granted
  AND l.locktype IN ('advisory', 'relation')
  AND COALESCE(c.relname, '') NOT LIKE 'pg\_%'
ORDER BY 1`)
	if err != nil {
		p.t.Errorf("read held locks: %v", err)
		return p.Transaction.Commit()
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var lock string
		if err := rows.Scan(&lock); err != nil {
			p.t.Errorf("scan held lock: %v", err)
			break
		}
		*p.locks = append(*p.locks, lock)
	}
	if err := rows.Err(); err != nil {
		p.t.Errorf("read held locks: %v", err)
	}
	_ = rows.Close()
	return p.Transaction.Commit()
}

// planLockMultiset returns the locks of a held-lock list, sorted, so two lists
// compare as multisets.
func planLockMultiset(locks []string) []string {
	out := slices.Clone(locks)
	slices.Sort(out)
	return out
}

// planLockMissing returns the entries of want that are not in have, counting
// repeats.
func planLockMissing(want, have []string) []string {
	remaining := slices.Clone(have)
	var missing []string
	for _, lock := range want {
		if i := slices.Index(remaining, lock); i >= 0 {
			remaining = slices.Delete(remaining, i, i+1)
			continue
		}
		missing = append(missing, lock)
	}
	return missing
}

// TestWorkflowControlBaselinePlansLockingLive proves the plan write adds no new
// lock hazard: it does not deadlock two admissions that share a baseline run, and
// the locks it holds are the admission's own plus a row-level lock on the plan
// table.
func TestWorkflowControlBaselinePlansLockingLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "baseline_plans_lock_10828", "ESHU_POSTGRES_DSN is not set")
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	handle := &planLiveDB{SQLDB: SQLDB{DB: database}}
	store := NewWorkflowControlStore(handle)

	t.Run("two instances sharing one baseline run name the years in opposite order without a deadlock", func(t *testing.T) {
		planLiveReset(ctx, t, database)
		// Both coordinators already planned both years once, so the contention is
		// on the two existing row locks.
		seeded := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
		for _, year := range []int{2026, 2025} {
			if _, err := database.ExecContext(ctx, `
INSERT INTO vulnerability_nvd_baseline_plans (baseline_run_id, year, last_planned_scope_id, last_planned_at)
VALUES ($1, $2, 'seed', $3)`, baselineTestRunID, year, seeded); err != nil {
				t.Fatalf("seed plan row %d: %v", year, err)
			}
		}
		// Each row write sleeps before it locks the row and again while it holds
		// it, so two admissions that lock in opposite orders hold one row each
		// before either asks for the second. The default deadlock_timeout (1 s)
		// then picks a victim and the store retries it, which the Begin count
		// shows.
		planLiveTrigger(ctx, t, database, "slow_plan_write_10828", `PERFORM pg_sleep(0.3);`)

		// Different instances take different advisory locks, so only the plan row
		// lock stands between them. The supplied plan order is opposite on purpose.
		runA, itemsA, plansA := planLiveAdmission("opposite-a", "vuln-a", baselineTestStart, 2026, 2025)
		runB, itemsB, plansB := planLiveAdmission("opposite-b", "vuln-b", baselineTestStart.Add(time.Hour), 2025, 2026)
		begins := handle.beginCount()

		var wg sync.WaitGroup
		admissions := make([]workflow.RunAdmission, 2)
		errs := make([]error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			admissions[0], _, errs[0] = store.CreateRunWithWorkItemsRecordingBaselinePlans(ctx, runA, itemsA, plansA, nil)
		}()
		go func() {
			defer wg.Done()
			admissions[1], _, errs[1] = store.CreateRunWithWorkItemsRecordingBaselinePlans(ctx, runB, itemsB, plansB, nil)
		}()
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("admission %d error = %v", i, err)
			}
			if admissions[i].InsertedWorkItems != 2 || admissions[i].BaselinePlansRecorded != 2 {
				t.Fatalf("admission %d = %+v, want 2 items and 2 plan rows", i, admissions[i])
			}
		}
		if got := handle.beginCount() - begins; got != 2 {
			t.Fatalf("transactions begun = %d, want 2: a deadlock between the opposite-order admissions forced a retry", got)
		}
		// Row locks are held to commit, so the two statements cannot interleave:
		// both years name one admission's chunks.
		rows := planLiveRows(ctx, t, database)
		first, second := rows[2026].scopeID, rows[2025].scopeID
		wantA := itemsA[0].ScopeID == first && itemsA[1].ScopeID == second
		wantB := itemsB[1].ScopeID == first && itemsB[0].ScopeID == second
		if !wantA && !wantB {
			t.Fatalf("plan rows = %+v, want both years to name the same admission's chunks", rows)
		}
	})

	t.Run("an admission holds one advisory lock and adds only a row lock on the plan table", func(t *testing.T) {
		planLiveReset(ctx, t, database)
		var plain, withPlans []string
		defer handle.setWrap(nil)

		plainRun, plainItems, _ := planLiveAdmission("lock-plain", "vuln-lock-plain", baselineTestStart, 2026)
		handle.setWrap(func(tx db.Transaction) db.Transaction {
			return planLockProbeTx{Transaction: tx, ctx: ctx, t: t, locks: &plain}
		})
		if _, err := store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, plainRun, plainItems); err != nil {
			t.Fatalf("plain admission error = %v", err)
		}

		planRun, planItems, plans := planLiveAdmission("lock-plan", "vuln-lock-plan", baselineTestStart, 2026)
		handle.setWrap(func(tx db.Transaction) db.Transaction {
			return planLockProbeTx{Transaction: tx, ctx: ctx, t: t, locks: &withPlans}
		})
		admission, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(ctx, planRun, planItems, plans, nil)
		if err != nil {
			t.Fatalf("plan-carrying admission error = %v", err)
		}
		if admission.BaselinePlansRecorded != 1 {
			t.Fatalf("admission = %+v, want the plan row recorded", admission)
		}

		plain, withPlans = planLockMultiset(plain), planLockMultiset(withPlans)
		t.Logf("locks held at commit, without plan rows: %q", plain)
		t.Logf("locks held at commit, with plan rows:    %q", withPlans)
		advisory := func(locks []string) int {
			count := 0
			for _, lock := range locks {
				if lock == "advisory  ExclusiveLock" {
					count++
				}
			}
			return count
		}
		if advisory(plain) != 1 || advisory(withPlans) != 1 {
			t.Fatalf("advisory locks = %d without plan rows, %d with, want exactly the one instance planning lock in both", advisory(plain), advisory(withPlans))
		}
		wantAdded := []string{
			"relation vulnerability_nvd_baseline_plans RowExclusiveLock",
			"relation vulnerability_nvd_baseline_plans_pkey RowExclusiveLock",
		}
		if missing := planLockMissing(withPlans, plain); !slices.Equal(planLockMultiset(missing), wantAdded) {
			t.Fatalf("locks added by the plan write = %q, want exactly %q", missing, wantAdded)
		}
		if removed := planLockMissing(plain, withPlans); len(removed) != 0 {
			t.Fatalf("locks only the plain admission holds = %q, want none", removed)
		}
	})
}
