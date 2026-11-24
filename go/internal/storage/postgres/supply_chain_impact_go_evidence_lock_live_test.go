// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11327 real PostgreSQL proof that a supply-chain impact pass, which takes the shared go-evidence lock as the last statements before its commit, and the Go-modules fence cannot both commit around the same go scope: a fence between the Go evidence load and the pass write aborts the pass with the non-counting class, a pass that spends 3 s in its writes loses to a fence that takes its lock at once, a fence behind a pass paused between recheck and commit waits then reopens it, and a second pass queues behind a waiting fence
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/gomodules"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// goEvidenceLockDB opens an isolated bootstrapped database for the #11327 proofs
// through the readiness runner's own env contract (ESHU_POSTGRES_DSN with
// ESHU_POSTGRES_DISPOSABLE), so the proofs run in the live-postgres-readiness shards and
// add nothing to the reducer contention gate's supply-chain half.
func goEvidenceLockDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN is not set")
	}
	ctx, db := postgresproof.OpenDisposableBootstrapDatabase(t, dsn, os.Getenv("ESHU_POSTGRES_DISPOSABLE"), 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database})
		})
	db.SetMaxOpenConns(24)
	return ctx, db
}

// newGoEvidenceConvergence returns the fan-out convergence harness over the readiness
// database, with the supports table settled so the fence's readiness check passes.
func newGoEvidenceConvergence(t *testing.T) *convergence {
	t.Helper()
	ctx, db := goEvidenceLockDB(t)
	c := newConvergenceOn(t, ctx, db, false)
	settleSupplyChainImpactSupports(t, ctx, db)
	return c
}

// goEvidenceFenceStore is the production fence over the database.
func goEvidenceFenceStore(db *sql.DB, observe gomodules.GoEvidenceLockObserver) gomodules.Store {
	store := gomodules.NewStore(postgres.SQLDB{DB: db}).
		WithHolderReopener(postgres.ReopenSupplyChainImpactHolderInTransaction).
		WithSupportsReadiness(gomodules.SupportsReadiness(postgres.NewSupplyChainImpactSupportsReadyBinding(nil, "go_modules_fence")))
	if observe != nil {
		store = store.WithGoEvidenceLockObserver(observe)
	}
	return store
}

func goEvidenceFence(ctx context.Context, db *sql.DB, scope, generation string, observe gomodules.GoEvidenceLockObserver) (scannercomponent.StaleEvidenceFenceResult, error) {
	return goEvidenceFenceStore(db, observe).FenceStaleGoEvidence(ctx,
		[]scannercomponent.StaleEvidenceFence{{ScopeID: scope, GenerationID: generation}}, time.Now().UTC())
}

// hookingWriter runs before and after the real writer, so a proof can place a fence
// exactly between a pass's Go evidence load (already done when the writer is called)
// and its write transaction.
type hookingWriter struct {
	inner  supplychaincore.SupplyChainImpactWriter
	before func(supplychaincore.SupplyChainImpactWrite)
	after  func(supplychaincore.SupplyChainImpactWrite, error)
}

func (w hookingWriter) WriteSupplyChainImpactFindings(ctx context.Context, write supplychaincore.SupplyChainImpactWrite) (supplychaincore.SupplyChainImpactWriteResult, error) {
	if w.before != nil {
		w.before(write)
	}
	result, err := w.inner.WriteSupplyChainImpactFindings(ctx, write)
	if w.after != nil {
		w.after(write, err)
	}
	return result, err
}

// recheckHookBeginner wraps the production beginner with two test seams, each run once
// per transaction. beforeLock runs right before the pass takes the shared go-evidence
// lock: every write of the pass is done and the lock is not yet requested. hook runs
// right after the recheck returned: the pass holds the shared lock and has only its owed
// marker and the commit left. It forwards the whole optional surface (winners capture and
// the supports maintainer), so the pass commits exactly what the plain transaction would.
type recheckHookBeginner struct {
	inner      supplychaincore.SupplyChainImpactBeginner
	beforeLock func(ctx context.Context)
	hook       func(ctx context.Context)
	// onEnd, when set, receives every statement the transaction ran, in order, and
	// whether it committed: the recorded footprint of one pass.
	onEnd func(statements []string, committed bool)
}

func (b recheckHookBeginner) BeginSupplyChainImpactTx(ctx context.Context) (supplychaincore.SupplyChainImpactTx, error) {
	tx, err := b.inner.BeginSupplyChainImpactTx(ctx)
	if err != nil {
		return nil, err
	}
	return &recheckHookTx{tx: tx, beforeLock: b.beforeLock, hook: b.hook, onEnd: b.onEnd}, nil
}

