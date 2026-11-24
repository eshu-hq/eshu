// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11197 real PostgreSQL proof that the fence's holder statement reads the supports readiness predicate and the holders in one snapshot: a table that is not ready in that snapshot rolls the fence back with the pointer set, a drained scope's holder is reopened, a ready table with no holder commits, and under a free interleaving with the backfill drain every committed fence reopened the holder
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// afterReadinessDB wraps a database so a hook runs once per transaction right after the
// fence's stand-alone readiness check (check 1) closes its rows: the one place an
// old-writer commit or a drain can land between check 1 and the holder statement.
type afterReadinessDB struct {
	postgres.SQLDB
	hook func()
}

func (d afterReadinessDB) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := d.SQLDB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &afterReadinessTx{Transaction: tx, hook: d.hook}, nil
}

type afterReadinessTx struct {
	db.Transaction
	hook  func()
	fired bool
}

func (t *afterReadinessTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	rows, err := t.Transaction.QueryContext(ctx, query, args...)
	if err != nil || query != testSupportsReadiness.SQL || t.fired {
		return rows, err
	}
	t.fired = true
	return &afterCloseRows{Rows: rows, hook: t.hook}, nil
}

type afterCloseRows struct {
	db.Rows
	hook func()
}

func (r *afterCloseRows) Close() error {
	err := r.Rows.Close()
	if r.hook != nil {
		hook := r.hook
		r.hook = nil
		hook()
	}
	return err
}

func hookedStore(database *sql.DB, hook func()) Store {
	return NewStore(afterReadinessDB{SQLDB: postgres.SQLDB{DB: database}, hook: hook}).
		WithHolderReopener(postgres.ReopenSupplyChainImpactHolderInTransaction).
		WithSupportsReadiness(testSupportsReadiness)
}

func isNotReady(err error) bool {
	return errors.As(err, new(*supplychaincore.SupplyChainImpactSupportsNotReadyError))
}

// TestFenceHolderStatementReadsReadinessAndHoldersInOneSnapshotLive is the three-state
// proof of #11197. The hook commits an old-writer finding (its dirty mark, no support
// row) after check 1 has passed, so only the holder statement can see it.
func TestFenceHolderStatementReadsReadinessAndHoldersInOneSnapshotLive(t *testing.T) {
	database := openStoreProof(t)
	ctx := t.Context()
	list := func(fence scannercomponent.StaleEvidenceFence) []scannercomponent.StaleEvidenceFence {
		return []scannercomponent.StaleEvidenceFence{fence}
	}

	t.Run("mark present and support absent rolls back with the pointer set", func(t *testing.T) {
		settleSupports(t, database)
		repository, fence := seedFenceable(t, database, 1)
		store := hookedStore(database, func() { seedFindingHolderUnsettled(t, database, "holder-old-writer", repository, "") })
		result, err := store.FenceStaleGoEvidence(ctx, list(fence), time.Now().UTC())
		if !isNotReady(err) || result.Fenced != 0 || result.HoldersReopened != 0 {
			t.Fatalf("result = %+v, err = %v, want the typed not-ready refusal and nothing fenced", result, err)
		}
		if state := readScopeState(t, database, fence.ScopeID, fence.GenerationID); state.pointer.String != fence.GenerationID || state.status != "active" {
			t.Fatalf("state = %+v, want the pointer still set and the generation active", state)
		}
	})

	t.Run("mark cleared and support present reopens the holder", func(t *testing.T) {
		settleSupports(t, database)
		repository, fence := seedFenceable(t, database, 2)
		store := hookedStore(database, func() {
			seedFindingHolderUnsettled(t, database, "holder-drained", repository, "")
			settleSupports(t, database)
		})
		result, err := store.FenceStaleGoEvidence(ctx, list(fence), time.Now().UTC())
		if err != nil || result.Fenced != 1 || result.HoldersReopened != 1 {
			t.Fatalf("result = %+v, err = %v, want 1 fenced and the drained holder reopened", result, err)
		}
		var enqueued int
		if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items
WHERE scope_id = 'holder-drained' AND stage = 'reducer' AND domain = 'supply_chain_impact' AND status = 'pending'`).Scan(&enqueued); err != nil {
			t.Fatal(err)
		}
		if enqueued != 1 {
			t.Fatalf("items for the drained holder = %d, want 1 pending", enqueued)
		}
	})

	t.Run("ready with no holder commits", func(t *testing.T) {
		settleSupports(t, database)
		_, fence := seedFenceable(t, database, 3)
		result, err := fenceStore(t, database).FenceStaleGoEvidence(ctx, list(fence), time.Now().UTC())
		if err != nil || result.Fenced != 1 || result.HoldersReopened != 0 {
			t.Fatalf("result = %+v, err = %v, want 1 fenced and 0 holders", result, err)
		}
		if state := readScopeState(t, database, fence.ScopeID, fence.GenerationID); state.pointer.Valid || state.status != "superseded" {
			t.Fatalf("state = %+v, want the pointer detached and the generation superseded", state)
		}
	})
}

// TestFenceUnderAFreeInterleavingWithTheDrainReopensEveryCommittedHolderLive is the
// concurrency proof of #11197 (40 rounds). Each round commits an old-writer finding
// first, then runs the fence (retried until it commits) alongside the backfill drain
// with no hook between them. Every fence that commits must have reopened the holder,
// and the only errors the fence may return are the typed not-ready refusal.
func TestFenceUnderAFreeInterleavingWithTheDrainReopensEveryCommittedHolderLive(t *testing.T) {
	database := openStoreProof(t)
	ctx := t.Context()
	settleSupports(t, database)
	for round := 1; round <= 40; round++ {
		repository, fence := seedFenceable(t, database, round)
		holder := fmt.Sprintf("holder-round-%d", round)
		seedFindingHolderUnsettled(t, database, holder, repository, "")

		var wg sync.WaitGroup
		var drainErr, fenceErr error
		var result scannercomponent.StaleEvidenceFenceResult
		store := NewStore(postgres.SQLDB{DB: database}).
			WithHolderReopener(postgres.ReopenSupplyChainImpactHolderInTransaction).
			WithSupportsReadiness(testSupportsReadiness)
		wg.Add(2)
		go func() {
			defer wg.Done()
			backfill := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: database}, ChunkSize: 100}
			for step := 0; step < 500; step++ {
				report, err := backfill.RunStep(ctx)
				if err != nil {
					drainErr = err
					return
				}
				if report.Complete && report.DirtyScopes == 0 {
					return
				}
			}
			drainErr = errors.New("drain did not settle")
		}()
		go func() {
			defer wg.Done()
			for attempt := 0; attempt < 5000; attempt++ {
				res, err := store.FenceStaleGoEvidence(ctx, []scannercomponent.StaleEvidenceFence{fence}, time.Now().UTC())
				switch {
				case err == nil:
					result = res
					return
				case isNotReady(err):
					time.Sleep(time.Millisecond)
				default:
					fenceErr = err
					return
				}
			}
			fenceErr = errors.New("fence never committed")
		}()
		wg.Wait()
		if drainErr != nil || fenceErr != nil {
			t.Fatalf("round %d: drain error = %v, fence error = %v, want none (only typed not-ready refusals are retried)", round, drainErr, fenceErr)
		}
		if result.Fenced != 1 || result.HoldersReopened != 1 {
			t.Fatalf("round %d: result = %+v, want the committed fence to have reopened the holder", round, result)
		}
	}
}
