// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11327 real PostgreSQL proof of the owed marker that follows the go-evidence lock: the recorded footprint of a changed pass through the real handler, 40 rounds each of a fence queued behind a pass paused before its marker and of a fence-style reopen holding the marker's row, a retirement-style table lock under the marker, and a marker row held 3 s while a fence on that go scope times out and detaches nothing
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
	dbpkg "github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// isOwedMarker reports whether a statement is the pass's owed marker: the update of its
// own fact_work_items row that sets the fan-out owed flag.
func isOwedMarker(statement string) bool {
	return strings.Contains(statement, "UPDATE fact_work_items") && strings.Contains(statement, "fanout_owed")
}

// footprintViolation judges the recorded statements of one committed pass: from the first
// shared go-evidence lock on, only the locks, the recheck and at most the owed marker may
// run; before it the pre-lock classifier (core, plain reads allowed) must be clean. It
// returns "" for a clean footprint.
func footprintViolation(statements []string) string {
	lockAt := -1
	for i, statement := range statements {
		if statement == supplychaincore.SupplyChainImpactGoEvidenceSharedLockQuery {
			lockAt = i
			break
		}
	}
	if lockAt < 0 {
		return "no shared go-evidence lock"
	}
	for i, statement := range statements[:lockAt] {
		if why := supplychaincore.SupplyChainImpactPreLockStatementViolation(statement); why != "" {
			return fmt.Sprintf("statement %d before the lock %s", i, why)
		}
	}
	rest := statements[lockAt:]
	end := 0
	for end < len(rest) && rest[end] == supplychaincore.SupplyChainImpactGoEvidenceSharedLockQuery {
		end++
	}
	if end == len(rest) || !strings.Contains(rest[end], "AS pair(scope_id, generation_id)") {
		return "the recheck does not follow the locks"
	}
	switch tail := rest[end+1:]; {
	case len(tail) == 0:
		return ""
	case len(tail) == 1 && isOwedMarker(tail[0]):
		return ""
	}
	return "a statement other than the owed marker follows the recheck"
}

// TestFootprintViolationNamesTheSeededFootprints is the seeded RED/GREEN half of the
// recorded-footprint pin.
func TestFootprintViolationNamesTheSeededFootprints(t *testing.T) {
	lock, recheck := supplychaincore.SupplyChainImpactGoEvidenceSharedLockQuery, "SELECT 1 FROM unnest($1) AS pair(scope_id, generation_id)"
	marker := "UPDATE fact_work_items SET payload = jsonb_set(payload, '{fanout_owed}', 'true')"
	for name, tc := range map[string]struct {
		statements []string
		want       bool
	}{
		"writes, lock, recheck, marker":      {[]string{"INSERT INTO fact_records (x)", lock, recheck, marker}, false},
		"writes, lock, recheck":              {[]string{"INSERT INTO fact_records (x)", lock, recheck}, false},
		"marker before the lock":             {[]string{"INSERT INTO fact_records (x)", marker, lock, recheck}, true},
		"plain queue read before the lock":   {[]string{"SELECT 1 FROM fact_work_items", lock, recheck, marker}, false},
		"queue read FOR UPDATE before lock":  {[]string{"SELECT 1 FROM fact_work_items FOR UPDATE", lock, recheck, marker}, true},
		"queue table lock before the lock":   {[]string{"LOCK TABLE fact_work_items IN EXCLUSIVE MODE", lock, recheck, marker}, true},
		"scope row written before the lock":  {[]string{"UPDATE ingestion_scopes SET status = 'x'", lock, recheck}, true},
		"generation locked before the lock":  {[]string{"SELECT 1 FROM scope_generations FOR UPDATE", lock, recheck}, true},
		"a second statement after recheck":   {[]string{"INSERT INTO fact_records (x)", lock, recheck, marker, "UPDATE supply_chain_impact_scope_pass_state SET x = 1"}, true},
		"statement between lock and recheck": {[]string{lock, "SELECT 1", recheck}, true},
		"no lock":                            {[]string{"INSERT INTO fact_records (x)"}, true},
	} {
		if got := footprintViolation(tc.statements) != ""; got != tc.want {
			t.Errorf("%s: violation = %v, want %v", name, got, tc.want)
		}
	}
}

