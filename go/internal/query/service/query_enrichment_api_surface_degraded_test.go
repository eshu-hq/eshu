// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
)

// enrichServiceQueryContextAPISurfaceLimitations runs the real
// EnrichServiceQueryContextWithOptions (query_enrichment.go around lines
// 64-75) over a graph whose EXPOSES_ENDPOINT count read either fails with
// apiSurfaceErr or succeeds with zero endpoints, and returns the resulting
// workload context's limitations slice. It reuses
// provisioningTruncationWorkload from query_truncation_wiring_test.go so this
// site is exercised with the same production-shaped fixture the sibling
// truncation regressions already use.
func enrichServiceQueryContextAPISurfaceLimitations(t *testing.T, apiSurfaceErr error) []string {
	t.Helper()

	workloadContext := provisioningTruncationWorkload()
	graph := querytestutil.FakeWorkloadGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "RETURN count(endpoint) AS endpoint_count") {
				if apiSurfaceErr != nil {
					return nil, apiSurfaceErr
				}
				return []map[string]any{{"endpoint_count": int64(0)}}, nil
			}
			return nil, nil
		},
	}
	if err := EnrichServiceQueryContextWithOptions(
		context.Background(),
		graph,
		querytestutil.FakePortContentStore{},
		workloadContext,
		QueryEnrichmentOptions{
			IncludeRelatedModuleUsage: true,
			Operation:                 "service_context",
		},
	); err != nil {
		t.Fatalf("EnrichServiceQueryContextWithOptions() error = %v, want nil", err)
	}
	return querycontract.StringSliceVal(workloadContext, "limitations")
}

// TestEnrichServiceQueryContextReportsAPISurfaceReadDegraded is the #6810
// regression for queryServiceGraphAPISurface's caller in
// EnrichServiceQueryContextWithOptions (query_enrichment.go around lines
// 64-75): a failed API-surface count read must surface as
// api_surface_read_degraded on the workload context's limitations instead of
// rendering an authoritative empty api_surface panel.
func TestEnrichServiceQueryContextReportsAPISurfaceReadDegraded(t *testing.T) {
	t.Parallel()

	limitations := enrichServiceQueryContextAPISurfaceLimitations(t, errors.New("graph query exceeded its deadline"))
	found := false
	for _, reason := range limitations {
		if reason == repository.APISurfaceReadDegradedReason {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("limitations = %#v, want %q", limitations, repository.APISurfaceReadDegradedReason)
	}
}

// TestEnrichServiceQueryContextHealthyAPISurfaceReadAddsNoReason is the other
// half: a healthy read with zero endpoints is a true empty answer and must
// not add the reason.
func TestEnrichServiceQueryContextHealthyAPISurfaceReadAddsNoReason(t *testing.T) {
	t.Parallel()

	limitations := enrichServiceQueryContextAPISurfaceLimitations(t, nil)
	for _, reason := range limitations {
		if reason == repository.APISurfaceReadDegradedReason {
			t.Fatalf("limitations = %#v, want no %q for a healthy empty read", limitations, repository.APISurfaceReadDegradedReason)
		}
	}
}
