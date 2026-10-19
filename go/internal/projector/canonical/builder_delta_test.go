// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// unionTestOverlay overlays delta on base through the production union the
// delta-base ListFacts read returns, failing the test when the delta carries
// no delta scope.
func unionTestOverlay(t *testing.T, base, delta []facts.Envelope) []facts.Envelope {
	t.Helper()
	scope := facts.ParseDeltaScope(delta)
	if !scope.IsDelta {
		t.Fatal("ParseDeltaScope IsDelta = false, want true")
	}
	return facts.OverlayDeltaBase(base, delta, scope)
}

func TestBuildCanonicalMaterializationExtractsDeltaProjectionScope(t *testing.T) {
	t.Parallel()

	sc := testScope()
	sc.ActiveGenerationID = "gen-previous"
	sc.PreviousGenerationExists = true
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":                       "repo-abc",
				"name":                          "my-project",
				"path":                          "/repos/my-project",
				"delta_generation":              true,
				"delta_relative_paths":          []string{"src/main.go", "src/deleted.go"},
				"delta_deleted_relative_paths":  []string{"src/deleted.go"},
				"delta_unrelated_empty_ignored": []string{""},
			},
		},
		{
			FactID:   "f-1",
			ScopeID:  "scope-1",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":          "repo-abc",
				"relative_path":    "src/main.go",
				"language":         "go",
				"parsed_file_data": map[string]any{},
			},
		},
	}

	result, _, _ := BuildMaterialization(sc, gen, envelopes)

	if !result.DeltaProjection {
		t.Fatal("DeltaProjection = false, want true")
	}
	if got, want := result.DeltaFilePaths, []string{"/repos/my-project/src/main.go", "/repos/my-project/src/deleted.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaFilePaths = %#v, want %#v", got, want)
	}
	if got, want := result.DeltaDeletedFilePaths, []string{"/repos/my-project/src/deleted.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaDeletedFilePaths = %#v, want %#v", got, want)
	}
}

func TestBuildCanonicalMaterializationExtractsReconciliationProjection(t *testing.T) {
	t.Parallel()

	sc := testScope()
	sc.ActiveGenerationID = "gen-previous"
	sc.PreviousGenerationExists = true
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":                   "repo-abc",
				"name":                      "my-project",
				"path":                      "/repos/my-project",
				"reconciliation_generation": true,
			},
		},
	}

	result, _, _ := BuildMaterialization(sc, gen, envelopes)

	if !result.ReconciliationProjection {
		t.Fatal("ReconciliationProjection = false, want true")
	}
	if result.DeltaProjection {
		t.Fatal("DeltaProjection = true, want false for reconciliation full snapshot")
	}
}

func TestBuildCanonicalMaterializationPreservesDeltaPathWhitespace(t *testing.T) {
	t.Parallel()

	sc := testScope()
	sc.ActiveGenerationID = "gen-previous"
	sc.PreviousGenerationExists = true
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":                      "repo-abc",
				"name":                         "my-project",
				"path":                         "/repos/my-project",
				"delta_generation":             true,
				"delta_relative_paths":         []string{"src/ file.go", "src/deleted .go", "/absolute-skipped.go", "../skipped.go"},
				"delta_deleted_relative_paths": []any{"src/deleted .go"},
			},
		},
	}

	result, _, _ := BuildMaterialization(sc, gen, envelopes)

	if !result.DeltaProjection {
		t.Fatal("DeltaProjection = false, want true")
	}
	if got, want := result.DeltaFilePaths, []string{"/repos/my-project/src/ file.go", "/repos/my-project/src/deleted .go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaFilePaths = %#v, want %#v", got, want)
	}
	if got, want := result.DeltaDeletedFilePaths, []string{"/repos/my-project/src/deleted .go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaDeletedFilePaths = %#v, want %#v", got, want)
	}
}