// TestGoEvidenceRealHandlerPassFootprintLive records the transaction of one changed pass
// through the real handler (so the owed marker runs) and pins its footprint by what it
// ran, not by reading the code: nothing before the shared lock names fact_work_items or
// writes or locks a scope or generation row, and after the lock only the recheck and the
// owed marker run, then the commit.
func TestGoEvidenceRealHandlerPassFootprintLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	seed := seedGoEvidenceChain(c, "fp", 1)
	var mu sync.Mutex
	var committed [][]string
	c.handler.Writer = supplychaincore.PostgresSupplyChainImpactWriter{DB: recheckHookBeginner{
		inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}},
		onEnd: func(statements []string, ok bool) {
			if !ok {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			committed = append(committed, append([]string(nil), statements...))
		},
	}}
	c.drain()
	mu.Lock()
	defer mu.Unlock()
	checked, markers := 0, 0
	for _, statements := range committed {
		hasLock := false
		for _, statement := range statements {
			hasLock = hasLock || statement == supplychaincore.SupplyChainImpactGoEvidenceSharedLockQuery
		}
		if !hasLock {
			continue
		}
		checked++
		if why := footprintViolation(statements); why != "" {
			t.Errorf("the pass for %s broke the footprint: %s", seed.holder, why)
		}
		for _, statement := range statements {
			if isOwedMarker(statement) {
				markers++
			}
		}
	}
	if checked == 0 || markers == 0 {
		t.Fatalf("committed passes with the lock = %d, owed markers = %d: the pass did not run the footprint under test", checked, markers)
	}
}

// pausedPassRound runs one changed pass of an existing holder with a seam between its
// recheck and its owed marker, and returns the pass error. seam runs on the pass's own
// goroutine while it holds the shared go-evidence lock.
func pausedPassRound(c *convergence, seam func(ctx context.Context)) (hookReturned, committedAt time.Time, passErr error) {
	var mu sync.Mutex
	c.handler.Writer = hookingWriter{
		inner: supplychaincore.PostgresSupplyChainImpactWriter{DB: recheckHookBeginner{
			inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}},
			hook: func(ctx context.Context) {
				seam(ctx)
				mu.Lock()
				hookReturned = time.Now()
				mu.Unlock()
			},
			onEnd: func(_ []string, ok bool) {
				mu.Lock()
				defer mu.Unlock()
				if ok {
					committedAt = time.Now()
				}
			},
		}},
		after: func(_ supplychaincore.SupplyChainImpactWrite, err error) {
			if err != nil {
				passErr = err
			}
		},
	}
	c.drain()
	return hookReturned, committedAt, passErr
}

// TestGoEvidenceFenceQueuesBehindPassPausedBeforeItsMarkerLive is probe (a) of the
// marker ruling: the pass's own scope is already a holder with a changed set, so its
// marker runs; a seam pauses the pass between recheck and marker while the fence starts.
// The fence waits, the pass commits, then the fence commits, sees the pass's support and
// reopens its item. 40 rounds, zero 40P01.
func TestGoEvidenceFenceQueuesBehindPassPausedBeforeItsMarkerLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	plainWriter := c.handler.Writer // every round starts from the production writer, never a prior round's seam
	for round := 0; round < 40; round++ {
		c.handler.Writer = plainWriter
		seed := seedGoEvidenceChain(c, fmt.Sprintf("pm%02d", round), 1)
		c.drain()
		settleSupplyChainImpactSupports(c.t, c.ctx, c.db)
		c.rerun(seed.holder)

		var wg sync.WaitGroup
		var fenced scannercomponent.StaleEvidenceFenceResult
		var fenceErr error
		_, _, passErr := pausedPassRound(c, func(ctx context.Context) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				fenced, fenceErr = goEvidenceFence(c.ctx, c.db, seed.goScopes[0], seed.generations[0], nil)
			}()
			waitForAdvisoryWaiter(ctx, c.t, c.db, 1)
		})
		wg.Wait()
		if passErr != nil || fenceErr != nil || fenced.Fenced != 1 || fenced.HoldersReopened < 1 {
			t.Fatalf("round %d: pass error = %v, fence = %+v, err = %v; want the pass committed and the fence to fence and reopen its scope", round, passErr, fenced, fenceErr)
		}
		c.handler.Writer = plainWriter // the follow-up drain must not re-fire the round's seam
		c.drain()
		if holderVerdictNotCalled(c, seed.holder) {
			t.Fatalf("round %d: the holder still carries the retracted generation's verdict after the fence reopened it", round)
		}
	}
}

