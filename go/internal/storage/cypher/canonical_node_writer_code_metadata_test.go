// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalEntityPropertiesPinsCyclomaticComplexity(t *testing.T) {
	t.Parallel()

	properties := canonicalEntityProperties(projector.EntityRow{
		EntityID:             "content-entity:handler",
		Label:                "Function",
		EntityName:           "GoldenDataflowHandler",
		FilePath:             "/repos/go-comprehensive/dataflow_proof.go",
		RelativePath:         "dataflow_proof.go",
		StartLine:            12,
		EndLine:              17,
		Language:             "go",
		RepoID:               "repository:go-comprehensive",
		CyclomaticComplexity: 2,
		Metadata: map[string]any{
			"cyclomatic_complexity": float64(2),
		},
	}, "scope-1", "generation-1")

	if got, want := properties["cyclomatic_complexity"], 2; got != want {
		t.Fatalf("cyclomatic_complexity property = %#v, want %#v", got, want)
	}
}
