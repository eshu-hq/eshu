// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Contract decision proofs for #11117 A: the maintainer decides every tick
// from the persisted winners contract instead of a per-process "full done"
// flag, so a roll with a matching contract runs zero fulls.

// fakeContractWinners is one fake winners store: the full rebuilder, the
// guarded full, the keyed rebuilder and the contract read share state, the
// way the Postgres store's methods share one table. The first skips full
// calls report Skipped (another instance holds the resweep lock); with
// otherCommits set, the last of them also records the binary's contract, as
// the other instance's commit would.
type fakeContractWinners struct {
	mu           sync.Mutex
	contract     SupplyChainImpactWinnersContract
	skips        int
	otherCommits bool
	reads        int
	fullCalls    int
	fullCommits  int
	keyedCalls   int
}

func (f *fakeContractWinners) current() bool {
	return f.contract.HasVersion && f.contract.StoredVersion >= f.contract.BinaryVersion
}

func (f *fakeContractWinners) full(guarded bool) SupplyChainImpactWinnersResweep {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fullCalls++
	if f.fullCalls <= f.skips {
		if f.fullCalls == f.skips && f.otherCommits {
			f.record()
		}
		return SupplyChainImpactWinnersResweep{Skipped: true}
	}
	if guarded && f.current() {
		return SupplyChainImpactWinnersResweep{Skipped: true, ContractCurrent: true}
	}
	f.fullCommits++
	f.record()
	return SupplyChainImpactWinnersResweep{Path: telemetry.SupplyChainImpactWinnersPathFull, Watermark: time.Now().UTC()}
}

func (f *fakeContractWinners) record() {
	f.contract.Present, f.contract.HasVersion = true, true
	f.contract.StoredVersion, f.contract.StoredHash = f.contract.BinaryVersion, f.contract.BinaryHash
}

func (f *fakeContractWinners) ResweepWinners(context.Context) (SupplyChainImpactWinnersResweep, error) {
	return f.full(false), nil
}

func (f *fakeContractWinners) ResweepWinnersUnlessCurrent(context.Context) (SupplyChainImpactWinnersResweep, error) {
	return f.full(true), nil
}

func (f *fakeContractWinners) ResweepWinnersKeyed(context.Context) (SupplyChainImpactWinnersResweep, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keyedCalls++
	return SupplyChainImpactWinnersResweep{Path: telemetry.SupplyChainImpactWinnersPathKeyed, Watermark: time.Now().UTC()}, nil
}

func (f *fakeContractWinners) WinnersContract(context.Context) (SupplyChainImpactWinnersContract, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	return f.contract, nil
}

func (f *fakeContractWinners) counts() (reads, fullCalls, fullCommits, keyed int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads, f.fullCalls, f.fullCommits, f.keyedCalls
}

func contractMaintainer(f *fakeContractWinners) SupplyChainImpactWinnersMaintainer {
	return SupplyChainImpactWinnersMaintainer{
		Rebuilder: f, KeyedRebuilder: f, Contract: f,
		LeaseManager: &fakeWinnersLeaseManager{claim: true},
		Interval:     time.Millisecond,
	}
}

