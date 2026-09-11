// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestFreshnessCauseNextCheckUnknownCauseIsRejected proves an out-of-enum value
// has no mapping and is not invented.
func TestFreshnessCauseNextCheckUnknownCauseIsRejected(t *testing.T) {
	if _, ok := CauseNextCheck(Cause("totally_made_up")); ok {
		t.Fatalf("expected no next-check for an unknown cause")
	}
	if ValidCause(Cause("totally_made_up")) {
		t.Fatalf("expected unknown cause to be invalid")
	}
}

// TestWithFreshnessCauseAttachesProvenCause proves a handler holding evidence of
// a stale state can attach a cause plus its bounded next check.
func TestWithFreshnessCauseAttachesProvenCause(t *testing.T) {
	truth := &querycontract.TruthEnvelope{
		Level:     querycontract.TruthLevelDerived,
		Freshness: querycontract.TruthFreshness{State: querycontract.FreshnessStale},
	}
	WithCause(truth, CauseReducerBacklog)
	if truth.Freshness.Cause != CauseReducerBacklog {
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
	truth := &querycontract.TruthEnvelope{
		Level:     querycontract.TruthLevelExact,
		Freshness: querycontract.TruthFreshness{State: querycontract.FreshnessFresh},
	}
	WithCause(truth, CauseReducerBacklog)
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
	truth := &querycontract.TruthEnvelope{
		Level:     querycontract.TruthLevelFallback,
		Freshness: querycontract.TruthFreshness{State: querycontract.FreshnessUnavailable},
	}
	WithCause(truth, Cause("invented"))
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
	WithCause(nil, CausePendingRepoGeneration)
}

// TestWithFreshnessCauseUnsupportedProfile proves the unsupported_profile cause
// attaches to a fallback/unavailable answer, covering the unsupported case at
// the contract layer so a profile-limited capability can explain itself.
func TestWithFreshnessCauseUnsupportedProfile(t *testing.T) {
	truth := &querycontract.TruthEnvelope{
		Level:     querycontract.TruthLevelFallback,
		Profile:   querycontract.ProfileLocalLightweight,
		Freshness: querycontract.TruthFreshness{State: querycontract.FreshnessUnavailable},
	}
	WithCause(truth, CauseUnsupportedProfile)
	if truth.Freshness.Cause != CauseUnsupportedProfile {
		t.Fatalf("expected unsupported_profile cause, got %q", truth.Freshness.Cause)
	}
	if truth.Freshness.NextCheck == nil {
		t.Fatalf("expected a next check for the unsupported_profile cause")
	}
}

// TestWithFreshnessCausePendingRepoGeneration proves the pending_repo_generation
// cause attaches to a building answer, covering the remaining enum value.
func TestWithFreshnessCausePendingRepoGeneration(t *testing.T) {
	truth := &querycontract.TruthEnvelope{Freshness: querycontract.TruthFreshness{State: querycontract.FreshnessBuilding}}
	WithCause(truth, CausePendingRepoGeneration)
	if truth.Freshness.Cause != CausePendingRepoGeneration {
		t.Fatalf("expected pending_repo_generation cause, got %q", truth.Freshness.Cause)
	}
}

// TestWithFreshnessCauseContentCoverageUnavailable covers the remaining content
// coverage enum value through the helper.
func TestWithFreshnessCauseContentCoverageUnavailable(t *testing.T) {
	truth := &querycontract.TruthEnvelope{Freshness: querycontract.TruthFreshness{State: querycontract.FreshnessStale}}
	WithCause(truth, CauseContentCoverageUnavailable)
	if truth.Freshness.Cause != CauseContentCoverageUnavailable {
		t.Fatalf("expected content_coverage_unavailable cause, got %q", truth.Freshness.Cause)
	}
}

// TestWithFreshnessCauseBuildingState proves a building answer can carry a
// cause and next check.
func TestWithFreshnessCauseBuildingState(t *testing.T) {
	truth := &querycontract.TruthEnvelope{Freshness: querycontract.TruthFreshness{State: querycontract.FreshnessBuilding}}
	WithCause(truth, CauseMissingCollectorCompletion)
	if truth.Freshness.Cause != CauseMissingCollectorCompletion {
		t.Fatalf("expected missing_collector_completion cause, got %q", truth.Freshness.Cause)
	}
	if truth.Freshness.NextCheck == nil || truth.Freshness.NextCheck.Tool == "" {
		t.Fatalf("expected a tool-bearing next check on the building answer")
	}
}
