// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestFreshnessCauseNextCheckCoversEveryEnumValue asserts the cause→next-call
// mapping is total over the closed enumeration, so a new cause cannot ship
// without a bounded drilldown.
func TestFreshnessCauseNextCheckCoversEveryEnumValue(t *testing.T) {
	for cause := range freshnessCauses {
		check, ok := FreshnessCauseNextCheck(cause)
		if !ok {
			t.Fatalf("cause %q has no next-check mapping", cause)
		}
		if check.Tool == "" && check.Route == "" {
			t.Fatalf("cause %q next-check has neither tool nor route", cause)
		}
		if check.Reason == "" {
			t.Fatalf("cause %q next-check has no reason", cause)
		}
		call := check.asRecommendedNextCall()
		if len(call) == 0 {
			t.Fatalf("cause %q renders an empty recommended next call", cause)
		}
	}
}

// TestFreshnessCauseNextCheckUnknownCauseIsRejected proves an out-of-enum value
// has no mapping and is not invented.
func TestFreshnessCauseNextCheckUnknownCauseIsRejected(t *testing.T) {
	if _, ok := FreshnessCauseNextCheck(FreshnessCause("totally_made_up")); ok {
		t.Fatalf("expected no next-check for an unknown cause")
	}
	if ValidFreshnessCause(FreshnessCause("totally_made_up")) {
		t.Fatalf("expected unknown cause to be invalid")
	}
}

// TestWithFreshnessCauseAttachesProvenCause proves a handler holding evidence of
// a stale state can attach a cause plus its bounded next check.
func TestWithFreshnessCauseAttachesProvenCause(t *testing.T) {
	truth := &TruthEnvelope{
		Level:     TruthLevelDerived,
		Freshness: TruthFreshness{State: FreshnessStale},
	}
	WithFreshnessCause(truth, FreshnessCauseReducerBacklog)
	if truth.Freshness.Cause != FreshnessCauseReducerBacklog {
		t.Fatalf("expected reducer_backlog cause, got %q", truth.Freshness.Cause)
	}
	if truth.Freshness.NextCheck == nil {
		t.Fatalf("expected a next check to be attached with the cause")
	}
	if truth.Freshness.NextCheck.Reason == "" {
		t.Fatalf("expected the attached next check to carry a reason")
	}
}

// TestWithFreshnessCauseRefusesFreshState proves no cause is invented for a
// fresh answer: a fresh answer has nothing to explain.
func TestWithFreshnessCauseRefusesFreshState(t *testing.T) {
	truth := &TruthEnvelope{
		Level:     TruthLevelExact,
		Freshness: TruthFreshness{State: FreshnessFresh},
	}
	WithFreshnessCause(truth, FreshnessCauseReducerBacklog)
	if truth.Freshness.Cause != "" {
		t.Fatalf("expected no cause on a fresh answer, got %q", truth.Freshness.Cause)
	}
	if truth.Freshness.NextCheck != nil {
		t.Fatalf("expected no next check on a fresh answer")
	}
}

// TestWithFreshnessCauseRefusesUnknownCause proves an out-of-enum cause is never
// attached, even when the state is stale.
func TestWithFreshnessCauseRefusesUnknownCause(t *testing.T) {
	truth := &TruthEnvelope{
		Level:     TruthLevelFallback,
		Freshness: TruthFreshness{State: FreshnessUnavailable},
	}
	WithFreshnessCause(truth, FreshnessCause("invented"))
	if truth.Freshness.Cause != "" {
		t.Fatalf("expected no cause for an invalid value, got %q", truth.Freshness.Cause)
	}
	if truth.Freshness.NextCheck != nil {
		t.Fatalf("expected no next check for an invalid cause")
	}
}

// TestWithFreshnessCauseNilEnvelopeIsSafe proves the helper tolerates a nil
// envelope without panicking.
func TestWithFreshnessCauseNilEnvelopeIsSafe(t *testing.T) {
	WithFreshnessCause(nil, FreshnessCausePendingRepoGeneration)
}

// TestWithFreshnessCauseUnsupportedProfile proves the unsupported_profile cause
// attaches to a fallback/unavailable answer, covering the unsupported case at
// the contract layer so a profile-limited capability can explain itself.
func TestWithFreshnessCauseUnsupportedProfile(t *testing.T) {
	truth := &TruthEnvelope{
		Level:     TruthLevelFallback,
		Profile:   ProfileLocalLightweight,
		Freshness: TruthFreshness{State: FreshnessUnavailable},
	}
	WithFreshnessCause(truth, FreshnessCauseUnsupportedProfile)
	if truth.Freshness.Cause != FreshnessCauseUnsupportedProfile {
		t.Fatalf("expected unsupported_profile cause, got %q", truth.Freshness.Cause)
	}
	if truth.Freshness.NextCheck == nil {
		t.Fatalf("expected a next check for the unsupported_profile cause")
	}
}

// TestWithFreshnessCausePendingRepoGeneration proves the pending_repo_generation
// cause attaches to a building answer, covering the remaining enum value.
func TestWithFreshnessCausePendingRepoGeneration(t *testing.T) {
	truth := &TruthEnvelope{Freshness: TruthFreshness{State: FreshnessBuilding}}
	WithFreshnessCause(truth, FreshnessCausePendingRepoGeneration)
	if truth.Freshness.Cause != FreshnessCausePendingRepoGeneration {
		t.Fatalf("expected pending_repo_generation cause, got %q", truth.Freshness.Cause)
	}
}

// TestWithFreshnessCauseContentCoverageUnavailable covers the remaining content
// coverage enum value through the helper.
func TestWithFreshnessCauseContentCoverageUnavailable(t *testing.T) {
	truth := &TruthEnvelope{Freshness: TruthFreshness{State: FreshnessStale}}
	WithFreshnessCause(truth, FreshnessCauseContentCoverageUnavailable)
	if truth.Freshness.Cause != FreshnessCauseContentCoverageUnavailable {
		t.Fatalf("expected content_coverage_unavailable cause, got %q", truth.Freshness.Cause)
	}
}

// TestWithFreshnessCauseBuildingState proves a building answer can carry a
// cause and next check.
func TestWithFreshnessCauseBuildingState(t *testing.T) {
	truth := &TruthEnvelope{Freshness: TruthFreshness{State: FreshnessBuilding}}
	WithFreshnessCause(truth, FreshnessCauseMissingCollectorCompletion)
	if truth.Freshness.Cause != FreshnessCauseMissingCollectorCompletion {
		t.Fatalf("expected missing_collector_completion cause, got %q", truth.Freshness.Cause)
	}
	if truth.Freshness.NextCheck == nil || truth.Freshness.NextCheck.Tool == "" {
		t.Fatalf("expected a tool-bearing next check on the building answer")
	}
}
