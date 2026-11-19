// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeintel

import (
	"context"
	"errors"
	"testing"
	"time"
)

// probeReading is one scripted cursor-probe answer.
type probeReading struct {
	at time.Time
	ok bool
}

// probeFakeCodeReachabilityInputLoader is the #10870 idle-probe fake: it
// records full-query calls and serves a scripted cursor probe. When the
// scripts are set, call N (0-based) serves entry N and any call past the end
// serves empty (no inputs / unknown probe); otherwise the static fields
// answer every call.
type probeFakeCodeReachabilityInputLoader struct {
	inputs   []CodeReachabilityProjectionInput
	probe    time.Time
	probeOK  bool
	probeErr error
	loads    int

	probeScript []probeReading
	loadScript  [][]CodeReachabilityProjectionInput
	probeCalls  int
}

func (f *probeFakeCodeReachabilityInputLoader) LoadPendingCodeReachabilityInputs(
	_ context.Context,
	_ int,
) ([]CodeReachabilityProjectionInput, error) {
	f.loads++
	if f.loadScript != nil {
		if f.loads-1 < len(f.loadScript) {
			return append([]CodeReachabilityProjectionInput(nil), f.loadScript[f.loads-1]...), nil
		}
		return nil, nil
	}
	return append([]CodeReachabilityProjectionInput(nil), f.inputs...), nil
}

func (f *probeFakeCodeReachabilityInputLoader) LatestCompletedIntentTime(
	_ context.Context,
) (time.Time, bool, error) {
	if f.probeErr != nil {
		return time.Time{}, false, f.probeErr
	}
	if f.probeScript != nil {
		if f.probeCalls < len(f.probeScript) {
			reading := f.probeScript[f.probeCalls]
			f.probeCalls++
			return reading.at, reading.ok, nil
		}
		f.probeCalls++
		return time.Time{}, false, nil
	}
	return f.probe, f.probeOK, f.probeErr
}

var errIdleProbeTest = errors.New("idle probe test failure")

// TestProcessNextCycleSkipsFullQueryWhenProbeEqual is the #10870 idle
// contract: a probe that has not advanced past the cursor must not run the
// expensive full candidate query — once the previous full query came back
// empty. A productive predecessor forces another full query first, because
// the probe tracks upstream completions rather than projection progress and
// a static backlog larger than one batch would otherwise drain at one batch
// per audit interval.
func TestProcessNextCycleSkipsFullQueryWhenProbeEqual(t *testing.T) {
	t.Parallel()

	cursor := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
	// The loader models watermark advancement: the first load returns the
	// one pending input, later loads are empty because projection advanced
	// the watermark past it.
	loader := &probeFakeCodeReachabilityInputLoader{
		loadScript: [][]CodeReachabilityProjectionInput{
			{{
				ScopeID:      "scope-1",
				GenerationID: "generation-1",
				RepositoryID: "repo-1",
				UpdatedAt:    cursor,
			}},
			{},
		},
		probe:   cursor,
		probeOK: true,
	}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{
		InputLoader: loader,
		RowWriter:   writer,
		Config:      CodeReachabilityProjectionRunnerConfig{BatchLimit: 10},
	}

	// First cycle learns the cursor from the probe (startup audit).
	first, err := runner.ProcessNextCycle(context.Background(), cursor.Add(time.Minute))
	if err != nil {
		t.Fatalf("ProcessNextCycle() error = %v", err)
	}
	if first.InputsProcessed != 1 {
		t.Fatalf("first InputsProcessed = %d, want 1 (startup audit runs the full query)", first.InputsProcessed)
	}

	// Second cycle with an unchanged probe must run the full query again:
	// the first cycle was productive, so the backlog may hold more.
	second, err := runner.ProcessNextCycle(context.Background(), cursor.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("ProcessNextCycle() error = %v", err)
	}
	if second.InputsProcessed != 0 {
		t.Fatalf("second InputsProcessed = %d, want 0 (watermark advanced past the input)", second.InputsProcessed)
	}
	if loader.loads != 2 {
		t.Fatalf("loader calls = %d, want 2 (productive predecessor forces another full query)", loader.loads)
	}

	// Third cycle with an unchanged probe skips: the previous full query
	// came back empty, so there is nothing left to drain.
	third, err := runner.ProcessNextCycle(context.Background(), cursor.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("ProcessNextCycle() error = %v", err)
	}
	if third.InputsProcessed != 0 {
		t.Fatalf("third InputsProcessed = %d, want 0 (empty predecessor skips)", third.InputsProcessed)
	}
	if loader.loads != 2 {
		t.Fatalf("loader calls = %d, want 2 (no full query on probe-equal cycle after an empty one)", loader.loads)
	}
}