// TestBuildMaterializationOverDeltaUnionCarriesFullTree proves the #7797
// contract at the builder: a delta generation overlaid on its base full
// generation (facts.OverlayDeltaBase, the union ListFacts returns)
// materializes the full repository tree, so a from-empty projection of the
// delta writes every file and entity. The delta repository fact survives, so
// retracts stay file-scoped while writes carry the full set.
func TestBuildMaterializationOverDeltaUnionCarriesFullTree(t *testing.T) {
	t.Parallel()

	fileFact := func(id, key, rel string) facts.Envelope {
		return facts.Envelope{
			FactID: id, ScopeID: "scope-1", FactKind: "file", StableFactKey: key,
			Payload: map[string]any{
				"repo_id": "repo-abc", "relative_path": rel,
				"language": "go", "parsed_file_data": map[string]any{},
			},
		}
	}
	entityFact := func(id, key, rel, name string) facts.Envelope {
		return facts.Envelope{
			FactID: id, ScopeID: "scope-1", FactKind: "content_entity", StableFactKey: key,
			Payload: map[string]any{
				"repo_id": "repo-abc", "relative_path": rel,
				"entity_type": "Function", "entity_name": name,
			},
		}
	}
	base := []facts.Envelope{
		{
			FactID: "base-repo", ScopeID: "scope-1", FactKind: "repository",
			StableFactKey: "repository:repo-abc",
			Payload: map[string]any{
				"repo_id": "repo-abc", "name": "my-project", "path": "/repos/my-project",
			},
		},
		fileFact("base-keep", "file:repo-abc:keep.go", "keep.go"),
		fileFact("base-w", "file:repo-abc:w.go", "w.go"),
		fileFact("base-other", "file:repo-abc:other.go", "other.go"),
		entityFact("base-keep-fn", "content_entity:keep-fn", "keep.go", "keep"),
		entityFact("base-w-fn", "content_entity:w-fn", "w.go", "w"),
		entityFact("base-other-fn", "content_entity:other-fn", "other.go", "other"),
	}
	delta := []facts.Envelope{
		{
			FactID: "delta-repo", ScopeID: "scope-1", FactKind: "repository",
			StableFactKey: "repository:repo-abc",
			Payload: map[string]any{
				"repo_id": "repo-abc", "name": "my-project", "path": "/repos/my-project",
				"delta_generation":             true,
				"delta_relative_paths":         []any{"keep.go"},
				"delta_deleted_relative_paths": []any{"w.go"},
			},
		},
		fileFact("delta-keep", "file:repo-abc:keep.go", "keep.go"),
		entityFact("delta-keep-fn", "content_entity:keep-fn-v2", "keep.go", "keepV2"),
	}
	union := unionTestOverlay(t, base, delta)

	sc := testScope()
	sc.PreviousGenerationExists = true
	gen := testGeneration()
	result, quarantined, _ := BuildMaterialization(sc, gen, union)
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d facts, want 0", len(quarantined))
	}
	if !result.DeltaProjection {
		t.Fatal("DeltaProjection = false, want true (the delta repo fact survives the union)")
	}
	if got, want := result.DeltaDeletedFilePaths, []string{"/repos/my-project/w.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaDeletedFilePaths = %#v, want %#v", got, want)
	}
	filePaths := make(map[string]bool)
	for _, file := range result.Files {
		filePaths[file.Path] = true
	}
	for _, want := range []string{"/repos/my-project/keep.go", "/repos/my-project/other.go"} {
		if !filePaths[want] {
			t.Fatalf("materialized files = %v, want %q", filePaths, want)
		}
	}
	if filePaths["/repos/my-project/w.go"] {
		t.Fatalf("materialized files = %v, want no deleted w.go", filePaths)
	}
	entityNames := make(map[string]bool)
	for _, entity := range result.Entities {
		entityNames[entity.EntityName] = true
	}
	for _, want := range []string{"keepV2", "other"} {
		if !entityNames[want] {
			t.Fatalf("materialized entities = %v, want %q", entityNames, want)
		}
	}
	if entityNames["w"] || entityNames["keep"] {
		t.Fatalf("materialized entities = %v, want no stale keep/w rows", entityNames)
	}
}
