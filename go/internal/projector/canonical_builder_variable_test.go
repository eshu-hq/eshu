// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCanonicalMaterializationSkipsPlainVariableEntities(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		{
			FactID:   "r-1",
			ScopeID:  "scope-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"path":    "/repos/my-project",
			},
		},
		{
			FactID:   "v-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_id":     "variable-1",
				"entity_type":   "Variable",
				"entity_name":   "config",
				"relative_path": "src/config.ts",
				"start_line":    7,
				"end_line":      7,
				"language":      "typescript",
				"repo_id":       "repo-abc",
			},
		},
		{
			FactID:   "f-1",
			ScopeID:  "scope-1",
			FactKind: "content_entity",
			Payload: map[string]any{
				"entity_id":     "function-1",
				"entity_type":   "Function",
				"entity_name":   "handler",
				"relative_path": "src/config.ts",
				"start_line":    10,
				"end_line":      20,
				"language":      "typescript",
				"repo_id":       "repo-abc",
			},
		},
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)

	var labels []string
	for _, entity := range result.Entities {
		labels = append(labels, entity.Label)
		if entity.Label == "Variable" {
			t.Fatalf("canonical materialization emitted plain Variable entity %#v; labels=%v", entity, labels)
		}
	}
	if got, want := len(result.Entities), 1; got != want {
		t.Fatalf("len(Entities) = %d, want %d; labels=%v", got, want, labels)
	}
	if got, want := result.Entities[0].Label, "Function"; got != want {
		t.Fatalf("Entities[0].Label = %q, want %q", got, want)
	}
}
