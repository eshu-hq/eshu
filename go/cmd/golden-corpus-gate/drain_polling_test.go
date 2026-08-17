// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeDrainQuerier returns a scripted sequence of counts so the poll loop can
// be tested without Postgres.
type fakeDrainQuerier struct {
	seq   []DrainCounts
	i     int
	errOn int // 1-based index to return an error on; 0 disables
	// breakdown is returned by ResidualBreakdown, which the runner calls only
	// after a drain has already failed. breakdownErr models the read failing:
	// the runner must degrade its message, never its verdict.
	breakdown    []residualRow
	breakdownErr error
}

// ResidualBreakdown implements drainQuerier.
func (f *fakeDrainQuerier) ResidualBreakdown(_ context.Context) ([]residualRow, error) {
	return f.breakdown, f.breakdownErr
}

// CompletionEventBreakdown implements drainQuerier.
func (*fakeDrainQuerier) CompletionEventBreakdown(_ context.Context) ([]completionEventRow, error) {
	return nil, nil
}

func (f *fakeDrainQuerier) Counts(_ context.Context) (DrainCounts, error) {
	f.i++
	if f.errOn == f.i {
		return DrainCounts{}, errors.New("boom")
	}
	if f.i-1 < len(f.seq) {
		return f.seq[f.i-1], nil
	}
	return f.seq[len(f.seq)-1], nil
}

func TestPollUntilDrainedConvergesAfterRetries(t *testing.T) {
	q := &fakeDrainQuerier{seq: []DrainCounts{
		{FactWorkItemsResidual: 5, SharedIntentsNonterminal: 3},
		{FactWorkItemsResidual: 1, SharedIntentsNonterminal: 1},
		{}, // drained
	}}
	counts, ok, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 0, time.Second, time.Millisecond, nil, 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatalf("expected drained, got counts %+v", counts)
	}
}

func TestPollUntilDrainedWaitsForPopulation(t *testing.T) {
	// Both queues read empty from the start, but the reducer has not emitted the
	// required domain until the third poll. The poll must NOT converge on the
	// early empty reads (the premature-convergence bug).
	q := &fakeDrainQuerier{seq: []DrainCounts{
		{PopulatedDomainsPresent: 0}, // empty + unpopulated — must not converge
		{PopulatedDomainsPresent: 0},
		{PopulatedDomainsPresent: 1}, // reducer emitted; empty + populated — converge
	}}
	counts, ok, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 1, time.Second, time.Millisecond, nil, 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatalf("expected convergence after population, got %+v", counts)
	}
	if q.i < 3 {
		t.Errorf("converged after %d polls; must wait for population (>=3)", q.i)
	}
}

func TestPollUntilDrainedWaitsForCrossScopeCompletionEvents(t *testing.T) {
	q := &fakeDrainQuerier{seq: []DrainCounts{
		{CrossScopeCompletionEventsNonterminal: 1},
		{},
	}}
	counts, ok, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 0, time.Second, time.Millisecond, nil, 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatalf("expected convergence after completion event drained, got %+v", counts)
	}
	if q.i < 2 {
		t.Errorf("converged after %d poll; must wait for completion event", q.i)
	}
}

func TestPollUntilDrainedTimesOutWhenNeverPopulated(t *testing.T) {
	// Queues are empty but the reducer never emits the required domain, so the
	// gate must not report drained on an unreduced pipeline.
	q := &fakeDrainQuerier{seq: []DrainCounts{{PopulatedDomainsPresent: 0}}}
	_, ok, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 1, 5*time.Millisecond, time.Millisecond, nil, 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Fatal("must not converge when the required domain is never populated")
	}
}

func TestPollUntilDrainedTimeoutReturnsLastCounts(t *testing.T) {
	q := &fakeDrainQuerier{seq: []DrainCounts{{FactWorkItemsResidual: 9}}}
	counts, drained, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 0, 5*time.Millisecond, time.Millisecond, nil, 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if drained {
		t.Fatal("expected timeout, got drained")
	}
	if counts.FactWorkItemsResidual != 9 {
		t.Errorf("want last residual 9, got %d", counts.FactWorkItemsResidual)
	}
}

func TestPollUntilDrainedPropagatesQueryError(t *testing.T) {
	q := &fakeDrainQuerier{seq: []DrainCounts{{}}, errOn: 1}
	if _, _, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 0, time.Second, time.Millisecond, nil, 0); err == nil {
		t.Fatal("expected query error to propagate")
	}
}

// Before this item, pollUntilDrained reported its residual only at the
// bound (via the caller's own post-return diagnostic) — nothing distinguished
// "still draining" from "wedged" while it was actually running, which cost
// real time diagnosing a flake (#6149 follow-up item 7). A stuck querier
// (the same non-empty count on every poll, never converging) run long enough
// to cross several progressEvery boundaries must produce more than one
// progress line, and each line must name the still-outstanding residual so a
// reader tailing output mid-run sees the same trend the final message would
// only report after the fact.
func TestPollUntilDrainedEmitsPeriodicProgress(t *testing.T) {
	q := &fakeDrainQuerier{seq: []DrainCounts{{FactWorkItemsResidual: 7}}} // never drains
	var out bytes.Buffer
	_, ok, err := pollUntilDrained(
		context.Background(), q, strictDrainAssertions(), 0,
		40*time.Millisecond, time.Millisecond, // timeout, poll
		&out, 5*time.Millisecond, // progress, progressEvery
	)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Fatal("a stuck querier must not report drained")
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 progress lines over a 40ms window with a 5ms cadence, got %d:\n%s", len(lines), out.String())
	}
	for _, line := range lines {
		if !strings.Contains(line, "fact residual=7") {
			t.Errorf("progress line missing the outstanding residual count: %q", line)
		}
	}
}

// progressEvery <= 0 disables periodic progress entirely, even with a
// non-nil writer -- the zero value from every pre-existing call site above
// must stay silent, not panic or default to some implicit cadence.
func TestPollUntilDrainedProgressDisabledByZeroInterval(t *testing.T) {
	q := &fakeDrainQuerier{seq: []DrainCounts{{FactWorkItemsResidual: 3}}}
	var out bytes.Buffer
	_, _, err := pollUntilDrained(
		context.Background(), q, strictDrainAssertions(), 0,
		20*time.Millisecond, time.Millisecond,
		&out, 0,
	)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no progress output with progressEvery=0, got:\n%s", out.String())
	}
}
