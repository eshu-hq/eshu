// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// gitManifestProjectionFacts builds a git generation of one file fact plus
// pins dependency rows and one non-dependency Variable entity.
func gitManifestProjectionFacts(scopeID, generationID string, pins int) []facts.Envelope {
	envelopes := []facts.Envelope{{
		FactID: "file-lock", ScopeID: scopeID, GenerationID: generationID, FactKind: "file",
		SourceRef: facts.Ref{SourceSystem: "git"},
		Payload:   map[string]any{"relative_path": "package-lock.json", "repo_id": "repository:r_checkout"},
	}, {
		FactID: "var-plain", ScopeID: scopeID, GenerationID: generationID, FactKind: "content_entity",
		SourceRef: facts.Ref{SourceSystem: "git"},
		Payload: map[string]any{
			"entity_type": "Variable", "entity_name": "timeout", "repo_id": "repository:r_checkout",
			"relative_path": "main.go",
		},
	}}
	for i := range pins {
		envelopes = append(envelopes, facts.Envelope{
			FactID:       fmt.Sprintf("dep-%03d", i),
			ScopeID:      scopeID,
			GenerationID: generationID,
			FactKind:     "content_entity",
			SourceRef:    facts.Ref{SourceSystem: "git"},
			Payload: map[string]any{
				"entity_type": "Variable", "entity_name": fmt.Sprintf("pkg-%03d", i),
				"repo_id": "repository:r_checkout", "relative_path": "package-lock.json",
				"entity_metadata": map[string]any{
					"config_kind": "dependency", "package_manager": "npm", "value": "1.0.0",
				},
			},
		})
	}
	return envelopes
}

func supplyChainImpactIntents(intents []ReducerIntent) []ReducerIntent {
	var out []ReducerIntent
	for _, intent := range intents {
		if intent.Domain == reducer.DomainSupplyChainImpact {
			out = append(out, intent)
		}
	}
	return out
}

// TestBuildProjectionQueuesOneSupplyChainImpactForGitManifestChange is the
// #9874 dispatcher regression: a git generation whose only changed facts are
// 500 dependency pins enqueues exactly one supply_chain_impact intent (the
// fan-out bound), and it carries the manifest marker.
func TestBuildProjectionQueuesOneSupplyChainImpactForGitManifestChange(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "git-repo-checkout", SourceSystem: "git"}
	generation := scope.ScopeGeneration{ScopeID: scopeValue.ScopeID, GenerationID: "generation-delta-2"}
	projection, err := buildProjection(scopeValue, generation,
		gitManifestProjectionFacts(scopeValue.ScopeID, generation.GenerationID, 500))
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	impacts := supplyChainImpactIntents(projection.reducerIntents)
	if len(impacts) != 1 {
		t.Fatalf("supply_chain_impact intents = %d, want exactly 1 for a 500-pin push", len(impacts))
	}
	if got, want := impacts[0].EntityKey, "supply_chain_impact:"+scopeValue.ScopeID; got != want {
		t.Fatalf("EntityKey = %q, want %q", got, want)
	}
	if got := impacts[0].Payload["trigger"]; got != "manifest_dependency" {
		t.Fatalf("Payload[trigger] = %v, want manifest_dependency", got)
	}
}

// TestBuildProjectionSkipsSupplyChainImpactForGitGenerationWithoutDependency
// proves a git generation that changed only code (no dependency row) still
// enqueues no impact intent.
func TestBuildProjectionSkipsSupplyChainImpactForGitGenerationWithoutDependency(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "git-repo-checkout", SourceSystem: "git"}
	generation := scope.ScopeGeneration{ScopeID: scopeValue.ScopeID, GenerationID: "generation-delta-3"}
	projection, err := buildProjection(scopeValue, generation,
		gitManifestProjectionFacts(scopeValue.ScopeID, generation.GenerationID, 0))
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	if impacts := supplyChainImpactIntents(projection.reducerIntents); len(impacts) != 0 {
		t.Fatalf("buildProjection() queued %#v for a git generation with no dependency row", impacts)
	}
}

// TestBuildProjectionSkipsSupplyChainImpactForDeltaThatTouchedNoManifest pins
// the delta-union boundary for the #9874 marker: a delta generation that
// changed only code projects over its base full generation's facts, and those
// base rows include every dependency row of the repository. They must not
// trigger the marker, because the delta changed no manifest. A sibling delta
// whose own facts hold one dependency row enqueues one intent anchored on it.
func TestBuildProjectionSkipsSupplyChainImpactForDeltaThatTouchedNoManifest(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "git-repo-checkout", SourceSystem: "git"}
	generation := scope.ScopeGeneration{
		ScopeID: scopeValue.ScopeID, GenerationID: "generation-delta-4",
		DeltaUnionBaseGenerationID: "generation-full-1",
	}
	base := gitManifestProjectionFacts(scopeValue.ScopeID, "generation-full-1", 20)
	deltaRepository := facts.Envelope{
		FactID: "repo-delta-4", ScopeID: scopeValue.ScopeID, GenerationID: generation.GenerationID,
		FactKind: "repository", SourceRef: facts.Ref{SourceSystem: "git"},
		Payload: map[string]any{
			"repo_id": "repository:r_checkout", "graph_id": "repository:r_checkout", "graph_kind": "repository",
			"name": "checkout", "delta_generation": true,
			"delta_relative_paths": []any{"main.go"}, "delta_deleted_relative_paths": []any{},
		},
	}
	codeOnly := append(append([]facts.Envelope{}, base...), deltaRepository)
	projection, err := buildProjection(scopeValue, generation, codeOnly)
	if err != nil {
		t.Fatalf("buildProjection(code-only delta) error = %v, want nil", err)
	}
	if impacts := supplyChainImpactIntents(projection.reducerIntents); len(impacts) != 0 {
		t.Fatalf("code-only delta queued %#v from its union base rows", impacts)
	}

	changed := gitManifestProjectionFacts(scopeValue.ScopeID, generation.GenerationID, 1)[2]
	changed.FactID = "dep-delta-changed"
	withManifest := append(append([]facts.Envelope{}, codeOnly...), changed)
	projection, err = buildProjection(scopeValue, generation, withManifest)
	if err != nil {
		t.Fatalf("buildProjection(manifest delta) error = %v, want nil", err)
	}
	impacts := supplyChainImpactIntents(projection.reducerIntents)
	if len(impacts) != 1 || impacts[0].FactID != "dep-delta-changed" {
		t.Fatalf("manifest delta intents = %#v, want one anchored on dep-delta-changed", impacts)
	}
}
