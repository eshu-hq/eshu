// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11169 disposable PostgreSQL 18 proof that the winners-served aggregate read fails closed (absent, stale, or unprobed watermark answers ErrAggregateReadModelUnavailable, never an approximate total) and that parallel winners reads during an in-flight maintainer resweep all succeed with identical totals; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

func openAggregateFailClosedDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	return postgresproof.OpenDisposableBootstrapDatabase(
		t,
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DSN"),
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE"),
		5*time.Minute,
		func(ctx context.Context, db *sql.DB) error {
			return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
		},
	)
}

// TestAggregatesWinnersFailClosedLive proves the #11169 fail-closed
// contract against a real maintainer watermark: a winners-eligible call
// answers ErrAggregateReadModelUnavailable when the model was never
// populated and when its watermark is older than WinnersFreshnessWindow,
// and serves with a fresh watermark carrying its timestamp.
func TestAggregatesWinnersFailClosedLive(t *testing.T) {
	ctx, database := openAggregateFailClosedDB(t)
	seedGroupedInventoryLive(t, ctx, database)

	fixedNow := time.Now().UTC()
	gated := NewPostgresAggregateStoreWithReadModel(database, true)
	gated.Now = func() time.Time { return fixedNow }

	filter := AggregateFilter{Status: "affected_exact"}
	if _, err := gated.CountSupplyChainImpactFindings(ctx, filter); !errors.Is(err, ErrAggregateReadModelUnavailable) {
		t.Fatalf("count on never-populated model = %v, want ErrAggregateReadModelUnavailable", err)
	}
	if _, err := gated.SupplyChainImpactInventory(ctx, filter, InventoryByStatus, 10, 0); !errors.Is(err, ErrAggregateReadModelUnavailable) {
		t.Fatalf("inventory on never-populated model = %v, want ErrAggregateReadModelUnavailable", err)
	}

	if err := storagepostgres.NewSupplyChainImpactWinnersStore(storagepostgres.SQLDB{DB: database}).
		RebuildAllWinners(ctx); err != nil {
		t.Fatalf("RebuildAllWinners: %v", err)
	}
	count, err := gated.CountSupplyChainImpactFindings(ctx, filter)
	if err != nil {
		t.Fatalf("count on fresh model: %v", err)
	}
	if count.Freshness.State != "fresh" || count.Freshness.LatestObservedAt == "" {
		t.Fatalf("fresh count freshness = %+v, want fresh with watermark", count.Freshness)
	}
	page, err := gated.SupplyChainImpactInventory(ctx, filter, InventoryByStatus, 10, 0)
	if err != nil {
		t.Fatalf("inventory on fresh model: %v", err)
	}
	if page.Freshness.State != "fresh" || page.Freshness.LatestObservedAt == "" {
		t.Fatalf("fresh inventory freshness = %+v, want fresh with watermark", page.Freshness)
	}

	staleAt := fixedNow.Add(-(WinnersFreshnessWindow + time.Minute)).UTC()
	if _, err := database.ExecContext(ctx,
		`UPDATE supply_chain_impact_winners_materialization SET materialized_at = $1`, staleAt); err != nil {
		t.Fatalf("backdate watermark: %v", err)
	}
	if _, err := gated.CountSupplyChainImpactFindings(ctx, filter); !errors.Is(err, ErrAggregateReadModelUnavailable) {
		t.Fatalf("count on stale model = %v, want ErrAggregateReadModelUnavailable", err)
	}
	if _, err := gated.SupplyChainImpactInventory(ctx, filter, InventoryByStatus, 10, 0); !errors.Is(err, ErrAggregateReadModelUnavailable) {
		t.Fatalf("inventory on stale model = %v, want ErrAggregateReadModelUnavailable", err)
	}
}

// TestAggregatesWinnersConcurrentResweepReadLive proves the concurrency
// leg of the #11169 contract: parallel winners-served count reads issued
// while the maintainer resweeps all succeed with identical totals. The
// read path takes no lock beyond the ACCESS SHARE of its two plain SELECT
// statements and holds no transaction, so readers never contend with the
// maintainer's writes or with each other.
func TestAggregatesWinnersConcurrentResweepReadLive(t *testing.T) {
	ctx, database := openAggregateFailClosedDB(t)
	seedGroupedInventoryLive(t, ctx, database)
	if err := storagepostgres.NewSupplyChainImpactWinnersStore(storagepostgres.SQLDB{DB: database}).
		RebuildAllWinners(ctx); err != nil {
		t.Fatalf("RebuildAllWinners: %v", err)
	}

	fixedNow := time.Now().UTC()
	filter := AggregateFilter{Status: "affected_exact"}
	newGated := func() PostgresAggregateStore {
		store := NewPostgresAggregateStoreWithReadModel(database, true)
		store.Now = func() time.Time { return fixedNow }
		return store
	}

	const readers = 8
	totals := make([]int, readers)
	errs := make([]error, readers)
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			count, err := newGated().CountSupplyChainImpactFindings(ctx, filter)
			if err != nil {
				errs[i] = err
				return
			}
			totals[i] = count.TotalFindings
		}(i)
	}
	// The resweep runs while the readers are in flight; a torn or blocked
	// read fails here, not in a unit fake.
	if err := storagepostgres.NewSupplyChainImpactWinnersStore(storagepostgres.SQLDB{DB: database}).
		RebuildAllWinners(ctx); err != nil {
		t.Fatalf("concurrent RebuildAllWinners: %v", err)
	}
	wg.Wait()
	for i := 0; i < readers; i++ {
		if errs[i] != nil {
			t.Fatalf("reader %d: %v", i, errs[i])
		}
		if totals[i] != totals[0] {
			t.Fatalf("reader %d total = %d, want %d (identical totals under resweep)", i, totals[i], totals[0])
		}
	}
}
