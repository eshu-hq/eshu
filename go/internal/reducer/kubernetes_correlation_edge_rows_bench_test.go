// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// kubernetesCorrelationEdgeBenchCorpus builds a realistic single-cluster
// generation: workloadCount live pod-template workloads, each running one image
// pinned by a distinct digest, plus the matching active OCI manifest source fact
// per digest (carrying both the registry/repository/digest the classifier reads
// and the descriptor_id the digest->uid join index reads). Every workload thus
// resolves to exactly one RUNS_IMAGE edge.
func kubernetesCorrelationEdgeBenchCorpus(workloadCount int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, workloadCount*2)
	for i := 0; i < workloadCount; i++ {
		repo := fmt.Sprintf("team/svc-%d", i)
		digest := fmt.Sprintf("sha256:%064d", i)
		imageRef := testK8sRegistry + "/" + repo + "@" + digest
		desc := "oci-descriptor://" + testK8sRegistry + "/" + repo + "@" + digest
		envelopes = append(
			envelopes,
			podTemplateFact(
				fmt.Sprintf("pod-%d", i),
				fmt.Sprintf("svc-%d", i),
				fmt.Sprintf("uid-%d", i),
				[]string{imageRef},
				map[string]string{"app": fmt.Sprintf("svc-%d", i)},
				false,
			),
			k8sSourceManifestWithNode(fmt.Sprintf("oci-%d", i), testK8sRegistry, repo, digest, desc, false),
		)
	}
	return envelopes
}

// BenchmarkExtractKubernetesCorrelationEdgeRows measures the bounded reducer-side
// resolution: the pure classifier (O(W·C) over workloads and their containers)
// plus the O(M) digest->uid index build and O(1) per-edge source resolution. There
// is no per-edge graph round trip and no N+1 Cypher — the cost must scale linearly
// with the corpus, proving the Eshu-owned resolution path stays within the #805
// §5.1 bounded-join contract.
func BenchmarkExtractKubernetesCorrelationEdgeRows(b *testing.B) {
	const workloadCount = 5000
	envelopes := kubernetesCorrelationEdgeBenchCorpus(workloadCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _ := ExtractKubernetesCorrelationEdgeRows(envelopes)
		if len(rows) != workloadCount {
			b.Fatalf("len(rows) = %d, want %d", len(rows), workloadCount)
		}
	}
}
