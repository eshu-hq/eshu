// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"testing"
	"time"
)

// TestApplySearchVectorFreshnessNoSignalIsNoop proves a handler with no
// configured ready-signal reader leaves the envelope fresh: the probe only
// engages when the search backend actually reports a signal.
func TestApplySearchVectorFreshnessNoSignalIsNoop(t *testing.T) {
	truth := BuildTruthEnvelope(ProfileProduction, semanticSearchCapability, TruthBasisHybrid, "resolved")
	applySearchVectorFreshness(truth, SearchVectorReadyFreshness{Signaled: false}, nil, time.Now())
	if truth.Freshness.State != FreshnessFresh {
		t.Fatalf("expected fresh state with no configured signal, got %q", truth.Freshness.State)
	}
	if truth.Freshness.Cause != "" {
		t.Fatalf("expected no cause with no configured signal, got %q", truth.Freshness.Cause)
	}
}

// TestApplySearchVectorFreshnessProbeErrorReportsUnavailable proves a failed
// watermark probe never falls back to a false-fresh envelope.
func TestApplySearchVectorFreshnessProbeErrorReportsUnavailable(t *testing.T) {
	truth := BuildTruthEnvelope(ProfileProduction, semanticSearchCapability, TruthBasisHybrid, "resolved")
	probeErr := errors.New("statement timeout")
	applySearchVectorFreshness(truth, SearchVectorReadyFreshness{Signaled: true}, probeErr, time.Now())
	if truth.Freshness.State != FreshnessUnavailable {
		t.Fatalf("expected unavailable state on probe error, got %q", truth.Freshness.State)
	}
}

// TestApplySearchVectorFreshnessNeverPublishedIsBuilding proves that a
// configured signal that has never published search_vector_ready (no
// watermark row) reports a building state with the pending_search_vector
// cause attached, mirroring the never-populated case for the winners read
// model.
func TestApplySearchVectorFreshnessNeverPublishedIsBuilding(t *testing.T) {
	truth := BuildTruthEnvelope(ProfileProduction, semanticSearchCapability, TruthBasisHybrid, "resolved")
	applySearchVectorFreshness(truth, SearchVectorReadyFreshness{Signaled: true, Present: false}, nil, time.Now())
	if truth.Freshness.State != FreshnessBuilding {
		t.Fatalf("expected building state, got %q", truth.Freshness.State)
	}
	if truth.Freshness.Cause != FreshnessCausePendingSearchVector {
		t.Fatalf("expected pending_search_vector cause, got %q", truth.Freshness.Cause)
	}
	if truth.Freshness.NextCheck == nil {
		t.Fatalf("expected a next check to be attached with the cause")
	}
}

// TestApplySearchVectorFreshnessWithinWindowIsFresh proves a watermark
// published within the freshness window is served fresh with no cause,
// carrying the observed_at watermark for consumer visibility.
func TestApplySearchVectorFreshnessWithinWindowIsFresh(t *testing.T) {
	truth := BuildTruthEnvelope(ProfileProduction, semanticSearchCapability, TruthBasisHybrid, "resolved")
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	materializedAt := now.Add(-30 * time.Second)
	applySearchVectorFreshness(truth, SearchVectorReadyFreshness{
		Signaled:       true,
		Present:        true,
		MaterializedAt: materializedAt,
	}, nil, now)
	if truth.Freshness.State != FreshnessFresh {
		t.Fatalf("expected fresh state within the watermark window, got %q", truth.Freshness.State)
	}
	if truth.Freshness.Cause != "" {
		t.Fatalf("expected no cause on a fresh answer, got %q", truth.Freshness.Cause)
	}
	if truth.Freshness.ObservedAt == "" {
		t.Fatalf("expected the watermark to be surfaced as observed_at")
	}
}

// TestApplySearchVectorFreshnessBehindCadenceIsStale proves a watermark older
// than the freshness window is reported stale with the pending_search_vector
// cause, so a lagging search-vector build is attributable rather than served
// as silently fresh.
func TestApplySearchVectorFreshnessBehindCadenceIsStale(t *testing.T) {
	truth := BuildTruthEnvelope(ProfileProduction, semanticSearchCapability, TruthBasisHybrid, "resolved")
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	materializedAt := now.Add(-1 * time.Hour)
	applySearchVectorFreshness(truth, SearchVectorReadyFreshness{
		Signaled:       true,
		Present:        true,
		MaterializedAt: materializedAt,
	}, nil, now)
	if truth.Freshness.State != FreshnessStale {
		t.Fatalf("expected stale state behind the watermark window, got %q", truth.Freshness.State)
	}
	if truth.Freshness.Cause != FreshnessCausePendingSearchVector {
		t.Fatalf("expected pending_search_vector cause, got %q", truth.Freshness.Cause)
	}
}
