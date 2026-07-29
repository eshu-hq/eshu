// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// benchmarkLegacySuppressionSet preserves the exact pre-#5466 input shape used
// for the same-shape base/current comparison: 50 suppressions with no
// deployment scope, most rejected on legacy identity fields, and one match.
func benchmarkLegacySuppressionSet() []vulnerabilitySuppression {
	const count = 50
	suppressions := make([]vulnerabilitySuppression, 0, count)
	for i := 0; i < count; i++ {
		scope := vulnerabilitySuppressionScope{
			CVEID:         fmt.Sprintf("CVE-2026-%05d", i),
			PackageID:     "pkg:npm/bench-package",
			RepositoryID:  "repo://example/bench",
			SubjectDigest: fmt.Sprintf("sha256:bench-digest-%d", i),
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
	suppressions[0] = vulnerabilitySuppression{
		SuppressionID: "suppression-0",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Scope:         scope,
	}
	return suppressions
}

// benchmarkDeploymentSuppressionSet builds count identity-adjacent candidates.
// Every candidate matches all legacy identity anchors and reaches the #5466
// deployment checks. Candidate zero matches the genuine service/workload pair;
// the rest fail only when that correlated deployment pair is checked.
func benchmarkDeploymentSuppressionSet(count int) []vulnerabilitySuppression {
	suppressions := make([]vulnerabilitySuppression, 0, count)
	for i := 0; i < count; i++ {
		suppressions = append(suppressions, vulnerabilitySuppression{
			SuppressionID: fmt.Sprintf("suppression-%d", i),
			Source:        facts.VulnerabilitySuppressionSourcePolicy,
			Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
			AuthoredAt:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			Scope: vulnerabilitySuppressionScope{
				CVEID:         "CVE-2026-00000",
				PackageID:     "pkg:npm/bench-package",
				RepositoryID:  "repo://example/bench",
				SubjectDigest: "sha256:bench-digest-0",
				Environment:   "stage",
				WorkloadID:    fmt.Sprintf("workload-%d", i),
				ServiceID:     fmt.Sprintf("service-%d", i),
			},
		})
	}
	return suppressions
}

func benchmarkDeploymentFinding() SupplyChainImpactFinding {
	return SupplyChainImpactFinding{
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
	suppressions := benchmarkLegacySuppressionSet()
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EvaluateSupplyChainSuppression(finding, suppressions, now)
	}
}

// BenchmarkEvaluateSupplyChainSuppression_WithEnvironmentWorkloadServiceScope
// measures 50 identity-adjacent candidates. Every candidate reaches the new
// Environment/WorkloadID/ServiceID checks; 49 reject on their unverified
// service/workload pair and one produces the asserted matching decision.
func BenchmarkEvaluateSupplyChainSuppression_WithEnvironmentWorkloadServiceScope(b *testing.B) {
	finding := benchmarkDeploymentFinding()
	suppressions := benchmarkDeploymentSuppressionSet(50)
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

// BenchmarkEvaluateSupplyChainSuppression_AtCandidateCap measures the bounded
// worst-case retained set: 2,000 identity-adjacent candidates that all reach
// correlated deployment-pair evaluation before one winner is selected.
func BenchmarkEvaluateSupplyChainSuppression_AtCandidateCap(b *testing.B) {
	finding := benchmarkDeploymentFinding()
	suppressions := benchmarkDeploymentSuppressionSet(2000)
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
