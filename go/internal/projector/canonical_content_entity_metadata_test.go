// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildCanonicalMaterializationPromotesFunctionAnalysisMetadata(t *testing.T) {
	t.Parallel()

	materialization, _ := buildCanonicalMaterialization(testScope(), testGeneration(), []facts.Envelope{
		{
			FactID:   "repository",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "dog-class",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_id":     "content-entity:dog",
				"repo_id":       "repo-abc",
				"relative_path": "Classes.swift",
				"entity_type":   "Class",
				"entity_name":   "Dog",
				"start_line":    float64(20),
				"end_line":      float64(34),
				"language":      "swift",
			},
		},
		{
			FactID:   "dog-fetch",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_id":     "content-entity:dog-fetch",
				"repo_id":       "repo-abc",
				"relative_path": "Classes.swift",
				"entity_type":   "Function",
				"entity_name":   "fetch",
				"start_line":    float64(30),
				"end_line":      float64(32),
				"language":      "swift",
				"entity_metadata": map[string]any{
					"class_context":         "Dog",
					"cyclomatic_complexity": float64(2),
				},
			},
		},
	})

	if got, want := len(materialization.Entities), 2; got != want {
		t.Fatalf("entity count = %d, want %d", got, want)
	}
	var function EntityRow
	for _, entity := range materialization.Entities {
		if entity.Label == "Function" && entity.EntityName == "fetch" {
			function = entity
		}
	}
	if got, want := function.CyclomaticComplexity, 2; got != want {
		t.Fatalf("cyclomatic complexity = %d, want %d", got, want)
	}
	if got, want := len(materialization.ClassMembers), 1; got != want {
		t.Fatalf("class member count = %d, want %d", got, want)
	}
	member := materialization.ClassMembers[0]
	if member.ClassName != "Dog" || member.FunctionName != "fetch" || member.FunctionLine != 30 || member.FilePath != "/repos/my-project/Classes.swift" {
		t.Fatalf("class member = %+v, want Dog.fetch at Classes.swift:30", member)
	}
}