// reopenHeldTx opens a transaction that has run the production holder reopen for the
// holder, so it holds the holder's work-item row until it ends: the state of a fence
// between its reopen and its commit.
func reopenHeldTx(ctx context.Context, t *testing.T, database *sql.DB, holder string) dbpkg.Transaction {
	t.Helper()
	tx, err := postgres.SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin the reopen transaction: %v", err)
	}
	if err := postgres.ReopenSupplyChainImpactHolderInTransaction(ctx, tx, holder, "go-reachability://repository:r_other", "gen-other", time.Now().UTC()); err != nil {
		_ = tx.Rollback()
		t.Fatalf("reopen the holder: %v", err)
	}
	return tx
}

// TestGoEvidenceMarkerWaitsBehindAFenceStyleReopenLive is probe (b): the fence is on a go
// scope the pass did not load and its holders include the pass's scope; it has reopened
// the pass's work item and not yet committed while the pass reaches its marker. The pass
// waits on the row, then proceeds. The reopen of a claimed or running item leaves its
// lease and status and only sets cross_scope_replay_required
// (reopenSupplyChainImpactSetsFlag, supply_chain_impact_fanout_reopen.go:52, and the
// stamped-pass paragraph of core/HOLDER_FANOUT.md:95-102), so under READ COMMITTED the
// marker's predicate still matches the updated row once the fence commits and the pass
// commits. The lease-lost retryable error would need a reopen that moves the item to
// pending, which is for succeeded and dead-letter rows only. 40 rounds, zero 40P01.
func TestGoEvidenceMarkerWaitsBehindAFenceStyleReopenLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	plainWriter := c.handler.Writer // every round starts from the production writer, never a prior round's seam
	for round := 0; round < 40; round++ {
		c.handler.Writer = plainWriter
		seed := seedGoEvidenceChain(c, fmt.Sprintf("dr%02d", round), 1)
		c.drain()
		settleSupplyChainImpactSupports(c.t, c.ctx, c.db)
		c.rerun(seed.holder)

		var held dbpkg.Transaction
		var wg sync.WaitGroup
		hookReturned, committedAt, passErr := pausedPassRound(c, func(context.Context) {
			held = reopenHeldTx(c.ctx, c.t, c.db, seed.holder)
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(200 * time.Millisecond)
				if err := held.Commit(); err != nil {
					c.t.Errorf("commit the fence-style reopen: %v", err)
				}
			}()
		})
		wg.Wait()
		if passErr != nil {
			t.Fatalf("round %d: the pass failed with %v, want its marker to match after the reopen commits (a deadlock reads 40P01)", round, passErr)
		}
		if waited := committedAt.Sub(hookReturned); waited < 150*time.Millisecond {
			t.Fatalf("round %d: the marker did not wait on the reopened row (%v after the seam)", round, waited)
		}
	}
}

// TestGoEvidenceMarkerWaitsBehindATableLockWithoutDeadlockLive is probe 2.2: a transaction
// holding the repository-retirement fence's LOCK TABLE fact_work_items IN EXCLUSIVE MODE
// while the pass reaches its marker. The pass waits for the release and commits.
func TestGoEvidenceMarkerWaitsBehindATableLockWithoutDeadlockLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	seed := seedGoEvidenceChain(c, "tl", 1)
	c.drain()
	settleSupplyChainImpactSupports(c.t, c.ctx, c.db)
	c.rerun(seed.holder)

	var wg sync.WaitGroup
	hookReturned, committedAt, passErr := pausedPassRound(c, func(ctx context.Context) {
		tx, err := c.db.BeginTx(c.ctx, nil)
		if err != nil {
			c.t.Fatalf("begin: %v", err)
		}
		if _, err := tx.ExecContext(c.ctx, `LOCK TABLE fact_work_items IN EXCLUSIVE MODE`); err != nil {
			_ = tx.Rollback()
			c.t.Fatalf("lock the queue table: %v", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Second)
			if err := tx.Commit(); err != nil {
				c.t.Errorf("release the table lock: %v", err)
			}
		}()
	})
	wg.Wait()
	if passErr != nil {
		t.Fatalf("the pass failed with %v under a retirement-style table lock, want it to wait and commit", passErr)
	}
	if waited := committedAt.Sub(hookReturned); waited < 900*time.Millisecond {
		t.Fatalf("the marker did not wait for the table lock (%v)", waited)
	}
}

