// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

const (
	relationshipFamilyWriteTaxFacts       = 100000
	relationshipFamilyWriteTaxSourceFacts = 4432
	relationshipFamilyWriteTaxFamilyFacts = 986
	relationshipFamilyWriteTaxWorkers     = 8
	relationshipFamilyWriteTaxRounds      = 5
	relationshipFamilyRetainedFacts       = 6200524
	relationshipFamilyReadSavingSeconds   = 282.440
	relationshipFamilyHardHeadroomSeconds = 61.749
	relationshipFamilyPreferredTaxSeconds = 30.000
)

// TestRelationshipFamilyIndexOduEightWorkerWriteTax measures the proposed
// partial index's accepted-fact write tax with the retained global fact mix.
// Both arms use ordinary WAL-backed tables and the production multi-row
// INSERT ... ON CONFLICT ... RETURNING path under eight simultaneous writers.
func TestRelationshipFamilyIndexOduEightWorkerWriteTax(t *testing.T) {
	proof := openRelationshipFamilyWriteTaxProof(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	fixture := relationshipFamilyWriteTaxFixture(t)
	assertRelationshipFamilyWriteTaxFixture(t, fixture)

	controlDurations := make([]time.Duration, 0, relationshipFamilyWriteTaxRounds)
	candidateDurations := make([]time.Duration, 0, relationshipFamilyWriteTaxRounds)
	pairedDeltas := make([]time.Duration, 0, relationshipFamilyWriteTaxRounds)
	for round := 0; round < relationshipFamilyWriteTaxRounds; round++ {
		var control, candidate relationshipFamilyWriteTaxResult
		if round%2 == 0 {
			control = runRelationshipFamilyWriteTaxRound(t, ctx, proof.control, fixture)
			candidate = runRelationshipFamilyWriteTaxRound(t, ctx, proof.candidate, fixture)
		} else {
			candidate = runRelationshipFamilyWriteTaxRound(t, ctx, proof.candidate, fixture)
			control = runRelationshipFamilyWriteTaxRound(t, ctx, proof.control, fixture)
		}
		assertRelationshipFamilyWriteTaxResult(t, "control", control)
		assertRelationshipFamilyWriteTaxResult(t, "candidate", candidate)
		controlDurations = append(controlDurations, control.duration)
		candidateDurations = append(candidateDurations, candidate.duration)
		pairedDeltas = append(pairedDeltas, candidate.duration-control.duration)
		t.Logf(
			"relationship-family write-tax round=%d control=%s candidate=%s delta=%s control_peak=%d candidate_peak=%d",
			round+1,
			control.duration,
			candidate.duration,
			candidate.duration-control.duration,
			control.peakWriters,
			candidate.peakWriters,
		)
	}

	controlMedian := medianDuration(controlDurations)
	candidateMedian := medianDuration(candidateDurations)
	pairedMedian := medianDuration(pairedDeltas)
	if pairedMedian < 0 {
		pairedMedian = 0
	}
	upperDelta := maximumDuration(pairedDeltas)
	if upperDelta < 0 {
		upperDelta = 0
	}
	projectedTaxSeconds := pairedMedian.Seconds() * relationshipFamilyRetainedFacts / relationshipFamilyWriteTaxFacts
	projectedUpperTaxSeconds := upperDelta.Seconds() * relationshipFamilyRetainedFacts / relationshipFamilyWriteTaxFacts
	projectedNetSavingSeconds := relationshipFamilyReadSavingSeconds - projectedUpperTaxSeconds
	t.Logf(
		"relationship-family write-tax summary facts=%d source=%d family=%d workers=%d rounds=%d control_median=%s candidate_median=%s paired_delta_median=%s paired_delta_upper=%s projected_median_tax_seconds=%.3f projected_upper_tax_seconds=%.3f projected_net_saving_seconds=%.3f preferred_tax_seconds=%.3f hard_headroom_seconds=%.3f",
		relationshipFamilyWriteTaxFacts,
		relationshipFamilyWriteTaxSourceFacts,
		relationshipFamilyWriteTaxFamilyFacts,
		relationshipFamilyWriteTaxWorkers,
		relationshipFamilyWriteTaxRounds,
		controlMedian,
		candidateMedian,
		pairedMedian,
		upperDelta,
		projectedTaxSeconds,
		projectedUpperTaxSeconds,
		projectedNetSavingSeconds,
		relationshipFamilyPreferredTaxSeconds,
		relationshipFamilyHardHeadroomSeconds,
	)
	if projectedUpperTaxSeconds > relationshipFamilyHardHeadroomSeconds {
		t.Errorf(
			"projected relationship-family index write tax %.3fs exceeds %.3fs hard headroom",
			projectedUpperTaxSeconds,
			relationshipFamilyHardHeadroomSeconds,
		)
	}
}
