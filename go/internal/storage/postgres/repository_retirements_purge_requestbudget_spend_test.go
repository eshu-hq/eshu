// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestPurgeRetiredChargesOnlyTheElapsedLockWait pins what a lock statement
// costs the request budget: the time it took, not the lock_timeout it was
// given. Four repositories whose derive locks are free take no time, so they
// spend none of the 30 s, and a held lock after them still gets a full 10 s.
// Charging the timeout that was set would make each free lock statement cost
// 10 s and leave the fifth repository the 100 ms floor.
func TestPurgeRetiredChargesOnlyTheElapsedLockWait(t *testing.T) {
	t.Parallel()

	world := newBudgetDB(
		repoLocks{}, repoLocks{}, repoLocks{}, repoLocks{},
		repoLocks{infra: lockHeld},
	)
	purger, logs := newBudgetPurger(world)

	result, err := purger.PurgeRetired(context.Background(), PurgeRequest{RepoIDs: budgetRepoIDs("free", 5)})
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(result.Completed) != 4 || len(result.Blocked) != 1 {
		t.Fatalf("completed %v, blocked %+v, want the four free repositories completed and the held one blocked",
			result.Completed, result.Blocked)
	}
	// Four repositories take two lock statements each and the fifth blocks on
	// its first, so nine settings. Checking the count keeps the loop below from
	// passing on an empty list.
	if len(world.settings) != 9 {
		t.Fatalf("lock_timeout settings = %v, want nine (four repositories x two locks, then the held one)", world.settings)
	}
	for i, got := range world.settings {
		if got != 10*time.Second {
			t.Errorf("lock_timeout set #%d = %s, want a full 10 s: a lock taken at once costs the request budget nothing",
				i+1, got)
		}
	}
	busy := logs.named(deriveBusyLogMessage)
	if len(busy) != 1 || busy[0].attrs["derive_lock_timeout_ms"] != int64(10000) {
		t.Errorf("busy WARN lines = %+v, want one carrying derive_lock_timeout_ms=10000", busy)
	}
	if world.waited != 10*time.Second {
		t.Errorf("lock waiting = %s, want only the held lock's 10 s", world.waited)
	}
}

// TestPurgeRetiredChargesASlowLockAcquireItsElapsedTime pins the other half of
// the accounting: a lock taken after a wait, neither at once nor at the
// timeout, costs exactly that wait. Fifteen repositories each take the infra
// lock after 2 s, which spends the 30 s, so the sixteenth finds the budget
// spent and is blocked on the 100 ms floor. Every setting along the way is
// what is left of the 30 s, capped at 10 s, so the whole sequence is listed.
func TestPurgeRetiredChargesASlowLockAcquireItsElapsedTime(t *testing.T) {
	t.Parallel()

	plans := make([]repoLocks, 16)
	for i := range plans {
		plans[i] = repoLocks{infra: lockSlow}
	}
	world := newBudgetDB(plans...)
	purger, _ := newBudgetPurger(world)

	result, err := purger.PurgeRetired(context.Background(), PurgeRequest{RepoIDs: budgetRepoIDs("slow", len(plans))})
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(result.Completed) != 15 || len(result.Blocked) != 1 {
		t.Fatalf("completed %d, blocked %d, want 15 completed and the sixteenth blocked (15 x 2 s spends the 30 s)",
			len(result.Completed), len(result.Blocked))
	}

	// One repository sets its infra wait and then its config wait (that lock is
	// free). Ten repositories still see 10 s or more left at both settings; the
	// next five count down by 2 s; the blocked sixteenth sets only its infra
	// wait, on the floor.
	perLock, floor := 10*time.Second, 100*time.Millisecond
	var want []time.Duration
	for i := 0; i < 10; i++ {
		want = append(want, perLock, perLock)
	}
	want = append(want,
		perLock, 8*time.Second, // repository 11
		8*time.Second, 6*time.Second, // repository 12
		6*time.Second, 4*time.Second, // repository 13
		4*time.Second, 2*time.Second, // repository 14
		2*time.Second, floor, // repository 15 spends the last 2 s
		floor, // repository 16
	)
	if fmt.Sprint(world.settings) != fmt.Sprint(want) {
		t.Errorf("lock_timeout settings =\n  %v\nwant\n  %v", world.settings, want)
	}
}