// TestProcessNextCycleRunsFullQueryWhenProbeAdvances proves a completion
// after the cursor triggers the full candidate query, and that the cursor
// moves to the probe value read BEFORE that query (so completions landing
// during the query are caught next cycle).
func TestProcessNextCycleRunsFullQueryWhenProbeAdvances(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
	loader := &probeFakeCodeReachabilityInputLoader{probeOK: false}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{
		InputLoader: loader,
		RowWriter:   writer,
		Config:      CodeReachabilityProjectionRunnerConfig{BatchLimit: 10},
	}

	if _, err := runner.ProcessNextCycle(context.Background(), start); err != nil {
		t.Fatalf("startup ProcessNextCycle() error = %v", err)
	}
	if loader.loads != 1 {
		t.Fatalf("loader calls = %d, want 1 (unknown cursor audits)", loader.loads)
	}

	advanced := start.Add(time.Hour)
	loader.probe, loader.probeOK = advanced, true
	loader.inputs = []CodeReachabilityProjectionInput{{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		RepositoryID: "repo-1",
		UpdatedAt:    advanced,
	}}
	if _, err := runner.ProcessNextCycle(context.Background(), advanced); err != nil {
		t.Fatalf("advanced ProcessNextCycle() error = %v", err)
	}
	if loader.loads != 2 {
		t.Fatalf("loader calls = %d, want 2 (probe advance triggers full query)", loader.loads)
	}

	// Same probe value again, with the watermark advanced past the projected
	// input: the previous full query was productive, so this cycle drains
	// once more instead of skipping.
	loader.inputs = nil
	result, err := runner.ProcessNextCycle(context.Background(), advanced.Add(time.Minute))
	if err != nil {
		t.Fatalf("repeat ProcessNextCycle() error = %v", err)
	}
	if result.InputsProcessed != 0 || loader.loads != 3 {
		t.Fatalf("repeat InputsProcessed = %d loads = %d, want 0/3 (productive predecessor drains again)", result.InputsProcessed, loader.loads)
	}

	// Fourth cycle with the same probe and an empty predecessor skips.
	result, err = runner.ProcessNextCycle(context.Background(), advanced.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("fourth ProcessNextCycle() error = %v", err)
	}
	if result.InputsProcessed != 0 || loader.loads != 3 {
		t.Fatalf("fourth InputsProcessed = %d loads = %d, want 0/3 (empty predecessor skips)", result.InputsProcessed, loader.loads)
	}
}

// TestProcessNextCycleAuditsOnProbeError proves a probe failure falls
// through to the full query instead of skipping work.
func TestProcessNextCycleAuditsOnProbeError(t *testing.T) {
	t.Parallel()

	loader := &probeFakeCodeReachabilityInputLoader{probeErr: errIdleProbeTest}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{
		InputLoader: loader,
		RowWriter:   writer,
		Config:      CodeReachabilityProjectionRunnerConfig{BatchLimit: 10},
	}

	if _, err := runner.ProcessNextCycle(context.Background(), time.Now().UTC()); err != nil {
		t.Fatalf("ProcessNextCycle() error = %v", err)
	}
	if loader.loads != 1 {
		t.Fatalf("loader calls = %d, want 1 (probe error forces full query)", loader.loads)
	}
}

// TestProcessNextCycleAuditsOnProbeRegression proves a probe value older
// than the cursor (reopen/failover) forces the full query.
func TestProcessNextCycleAuditsOnProbeRegression(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
	loader := &probeFakeCodeReachabilityInputLoader{probe: start, probeOK: true}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{
		InputLoader: loader,
		RowWriter:   writer,
		Config:      CodeReachabilityProjectionRunnerConfig{BatchLimit: 10},
	}

	if _, err := runner.ProcessNextCycle(context.Background(), start); err != nil {
		t.Fatalf("startup ProcessNextCycle() error = %v", err)
	}
	loader.probe = start.Add(-time.Hour)
	result, err := runner.ProcessNextCycle(context.Background(), start.Add(time.Minute))
	if err != nil {
		t.Fatalf("regressed ProcessNextCycle() error = %v", err)
	}
	if loader.loads != 2 {
		t.Fatalf("loader calls = %d, want 2 (probe regression forces full query)", loader.loads)
	}
	if result.InputsProcessed != 0 {
		t.Fatalf("InputsProcessed = %d, want 0 (no inputs seeded)", result.InputsProcessed)
	}
}

// TestProcessNextCycleAuditsWhenDue proves the periodic audit runs the full
// query even when the probe has not moved.
func TestProcessNextCycleAuditsWhenDue(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
	loader := &probeFakeCodeReachabilityInputLoader{probe: start, probeOK: true}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{
		InputLoader: loader,
		RowWriter:   writer,
		Config: CodeReachabilityProjectionRunnerConfig{
			BatchLimit:    10,
			AuditInterval: time.Hour,
		},
	}

	if _, err := runner.ProcessNextCycle(context.Background(), start); err != nil {
		t.Fatalf("startup ProcessNextCycle() error = %v", err)
	}
	// Probe unchanged and audit not due: skip.
	if _, err := runner.ProcessNextCycle(context.Background(), start.Add(time.Minute)); err != nil {
		t.Fatalf("skip ProcessNextCycle() error = %v", err)
	}
	if loader.loads != 1 {
		t.Fatalf("loader calls = %d, want 1 (probe-equal skips before audit due)", loader.loads)
	}
	// Past the audit interval with the same probe: full query.
	if _, err := runner.ProcessNextCycle(context.Background(), start.Add(2*time.Hour)); err != nil {
		t.Fatalf("audit ProcessNextCycle() error = %v", err)
	}
	if loader.loads != 2 {
		t.Fatalf("loader calls = %d, want 2 (audit-due forces full query)", loader.loads)
	}
}

// TestProcessNextCycleDisabledProbeRestoresFullPoll proves the kill-switch:
// with the probe disabled every cycle runs the full query, as before #10870.
func TestProcessNextCycleDisabledProbeRestoresFullPoll(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
	loader := &probeFakeCodeReachabilityInputLoader{probe: now, probeOK: true}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{
		InputLoader: loader,
		RowWriter:   writer,
		Config: CodeReachabilityProjectionRunnerConfig{
			BatchLimit:       10,
			DisableIdleProbe: true,
		},
	}

	for i := 0; i < 3; i++ {
		if _, err := runner.ProcessNextCycle(context.Background(), now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("cycle %d ProcessNextCycle() error = %v", i, err)
		}
	}
	if loader.loads != 3 {
		t.Fatalf("loader calls = %d, want 3 (disabled probe polls full every cycle)", loader.loads)
	}
}
