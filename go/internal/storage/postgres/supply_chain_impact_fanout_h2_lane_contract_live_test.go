// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"errors"
	"slices"
	"sort"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// H2 lane-split contract, live store boundary (#11520): what the hot/cold
// lane split must NOT change, proven against live Postgres on the golden
// family corpus (holderFamilySeed, shared with the fact-oracle proof). Per
// the #11368 decision, live tests are the home for this surface: no
// manifest or Odù edit. All GREEN on base.

// h2LaneContractChunks is the golden family chunk set: the same 5-id mixes,
// singletons, and token-edge ids the fact-oracle proof covers, so every
// equality below runs over all three holder arms.
var h2LaneContractChunks = [][]string{
	{"CVE-HOT", "CVE-3", "GHSA-HOT", "GHSA-7", "adv-foreign-hot"},
	{"CVE-1", "CVE-2", "GHSA-1", "adv-foreign-2", "own-8"},
	{"adv-string-1", "adv-object-2", "CVE-none", "GHSA-none", "adv-foreign-0"},
	{"CVE-HOT"},
	{"GHSA-HOT"},
	{"adv-foreign-hot"},
	{"adv-string-0"},
	{"adv-object-1"},
}

// TestH2LaneContractSplitUnionEqualsShippedLive is GREEN: any partition of
// a chunk — here the finest, one singleton statement per id, each under the
// same 10s guard transaction boundary the split arms use — returns exactly
// the shipped whole-chunk statement's holder set. The lane split only
// re-partitions; the admitted set is identical.
func TestH2LaneContractSplitUnionEqualsShippedLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	holderFamilySeed(t, ctx, db)
	settleSupplyChainImpactSupports(t, ctx, db)
	store := fanOutLiveStore(db)

	for i, ids := range h2LaneContractChunks {
		whole, err := store.FindHolderScopes(ctx, "hf-own", ids)
		if err != nil {
			t.Fatalf("chunk %d FindHolderScopes(%v): %v", i, ids, err)
		}
		var singles []string
		for _, id := range ids {
			got, err := store.FindHolderScopes(ctx, "hf-own", []string{id})
			if err != nil {
				t.Fatalf("chunk %d singleton FindHolderScopes(%q): %v", i, id, err)
			}
			singles = append(singles, got...)
		}
		sort.Strings(singles)
		singles = slices.Compact(singles)
		if !sameStrings(singles, whole) {
			t.Errorf("chunk %d: singleton union %v differs from the whole-chunk read %v", i, singles, whole)
		}
	}
	if whole, err := store.FindHolderScopes(ctx, "hf-own", h2LaneContractChunks[0]); err != nil || len(whole) == 0 {
		t.Fatalf("hot chunk read = %v, %v, want holders (vacuous equality proves nothing)", whole, err)
	}
	if hot, err := store.FindHolderScopes(ctx, "hf-own", h2LaneContractChunks[3]); err != nil || len(hot) == 0 {
		t.Fatalf("CVE-HOT singleton read = %v, %v, want holders", hot, err)
	}
}

// TestH2LaneContractShippedEqualsFactOracleLive is GREEN: the shipped
// holder statement returns exactly the retired finding-fact statement's
// set on every golden chunk. H2 keeps the statement text byte-identical, so
// this identity must hold before and after the lane split.
func TestH2LaneContractShippedEqualsFactOracleLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	holderFamilySeed(t, ctx, db)
	settleSupplyChainImpactSupports(t, ctx, db)
	store := fanOutLiveStore(db)

	for i, ids := range h2LaneContractChunks {
		oracle := queryScopes(t, ctx, db, oracleHoldersQuery, ids, "hf-own")
		got, err := store.FindHolderScopes(ctx, "hf-own", ids)
		if err != nil {
			t.Fatalf("chunk %d FindHolderScopes(%v): %v", i, ids, err)
		}
		if !sameStrings(got, oracle) {
			t.Errorf("chunk %d: shipped %v differs from the fact oracle %v", i, got, oracle)
		}
	}
	if oracle := queryScopes(t, ctx, db, oracleHoldersQuery, h2LaneContractChunks[0], "hf-own"); len(oracle) == 0 {
		t.Fatal("fact oracle returned no holders for the hot chunk: the corpus failed to match")
	}
}

// TestH2LaneContractGuardOnEveryStatementLive is GREEN: every holder
// statement — the whole-chunk read, a singleton arm, and the mass probe —
// runs under the per-statement guard. With the supports table pinned by an
// exclusive lock, all three must fail with the typed timeout instead of
// waiting; once the lock releases, the same read succeeds, proving the
// failure was the guard and not the corpus.
func TestH2LaneContractGuardOnEveryStatementLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	holderFamilySeed(t, ctx, db)
	settleSupplyChainImpactSupports(t, ctx, db)
	guarded := postgres.SupplyChainImpactFanOutStore{
		Beginner: postgres.SQLDB{DB: db}, DB: postgres.SQLDB{DB: db},
		LeaseOwner: fanOutLiveOwner, ReadTimeout: 2 * time.Second,
	}
	chunk := h2LaneContractChunks[0]
	if got, err := guarded.FindHolderScopes(ctx, "hf-own", chunk); err != nil || len(got) == 0 {
		t.Fatalf("control read = %v, %v, want the hot-chunk holders", got, err)
	}

	lockTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin lock holder: %v", err)
	}
	defer func() { _ = lockTx.Rollback() }()
	if _, err := lockTx.ExecContext(ctx, `LOCK TABLE supply_chain_impact_finding_supports IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("pin supports table: %v", err)
	}
	for _, tc := range []struct {
		name string
		read func() error
	}{
		{"whole chunk", func() error {
			_, err := guarded.FindHolderScopes(ctx, "hf-own", chunk)
			return err
		}},
		{"singleton arm", func() error {
			_, err := guarded.FindHolderScopes(ctx, "hf-own", []string{"CVE-HOT"})
			return err
		}},
		{"mass probe", func() error {
			_, err := guarded.EstimateHolderMasses(ctx, chunk)
			return err
		}},
	} {
		if err := tc.read(); !errors.Is(err, supplychaincore.ErrFanOutReadTimeout) {
			t.Errorf("%s under lock: error = %v, want %v", tc.name, err, supplychaincore.ErrFanOutReadTimeout)
		}
	}
	if err := lockTx.Rollback(); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	if got, err := guarded.FindHolderScopes(ctx, "hf-own", chunk); err != nil || len(got) == 0 {
		t.Fatalf("post-lock read = %v, %v, want the hot-chunk holders", got, err)
	}
}
