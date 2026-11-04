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

// TestBuildProjectionQueuesOneMarkerForDeltaThatTouchedNoManifest pins the
// #9874 F2 ruling at the dispatcher, and the delta-union boundary: a delta that
// changed only code projects over its base full generation's facts, whose
// dependency rows must not anchor anything, yet the delta still raises one
// marker anchored on its own repository fact (the reducer then finds nothing
// changed and reopens nothing). A sibling delta whose own facts hold a
// dependency row anchors on that row instead.
func TestBuildProjectionQueuesOneMarkerForDeltaThatTouchedNoManifest(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "git-repo-checkout", SourceSystem: "git"}
	generation := scope.ScopeGeneration{
		ScopeID: scopeValue.ScopeID, GenerationID: "generation-delta-4",
		DeltaUnionBaseGenerationID: "generation-full-1",
	}
	base := gitManifestProjectionFacts(scopeValue.ScopeID, "generation-full-1", 20)
	deltaRepository := gitDeltaRepositoryFact(scopeValue.ScopeID, generation.GenerationID, []any{"main.go"}, []any{})
	codeOnly := append(append([]facts.Envelope{}, base...), deltaRepository)
	projection, err := buildProjection(scopeValue, generation, codeOnly)
	if err != nil {
		t.Fatalf("buildProjection(code-only delta) error = %v, want nil", err)
	}
	impacts := supplyChainImpactIntents(projection.reducerIntents)
	if len(impacts) != 1 || impacts[0].FactID != "repo-"+generation.GenerationID {
		t.Fatalf("code-only delta intents = %#v, want one marker anchored on the delta repository fact", impacts)
	}

	changed := gitManifestProjectionFacts(scopeValue.ScopeID, generation.GenerationID, 1)[2]
	changed.FactID = "dep-delta-changed"
	withManifest := append(append([]facts.Envelope{}, codeOnly...), changed)
	projection, err = buildProjection(scopeValue, generation, withManifest)
	if err != nil {
		t.Fatalf("buildProjection(manifest delta) error = %v, want nil", err)
	}
	impacts = supplyChainImpactIntents(projection.reducerIntents)
	if len(impacts) != 1 || impacts[0].FactID != "dep-delta-changed" {
		t.Fatalf("manifest delta intents = %#v, want one anchored on dep-delta-changed", impacts)
	}
}

// TestBuildProjectionQueuesOneMarkerForDeletionOnlyDelta is ruling test 14:
// a delta that only deleted files (a removed lockfile) holds no live
// dependency row, and still raises one marker.
func TestBuildProjectionQueuesOneMarkerForDeletionOnlyDelta(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "git-repo-checkout", SourceSystem: "git"}
	generation := scope.ScopeGeneration{
		ScopeID: scopeValue.ScopeID, GenerationID: "generation-delta-5",
		DeltaUnionBaseGenerationID: "generation-full-1",
	}
	inputs := append(gitManifestProjectionFacts(scopeValue.ScopeID, "generation-full-1", 3),
		gitDeltaRepositoryFact(scopeValue.ScopeID, generation.GenerationID, []any{}, []any{"package-lock.json"}))
	projection, err := buildProjection(scopeValue, generation, inputs)
	if err != nil {
		t.Fatalf("buildProjection(deletion-only delta) error = %v, want nil", err)
	}
	if impacts := supplyChainImpactIntents(projection.reducerIntents); len(impacts) != 1 {
		t.Fatalf("deletion-only delta intents = %#v, want exactly one marker", impacts)
	}
}

// TestBuildProjectionQueuesOneMarkerForZeroRowFullGeneration is the projector
// half of ruling test 15: a full generation whose manifests were all deleted
// holds no dependency row and still raises one marker, so the reducer can fan
// out the prior generation's packages.
func TestBuildProjectionQueuesOneMarkerForZeroRowFullGeneration(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "git-repo-checkout", SourceSystem: "git"}
	generation := scope.ScopeGeneration{ScopeID: scopeValue.ScopeID, GenerationID: "generation-full-6"}
	repository := gitDeltaRepositoryFact(scopeValue.ScopeID, generation.GenerationID, nil, nil)
	delete(repository.Payload, "delta_generation")
	delete(repository.Payload, "delta_relative_paths")
	delete(repository.Payload, "delta_deleted_relative_paths")
	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{repository})
	if err != nil {
		t.Fatalf("buildProjection(zero-row full) error = %v, want nil", err)
	}
	impacts := supplyChainImpactIntents(projection.reducerIntents)
	if len(impacts) != 1 || impacts[0].FactID != repository.FactID {
		t.Fatalf("zero-row full intents = %#v, want one marker anchored on the repository fact", impacts)
	}
}

// gitDeltaRepositoryFact is a delta generation's git repository fact.
func gitDeltaRepositoryFact(scopeID, generationID string, changed, deleted []any) facts.Envelope {
	return facts.Envelope{
		FactID: "repo-" + generationID, ScopeID: scopeID, GenerationID: generationID,
		FactKind: "repository", SourceRef: facts.Ref{SourceSystem: "git"},
		Payload: map[string]any{
			"repo_id": "repository:r_checkout", "graph_id": "repository:r_checkout", "graph_kind": "repository",
			"name": "checkout", "delta_generation": true,
			"delta_relative_paths": changed, "delta_deleted_relative_paths": deleted,
		},
	}
}

// withoutManifestMarker asserts intents hold exactly one #9874 manifest marker
// for scopeID, keyed supply_chain_impact:<scope>, and returns the others.
func withoutManifestMarker(t *testing.T, scopeID string, intents []ReducerIntent) []ReducerIntent {
	t.Helper()
	var rest []ReducerIntent
	markers := 0
	for _, intent := range intents {
		if intent.Domain != reducer.DomainSupplyChainImpact || intent.Payload["trigger"] != "manifest_dependency" {
			rest = append(rest, intent)
			continue
		}
		markers++
		if want := "supply_chain_impact:" + scopeID; intent.EntityKey != want {
			t.Errorf("manifest marker entity key = %q, want %q", intent.EntityKey, want)
		}
	}
	if markers != 1 {
		t.Errorf("scope %q emitted %d manifest markers, want exactly 1", scopeID, markers)
	}
	return rest
}
