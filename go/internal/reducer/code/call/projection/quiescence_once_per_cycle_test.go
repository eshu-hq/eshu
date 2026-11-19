// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// countingQuiescenceDrain is a ReducerGraphDrain that counts gate
// consultations, so a test can prove how many database probes one runner
// cycle performs. All methods are mutex-guarded because the concurrent
// runner fans partitions out over workers.
type countingQuiescenceDrain struct {
	mu              sync.Mutex
	active          bool
	activeErr       error
	uncommitted     bool
	uncommittedErr  error
	activeCalls     int
	quiescenceCalls int
}

func (d *countingQuiescenceDrain) HasActiveReducerGraphWork(context.Context) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.activeCalls++
	return d.active, d.activeErr
}

func (d *countingQuiescenceDrain) HasUncommittedCanonicalCodeScopes(context.Context) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.quiescenceCalls++
	return d.uncommitted, d.uncommittedErr
}

func (d *countingQuiescenceDrain) setUncommitted(uncommitted bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.uncommitted = uncommitted
}

func (d *countingQuiescenceDrain) calls() (active, quiescence int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.activeCalls, d.quiescenceCalls
}

func oncePerCycleRunner(
	store *fakeCodeCallIntentStore,
	drain *countingQuiescenceDrain,
	workers int,
) Runner {
	return Runner{
		IntentReader:      store,
		LeaseManager:      store,
		EdgeWriter:        &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:       acceptedGenerationFixed("gen-1", true),
		ReadinessLookup:   readinessLookupFixed(false, false),
		ReducerGraphDrain: drain,
		Config:            RunnerConfig{BatchLimit: 10, PartitionCount: 8, Workers: workers},
	}
}

// TestCodeCallProjectionRunnerProbesQuiescenceOncePerCycleSequential is the
// #10871 regression: one code-call cycle must consult each gate dependency
// exactly once no matter how many partitions the cycle covers, instead of
// once per partition.
func TestCodeCallProjectionRunnerProbesQuiescenceOncePerCycleSequential(t *testing.T) {
	t.Parallel()

	store := &fakeCodeCallIntentStore{leaseGranted: true}
	drain := &countingQuiescenceDrain{}
	runner := oncePerCycleRunner(store, drain, 1)

	if _, err := runner.runOneCycle(context.Background()); err != nil {
		t.Fatalf("runOneCycle() error = %v, want nil", err)
	}
	if active, quiescence := drain.calls(); active != 1 || quiescence != 1 {
		t.Fatalf("gate calls = (active=%d, quiescence=%d), want exactly (1, 1) per cycle", active, quiescence)
	}
	if got := store.claimsCount(); got != 8 {
		t.Fatalf("lease claims = %d, want 8 (every partition still runs after one open gate)", got)
	}
}

// TestCodeCallProjectionRunnerProbesQuiescenceOncePerCycleConcurrent pins
// the same once-per-cycle contract when partitions fan out over workers:
// the gate answer is evaluated before the fan-out, not once per worker.
func TestCodeCallProjectionRunnerProbesQuiescenceOncePerCycleConcurrent(t *testing.T) {
	t.Parallel()

	store := &fakeCodeCallIntentStore{leaseGranted: true}
	drain := &countingQuiescenceDrain{}
	runner := oncePerCycleRunner(store, drain, 8)

	if _, err := runner.runOneCycle(context.Background()); err != nil {
		t.Fatalf("runOneCycle() error = %v, want nil", err)
	}
	if active, quiescence := drain.calls(); active != 1 || quiescence != 1 {
		t.Fatalf("gate calls = (active=%d, quiescence=%d), want exactly (1, 1) per cycle", active, quiescence)
	}
	if got := store.claimsCount(); got != 8 {
		t.Fatalf("lease claims = %d, want 8 (every partition still runs after one open gate)", got)
	}
}

// TestCodeCallProjectionRunnerBlockedCycleProbesOnceAndClaimsNothing pins
// that a blocked cycle still waits: one probe, no lease claim on any
// partition, and a single per-cycle BlockedReadiness (not one per
// partition).
func TestCodeCallProjectionRunnerBlockedCycleProbesOnceAndClaimsNothing(t *testing.T) {
	t.Parallel()

	store := &fakeCodeCallIntentStore{leaseGranted: true}
	drain := &countingQuiescenceDrain{uncommitted: true}
	runner := oncePerCycleRunner(store, drain, 1)

	result, err := runner.runOneCycle(context.Background())
	if err != nil {
		t.Fatalf("runOneCycle() error = %v, want nil", err)
	}
	if _, quiescence := drain.calls(); quiescence != 1 {
		t.Fatalf("quiescence calls = %d, want exactly 1 per blocked cycle", quiescence)
	}
	if got := store.claimsCount(); got != 0 {
		t.Fatalf("lease claims = %d, want 0 while canonical code scopes are uncommitted", got)
	}
	if result.BlockedReadiness != 1 {
		t.Fatalf("BlockedReadiness = %d, want 1 per blocked cycle", result.BlockedReadiness)
	}
}

