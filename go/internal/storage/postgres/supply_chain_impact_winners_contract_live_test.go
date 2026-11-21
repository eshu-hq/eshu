// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"sync"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Contract proofs for #11117 A on live Postgres: two maintainers sharing one
// database, the real lease store and the real resweep lock.

// countingWinners wraps the store as every maintainer port and counts the
// fulls that committed and the keyed ticks that ran.
type countingWinners struct {
	store       postgres.SupplyChainImpactWinnersStore
	mu          sync.Mutex
	fullCommits int
	keyedTicks  int
}

func (c *countingWinners) countFull(r supplychaincore.SupplyChainImpactWinnersResweep, err error) (supplychaincore.SupplyChainImpactWinnersResweep, error) {
	if err == nil && !r.Skipped {
		c.mu.Lock()
		c.fullCommits++
		c.mu.Unlock()
	}
	return r, err
}

func (c *countingWinners) ResweepWinners(ctx context.Context) (supplychaincore.SupplyChainImpactWinnersResweep, error) {
	return c.countFull(c.store.ResweepWinners(ctx))
}

func (c *countingWinners) ResweepWinnersUnlessCurrent(ctx context.Context) (supplychaincore.SupplyChainImpactWinnersResweep, error) {
	return c.countFull(c.store.ResweepWinnersUnlessCurrent(ctx))
}

func (c *countingWinners) ResweepWinnersKeyed(ctx context.Context) (supplychaincore.SupplyChainImpactWinnersResweep, error) {
	r, err := c.store.ResweepWinnersKeyed(ctx)
	if err == nil && !r.Skipped {
		c.mu.Lock()
		c.keyedTicks++
		c.mu.Unlock()
	}
	return r, err
}

func (c *countingWinners) WinnersContract(ctx context.Context) (supplychaincore.SupplyChainImpactWinnersContract, error) {
	return c.store.WinnersContract(ctx)
}

func (c *countingWinners) counts() (fulls, keyed int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fullCommits, c.keyedTicks
}

// TestSupplyChainImpactWinnersResweepContractRunsOneFullAcrossMaintainersLive
// proves a contract that is missing, NULL or lower runs exactly one full
// across two concurrent maintainers, records the binary's contract, and then
// both maintainers run keyed ticks.
func TestSupplyChainImpactWinnersResweepContractRunsOneFullAcrossMaintainersLive(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed string
	}{
		{"no watermark row", ""},
		{"null contract version", `INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at) VALUES (1, clock_timestamp() - interval '1 hour')`},
		{"lower contract version", `INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at, contract_version, contract_hash) VALUES (1, clock_timestamp() - interval '1 hour', 0, 'older')`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newKeyedHarness(t, "#11117 one full across maintainers proof")
			h.seedScope("vuln-intel://11117/contract", "vuln-intel://11117/contract/g1")
			h.write("vuln-intel://11117/contract", "vuln-intel://11117/contract/g1", chunkedFindings("contract", 4, 2, 0)...)
			if tc.seed != "" {
				h.exec(tc.seed)
			}
			winners := &countingWinners{store: h.store}
			leases := postgres.NewSharedIntentStore(postgres.SQLDB{DB: h.db})
			maintainer := func(owner string) supplychaincore.SupplyChainImpactWinnersMaintainer {
				return supplychaincore.SupplyChainImpactWinnersMaintainer{
					Rebuilder: winners, KeyedRebuilder: winners, Contract: winners,
					LeaseManager: leases, LeaseOwner: owner, Interval: 20 * time.Millisecond,
				}
			}
			ctx, cancel := context.WithCancel(h.ctx)
			var wg sync.WaitGroup
			for _, owner := range []string{"winners-11117-a", "winners-11117-b"} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := maintainer(owner).Run(ctx); err != nil {
						t.Errorf("maintainer %s: %v", owner, err)
					}
				}()
			}
			deadline := time.After(90 * time.Second)
			for {
				if _, keyed := winners.counts(); keyed >= 6 {
					break
				}
				select {
				case <-deadline:
					cancel()
					wg.Wait()
					fulls, keyed := winners.counts()
					t.Fatalf("after 90s: fulls %d, keyed ticks %d; want keyed ticks after one full", fulls, keyed)
				case <-time.After(20 * time.Millisecond):
				}
			}
			cancel()
			wg.Wait()
			if fulls, _ := winners.counts(); fulls != 1 {
				t.Fatalf("committed fulls across both maintainers = %d, want exactly 1", fulls)
			}
			got, err := h.store.WinnersContract(h.ctx)
			if err != nil {
				t.Fatalf("read contract: %v", err)
			}
			if !got.Present || !got.HasVersion || got.StoredVersion != got.BinaryVersion || got.StoredHash != got.BinaryHash {
				t.Fatalf("stored contract = %+v, want the binary's version and hash", got)
			}
			h.assertParity("after the single full and keyed ticks")
		})
	}
}