type recheckHookTx struct {
	tx            supplychaincore.SupplyChainImpactTx
	beforeLock    func(ctx context.Context)
	hook          func(ctx context.Context)
	onEnd         func(statements []string, committed bool)
	statements    []string
	fired         bool
	lockedAlready bool
}

func (t *recheckHookTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	t.statements = append(t.statements, query)
	if query == supplychaincore.SupplyChainImpactGoEvidenceSharedLockQuery && !t.lockedAlready {
		t.lockedAlready = true
		if t.beforeLock != nil {
			t.beforeLock(ctx)
		}
	}
	return t.tx.ExecContext(ctx, query, args...)
}

func (t *recheckHookTx) QueryContext(ctx context.Context, query string, args ...any) (supplychaincore.SupplyChainImpactRows, error) {
	t.statements = append(t.statements, query)
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err == nil && !t.fired && strings.Contains(query, "AS pair(scope_id, generation_id)") {
		t.fired = true
		t.hook(ctx)
	}
	return rows, err
}

func (t *recheckHookTx) Commit() error {
	err := t.tx.Commit()
	if t.onEnd != nil {
		t.onEnd(t.statements, err == nil)
	}
	return err
}

func (t *recheckHookTx) Rollback() error {
	err := t.tx.Rollback()
	if t.onEnd != nil {
		t.onEnd(t.statements, false)
	}
	return err
}

func (t *recheckHookTx) CaptureWinnersDirtyFacts(ctx context.Context, identifiers []supplychaincore.WinnersDirtyIdentifier) (int64, error) {
	if capturer, ok := t.tx.(supplychaincore.SupplyChainImpactWinnersCapturer); ok {
		return capturer.CaptureWinnersDirtyFacts(ctx, identifiers)
	}
	return 0, nil
}

func (t *recheckHookTx) RecordWinnersCapture(ctx context.Context, source string, rows int64) {
	if capturer, ok := t.tx.(supplychaincore.SupplyChainImpactWinnersCapturer); ok {
		capturer.RecordWinnersCapture(ctx, source, rows)
	}
}

func (t *recheckHookTx) UpsertImpactSupports(ctx context.Context, factIDs []string) (supplychaincore.SupplyChainImpactSupportsWrite, error) {
	if maintainer, ok := t.tx.(supplychaincore.SupplyChainImpactSupportsMaintainer); ok {
		return maintainer.UpsertImpactSupports(ctx, factIDs)
	}
	return supplychaincore.SupplyChainImpactSupportsWrite{}, nil
}

func (t *recheckHookTx) DeleteRetractedImpactSupports(ctx context.Context, scopeID, generationID string) (int64, error) {
	if maintainer, ok := t.tx.(supplychaincore.SupplyChainImpactSupportsMaintainer); ok {
		return maintainer.DeleteRetractedImpactSupports(ctx, scopeID, generationID)
	}
	return 0, nil
}

