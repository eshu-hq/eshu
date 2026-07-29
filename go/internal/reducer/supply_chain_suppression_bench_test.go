// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// benchmarkSuppressionSet builds a representative per-finding suppression
// batch: 50 suppressions, most adjacent-but-scope-mismatched (the worst case
// for suppressionAdjacent/suppressionScopeMatchesFinding, which must walk
// every scope key before rejecting), one active match. withDeploymentScope
// additionally sets Environment/WorkloadID/ServiceID on every suppression so
// the benchmark also measures the #5466 fields' comparisons on the hot path,
// not just their zero-value fast path.
func benchmarkSuppressionSet(withDeploymentScope bool) []vulnerabilitySuppression {
	const count = 50
	suppressions := make([]vulnerabilitySuppression, 0, count)
	for i := 0; i < count; i++ {
		scope := vulnerabilitySuppressionScope{
			CVEID:         fmt.Sprintf("CVE-2026-%05d", i),
			PackageID:     "pkg:npm/bench-package",
			RepositoryID:  "repo://example/bench",
			SubjectDigest: fmt.Sprintf("sha256:bench-digest-%d", i),
		}
		if withDeploymentScope {
			scope.Environment = "stage"
			scope.WorkloadID = fmt.Sprintf("workload-%d", i)
			scope.ServiceID = fmt.Sprintf("service-%d", i)
		}
		suppressions = append(suppressions, vulnerabilitySuppression{
			SuppressionID: fmt.Sprintf("suppression-%d", i),
			Source:        facts.VulnerabilitySuppressionSourcePolicy,
			Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
			AuthoredAt:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			Scope:         scope,
		})
	}
	// One suppression actually matches the benchmark finding below, so
	// EvaluateSupplyChainSuppression exercises pickPreferredSuppression too.
	scope := vulnerabilitySuppressionScope{
		CVEID:         "CVE-2026-00000",
		PackageID:     "pkg:npm/bench-package",
		RepositoryID:  "repo://example/bench",
		SubjectDigest: "sha256:bench-digest-0",
	}
	if withDeploymentScope {
		scope.Environment = "stage"
		scope.WorkloadID = "workload-0"
		scope.ServiceID = "service-0"
	}
	suppressions[0] = vulnerabilitySuppression{
		SuppressionID: "suppression-0",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Scope:         scope,
	}
	return suppressions
}

// BenchmarkEvaluateSupplyChainSuppression_LegacyScopeOnly measures the matcher
// hot path with a finding shape that predates #5466: no Environments,
// WorkloadIDs, or ServiceIDs evidence, and suppressions that never set the new
// scope keys. This is the identical-input-shape baseline compared against
// origin/main to prove the new scope keys add no measurable cost when unused.
func BenchmarkEvaluateSupplyChainSuppression_LegacyScopeOnly(b *testing.B) {
	finding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-00000",
		PackageID:     "pkg:npm/bench-package",
		RepositoryID:  "repo://example/bench",
		SubjectDigest: "sha256:bench-digest-0",
	}
	suppressions := benchmarkSuppressionSet(false)
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EvaluateSupplyChainSuppression(finding, suppressions, now)
	}
}

// BenchmarkEvaluateSupplyChainSuppression_WithEnvironmentWorkloadServiceScope
// measures the same matcher with every suppression AND the finding exercising
// the new Environment/WorkloadID/ServiceID scope keys (#5466), so it captures
// the added per-comparison cost when the new fields are actually populated,
// not only their zero-value fast path.
func BenchmarkEvaluateSupplyChainSuppression_WithEnvironmentWorkloadServiceScope(b *testing.B) {
	finding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-00000",
		PackageID:     "pkg:npm/bench-package",
		RepositoryID:  "repo://example/bench",
		SubjectDigest: "sha256:bench-digest-0",
		Environments:  []string{"stage"},
		WorkloadIDs:   []string{"workload-0"},
		ServiceIDs:    []string{"service-0"},
		ServiceWorkloadPairs: []SupplyChainServiceWorkloadPair{{
			ServiceID:  "service-0",
			WorkloadID: "workload-0",
		}},
	}
	suppressions := benchmarkSuppressionSet(true)
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	if got, want := EvaluateSupplyChainSuppression(finding, suppressions, now).State, SupplyChainSuppressionStateNotAffected; got != want {
		b.Fatalf("benchmark setup state = %q, want matching state %q", got, want)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EvaluateSupplyChainSuppression(finding, suppressions, now)
	}
}
