// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	pgstore "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fakeSupportsBackfiller stands in for pgstore.SupplyChainImpactSupportsBackfill
// so the seed step's decision logic is testable without Postgres. The real
// RunStep is proven by the product suite and the gate's live run, where the
// winners oracle proof re-checks the outcome from outside.
type fakeSupportsBackfiller struct {
	reports []pgstore.SupplyChainImpactSupportsBackfillReport
	err     error
	calls   int
}

func (f *fakeSupportsBackfiller) RunStep(context.Context) (pgstore.SupplyChainImpactSupportsBackfillReport, error) {
	f.calls++
	if f.err != nil {
		return pgstore.SupplyChainImpactSupportsBackfillReport{}, f.err
	}
	if f.calls-1 < len(f.reports) {
		return f.reports[f.calls-1], nil
	}
	// Exhausted scripts repeat the last report: a stepper that never reports
	// Complete must keep failing the budget, not flip to Complete by default.
	return f.reports[len(f.reports)-1], nil
}

func fixedSupportsReady(ready bool, err error) func(context.Context) (bool, error) {
	return func(context.Context) (bool, error) { return ready, err }
}

func TestRunSupportsBackfillStandInLoopsUntilComplete(t *testing.T) {
	stepper := &fakeSupportsBackfiller{reports: []pgstore.SupplyChainImpactSupportsBackfillReport{
		{Verified: true},
		{Complete: true},
	}}
	var log bytes.Buffer

	if err := runSupportsBackfillStandIn(context.Background(), stepper, 10, fixedSupportsReady(true, nil), &log); err != nil {
		t.Fatalf("runSupportsBackfillStandIn error = %v, want nil", err)
	}
	if stepper.calls != 2 {
		t.Fatalf("RunStep calls = %d, want 2 (the step must loop until Complete)", stepper.calls)
	}
}

func TestRunSupportsBackfillStandInNamesTheStandInInOneLogLine(t *testing.T) {
	stepper := &fakeSupportsBackfiller{reports: []pgstore.SupplyChainImpactSupportsBackfillReport{{Complete: true}}}
	var log bytes.Buffer
	if err := runSupportsBackfillStandIn(context.Background(), stepper, 10, fixedSupportsReady(true, nil), &log); err != nil {
		t.Fatalf("runSupportsBackfillStandIn error = %v", err)
	}
	standInLines := 0
	for _, line := range strings.Split(strings.TrimSpace(log.String()), "\n") {
		if strings.Contains(line, supportsBackfillStandIn) {
			standInLines++
			// The stand-in is permanent: no reducer runs beside the gate, so the
			// seed drives the product backfill itself; the line says why and
			// where the shared code lives.
			for _, want := range []string{"no reducer runs beside the gate", "product", "#11658"} {
				if !strings.Contains(line, want) {
					t.Fatalf("stand-in line %q does not say %q", line, want)
				}
			}
		}
	}
	if standInLines != 1 {
		t.Fatalf("log %q holds %d lines naming %q, want exactly 1", log.String(), standInLines, supportsBackfillStandIn)
	}
}

func TestRunSupportsBackfillStandInFailsWrappingAStepError(t *testing.T) {
	boom := errors.New("drain failed")
	err := runSupportsBackfillStandIn(context.Background(),
		&fakeSupportsBackfiller{err: boom}, 10, fixedSupportsReady(true, nil), &bytes.Buffer{})
	if !errors.Is(err, boom) {
		t.Fatalf("runSupportsBackfillStandIn error = %v, want it to wrap %v", err, boom)
	}
}

func TestRunSupportsBackfillStandInFailsWhenTheBudgetRunsOut(t *testing.T) {
	stepper := &fakeSupportsBackfiller{reports: []pgstore.SupplyChainImpactSupportsBackfillReport{
		{DirtyScopes: 3, Missing: 7},
	}}
	err := runSupportsBackfillStandIn(context.Background(), stepper, 3, fixedSupportsReady(true, nil), &bytes.Buffer{})
	if err == nil {
		t.Fatalf("runSupportsBackfillStandIn error = nil, want a failure when Complete never arrives")
	}
	for _, want := range []string{"DirtyScopes", "Missing"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("runSupportsBackfillStandIn error = %q, want it to name %q", err, want)
		}
	}
	if stepper.calls != 3 {
		t.Fatalf("RunStep calls = %d, want exactly the 3-step budget", stepper.calls)
	}
}

// TestRunSupportsBackfillStandInRefusesAStampedButUnreadyMarker is the
// anti-blind-stamp RED half: a Complete report with the readiness predicate
// still false (a hand-copied marker row over a non-empty fence, or any future
// seeded finding fact the sweep cannot see) must fail the seed. Checking the
// marker row exists is NOT sufficient.
func TestRunSupportsBackfillStandInRefusesAStampedButUnreadyMarker(t *testing.T) {
	stepper := &fakeSupportsBackfiller{reports: []pgstore.SupplyChainImpactSupportsBackfillReport{{Complete: true}}}
	err := runSupportsBackfillStandIn(context.Background(), stepper, 10, fixedSupportsReady(false, nil), &bytes.Buffer{})
	if err == nil {
		t.Fatalf("runSupportsBackfillStandIn error = nil, want a failure when the readiness predicate reads false after Complete")
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("runSupportsBackfillStandIn error = %q, want it to say the read model is not ready", err)
	}
}

// TestRunSupportsBackfillStandInFailsWhenTheReadinessReadFails pins that a
// readiness-read error fails the seed with its cause rather than passing on an
// unknown state.
func TestRunSupportsBackfillStandInFailsWhenTheReadinessReadFails(t *testing.T) {
	boom := errors.New("readiness read failed")
	stepper := &fakeSupportsBackfiller{reports: []pgstore.SupplyChainImpactSupportsBackfillReport{{Complete: true}}}
	err := runSupportsBackfillStandIn(context.Background(), stepper, 10, fixedSupportsReady(false, boom), &bytes.Buffer{})
	if !errors.Is(err, boom) {
		t.Fatalf("runSupportsBackfillStandIn error = %v, want it to wrap %v", err, boom)
	}
}

// TestRunSupportsBackfillStandInAcceptsAReadyMarkerGREEN is the mirror: the
// identical path with the readiness predicate true must pass, so the RED test
// above is not just "everything fails".
func TestRunSupportsBackfillStandInAcceptsAReadyMarkerGREEN(t *testing.T) {
	stepper := &fakeSupportsBackfiller{reports: []pgstore.SupplyChainImpactSupportsBackfillReport{{Complete: true}}}
	if err := runSupportsBackfillStandIn(context.Background(), stepper, 10, fixedSupportsReady(true, nil), &bytes.Buffer{}); err != nil {
		t.Fatalf("runSupportsBackfillStandIn error = %v, want nil when Complete and ready", err)
	}
}
