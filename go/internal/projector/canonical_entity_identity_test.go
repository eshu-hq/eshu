// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildCanonicalMaterializationCanonicalizesDuplicateCodeEntityIdentity(t *testing.T) {
	t.Parallel()

	sc := testScope()
	gen := testGeneration()
	envelopes := []facts.Envelope{
		duplicateEntityFact("function-1", "legacy-function-id-1", "function", "Handle", "src/server.go", 42, "go-old"),
		duplicateEntityFact("function-2", "legacy-function-id-2", "Function", "Handle", "src/server.go", 42, "go"),
		duplicateEntityFact("class-1", "legacy-class-id-1", "class", "Server", "src/server.py", 9, "python-old"),
		duplicateEntityFact("class-2", "legacy-class-id-2", "Class", "Server", "src/server.py", 9, "python"),
	}

	result := buildCanonicalMaterialization(sc, gen, envelopes)
	if got, want := len(result.Entities), 2; got != want {
		t.Fatalf("len(Entities) = %d, want %d", got, want)
	}

	gotByLabel := map[string]EntityRow{}
	for _, entity := range result.Entities {
		gotByLabel[entity.Label] = entity
	}
	wantFunctionID := content.CanonicalEntityID("repo-abc", "src/server.go", "Function", "Handle", 42)
	if got := gotByLabel["Function"].EntityID; got != wantFunctionID {
		t.Fatalf("Function EntityID = %q, want %q", got, wantFunctionID)
	}
	if got := gotByLabel["Function"].Language; got != "go" {
		t.Fatalf("Function Language = %q, want %q", got, "go")
	}
	wantClassID := content.CanonicalEntityID("repo-abc", "src/server.py", "Class", "Server", 9)
	if got := gotByLabel["Class"].EntityID; got != wantClassID {
		t.Fatalf("Class EntityID = %q, want %q", got, wantClassID)
	}
	if got := gotByLabel["Class"].Language; got != "python" {
		t.Fatalf("Class Language = %q, want %q", got, "python")
	}
}

func TestCanonicalGraphEntityIDPreservesIncomingIDForNonNamePathLineLabels(t *testing.T) {
	t.Parallel()

	for _, label := range []string{"K8sResource", "TerraformModule", "Module", "Parameter"} {
		got := canonicalGraphEntityID(
			label,
			"repo-abc",
			"infra/main.tf",
			label,
			"shared",
			10,
			"incoming-id",
		)
		if got != "incoming-id" {
			t.Fatalf("canonicalGraphEntityID(%q) = %q, want incoming ID preserved", label, got)
		}
	}
}

func duplicateEntityFact(
	factID string,
	entityID string,
	entityType string,
	entityName string,
	relativePath string,
	startLine int,
	language string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		ScopeID:  "scope-1",
		FactKind: "content_entity",
		Payload: map[string]any{
			"repo_id":       "repo-abc",
			"entity_id":     entityID,
			"entity_type":   entityType,
			"entity_name":   entityName,
			"relative_path": relativePath,
			"start_line":    startLine,
			"end_line":      startLine + 6,
			"language":      language,
		},
	}
}