// runContractMaintainer runs m until until reports true or 5 s pass, then
// cancels it and waits for Run to return.
func runContractMaintainer(t *testing.T, m SupplyChainImpactWinnersMaintainer, until func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	deadline := time.After(5 * time.Second)
	for !until() {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("maintainer did not reach the expected state within 5s")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func matchingContract() SupplyChainImpactWinnersContract {
	return SupplyChainImpactWinnersContract{
		Present: true, HasVersion: true, StoredVersion: 3, StoredHash: "h3", BinaryVersion: 3, BinaryHash: "h3",
	}
}

// TestWinnersMaintainerMatchingContractRunsKeyedWithoutAFull proves a roll
// with an unchanged contract runs zero fulls: the first tick is keyed.
func TestWinnersMaintainerMatchingContractRunsKeyedWithoutAFull(t *testing.T) {
	t.Parallel()

	f := &fakeContractWinners{contract: matchingContract()}
	runContractMaintainer(t, contractMaintainer(f), func() bool {
		_, _, _, keyed := f.counts()
		return keyed >= 3
	})
	if _, fullCalls, _, _ := f.counts(); fullCalls != 0 {
		t.Fatalf("full resweep calls = %d with a matching stored contract, want 0", fullCalls)
	}
}

// TestWinnersMaintainerSkippedFullsThenMatchingContractRunsNoOwnFull proves a
// full skipped because another instance holds the lock is satisfied by that
// instance's commit: once the stored contract matches, this instance never
// runs its own full.
func TestWinnersMaintainerSkippedFullsThenMatchingContractRunsNoOwnFull(t *testing.T) {
	t.Parallel()

	contract := matchingContract()
	contract.HasVersion, contract.StoredVersion, contract.StoredHash = false, 0, ""
	f := &fakeContractWinners{contract: contract, skips: 4, otherCommits: true}
	runContractMaintainer(t, contractMaintainer(f), func() bool {
		_, _, _, keyed := f.counts()
		return keyed >= 3
	})
	if _, fullCalls, fullCommits, _ := f.counts(); fullCommits != 0 || fullCalls != 4 {
		t.Fatalf("full calls = %d, own committed fulls = %d; want the 4 skipped attempts and no own full", fullCalls, fullCommits)
	}
}

// TestWinnersMaintainerLowerContractRunsOneFull proves a stored version below
// the binary's runs exactly one full, then keyed ticks.
func TestWinnersMaintainerLowerContractRunsOneFull(t *testing.T) {
	t.Parallel()

	contract := matchingContract()
	contract.StoredVersion, contract.StoredHash = 2, "h2"
	f := &fakeContractWinners{contract: contract}
	runContractMaintainer(t, contractMaintainer(f), func() bool {
		_, _, _, keyed := f.counts()
		return keyed >= 3
	})
	if _, _, fullCommits, _ := f.counts(); fullCommits != 1 {
		t.Fatalf("committed fulls = %d with a lower stored contract, want exactly 1", fullCommits)
	}
}

// TestWinnersMaintainerHigherContractStandsDown proves an older binary that
// finds a newer contract stored writes nothing: no full, no keyed tick and no
// audit, and it reports the stand-down.
func TestWinnersMaintainerHigherContractStandsDown(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	contract := matchingContract()
	contract.StoredVersion, contract.StoredHash = 4, "h4"
	f := &fakeContractWinners{contract: contract}
	auditor := &fakeWinnersAuditor{}
	m := contractMaintainer(f)
	m.Auditor, m.AuditInterval, m.Instruments = auditor, time.Millisecond, inst
	runContractMaintainer(t, m, func() bool {
		reads, _, _, _ := f.counts()
		return reads >= 10
	})
	if _, fullCalls, _, keyed := f.counts(); fullCalls != 0 || keyed != 0 || auditor.count() != 0 {
		t.Fatalf("full calls = %d, keyed = %d, audits = %d under a newer stored contract, want all 0", fullCalls, keyed, auditor.count())
	}
	if got := winnersCounterSum(t, reader, "eshu_dp_supply_chain_impact_winners_stand_downs_total"); got < 1 {
		t.Fatalf("stand-down count = %d, want at least 1", got)
	}
}

// TestWinnersMaintainerAuditRunsOnAMatchingContractWithoutAFull pins the audit
// gate of #11117 (arbiter addendum item 5d): the audit is gated by the
// persisted contract only. A fresh process whose stored contract matches runs
// the audit with no full of its own; before #11117 the audit waited for a
// per-process "full done" flag that every roll reset, so it never ran on QA.
func TestWinnersMaintainerAuditRunsOnAMatchingContractWithoutAFull(t *testing.T) {
	t.Parallel()

	f := &fakeContractWinners{contract: matchingContract()}
	auditor := &fakeWinnersAuditor{}
	m := contractMaintainer(f)
	m.Auditor, m.AuditInterval = auditor, time.Millisecond
	runContractMaintainer(t, m, func() bool { return auditor.count() >= 2 })
	if _, fullCalls, _, _ := f.counts(); fullCalls != 0 {
		t.Fatalf("full calls = %d, want the audit to run with no full", fullCalls)
	}
}

// winnersCounterSum sums every data point of the named Int64 counter.
func winnersCounterSum(t *testing.T, reader *sdkmetric.ManualReader, name string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
				for _, dp := range sum.DataPoints {
					total += dp.Value
				}
			}
		}
	}
	return total
}

// fakeFullContract adapts a full rebuilder into the contract store the way the
// Postgres store behaves: no contract until a guarded full commits, then the
// binary's. The keyed-maintainer tests use it so their first tick is the one
// full and every later tick is keyed.
type fakeFullContract struct {
	full      SupplyChainImpactWinnersRebuilder
	mu        sync.Mutex
	committed bool
}

func (f *fakeFullContract) WinnersContract(context.Context) (SupplyChainImpactWinnersContract, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := SupplyChainImpactWinnersContract{BinaryVersion: 1, BinaryHash: "h1"}
	if f.committed {
		c.Present, c.HasVersion, c.StoredVersion, c.StoredHash = true, true, 1, "h1"
	}
	return c, nil
}

func (f *fakeFullContract) ResweepWinnersUnlessCurrent(ctx context.Context) (SupplyChainImpactWinnersResweep, error) {
	f.mu.Lock()
	if f.committed {
		f.mu.Unlock()
		return SupplyChainImpactWinnersResweep{Skipped: true, ContractCurrent: true}, nil
	}
	f.mu.Unlock()
	r, err := f.full.ResweepWinners(ctx)
	if err == nil && !r.Skipped {
		f.mu.Lock()
		f.committed = true
		f.mu.Unlock()
	}
	return r, err
}
