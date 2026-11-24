// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
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
	// keyedStandsDown makes every keyed tick report the under-lock
	// stand-down (#11117): a newer full committed after the decision read.
	keyedStandsDown bool
	// readErr makes every contract read fail, as a database error would.
	readErr error
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

func (f *fakeContractWinners) ResweepWinnersUnlessNewer(context.Context) (SupplyChainImpactWinnersResweep, error) {
	f.mu.Lock()
	newer := f.contract.HasVersion && f.contract.StoredVersion > f.contract.BinaryVersion
	f.mu.Unlock()
	if newer {
		return SupplyChainImpactWinnersResweep{Skipped: true, StoodDown: true}, nil
	}
	return f.full(false), nil
}

func (f *fakeContractWinners) ResweepWinnersKeyed(context.Context) (SupplyChainImpactWinnersResweep, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keyedCalls++
	if f.keyedStandsDown {
		return SupplyChainImpactWinnersResweep{Skipped: true, StoodDown: true}, nil
	}
	return SupplyChainImpactWinnersResweep{Path: telemetry.SupplyChainImpactWinnersPathKeyed, Watermark: time.Now().UTC()}, nil
}

func (f *fakeContractWinners) WinnersContract(context.Context) (SupplyChainImpactWinnersContract, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.readErr != nil {
		return SupplyChainImpactWinnersContract{}, f.readErr
	}
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

// TestWinnersMaintainerRePinnedHashAtEqualVersionRunsKeyed pins what a re-pin
// under the same version does on the roll (#11145): the stored hash is the
// one the last full recorded, the binary's differs, the versions are equal.
// The decision reads versions only, so the tick is keyed, no full runs and
// nothing stands down; the stored hash stays stale until a later full.
func TestWinnersMaintainerRePinnedHashAtEqualVersionRunsKeyed(t *testing.T) {
	t.Parallel()

	contract := matchingContract()
	contract.StoredHash = "h3-before-the-re-pin"
	if action, reason := decideWinnersContract(contract); action != winnersContractRunKeyed || reason != "" {
		t.Fatalf("decide(equal version, different hash) = %v %q, want a keyed tick", action, reason)
	}
	f := &fakeContractWinners{contract: contract}
	runContractMaintainer(t, contractMaintainer(f), func() bool {
		_, _, _, keyed := f.counts()
		return keyed >= 3
	})
	if _, fullCalls, _, _ := f.counts(); fullCalls != 0 {
		t.Fatalf("full resweep calls = %d after a same-version re-pin, want 0", fullCalls)
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

// TestWinnersMaintainerContractReadFailureFailsClosed proves a contract read
// that fails runs nothing: no full, no keyed tick and no audit, because the
// maintainer cannot tell which statements produced the stored winners. It
// keeps retrying the read rather than falling back to either path.
func TestWinnersMaintainerContractReadFailureFailsClosed(t *testing.T) {
	t.Parallel()

	f := &fakeContractWinners{contract: matchingContract(), readErr: errors.New("contract read failed")}
	auditor := &fakeWinnersAuditor{}
	m := contractMaintainer(f)
	m.Auditor, m.AuditInterval = auditor, time.Millisecond
	runContractMaintainer(t, m, func() bool {
		reads, _, _, _ := f.counts()
		return reads >= 3
	})
	if _, fullCalls, _, keyed := f.counts(); fullCalls != 0 || keyed != 0 || auditor.count() != 0 {
		t.Fatalf("full calls = %d, keyed = %d, audits = %d after a failed contract read, want all 0", fullCalls, keyed, auditor.count())
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

// TestWinnersMaintainerCountsAnUnderLockStandDown pins the #11117 keyed-tick
// guard on the maintainer side: a keyed tick that stood down under the lock (a
// newer full committed after the decision read) counts as a stand-down, not as
// a resweep skipped behind another, and runs no full.
func TestWinnersMaintainerCountsAnUnderLockStandDown(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	f := &fakeContractWinners{contract: matchingContract(), keyedStandsDown: true}
	m := contractMaintainer(f)
	m.Instruments = inst
	runContractMaintainer(t, m, func() bool {
		_, _, _, keyed := f.counts()
		return keyed >= 3
	})
	if _, fullCalls, _, _ := f.counts(); fullCalls != 0 {
		t.Fatalf("full calls = %d, want 0", fullCalls)
	}
	if got := winnersCounterSum(t, reader, "eshu_dp_supply_chain_impact_winners_stand_downs_total"); got < 3 {
		t.Fatalf("stand-downs = %d after 3 stood-down keyed ticks, want at least 3", got)
	}
	if got := winnersCounterSum(t, reader, "eshu_dp_supply_chain_impact_winners_resweep_skipped_total"); got != 0 {
		t.Fatalf("resweep skips = %d, want 0: a stand-down is not a resweep in flight", got)
	}
}

// contractLine is the log message a guarded full that committed the contract
// emits; the tests below match on it as the operator signal (#11177).
const contractLine = "supply chain impact winners full resweep recorded the contract"

// guardedFullSignals runs one contract tick against f with the lease claimed
// or not, and returns what an operator sees: the full-resweep counter total,
// the count of contract log lines and the tick's result.
func guardedFullSignals(t *testing.T, f *fakeContractWinners, claim bool) (counter int64, lines int, result SupplyChainImpactWinnersResweep) {
	t.Helper()
	inst, reader := winnersSignalsHarness(t)
	var logs bytes.Buffer
	m := contractMaintainer(f)
	m.LeaseManager = &fakeWinnersLeaseManager{claim: claim}
	m.Instruments = inst
	m.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	result, _, standDown, err := m.runContractTick(context.Background())
	if err != nil || standDown {
		t.Fatalf("runContractTick() standDown = %v, error = %v; want a plain tick", standDown, err)
	}
	return winnersCounterSum(t, reader, "eshu_dp_supply_chain_impact_winners_full_resweeps_total"),
		strings.Count(logs.String(), contractLine), result
}

// TestWinnersGuardedFullWithoutTheLeaseRecordsNothing pins #11177: a tick that
// did not claim the winners lease ran no full, so it must neither count a
// full resweep nor claim in the log that the contract was recorded. The
// stored contract is absent, so the tick takes the guarded-full branch.
func TestWinnersGuardedFullWithoutTheLeaseRecordsNothing(t *testing.T) {
	t.Parallel()

	f := &fakeContractWinners{}
	counter, lines, result := guardedFullSignals(t, f, false)
	if _, fullCalls, _, _ := f.counts(); fullCalls != 0 {
		t.Fatalf("full calls = %d without the lease, want 0", fullCalls)
	}
	if result != (SupplyChainImpactWinnersResweep{}) {
		t.Fatalf("result = %+v without the lease, want the empty result", result)
	}
	if counter != 0 || lines != 0 {
		t.Fatalf("full-resweep counter = %d, contract log lines = %d without the lease, want 0 and 0", counter, lines)
	}
}

// TestWinnersGuardedFullThatCommitsRecordsOnce keeps the positive half of
// #11177: a tick that claimed the lease and committed the guarded full counts
// one full resweep and logs the contract line once.
func TestWinnersGuardedFullThatCommitsRecordsOnce(t *testing.T) {
	t.Parallel()

	f := &fakeContractWinners{}
	counter, lines, _ := guardedFullSignals(t, f, true)
	if _, _, commits, _ := f.counts(); commits != 1 {
		t.Fatalf("committed fulls = %d, want 1", commits)
	}
	if counter != 1 || lines != 1 {
		t.Fatalf("full-resweep counter = %d, contract log lines = %d after a committed full, want 1 and 1", counter, lines)
	}
}

// TestWinnersGuardedFullSkippedOrCurrentRecordsNothing pins that a guarded
// full skipped behind another instance, or found already current under the
// lock, still counts and logs nothing: the other instance's commit satisfies
// it and the next tick reads the row (#11117).
func TestWinnersGuardedFullSkippedOrCurrentRecordsNothing(t *testing.T) {
	t.Parallel()

	skipped := &fakeContractWinners{skips: 1}
	if counter, lines, result := guardedFullSignals(t, skipped, true); counter != 0 || lines != 0 || !result.Skipped {
		t.Fatalf("skipped full: counter = %d, lines = %d, skipped = %v; want 0, 0, true", counter, lines, result.Skipped)
	}
	// An absent row on the read, then current under the lock: the other
	// instance committed between the decision read and the lock.
	inst, reader := winnersSignalsHarness(t)
	var logs bytes.Buffer
	m := contractMaintainer(&fakeContractWinners{})
	m.Contract = &currentUnderLockContract{fakeContractWinners: &fakeContractWinners{}}
	m.Instruments = inst
	m.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	result, _, _, err := m.runContractTick(context.Background())
	if err != nil || !result.ContractCurrent {
		t.Fatalf("runContractTick() = %+v, %v; want the already-current result", result, err)
	}
	if got := winnersCounterSum(t, reader, "eshu_dp_supply_chain_impact_winners_full_resweeps_total"); got != 0 || strings.Contains(logs.String(), contractLine) {
		t.Fatalf("already-current tick counted %d full resweeps or logged the contract line", got)
	}
}

// currentUnderLockContract reports an absent contract on the decision read and
// a current one under the resweep lock, the race the guarded full exists for.
type currentUnderLockContract struct{ *fakeContractWinners }

func (c *currentUnderLockContract) ResweepWinnersUnlessCurrent(context.Context) (SupplyChainImpactWinnersResweep, error) {
	return SupplyChainImpactWinnersResweep{Skipped: true, ContractCurrent: true}, nil
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

// ResweepWinnersUnlessNewer is the full: this fake never stores a newer contract.
func (f *fakeFullContract) ResweepWinnersUnlessNewer(ctx context.Context) (SupplyChainImpactWinnersResweep, error) {
	return f.full.ResweepWinners(ctx)
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