// waitForAdvisoryWaiter polls pg_locks until at least want sessions wait for an advisory
// lock that is not granted, so a proof knows the fence (or a second pass) is queued
// behind the holder before it lets the holder continue.
func waitForAdvisoryWaiter(ctx context.Context, t *testing.T, db *sql.DB, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted`).Scan(&waiting); err != nil {
			t.Errorf("count advisory waiters: %v", err)
			return
		}
		if waiting >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("no advisory-lock waiter reached %d within 10 s", want)
}

// rowsWrittenFor counts the rows one aborted pass could have left for its scope: finding
// facts and supports.
func rowsWrittenFor(ctx context.Context, t *testing.T, db *sql.DB, scope string) int {
	t.Helper()
	var total int
	for _, query := range []string{
		`SELECT count(*) FROM fact_records WHERE scope_id = $1 AND fact_kind = 'reducer_supply_chain_impact_finding'`,
		`SELECT count(*) FROM supply_chain_impact_finding_supports WHERE scope_id = $1`,
	} {
		var n int
		if err := db.QueryRowContext(ctx, query, scope).Scan(&n); err != nil {
			t.Errorf("count rows (%s): %v", query, err)
			return -1
		}
		total += n
	}
	return total
}

func readItemStatus(ctx context.Context, t *testing.T, db *sql.DB, item string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, item).Scan(&status); err != nil {
		t.Fatalf("read item %s: %v", item, err)
	}
	return status
}

// TestGoEvidenceFenceBetweenLoadAndWriteAbortsThePassLive is the AC1 regression of
// #11327 and ruling 4.1 and 4.2. The holder's first pass loads the Go evidence of the
// go-reachability generation, the fence retracts that generation and commits (the
// fence's holder read finds no holder: the pass has written nothing yet), and then the
// pass writes. On main the pass commits a not_called verdict derived from the retracted
// generation and nothing schedules a pass to correct it, so the final winners still read
// not_called: this test is RED there. With the lock and recheck the pass aborts with the
// non-counting class and writes nothing, the retry reloads (no Go evidence now) and the
// winners read unknown. The pass does all its writes first and aborts at its closing
// lock and recheck, so the proof is that its rolled-back transaction left nothing.
func TestGoEvidenceFenceBetweenLoadAndWriteAbortsThePassLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	_, generationID := seedGoFenceChain(c)

	var fenced scannercomponent.StaleEvidenceFenceResult
	var fenceErr, firstErr error
	var fenceRuns, leftBehind int
	c.handler.Writer = hookingWriter{
		inner: c.handler.Writer,
		before: func(write supplychaincore.SupplyChainImpactWrite) {
			if write.ScopeID == goFenceHolder && fenceRuns == 0 {
				fenceRuns++
				fenced, fenceErr = goEvidenceFence(c.ctx, c.db, goFenceScope, generationID, nil)
			}
		},
		after: func(write supplychaincore.SupplyChainImpactWrite, err error) {
			if write.ScopeID == goFenceHolder && firstErr == nil && err != nil {
				firstErr = err
				leftBehind = rowsWrittenFor(c.ctx, c.t, c.db, goFenceHolder)
			}
		},
	}
	c.drain()

	if fenceErr != nil || fenced.Fenced != 1 {
		t.Fatalf("fence result = %+v, err = %v, want 1 fenced", fenced, fenceErr)
	}
	var changed *supplychaincore.SupplyChainImpactGoEvidenceChangedError
	if !errors.As(firstErr, &changed) || changed.Cause != supplychaincore.GoEvidenceCauseGenerationChanged {
		t.Fatalf("first write error = %v, want the go-evidence changed refusal (generation_changed): on main this is nil and the pass wrote a stale verdict", firstErr)
	}
	if leftBehind != 0 {
		t.Fatalf("the aborted pass left %d rows (finding fact or support), want none", leftBehind)
	}
	var attempts []string
	for _, handled := range c.handled {
		if strings.HasPrefix(handled, goFenceHolder+"#") {
			attempts = append(attempts, handled)
		}
	}
	if len(attempts) != 2 || attempts[0] != attempts[1] {
		t.Fatalf("holder claims = %v, want two claims at the same attempt number (the abort spent no attempt)", attempts)
	}
	assertGoFenceWinnerVerdict(c, goFenceAdvisory, "package_manifest", "unknown")
}

// TestGoEvidenceFenceWaitsForAPassHoldingTheSharedLockLive is ruling 4.2 for the other
// order (S1): the pass holds the shared lock after its recheck, the fence starts and
// queues behind it, the pass commits its first finding, and the fence's holder read
// then sees that finding's support, so the fence reopens the holder instead of
// missing it.
func TestGoEvidenceFenceWaitsForAPassHoldingTheSharedLockLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	holderItem, generationID := seedGoFenceChain(c)

	var wg sync.WaitGroup
	var fenced scannercomponent.StaleEvidenceFenceResult
	var fenceErr error
	c.handler.Writer = supplychaincore.PostgresSupplyChainImpactWriter{
		DB: recheckHookBeginner{
			inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}},
			hook: func(ctx context.Context) {
				wg.Add(1)
				go func() {
					defer wg.Done()
					fenced, fenceErr = goEvidenceFence(c.ctx, c.db, goFenceScope, generationID, nil)
				}()
				waitForAdvisoryWaiter(ctx, c.t, c.db, 1)
			},
		},
	}
	c.drain()
	wg.Wait()

	if fenceErr != nil || fenced.Fenced != 1 || fenced.HoldersReopened != 1 {
		t.Fatalf("fence result = %+v, err = %v, want 1 fenced and the committed pass's scope reopened as a holder", fenced, fenceErr)
	}
	if status := readItemStatus(c.ctx, t, c.db, holderItem); status != "pending" && status != "succeeded" {
		t.Fatalf("holder item = %q after the fence reopened it", status)
	}
	c.drain()
	assertGoFenceWinnerVerdict(c, goFenceAdvisory, "package_manifest", "unknown")
}

// TestGoEvidencePassSpendingThreeSecondsInItsWritesLosesToAFenceLive is ruling 4.5 as
// amended (lock last): the pass spends 3 s in its writes, a fence runs meanwhile, takes
// its lock at once (the pass holds no lock yet) and commits. The pass then takes its lock,
// fails the recheck, writes nothing and returns the typed non-counting class. The fence's
// wait is milliseconds, not the pass's 3 s, and it does not time out.
func TestGoEvidencePassSpendingThreeSecondsInItsWritesLosesToAFenceLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	_, generationID := seedGoFenceChain(c)

	var mu sync.Mutex
	var waits []time.Duration
	var timeouts int
	observe := func(_ context.Context, wait time.Duration, timedOut bool) {
		mu.Lock()
		defer mu.Unlock()
		waits = append(waits, wait)
		if timedOut {
			timeouts++
		}
	}
	var fenced scannercomponent.StaleEvidenceFenceResult
	var fenceErr, firstErr error
	var leftBehind int
	spent := false
	c.handler.Writer = hookingWriter{
		inner: supplychaincore.PostgresSupplyChainImpactWriter{DB: recheckHookBeginner{
			inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}},
			beforeLock: func(context.Context) {
				if spent {
					return
				}
				spent = true
				time.Sleep(3 * time.Second) // the pass's writes took 3 s
				fenced, fenceErr = goEvidenceFence(c.ctx, c.db, goFenceScope, generationID, observe)
			},
		}},
		after: func(write supplychaincore.SupplyChainImpactWrite, err error) {
			if write.ScopeID == goFenceHolder && firstErr == nil && err != nil {
				firstErr = err
				leftBehind = rowsWrittenFor(c.ctx, c.t, c.db, goFenceHolder)
			}
		},
	}
	c.drain()

	if fenceErr != nil || fenced.Fenced != 1 {
		t.Fatalf("fence result = %+v, err = %v, want 1 fenced", fenced, fenceErr)
	}
	mu.Lock()
	gotTimeouts, gotWaits := timeouts, append([]time.Duration(nil), waits...)
	mu.Unlock()
	if gotTimeouts != 0 || len(gotWaits) != 1 || gotWaits[0] > time.Second {
		t.Fatalf("fence lock timeouts = %d waits = %v, want one granted wait far below the pass's 3 s", gotTimeouts, gotWaits)
	}
	var changed *supplychaincore.SupplyChainImpactGoEvidenceChangedError
	if !errors.As(firstErr, &changed) || changed.Cause != supplychaincore.GoEvidenceCauseGenerationChanged {
		t.Fatalf("first write error = %v, want the typed go-evidence refusal (generation_changed)", firstErr)
	}
	if leftBehind != 0 {
		t.Fatalf("the aborted pass left %d rows after 3 s of writes, want none", leftBehind)
	}
	assertGoFenceWinnerVerdict(c, goFenceAdvisory, "package_manifest", "unknown")
}

// TestGoEvidenceSecondPassQueuesBehindAWaitingFenceLive is the fairness case: a pass is
// paused between its recheck and its commit, a fence queues for the exclusive lock, and a
// second pass that loaded the same Go evidence arrives. The second pass queues behind the
// waiting fence (it does not jump the queue), so after the first pass commits and the fence
// commits it takes its lock, fails the recheck and gets the typed class, not a stale write.
func TestGoEvidenceSecondPassQueuesBehindAWaitingFenceLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	_, generationID := seedGoFenceChain(c)
	second := "scope:11327:second-pass"
	fanOutSeedScope(c.t, c.ctx, c.db, second, "g1")
	secondGeneration := fanOutGen(second, "g1")

	var wg sync.WaitGroup
	var fenceErr, secondErr error
	var fenced scannercomponent.StaleEvidenceFenceResult
	c.handler.Writer = supplychaincore.PostgresSupplyChainImpactWriter{
		DB: recheckHookBeginner{
			inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}},
			hook: func(ctx context.Context) {
				wg.Add(2)
				go func() {
					defer wg.Done()
					fenced, fenceErr = goEvidenceFence(c.ctx, c.db, goFenceScope, generationID, nil)
				}()
				waitForAdvisoryWaiter(ctx, c.t, c.db, 1)
				go func() {
					defer wg.Done()
					write := fenceWrite("intent:11327:second", second, secondGeneration, fallbackFinding("CVE-2026-9101", "pkg:npm/a@1"))
					write.GoEvidence = []supplychaincore.GoEvidenceGeneration{{ScopeID: goFenceScope, GenerationID: generationID}}
					_, secondErr = supplychaincore.PostgresSupplyChainImpactWriter{
						DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}},
						Now: func() time.Time { return time.Now().UTC() },
					}.WriteSupplyChainImpactFindings(c.ctx, write)
				}()
				waitForAdvisoryWaiter(ctx, c.t, c.db, 2)
			},
		},
	}
	c.drain()
	wg.Wait()

	if fenceErr != nil || fenced.Fenced != 1 {
		t.Fatalf("fence result = %+v, err = %v, want it to wait for the paused pass and then fence", fenced, fenceErr)
	}
	var changed *supplychaincore.SupplyChainImpactGoEvidenceChangedError
	if !errors.As(secondErr, &changed) {
		t.Fatalf("second pass error = %v, want the typed refusal: it queued behind the fence and saw its commit", secondErr)
	}
	if rows := rowsWrittenFor(c.ctx, t, c.db, second); rows != 0 {
		t.Fatalf("the refused second pass left %d rows", rows)
	}
}
