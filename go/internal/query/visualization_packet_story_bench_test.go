// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"testing"
)

func BenchmarkBuildServiceStoryVisualizationPacketRetainedShape(b *testing.B) {
	response := benchmarkServiceStoryPacketResponse()
	truth := freshTruth()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = BuildServiceStoryVisualizationPacket(response, truth)
	}
}

func benchmarkServiceStoryPacketResponse() map[string]any {
	const repositoryCount = 37
	nodes := make([]map[string]any, 0, repositoryCount+1)
	nodes = append(nodes, map[string]any{
		"id":       "workload:api-node-boats",
		"label":    "api-node-boats",
		"kind":     "service",
		"category": "service",
	})
	for index := range repositoryCount {
		nodes = append(nodes, map[string]any{
			"id":       fmt.Sprintf("repository:r_%02d", index),
			"label":    fmt.Sprintf("repository-%02d", index),
			"kind":     "repository",
			"category": "upstream",
		})
	}

	edges := make([]map[string]any, 0, 56)
	for index := range 56 {
		edges = append(edges, map[string]any{
			"source":            fmt.Sprintf("repository:r_%02d", index%repositoryCount),
			"target":            "repository:r_service",
			"relationship_type": "DEPLOYS_FROM",
			"confidence":        0.9,
		})
	}
	return map[string]any{
		"service_identity": map[string]any{
			"service_id":   "workload:api-node-boats",
			"service_name": "api-node-boats",
			"repo_id":      "repository:r_service",
		},
		"evidence_graph": map[string]any{
			"nodes": nodes,
			"edges": edges,
		},
		"upstream_dependencies": []map[string]any{},
		"downstream_consumers":  map[string]any{},
	}
}
