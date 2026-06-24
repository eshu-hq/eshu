// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
)

func TestProviderParityEvidenceFromReadinessRequiresFreshEvidence(t *testing.T) {
	readiness := map[string]any{
		"evidence_sources": []any{
			map[string]any{
				"family":     "package.consumption",
				"fact_count": float64(1),
				"freshness":  "stale",
			},
			map[string]any{
				"family":     "vulnerability.advisory",
				"fact_count": float64(1),
				"freshness":  "fresh",
			},
		},
	}

	evidence := providerParityEvidenceFromReadiness(readiness)
	if evidence.HasDependency {
		t.Fatalf("stale package.consumption evidence counted as fresh dependency evidence")
	}
	if !evidence.HasAdvisory {
		t.Fatalf("fresh vulnerability.advisory evidence was not counted")
	}
}
