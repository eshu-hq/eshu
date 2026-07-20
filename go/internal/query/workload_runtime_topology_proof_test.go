// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func boundedTopologyProofEdges() []map[string]any {
	edges := make([]map[string]any, 0, contextStoryItemLimit*2)
	for index := range contextStoryItemLimit {
		instanceID := fmt.Sprintf("instance:%03d", index)
		edges = append(edges,
			observedTopologyEdge(
				"DEFINES", "repository:orders", "orders", fmt.Sprintf("workload:%03d", index), "orders",
				map[string]any{"confidence": 0.99, "source_fact_id": fmt.Sprintf("fact:defines:%03d", index)},
			),
			observedTopologyEdge(
				"INSTANCE_OF", instanceID, "", fmt.Sprintf("workload:%03d", index), "orders",
				map[string]any{"confidence": 0.94, "source_fact_id": fmt.Sprintf("fact:instance:%03d", index)},
			),
		)
	}
	return edges
}

func appendUniqueTopologyEdgeLegacy(rows *[]map[string]any, seen map[string]int, edge map[string]any) {
	key := topologyEdgeKey(edge)
	if key == "" {
		return
	}
	if index, exists := seen[key]; exists {
		candidate, _ := json.Marshal(mapValue(edge, "properties"))
		existing, _ := json.Marshal(mapValue((*rows)[index], "properties"))
		if string(candidate) < string(existing) {
			(*rows)[index] = edge
		}
		return
	}
	seen[key] = len(*rows)
	*rows = append(*rows, edge)
}

func selectBoundedTopologyLegacy(edges []map[string]any) []map[string]any {
	rows := make([]map[string]any, 0, len(edges))
	seen := make(map[string]int, len(edges))
	for _, edge := range edges {
		appendUniqueTopologyEdgeLegacy(&rows, seen, edge)
	}
	sortTopologyEdges(rows)
	return rows
}

func selectBoundedTopologyFailClosed(edges []map[string]any) ([]map[string]any, error) {
	rows := make([]map[string]any, 0, len(edges))
	seen := make(map[string]int, len(edges))
	for _, edge := range edges {
		if err := appendUniqueTopologyEdge(&rows, seen, edge); err != nil {
			return nil, err
		}
	}
	sortTopologyEdges(rows)
	return rows, nil
}

func TestBoundedTopologyFailClosedOutputEquivalence(t *testing.T) {
	edges := boundedTopologyProofEdges()
	legacy := selectBoundedTopologyLegacy(edges)
	failClosed, err := selectBoundedTopologyFailClosed(edges)
	if err != nil {
		t.Fatalf("selectBoundedTopologyFailClosed() error = %v", err)
	}
	if !reflect.DeepEqual(failClosed, legacy) {
		t.Fatalf("fail-closed output differs from legacy output")
	}
	encoded, err := json.Marshal(failClosed)
	if err != nil {
		t.Fatalf("json.Marshal(failClosed) error = %v", err)
	}
	digest := sha256.Sum256(encoded)
	t.Logf("bounded topology rows=%d sha256=%s", len(failClosed), hex.EncodeToString(digest[:]))
}

func BenchmarkAppendUniqueTopologyEdgeBounded(b *testing.B) {
	edges := boundedTopologyProofEdges()
	b.Run("legacy", func(b *testing.B) {
		for range b.N {
			_ = selectBoundedTopologyLegacy(edges)
		}
	})
	b.Run("fail_closed", func(b *testing.B) {
		for range b.N {
			if _, err := selectBoundedTopologyFailClosed(edges); err != nil {
				b.Fatal(err)
			}
		}
	})
}
