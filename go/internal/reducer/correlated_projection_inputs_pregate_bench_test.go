// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// benchPreGateLoader is self-contained (no shared stub fields) so this
// benchmark compiles on the pre-fix tree for before/after comparison.
type benchPreGateLoader struct {
	envelopes []facts.Envelope
	calls     int
	mayHold   bool
}

func (l *benchPreGateLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	l.calls++
	return l.envelopes, nil
}

func (l *benchPreGateLoader) ScopeMayHoldWorkloadCandidates(
	_ context.Context,
	_, _ string,
) (bool, error) {
	return l.mayHold, nil
}

func pregateBenchEnvelopes(files int) []facts.Envelope {
	now := time.Now().UTC()
	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo-bench",
			ScopeID:  "scope-service",
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": "repo-service",
				"name":     "service-repo",
			},
			ObservedAt: now,
		},
	}
	for i := 0; i < files; i++ {
		envelopes = append(envelopes, facts.Envelope{
			FactID:   "fact-file-bench",
			ScopeID:  "scope-service",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-service",
				"language":      "dockerfile",
				"relative_path": "Dockerfile",
				"parsed_file_data": map[string]any{
					"dockerfile_stages": []any{map[string]any{"name": "runtime"}},
				},
			},
			ObservedAt: now,
		})
	}
	return envelopes
}

// BenchmarkWorkloadProjectionDeferredScopeInputs measures one deferred
// workload-projection pass over a 200-file scope while the own generation is
// inactive (#9697): pre-fix it bulk-loads and decodes the whole generation
// before deferring; post-fix the probe sheds the load.
func BenchmarkWorkloadProjectionDeferredScopeInputs(b *testing.B) {
	loader := &benchPreGateLoader{envelopes: pregateBenchEnvelopes(200), mayHold: true}
	inputLoader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:             loader,
		ResolvedLoader:         &stubResolvedRelationshipLoader{},
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{}),
	}
	intent := dockerfileLoaderIntent()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = inputLoader.LoadWorkloadProjectionScopeInputs(context.Background(), intent)
	}
	b.ReportMetric(float64(loader.calls)/float64(b.N), "fact-loads/op")
}
