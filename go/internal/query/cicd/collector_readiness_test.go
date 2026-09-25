// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// probeErrStore is a configured-probe double that always fails, used to prove a
// non-empty page never consults (and is never downgraded by) a failing probe.
type probeErrStore struct{ calls int }

func (s *probeErrStore) CollectorConfigured(
	context.Context,
	scope.CollectorKind,
) (bool, error) {
	s.calls++
	return false, fmt.Errorf("probe boom")
}

// probeOKStore is a configured-probe double that records whether it was called.
type probeOKStore struct {
	configured bool
	calls      int
}

func (s *probeOKStore) CollectorConfigured(
	context.Context,
	scope.CollectorKind,
) (bool, error) {
	s.calls++
	return s.configured, nil
}

func TestCollectorListReadinessSkipsProbeWhenResultsReturned(t *testing.T) {
	t.Parallel()

	// A non-empty page is demonstrably configured+working, so a probe failure
	// must not downgrade it to readiness_unavailable. The probe must not even be
	// consulted, because rows are already proof the collector ran.
	store := &probeErrStore{}
	env, ok := collectorListReadiness(context.Background(), store, scope.CollectorPackageRegistry, 5, false)
	if !ok {
		t.Fatal("collectorListReadiness ok = false, want true for a wired store")
	}
	if env.State != querycontract.CollectorListReadinessStateReadyWithResults {
		t.Fatalf("state = %q, want %q", env.State, querycontract.CollectorListReadinessStateReadyWithResults)
	}
	if store.calls != 0 {
		t.Fatalf("probe calls = %d, want 0 (non-empty page must skip the probe)", store.calls)
	}
	if env.Counts.ResultsReturned != 5 {
		t.Fatalf("results_returned = %d, want 5", env.Counts.ResultsReturned)
	}
}

func TestCollectorListReadinessProbesOnlyWhenPageEmpty(t *testing.T) {
	t.Parallel()

	// An empty page is ambiguous, so the probe must be consulted to distinguish
	// not_configured from ready_zero_results.
	store := &probeOKStore{configured: true}
	env, ok := collectorListReadiness(context.Background(), store, scope.CollectorSBOMAttestation, 0, false)
	if !ok {
		t.Fatal("collectorListReadiness ok = false, want true for a wired store")
	}
	if env.State != querycontract.CollectorListReadinessStateReadyZeroResults {
		t.Fatalf("state = %q, want %q", env.State, querycontract.CollectorListReadinessStateReadyZeroResults)
	}
	if store.calls != 1 {
		t.Fatalf("probe calls = %d, want 1 (empty page must consult the probe)", store.calls)
	}
}
