// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// TestStrongestDeadCodeIncomingEdgeMergesPaths proves the per-entity merge of
// the content read-model and graph probes always keeps the strongest incoming
// edge, so a weak edge from one path can never override a strong edge from the
// other (#2719).
func TestStrongestDeadCodeIncomingEdgeMergesPaths(t *testing.T) {
	t.Parallel()

	weak := DeadCodeIncomingEdge{
		MaxConfidence: codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName),
		Method:        codeprovenance.MethodRepoUniqueName,
	}
	strong := DeadCodeIncomingEdge{
		MaxConfidence: codeprovenance.Confidence(codeprovenance.MethodSCIP),
		Method:        codeprovenance.MethodSCIP,
	}

	cases := []struct {
		name      string
		content   map[string]DeadCodeIncomingEdge
		graph     map[string]DeadCodeIncomingEdge
		wantFound bool
		wantConf  float64
	}{
		{"content-only-weak", map[string]DeadCodeIncomingEdge{"e": weak}, nil, true, weak.MaxConfidence},
		{"graph-only-strong", nil, map[string]DeadCodeIncomingEdge{"e": strong}, true, strong.MaxConfidence},
		{"content-weak-graph-strong", map[string]DeadCodeIncomingEdge{"e": weak}, map[string]DeadCodeIncomingEdge{"e": strong}, true, strong.MaxConfidence},
		{"content-strong-graph-weak", map[string]DeadCodeIncomingEdge{"e": strong}, map[string]DeadCodeIncomingEdge{"e": weak}, true, strong.MaxConfidence},
		{"neither", nil, nil, false, 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			edge, found := strongestDeadCodeIncomingEdge(tc.content, tc.graph, "e")
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v", found, tc.wantFound)
			}
			if found && edge.MaxConfidence != tc.wantConf {
				t.Fatalf("MaxConfidence = %v, want %v", edge.MaxConfidence, tc.wantConf)
			}
		})
	}
}
