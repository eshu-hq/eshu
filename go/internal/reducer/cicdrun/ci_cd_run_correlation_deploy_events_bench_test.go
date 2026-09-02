// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// benchmarkCICDDeploymentEventCorpus builds runCount ci.run facts that all
// share ONE repository and ONE head sha, plus eventCount ci.deployment_event
// facts carrying that same sha.
//
// This is the worst case for attachDeploymentEventsToRuns: it joins on sha
// equality with no index, so every event is compared against every run and
// every run receives every event. A corpus where each run has its own sha
// would be linear; this one is the quadratic shape, which is the number worth
// knowing.
func benchmarkCICDDeploymentEventCorpus(runCount, eventCount int) []facts.Envelope {
	const (
		repositoryID = "repository:r_bench_shared"
		sharedSHA    = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"
	)

	envelopes := make([]facts.Envelope, 0, runCount+eventCount)
	for i := range runCount {
		runID := fmt.Sprintf("run-%d", i)
		envelopes = append(envelopes, facts.Envelope{
			FactID:   runID,
			FactKind: facts.CICDRunFactKind,
			Payload: map[string]any{
				"provider":      "github_actions",
				"run_id":        runID,
				"run_attempt":   "1",
				"commit_sha":    sharedSHA,
				"repository_id": repositoryID,
			},
		})
	}
	for i := range eventCount {
		envelopes = append(envelopes, facts.Envelope{
			FactID:   fmt.Sprintf("deployment-%d", i),
			FactKind: facts.CICDDeploymentEventFactKind,
			Payload: map[string]any{
				"provider":      "github_actions",
				"deployment_id": fmt.Sprintf("%d", i/3),
				"status_id":     fmt.Sprintf("%d", i),
				"environment":   "production",
				"sha":           sharedSHA,
				"state":         []string{"pending", "in_progress", "success"}[i%3],
			},
		})
	}
	return envelopes
}

// BenchmarkBuildCICDRunCorrelationDecisionsSharedSHADeploymentEvents measures
// the deploy-event attach path (#5425) on the shared-sha worst case: 1,000 runs
// and 300 deployment events on one repository and one commit.
//
// It is the counterpart to BenchmarkBuildCICDRunCorrelationDecisions, which
// carries no deployment events and therefore measures only the cost of the new
// code being present rather than the cost of it doing work.
func BenchmarkBuildCICDRunCorrelationDecisionsSharedSHADeploymentEvents(b *testing.B) {
	const (
		runCount   = 1000
		eventCount = 300
	)
	envelopes := benchmarkCICDDeploymentEventCorpus(runCount, eventCount)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		decisions := BuildCICDRunCorrelationDecisions(envelopes)
		if len(decisions) != runCount {
			b.Fatalf("len(decisions) = %d, want %d", len(decisions), runCount)
		}
	}
}
