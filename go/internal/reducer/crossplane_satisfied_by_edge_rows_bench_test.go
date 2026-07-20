// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// benchmarkCrossplaneSatisfiedByCorpus builds a synthetic k8s-heavy content_entity
// corpus: k8sResourceCount generic K8sResource rows (ordinary Deployments/
// Services/ConfigMaps, never a Claim) plus claimPairCount real Claim/XRD pairs,
// each pair a distinct (group, kind) so every Claim resolves to exactly one
// XRD. This mirrors the widest realistic candidate set the correlation scans
// (every generic k8s_resources row in a generation, per the doc's No-Regression
// note), not just the matching subset.
func benchmarkCrossplaneSatisfiedByCorpus(k8sResourceCount, claimPairCount int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, k8sResourceCount+claimPairCount*2)
	for i := 0; i < k8sResourceCount; i++ {
		envelopes = append(envelopes, crossplaneContentEntityEnvelope(
			fmt.Sprintf("k8s:noise-%d", i), crossplaneEntityTypeK8sResource, map[string]any{
				"api_version": "apps/v1",
				"kind":        "Deployment",
			},
		))
	}
	for i := 0; i < claimPairCount; i++ {
		group := fmt.Sprintf("bench-%d.acme.internal", i)
		kind := fmt.Sprintf("AcmeResource%d", i)
		envelopes = append(
			envelopes,
			crossplaneContentEntityEnvelope(fmt.Sprintf("k8s:claim-%d", i), crossplaneEntityTypeK8sResource, map[string]any{
				"api_version": group + "/v1alpha1",
				"kind":        kind,
			}),
			crossplaneContentEntityEnvelope(fmt.Sprintf("xrd:%d", i), crossplaneEntityTypeXRD, map[string]any{
				"group":      group,
				"claim_kind": kind,
			}),
		)
	}
	return envelopes
}

// BenchmarkExtractCrossplaneSatisfiedByEdgeRows is the No-Regression Evidence
// benchmark for issue #5347's O(n) hash-join claim (documented in
// docs/public/reference/http-api/iac-content-infra.md): it measures
// ExtractCrossplaneSatisfiedByEdgeRows over a 5,000-K8sResource, 50-Claim/XRD-pair
// corpus (5,100 candidates total) -- a k8s-heavy scope wider than the matching
// subset, proving the single-pass hash join does not degrade with noise volume.
func BenchmarkExtractCrossplaneSatisfiedByEdgeRows(b *testing.B) {
	envelopes := benchmarkCrossplaneSatisfiedByCorpus(5000, 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, tally, err := ExtractCrossplaneSatisfiedByEdgeRows(envelopes)
		if err != nil {
			b.Fatalf("ExtractCrossplaneSatisfiedByEdgeRows() error = %v", err)
		}
		if len(rows) != 50 {
			b.Fatalf("len(rows) = %d, want 50", len(rows))
		}
		if tally.ambiguousSkipped != 0 {
			b.Fatalf("tally.ambiguousSkipped = %d, want 0", tally.ambiguousSkipped)
		}
	}
}
