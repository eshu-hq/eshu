// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

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

	weak := deadCodeIncomingEdge{
		MaxConfidence: codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName),
		Method:        codeprovenance.MethodRepoUniqueName,
	}
	strong := deadCodeIncomingEdge{
		MaxConfidence: codeprovenance.Confidence(codeprovenance.MethodSCIP),
		Method:        codeprovenance.MethodSCIP,
	}

	cases := []struct {
		name      string
		content   map[string]deadCodeIncomingEdge
		graph     map[string]deadCodeIncomingEdge
		wantFound bool
		wantConf  float64
	}{
		{"content-only-weak", map[string]deadCodeIncomingEdge{"e": weak}, nil, true, weak.MaxConfidence},
		{"graph-only-strong", nil, map[string]deadCodeIncomingEdge{"e": strong}, true, strong.MaxConfidence},
		{"content-weak-graph-strong", map[string]deadCodeIncomingEdge{"e": weak}, map[string]deadCodeIncomingEdge{"e": strong}, true, strong.MaxConfidence},
		{"content-strong-graph-weak", map[string]deadCodeIncomingEdge{"e": strong}, map[string]deadCodeIncomingEdge{"e": weak}, true, strong.MaxConfidence},
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
