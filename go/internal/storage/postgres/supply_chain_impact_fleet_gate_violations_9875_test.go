// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// fleetGateCleanObservation is what a passing gate run observes: the seeded
// service bucket is complete, carries the KEV-scored winner's priority, and the
// one unmapped control finding sits in the unattributed bucket.
func fleetGateCleanObservation() fleetGateObservation {
	start := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	return fleetGateObservation{
		RollupStatus: http.StatusOK,
		Buckets: []fleetGateBucket{
			{
				Value: fleetGateService, Count: 1, MaxPriorityScore: 100, Completeness: "complete",
				ByImpactStatus: map[string]int{"affected_exact": 1},
			},
			{
				Unattributed: true, Count: 1, MaxPriorityScore: 90, Completeness: "unattributed",
				ByImpactStatus: map[string]int{"affected_exact": 1}, MissingEvidence: []string{"service_mapping"},
			},
		},
		DistinctFindings: 2,
		TierStatus:       http.StatusOK,
		ServiceTiers:     map[string]int{"critical": 1},
		Winner: &fleetGateWinner{
			KnownExploited: true, PriorityScore: 100, PriorityBucket: "critical",
			PriorityReasonCodes: []string{"cvss_v3_high", "cisa_kev"},
			ServiceIDs:          []string{fleetGateService},
		},
		Stamps: fleetGateStamps{
			SourceCommitted: start,
			FindingWritten:  start.Add(400 * time.Millisecond),
			WinnersBuilt:    start.Add(500 * time.Millisecond),
			RollupRead:      start.Add(550 * time.Millisecond),
		},
	}
}

// fleetGateCleanExpectation is the expectation the live gate asserts.
func fleetGateCleanExpectation() fleetGateExpectation {
	return fleetGateExpectation{
		ServiceID:         fleetGateService,
		ImpactStatus:      "affected_exact",
		PriorityTier:      "critical",
		ServiceCount:      1,
		UnattributedCount: 1,
		DistinctFindings:  2,
		Bound:             fleetGateDefaultBound,
	}
}

