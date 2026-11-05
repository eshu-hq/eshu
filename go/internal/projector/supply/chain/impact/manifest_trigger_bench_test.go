// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
)

// BenchmarkManifestDependencyTriggerNoDependency measures the #9874 worst
// case: a git generation of only non-dependency content entities, so the
// predicate visits every content_entity row and finds none. The sizes are
// synthetic (see docs/internal/evidence/9874-lockfile-retrigger.md).
func BenchmarkManifestDependencyTriggerNoDependency(b *testing.B) {
	for _, size := range []int{10_000, 50_000, 100_000} {
		lookup := projectorintent.NewFactLookup(nonDependencyVariables(size))
		b.Run(fmt.Sprintf("content_entities=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, ok := BuildSupplyChainImpactReducerIntent("git-repo-1", "gen-1", lookup); ok {
					b.Fatal("queued an intent for a generation with no dependency row")
				}
			}
		})
	}
}

// BenchmarkFactLookupBuildScale is the scale reference for the predicate
// cost: the per-entity work the projector already does once per generation
// when it indexes the same facts for every intent builder.
func BenchmarkFactLookupBuildScale(b *testing.B) {
	for _, size := range []int{10_000, 100_000} {
		envelopes := nonDependencyVariables(size)
		b.Run(fmt.Sprintf("content_entities=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = projectorintent.NewFactLookup(envelopes)
			}
		})
	}
}

// nonDependencyVariables builds size git Variable entities whose config_kind
// is not dependency: the rows the predicate inspects longest before refusing.
func nonDependencyVariables(size int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, size)
	for i := range size {
		envelopes = append(envelopes, facts.Envelope{
			FactID:    fmt.Sprintf("entity-%d", i),
			FactKind:  "content_entity",
			SourceRef: facts.Ref{SourceSystem: "git"},
			Payload: map[string]any{
				"entity_type":     "Variable",
				"entity_name":     fmt.Sprintf("v%d", i),
				"repo_id":         "repository:r_checkout",
				"entity_metadata": map[string]any{"config_kind": "constant"},
			},
		})
	}
	return envelopes
}
