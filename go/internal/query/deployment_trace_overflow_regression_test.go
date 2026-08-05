// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// searchCallCountingContentStore wraps fakePortContentStore and records how
// many distinct SearchFileContentAnyRepoExactCase calls it receives. Every
// hostname search in searchConsumerEvidenceAnyRepo runs in its own goroutine
// (deployment_trace_consumer_search.go), so the counter must be safe for
// concurrent use.
type searchCallCountingContentStore struct {
	fakePortContentStore

	mu    sync.Mutex
	calls int
}

func (s *searchCallCountingContentStore) SearchFileContentAnyRepoExactCase(_ context.Context, _ string, _ int) ([]FileContent, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return nil, nil
}

func (s *searchCallCountingContentStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// TestBoundedTraceEnrichmentLimitClampsOverflowingMaxDepthEndToEnd is the
// #5720 P2-3 regression. An unvalidated `max_depth` large enough that
// boundedTraceEnrichmentLimit's `maxDepth * 10` multiply overflows int64
// (e.g. max_depth=922337203685477581) previously produced a negative limit
// that reached loadConsumerRepositoryEnrichmentFromCandidates unclamped.
// `if limit > 0` there gates two independent protections
// (deployment_trace_candidate_enrichment.go): the
// boundedIndirectEvidenceHostnamesForService call, which is the only thing that
// narrows an arbitrarily large hostname list down to that function's fixed
// indirectEvidenceHostnameLimit (4) before firing concurrent content
// searches, and the final `consumers[:limit]` truncation. A negative
// limit silently skipped both, so a single overflowing max_depth could fan
// out one content search per offered hostname and return an unbounded
// consumer list.
//
// This test drives the real overflow value through the real
// boundedTraceEnrichmentLimit and the real
// loadConsumerRepositoryEnrichmentFromCandidates and proves both bounds
// still apply after the fix: at most indirectEvidenceHostnameLimit hostname
// searches ever fire despite far more hostnames being offered, and the
// merged consumer list is truncated to maxIndirectEvidenceSearchLimit
// despite far more graph candidates being offered.
func TestBoundedTraceEnrichmentLimitClampsOverflowingMaxDepthEndToEnd(t *testing.T) {
	t.Parallel()

	const overflowMaxDepth = 922337203685477581 // maxDepth*10 wraps int64 negative
	limit := boundedTraceEnrichmentLimit(overflowMaxDepth)
	if limit <= 0 || limit > maxIndirectEvidenceSearchLimit {
		t.Fatalf("boundedTraceEnrichmentLimit(%d) = %d, want a value in (0, %d]", overflowMaxDepth, limit, maxIndirectEvidenceSearchLimit)
	}

	candidateCount := maxIndirectEvidenceSearchLimit + 50
	candidates := make([]provisioningRepositoryCandidate, 0, candidateCount)
	for i := 0; i < candidateCount; i++ {
		candidates = append(candidates, provisioningRepositoryCandidate{
			RepoID:            fmt.Sprintf("repository:candidate-%03d", i),
			RepoName:          fmt.Sprintf("candidate-%03d", i),
			RelationshipTypes: []string{"USES_MODULE"},
		})
	}

	hostnameCount := maxIndirectEvidenceSearchLimit + 50
	hostnames := make([]string, 0, hostnameCount)
	for i := 0; i < hostnameCount; i++ {
		hostnames = append(hostnames, fmt.Sprintf("svc-%03d.qa.example.test", i))
	}

	content := &searchCallCountingContentStore{}

	got, _, err := loadConsumerRepositoryEnrichmentFromCandidates(
		context.Background(),
		nil, // graph: backfillConsumerRepositoryDisplayNames no-ops on a nil graph
		content,
		"repo-sample-service-api",
		"", // empty service name isolates the assertion to hostname searches only
		hostnames,
		limit,
		candidates,
		false,
		false,
	)
	if err != nil {
		t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
	}

	if got, want := content.callCount(), indirectEvidenceHostnameLimit; got != want {
		t.Fatalf("hostname content searches fired = %d, want %d (hostname-affinity bounding must still cap the fan-out after an overflowing max_depth)", got, want)
	}
	if gotLen, wantLen := len(got), maxIndirectEvidenceSearchLimit; gotLen != wantLen {
		t.Fatalf("len(consumers) = %d, want %d (final consumers[:limit] truncation must still apply after an overflowing max_depth)", gotLen, wantLen)
	}
}