// TestCodeCallProjectionRunnerQuiescenceErrorFailsCycleBeforeAnyClaim pins
// the fail-closed contract under the hoist: a probe error fails the whole
// cycle before any partition can claim a lease.
func TestCodeCallProjectionRunnerQuiescenceErrorFailsCycleBeforeAnyClaim(t *testing.T) {
	t.Parallel()

	store := &fakeCodeCallIntentStore{leaseGranted: true}
	drain := &countingQuiescenceDrain{uncommittedErr: errors.New("test quiescence check failure")}
	runner := oncePerCycleRunner(store, drain, 8)

	if _, err := runner.runOneCycle(context.Background()); err == nil {
		t.Fatal("runOneCycle() error = nil, want quiescence check failure")
	}
	if got := store.claimsCount(); got != 0 {
		t.Fatalf("lease claims = %d, want 0 when the quiescence check fails", got)
	}
	if _, quiescence := drain.calls(); quiescence != 1 {
		t.Fatalf("quiescence calls = %d, want exactly 1 on the failed cycle", quiescence)
	}
}

// TestCodeCallProjectionRunnerHonorsFlippedAnswerNextCycle pins freshness:
// the shared answer lives only in its own cycle's call frame, so a checker
// that flips between cycles is honored by the next cycle.
func TestCodeCallProjectionRunnerHonorsFlippedAnswerNextCycle(t *testing.T) {
	t.Parallel()

	store := &fakeCodeCallIntentStore{leaseGranted: true}
	drain := &countingQuiescenceDrain{uncommitted: true}
	runner := oncePerCycleRunner(store, drain, 1)

	first, err := runner.runOneCycle(context.Background())
	if err != nil {
		t.Fatalf("first runOneCycle() error = %v, want nil", err)
	}
	if first.BlockedReadiness != 1 {
		t.Fatalf("first BlockedReadiness = %d, want 1", first.BlockedReadiness)
	}

	drain.setUncommitted(false)
	second, err := runner.runOneCycle(context.Background())
	if err != nil {
		t.Fatalf("second runOneCycle() error = %v, want nil", err)
	}
	if second.BlockedReadiness != 0 {
		t.Fatalf("second BlockedReadiness = %d, want 0 after the checker flipped open", second.BlockedReadiness)
	}
	if _, quiescence := drain.calls(); quiescence != 2 {
		t.Fatalf("quiescence calls = %d, want 2 (re-probed every cycle, never reused)", quiescence)
	}
	if got := store.claimsCount(); got != 8 {
		t.Fatalf("lease claims = %d, want 8 (open second cycle runs every partition)", got)
	}
}

// TestCodeCallProjectionRunnerMidCycleFlipActsOnCycleAnswer pins the
// staleness bound: a checker that flips after the cycle's gate evaluation
// does not change what the cycle's later partitions do. The answer is
// shared by value within the cycle only.
//
// Safety note for the dangerous direction (open at probe, uncommitted
// mid-cycle): first-commit absence needs no clear (nodes do not exist until
// canonical Write commits and the phase publishes after Write returns), and
// on Neo4j re-projection retract/files/entities run as one ExecuteWrite
// transaction — but the NornicDB PhaseGroupExecutor wiring and the
// sequential fallback commit retract before entities, an observable clear.
// The probe was also never atomic with the MATCH write: today's
// per-partition probe already spans lease, selection, load, and write
// before the edge write lands, and the per-intent readiness gate still
// covers the caller's own acceptance unit. The hoist extends that
// pre-existing window from one partition pipeline to one cycle; it adds no
// new hazard class. The residual race is tracked by #10938.
func TestCodeCallProjectionRunnerMidCycleFlipActsOnCycleAnswer(t *testing.T) {
	t.Parallel()

	store := &fakeCodeCallIntentStore{leaseGranted: true}
	drain := &countingQuiescenceDrain{}
	runner := oncePerCycleRunner(store, drain, 1)
	store.afterClaim = func(claims int) {
		if claims == 1 {
			drain.setUncommitted(true)
		}
	}

	result, err := runner.runOneCycle(context.Background())
	if err != nil {
		t.Fatalf("runOneCycle() error = %v, want nil", err)
	}
	if result.BlockedReadiness != 0 {
		t.Fatalf("BlockedReadiness = %d, want 0 (cycle acts on its start-of-cycle open answer)", result.BlockedReadiness)
	}
	if got := store.claimsCount(); got != 8 {
		t.Fatalf("lease claims = %d, want 8 (later partitions act on the cycle answer, not the flipped checker)", got)
	}
	if _, quiescence := drain.calls(); quiescence != 1 {
		t.Fatalf("quiescence calls = %d, want exactly 1 (no re-probe mid-cycle)", quiescence)
	}
	if _, err := runner.runOneCycle(context.Background()); err != nil {
		t.Fatalf("second runOneCycle() error = %v, want nil", err)
	}
	if _, quiescence := drain.calls(); quiescence != 2 {
		t.Fatalf("quiescence calls = %d, want 2 (next cycle re-probes and sees the flip)", quiescence)
	}
}
