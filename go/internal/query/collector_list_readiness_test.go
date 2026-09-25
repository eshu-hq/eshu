// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildCollectorListReadinessNotConfigured(t *testing.T) {
	t.Parallel()

	env := BuildCollectorListReadiness(scope.CollectorPackageRegistry, 0, false, false)
	if env.State != CollectorListReadinessStateNotConfigured {
		t.Fatalf("state = %q, want %q", env.State, CollectorListReadinessStateNotConfigured)
	}
	if env.CollectorKind != string(scope.CollectorPackageRegistry) {
		t.Fatalf("collector_kind = %q, want %q", env.CollectorKind, scope.CollectorPackageRegistry)
	}
	if env.Counts.ResultsReturned != 0 || env.Counts.ResultsTruncated {
		t.Fatalf("counts = %+v, want zero", env.Counts)
	}
}

func TestBuildCollectorListReadinessReadyZeroResults(t *testing.T) {
	t.Parallel()

	env := BuildCollectorListReadiness(scope.CollectorSBOMAttestation, 0, false, true)
	if env.State != CollectorListReadinessStateReadyZeroResults {
		t.Fatalf("state = %q, want %q", env.State, CollectorListReadinessStateReadyZeroResults)
	}
}

func TestBuildCollectorListReadinessReadyWithResults(t *testing.T) {
	t.Parallel()

	// A non-empty page is ready regardless of the configured probe: rows are
	// proof the collector ran, so a stale/failed probe never downgrades a page
	// that already carries collector evidence.
	env := BuildCollectorListReadiness(scope.CollectorCICDRun, 3, true, false)
	if env.State != CollectorListReadinessStateReadyWithResults {
		t.Fatalf("state = %q, want %q", env.State, CollectorListReadinessStateReadyWithResults)
	}
	if env.Counts.ResultsReturned != 3 || !env.Counts.ResultsTruncated {
		t.Fatalf("counts = %+v, want {3,true}", env.Counts)
	}
}

// The attach-step state-machine tests moved to internal/query/cicd with the
// last package-query handler that used the root copy (#6642): the root
// attachCollectorListReadiness/collectorListReadiness pair is deleted, and
// the cicd family's behavior-identical copy carries
// TestCollectorListReadinessSkipsProbeWhenResultsReturned and
// TestCollectorListReadinessProbesOnlyWhenPageEmpty.

func TestBuildCollectorListReadinessUnavailable(t *testing.T) {
	t.Parallel()

	env := BuildCollectorListReadinessUnavailable(scope.CollectorOCIRegistry, 0, false)
	if env.State != CollectorListReadinessStateReadinessUnavailable {
		t.Fatalf("state = %q, want %q", env.State, CollectorListReadinessStateReadinessUnavailable)
	}
	if env.CollectorKind != string(scope.CollectorOCIRegistry) {
		t.Fatalf("collector_kind = %q, want %q", env.CollectorKind, scope.CollectorOCIRegistry)
	}
}
