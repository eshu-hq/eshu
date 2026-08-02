// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
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
	counts, ok, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 0, time.Second, time.Millisecond)
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
	counts, ok, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 1, time.Second, time.Millisecond)
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
	counts, ok, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 0, time.Second, time.Millisecond)
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
	_, ok, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 1, 5*time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Fatal("must not converge when the required domain is never populated")
	}
}

func TestPollUntilDrainedTimeoutReturnsLastCounts(t *testing.T) {
	q := &fakeDrainQuerier{seq: []DrainCounts{{FactWorkItemsResidual: 9}}}
	counts, drained, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 0, 5*time.Millisecond, time.Millisecond)
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
	if _, _, err := pollUntilDrained(context.Background(), q, strictDrainAssertions(), 0, time.Second, time.Millisecond); err == nil {
		t.Fatal("expected query error to propagate")
	}
}
