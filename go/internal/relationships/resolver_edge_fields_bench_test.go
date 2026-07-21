// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"fmt"
	"testing"
)

// BenchmarkResolveCandidateAggregation measures Resolve's candidate
// aggregation path (buildCandidates -> aggregateCandidate) on a repo-scale
// shape: 2000 distinct (source, target, relationship type) candidates, each
// backed by 5 evidence facts -- the P1-2 finding from #5441 review round 2.
// aggregateCandidate is a repo-scale hot path (it runs once per resolver
// pass over every candidate's evidence facts), and the #5441 P0 fix added
// three evidenceFieldWinner.consider() calls plus three per-fact Details
// reads inside its main loop; this benchmark isolates that cost the same way
// edge_writer_repo_dependency_bench_test.go isolates the graph-writer side.
//
// Each fact carries both source_revision and first_party_ref_version (via
// source_ref, forcing the ExtractTerraformRefPin fallback path) so every new
// per-fact call in aggregateCandidate does real work, not an early-exit on an
// absent key -- the worst case for the #5441 fields, not the common case.
func BenchmarkResolveCandidateAggregation(b *testing.B) {
	const candidateCount = 2000
	const factsPerCandidate = 5

	facts := make([]EvidenceFact, 0, candidateCount*factsPerCandidate)
	for i := 0; i < candidateCount; i++ {
		srcRepo := fmt.Sprintf("repo-src-%04d", i)
		tgtRepo := fmt.Sprintf("repo-tgt-%04d", i)
		for j := 0; j < factsPerCandidate; j++ {
			facts = append(facts, EvidenceFact{
				EvidenceKind:     EvidenceKindArgoCDAppSource,
				RelationshipType: RelDeploysFrom,
				SourceRepoID:     srcRepo,
				TargetRepoID:     tgtRepo,
				Confidence:       0.80 + float64(j)*0.02,
				Rationale:        fmt.Sprintf("evidence %d for %s->%s", j, srcRepo, tgtRepo),
				Details: map[string]any{
					"argocd_application_name": fmt.Sprintf("app-%04d", i),
					"source_revision":         "main",
					"source_ref":              "git::https://example.test/org/mod.git?ref=v1.0.0",
				},
			})
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, resolved := Resolve(facts, nil, DefaultConfidenceThreshold)
		if len(resolved) != candidateCount {
			b.Fatalf("resolved = %d, want %d", len(resolved), candidateCount)
		}
	}
}
