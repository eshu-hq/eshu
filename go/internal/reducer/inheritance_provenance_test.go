// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractInheritanceRowsStampsDeclaredResolutionMethod(t *testing.T) {
	t.Parallel()

	_, rows := ExtractInheritanceRows(inheritanceEntityFacts())
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["resolution_method"], codeprovenance.MethodDeclared; got != want {
		t.Fatalf("resolution_method = %#v, want %#v", got, want)
	}
}

func TestExtractInheritanceRowsStampsDeclaredResolutionMethodForImplements(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_interface",
				"entity_type": "Interface",
				"entity_name": "Runnable",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_runner",
				"entity_type": "Class",
				"entity_name": "Runner",
				"entity_metadata": map[string]any{
					"implemented_interfaces": []any{"Runnable"},
				},
			},
		},
	}

	_, rows := ExtractInheritanceRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["relationship_type"], "IMPLEMENTS"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["resolution_method"], codeprovenance.MethodDeclared; got != want {
		t.Fatalf("resolution_method = %#v, want %#v", got, want)
	}
}
