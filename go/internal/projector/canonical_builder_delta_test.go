// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

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
				"relative_path": "src/main.go",
				"language":      "go",
			},
		},
	}

	result, _ := buildCanonicalMaterialization(sc, gen, envelopes)

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

	result, _ := buildCanonicalMaterialization(sc, gen, envelopes)

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

	result, _ := buildCanonicalMaterialization(sc, gen, envelopes)

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
