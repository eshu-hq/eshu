// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func benchPodTemplateEnvelopes(n int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, n)
	for i := 0; i < n; i++ {
		objectID := fmt.Sprintf("object-%d", i)
		envelopes = append(envelopes, facts.Envelope{
			FactKind: facts.KubernetesPodTemplateFactKind,
			FactID:   "fact-" + objectID,
			Payload: map[string]any{
				"object_id":              objectID,
				"cluster_id":             "prod-eks",
				"namespace":              "payments",
				"name":                   fmt.Sprintf("workload-%d", i),
				"uid":                    fmt.Sprintf("11111111-2222-3333-4444-%012d", i),
				"group_version_resource": "apps/v1/deployments",
				"service_account":        "checkout-sa",
				"image_refs":             []any{fmt.Sprintf("registry.example.com/checkout@sha256:%064d", i)},
				"selector":               map[string]string{"app": "checkout"},
				"correlation_anchors":    []any{objectID},
			},
		})
	}
	return envelopes
}

func benchOCIManifestEnvelopes(n int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, n)
	for i := 0; i < n; i++ {
		digest := fmt.Sprintf("sha256:%064d", i)
		envelopes = append(envelopes, facts.Envelope{
			FactKind: facts.OCIImageManifestFactKind,
			FactID:   "fact-" + digest,
			Payload: map[string]any{
				"repository_id": "oci-registry://registry.example.com/checkout",
				"descriptor_id": "oci-descriptor://registry.example.com/checkout@" + digest,
				"digest":        digest,
			},
		})
	}
	return envelopes
}

// BenchmarkExtractKubernetesWorkloadNodeRows measures the bounded O(W)
// projection of pod-template facts into deterministic node rows, the
// reducer-side cost of the live-workload node materialization handler.
func BenchmarkExtractKubernetesWorkloadNodeRows(b *testing.B) {
	envelopes := benchPodTemplateEnvelopes(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if rows := ExtractKubernetesWorkloadNodeRows(envelopes); len(rows) != 5000 {
			b.Fatalf("rows = %d, want 5000", len(rows))
		}
	}
}

// BenchmarkBuildSourceImageDigestJoinIndex measures the bounded source-side
// digest -> node uid index build, the resolver the #388 edge slice uses to
// anchor its source endpoint. The build is O(M) over manifest facts with O(1)
// map inserts; resolution is O(1) per edge, so there is no per-edge round trip.
func BenchmarkBuildSourceImageDigestJoinIndex(b *testing.B) {
	envelopes := benchOCIManifestEnvelopes(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		index := BuildSourceImageDigestJoinIndex(envelopes)
		if index.Len() != 5000 {
			b.Fatalf("index length = %d, want 5000", index.Len())
		}
	}
}