// TestGoEvidenceMarkerRowHeldThreeSecondsFenceTimesOutLive is probe 2.4: the marker's
// row is held for 3 s while the pass sits on its shared lock, so a fence on that go scope
// waits past its 2 s lock timeout and returns the typed timeout with the pointer still set
// and the generation still active. After the pass commits, its next attempt fences and
// reopens the pass's scope.
func TestGoEvidenceMarkerRowHeldThreeSecondsFenceTimesOutLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	seed := seedGoEvidenceChain(c, "hd", 1)
	c.drain()
	settleSupplyChainImpactSupports(c.t, c.ctx, c.db)
	c.rerun(seed.holder)

	var wg sync.WaitGroup
	var fenceErr error
	var timeouts int
	var mu sync.Mutex
	observe := func(_ context.Context, _ time.Duration, timedOut bool) {
		mu.Lock()
		defer mu.Unlock()
		if timedOut {
			timeouts++
		}
	}
	_, _, passErr := pausedPassRound(c, func(ctx context.Context) {
		tx, err := c.db.BeginTx(c.ctx, nil)
		if err != nil {
			c.t.Fatalf("begin: %v", err)
		}
		if _, err := tx.ExecContext(c.ctx, `UPDATE fact_work_items SET updated_at = updated_at WHERE work_item_id = $1`,
			fanOutItemID(seed.holder, fanOutGen(seed.holder, "g1"))); err != nil {
			_ = tx.Rollback()
			c.t.Fatalf("hold the marker's row: %v", err)
		}
		wg.Add(2)
		go func() {
			defer wg.Done()
			time.Sleep(3 * time.Second)
			if err := tx.Commit(); err != nil {
				c.t.Errorf("release the marker's row: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			_, fenceErr = goEvidenceFence(c.ctx, c.db, seed.goScopes[0], seed.generations[0], observe)
		}()
	})
	wg.Wait()
	if passErr != nil {
		t.Fatalf("the pass failed with %v after its marker row was held, want it committed", passErr)
	}
	if !coordination.IsLockNotAvailable(fenceErr) {
		t.Fatalf("fence error = %v, want the typed lock timeout (55P03) behind the pass stalled on its marker", fenceErr)
	}
	mu.Lock()
	gotTimeouts := timeouts
	mu.Unlock()
	var pointer sql.NullString
	var status string
	if err := c.db.QueryRowContext(c.ctx, `
SELECT scope.active_generation_id, generation.status FROM ingestion_scopes scope
JOIN scope_generations generation ON generation.scope_id = scope.scope_id AND generation.generation_id = $2
WHERE scope.scope_id = $1`, seed.goScopes[0], seed.generations[0]).Scan(&pointer, &status); err != nil {
		t.Fatalf("read the go scope: %v", err)
	}
	if gotTimeouts != 1 || pointer.String != seed.generations[0] || status != "active" {
		t.Fatalf("timeouts = %d pointer = %q status = %q, want one counted timeout with the pointer set and the generation active", gotTimeouts, pointer.String, status)
	}
	fenced, err := goEvidenceFence(c.ctx, c.db, seed.goScopes[0], seed.generations[0], nil)
	if err != nil || fenced.Fenced != 1 || fenced.HoldersReopened < 1 {
		t.Fatalf("next attempt = %+v, err = %v, want it to fence and reopen the pass's scope", fenced, err)
	}
}
