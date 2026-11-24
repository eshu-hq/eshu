// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestSupportsBackfillOutcomeClassifiesEveryStep(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		report postgres.SupplyChainImpactSupportsBackfillReport
		want   string
	}{
		"chunk":          {postgres.SupplyChainImpactSupportsBackfillReport{Written: 3}, telemetry.SupplyChainImpactSupportsBackfillStepChunk},
		"orphan cleanup": {postgres.SupplyChainImpactSupportsBackfillReport{Removed: 1}, telemetry.SupplyChainImpactSupportsBackfillStepChunk},
		"verify gap":     {postgres.SupplyChainImpactSupportsBackfillReport{Verified: true, Missing: 2}, telemetry.SupplyChainImpactSupportsBackfillStepGap},
		"verify slice": {postgres.SupplyChainImpactSupportsBackfillReport{
			Verified:    true,
			VerifySlice: telemetry.SupplyChainImpactSupportsVerifySliceAdvance,
		}, telemetry.SupplyChainImpactSupportsBackfillStepVerify},
		"verify orphan": {postgres.SupplyChainImpactSupportsBackfillReport{
			Verified: true, Removed: 1,
			VerifySlice: telemetry.SupplyChainImpactSupportsVerifySliceOrphan,
		}, telemetry.SupplyChainImpactSupportsBackfillStepVerify},
		"fence-blocked last slice": {postgres.SupplyChainImpactSupportsBackfillReport{
			Verified:    true,
			VerifySlice: telemetry.SupplyChainImpactSupportsVerifySliceLast,
		}, telemetry.SupplyChainImpactSupportsBackfillStepVerify},
		"verify complete": {postgres.SupplyChainImpactSupportsBackfillReport{Verified: true, Complete: true}, telemetry.SupplyChainImpactSupportsBackfillStepComplete},
		"drain":           {postgres.SupplyChainImpactSupportsBackfillReport{Drained: 1, Complete: true}, telemetry.SupplyChainImpactSupportsBackfillStepDrain},
		"already done":    {postgres.SupplyChainImpactSupportsBackfillReport{Complete: true}, telemetry.SupplyChainImpactSupportsBackfillStepIdle},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := supportsBackfillOutcome(tc.report); got != tc.want {
				t.Fatalf("outcome = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRunSupportsBackfillPassStopsAtCompletionAndSurfacesErrors proves a pass
// runs steps until the marker is set, reports the marker, and returns a step
// failure instead of swallowing it.
func TestRunSupportsBackfillPassStopsAtCompletionAndSurfacesErrors(t *testing.T) {
	t.Parallel()
	steps := 0
	complete, err := runSupportsBackfillPass(context.Background(), func(context.Context) (postgres.SupplyChainImpactSupportsBackfillReport, error) {
		steps++
		if steps == 3 {
			return postgres.SupplyChainImpactSupportsBackfillReport{Verified: true, Complete: true}, nil
		}
		return postgres.SupplyChainImpactSupportsBackfillReport{Written: 1}, nil
	}, nil, nil)
	if err != nil || !complete || steps != 3 {
		t.Fatalf("pass = complete %v, err %v after %d steps, want complete after 3", complete, err, steps)
	}

	boom := errors.New("boom")
	complete, err = runSupportsBackfillPass(context.Background(), func(context.Context) (postgres.SupplyChainImpactSupportsBackfillReport, error) {
		return postgres.SupplyChainImpactSupportsBackfillReport{}, boom
	}, nil, nil)
	if !errors.Is(err, boom) || complete {
		t.Fatalf("failing pass = complete %v, err %v, want the step error", complete, err)
	}

	steps = 0
	complete, err = runSupportsBackfillPass(context.Background(), func(context.Context) (postgres.SupplyChainImpactSupportsBackfillReport, error) {
		steps++
		return postgres.SupplyChainImpactSupportsBackfillReport{Written: 1}, nil
	}, nil, nil)
	if err != nil || complete || steps != supportsBackfillPassSteps {
		t.Fatalf("budgeted pass = complete %v, err %v after %d steps, want incomplete after %d", complete, err, steps, supportsBackfillPassSteps)
	}
}
