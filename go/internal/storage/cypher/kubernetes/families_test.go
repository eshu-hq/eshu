// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetes

import (
	"strings"
	"testing"
)

// TestKubernetesNodeWritersLiveInLeaf pins the #6694 home of the two root
// Kubernetes node writers: reducer constructs them from this leaf (see
// go/cmd/reducer/canonical_graph_writers.go), so both constructors must
// resolve here and every Cypher template the edge/materialized sibling reads
// must stay exported and non-empty.
func TestKubernetesNodeWritersLiveInLeaf(t *testing.T) {
	t.Parallel()

	if got := NewNamespaceNodeWriter(nil, 0); got == nil {
		t.Error("NewNamespaceNodeWriter returned nil")
	}
	if got := NewWorkloadNodeWriter(nil, 0); got == nil {
		t.Error("NewWorkloadNodeWriter returned nil")
	}
	for name, template := range map[string]string{
		"CanonicalKubernetesNamespaceUpsertCypher":                CanonicalKubernetesNamespaceUpsertCypher,
		"CanonicalKubernetesNamespaceWithEnvironmentUpsertCypher": CanonicalKubernetesNamespaceWithEnvironmentUpsertCypher,
		"RetractKubernetesNamespaceStaleTargetsEnvironmentCypher": RetractKubernetesNamespaceStaleTargetsEnvironmentCypher,
	} {
		if template == "" {
			t.Errorf("%s is empty", name)
		}
		if !strings.Contains(template, "UNWIND $rows AS row") {
			t.Errorf("%s lost its UNWIND batching shape", name)
		}
	}
}