// TestFleetGateViolationsSeededPairs9875 is the hermetic RED/GREEN pair for
// the #9875 gate classifier: the clean observation passes, and each planted
// defect (the same defects the live gate plants by withholding a fact, plus a
// blown elapsed bound) is reported under its own violation code. Without the
// RED rows the classifier could pass while checking nothing.
func TestFleetGateViolationsSeededPairs9875(t *testing.T) {
	t.Parallel()

	if got := fleetGateViolations(fleetGateCleanObservation(), fleetGateCleanExpectation()); len(got) != 0 {
		t.Fatalf("GREEN: clean observation reported %q, want no violations", got)
	}

	for _, tc := range []struct {
		name   string
		plant  func(*fleetGateObservation, *fleetGateExpectation)
		wanted string
	}{
		{"rollup route failed", func(o *fleetGateObservation, _ *fleetGateExpectation) { o.RollupStatus = http.StatusServiceUnavailable }, "rollup_unavailable"},
		{"service bucket missing", func(o *fleetGateObservation, _ *fleetGateExpectation) { o.Buckets = o.Buckets[1:] }, "service_absent"},
		{"service bucket unattributed", func(o *fleetGateObservation, _ *fleetGateExpectation) { o.Buckets[0].Completeness = "unattributed" }, "service_completeness"},
		{"service bucket stale", func(o *fleetGateObservation, _ *fleetGateExpectation) {
			o.Buckets[0].Completeness = "read_model_unavailable"
		}, "service_completeness"},
		{"service bucket lacks the affected status", func(o *fleetGateObservation, _ *fleetGateExpectation) {
			o.Buckets[0].ByImpactStatus = map[string]int{"possibly_affected": 1}
		}, "service_impact_status"},
		{"service bucket count inflated by a join fan-out", func(o *fleetGateObservation, _ *fleetGateExpectation) { o.Buckets[0].Count = 5 }, "service_count"},
		{"service impact status inflated", func(o *fleetGateObservation, _ *fleetGateExpectation) {
			o.Buckets[0].ByImpactStatus = map[string]int{"affected_exact": 5}
		}, "service_impact_status"},
		{"service tier breakdown inflated", func(o *fleetGateObservation, _ *fleetGateExpectation) {
			o.ServiceTiers = map[string]int{"critical": 5}
		}, "priority_tier"},
		{"inflated service bucket across count, status, and tier", func(o *fleetGateObservation, _ *fleetGateExpectation) {
			o.Buckets[0].Count = 5
			o.Buckets[0].ByImpactStatus = map[string]int{"affected_exact": 5}
			o.ServiceTiers = map[string]int{"critical": 5}
		}, "service_count"},
		{"extra possibly_affected rows in the service bucket", func(o *fleetGateObservation, _ *fleetGateExpectation) {
			o.Buckets[0].Count = 4
			o.Buckets[0].ByImpactStatus = map[string]int{"affected_exact": 1, "possibly_affected": 3}
		}, "service_count"},
		{"rollup priority below the KEV winner", func(o *fleetGateObservation, _ *fleetGateExpectation) { o.Buckets[0].MaxPriorityScore = 70 }, "service_priority"},
		{"winner withheld", func(o *fleetGateObservation, _ *fleetGateExpectation) { o.Winner = nil }, "winner_absent"},
		{"winner not KEV flagged", func(o *fleetGateObservation, _ *fleetGateExpectation) { o.Winner.KnownExploited = false }, "kev_missing"},
		{"winner without the cisa_kev reason", func(o *fleetGateObservation, _ *fleetGateExpectation) {
			o.Winner.PriorityReasonCodes = []string{"cvss_v3_high"}
		}, "kev_missing"},
		{"winner not attributed to the service by the reducer", func(o *fleetGateObservation, _ *fleetGateExpectation) {
			o.Winner.ServiceIDs = nil
		}, "winner_service"},
		{"winner tier below critical", func(o *fleetGateObservation, _ *fleetGateExpectation) { o.Winner.PriorityBucket = "high" }, "priority_tier"},
		{"service tier breakdown missing critical", func(o *fleetGateObservation, _ *fleetGateExpectation) {
			o.ServiceTiers = map[string]int{"high": 1}
		}, "priority_tier"},
		{"tier route failed", func(o *fleetGateObservation, _ *fleetGateExpectation) { o.TierStatus = http.StatusInternalServerError }, "priority_tier"},
		{"unattributed bucket not reported", func(o *fleetGateObservation, _ *fleetGateExpectation) { o.Buckets = o.Buckets[:1] }, "unattributed_bucket"},
		{"unattributed bucket without its reason", func(o *fleetGateObservation, _ *fleetGateExpectation) {
			o.Buckets[1].MissingEvidence = nil
		}, "unattributed_bucket"},
		{"distinct findings drifted", func(o *fleetGateObservation, _ *fleetGateExpectation) { o.DistinctFindings = 1 }, "distinct_findings"},
		{"elapsed bound exceeded", func(_ *fleetGateObservation, e *fleetGateExpectation) { e.Bound = 549 * time.Millisecond }, "elapsed_bound"},
		{"bound not positive", func(_ *fleetGateObservation, e *fleetGateExpectation) { e.Bound = 0 }, "elapsed_bound"},
		{"stage stamps out of order", func(o *fleetGateObservation, _ *fleetGateExpectation) {
			o.Stamps.WinnersBuilt = o.Stamps.SourceCommitted.Add(-time.Millisecond)
		}, "stage_order"},
		{"stage stamp missing", func(o *fleetGateObservation, _ *fleetGateExpectation) { o.Stamps.FindingWritten = time.Time{} }, "stage_order"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			observation, expectation := fleetGateCleanObservation(), fleetGateCleanExpectation()
			tc.plant(&observation, &expectation)
			got := fleetGateViolations(observation, expectation)
			if !fleetGateHasViolation(got, tc.wanted) {
				t.Fatalf("RED: planted %q reported %q, want a %s violation", tc.name, got, tc.wanted)
			}
		})
	}
}

// TestFleetGateBoundFromEnv9875 pins the bound parameter: unset takes the
// documented default, a Go duration overrides it, and an unparseable or
// non-positive value is an error rather than a silent default.
func TestFleetGateBoundFromEnv9875(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{"", fleetGateDefaultBound, false},
		{"  ", fleetGateDefaultBound, false},
		{"5s", 5 * time.Second, false},
		{"1500ms", 1500 * time.Millisecond, false},
		{"0s", 0, true},
		{"-1s", 0, true},
		{"thirty", 0, true},
	} {
		got, err := fleetGateBoundFrom(tc.raw)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Fatalf("fleetGateBoundFrom(%q) = %v, %v; want %v, error %v", tc.raw, got, err, tc.want, tc.wantErr)
		}
	}
}

// TestFleetGateStagesReportEveryStage9875 pins the per-stage breakdown the
// gate logs: each stage is measured from the previous stamp, and the total is
// source commit to rollup read.
func TestFleetGateStagesReportEveryStage9875(t *testing.T) {
	t.Parallel()

	stages := fleetGateCleanObservation().Stamps.stages()
	want := []fleetGateStage{
		{"finding_written", 400 * time.Millisecond},
		{"winners_built", 100 * time.Millisecond},
		{"rollup_read", 50 * time.Millisecond},
		{"total", 550 * time.Millisecond},
	}
	if !slices.Equal(stages, want) {
		t.Fatalf("stages = %v, want %v", stages, want)
	}
}

func fleetGateHasViolation(violations []string, code string) bool {
	for _, violation := range violations {
		if strings.HasPrefix(violation, code+":") {
			return true
		}
	}
	return false
}
