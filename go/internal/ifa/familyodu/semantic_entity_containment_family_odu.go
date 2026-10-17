// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The semantic_entity_containment family Odù (#8359, split out of #6228).
//
// A DIRECT-materialization family: the reducer's semantic-entity handler
// writes straight to cypher.SemanticEntityWriter through the
// WriteSemanticEntities port. The relationship type is CONTAINS, read off
// the writer templates' `MERGE (f)-[rel:CONTAINS]->(n)` — never derived
// from the port name, which is named for entities. CONTAINS is shared with
// the canonical node writer; this family's population is the subset the
// semantic writer stamps `parser/semantic-entities` between File|ImplBlock
// sources and the eleven semantic entity labels (the endpoint constraint
// in cypher/edge/materialized/endpoints.go).
//
// The extractor (semantic.ExtractEntityRowsForRepo) admits only the
// semantic subset of content_entity facts: the nine always-semantic types,
// Variable only as an Elixir module attribute or TSX component-type
// assertion, and Function only when a language-specific semantic marker
// is present. The non-producers below are the load-bearing half: a plain
// Python function and a Class sit in the same files and must produce no
// edge, so a regression that widened the subset would redden the guard
// instead of silently matching a bigger expected set.
//
// Rust ImplBlock->Function ownership edges are deliberately absent: the
// ownership derivation lives in the cypher writer, and reproducing it in
// the guard would be a second copy to drift. Writer unit tests cover that
// edge; this fixture proves File containment.

const (
	// SemanticEntityContainmentFamilyOduName is this Odù's catalog name.
	SemanticEntityContainmentFamilyOduName = "odu:ifa-semantic-entity-containment-family"

	// semanticEntityContainmentFamilyScopeID is dedicated to this family:
	// scopes carry one active generation, and sharing the code-calls
	// scope would supersede that family's generation in a shared cell.
	semanticEntityContainmentFamilyScopeID       = "scope-ifa-semantic-entity-containment-family"
	semanticEntityContainmentFamilyGenerationID  = "gen-ifa-semantic-entity-containment-family-1"
	semanticEntityContainmentFamilyRepoID        = "repo-ifa-semantic-entity-containment"
	semanticEntityContainmentFamilyLocalPath     = "/repo-semantic-containment"
	semanticEntityContainmentFamilyCollectorKind = "git"
)

// semanticEntityContainmentFixture is one content_entity fact in the Odù.
type semanticEntityContainmentFixture struct {
	EntityID     string
	EntityType   string
	EntityName   string
	RelativePath string
	Language     string
	StartLine    float64
	EndLine      float64
	Metadata     map[string]any
}

// The fixture entities: four edge producers across three admission paths
// (always-semantic Annotation, Go Function with docstring metadata,
// Python async Function, Elixir module-attribute Variable) and two
// non-producers (plain Python Function, Class).
var semanticEntityContainmentFixtures = []semanticEntityContainmentFixture{
	{
		// EDGE: Go function carrying semantic metadata (docstring).
		EntityID: "content-entity:sec-go-func", EntityType: "Function", EntityName: "Handle",
		RelativePath: "pkg/handle.go", Language: "go", StartLine: 10, EndLine: 20,
		Metadata: map[string]any{"docstring": "Handle serves the request."},
	},
	{
		// EDGE: Python async function.
		EntityID: "content-entity:sec-py-func", EntityType: "Function", EntityName: "fetch",
		RelativePath: "app/fetch.py", Language: "python", StartLine: 3, EndLine: 9,
		Metadata: map[string]any{"async": true},
	},
	{
		// EDGE: Annotation is always semantic.
		EntityID: "content-entity:sec-annotation", EntityType: "Annotation", EntityName: "route",
		RelativePath: "app/fetch.py", Language: "python", StartLine: 2, EndLine: 2,
		Metadata: map[string]any{"kind": "applied", "target_kind": "function_definition"},
	},
	{
		// EDGE: Elixir module attribute Variable.
		EntityID: "content-entity:sec-ex-var", EntityType: "Variable", EntityName: "@timeout",
		RelativePath: "lib/worker.ex", Language: "elixir", StartLine: 4, EndLine: 4,
		Metadata: map[string]any{"attribute_kind": "module_attribute"},
	},
	{
		// NO EDGE: plain Python function with no semantic marker.
		EntityID: "content-entity:sec-py-plain", EntityType: "Function", EntityName: "plain",
		RelativePath: "app/fetch.py", Language: "python", StartLine: 12, EndLine: 14,
	},
	{
		// NO EDGE: Class is not a semantic entity type.
		EntityID: "content-entity:sec-go-class", EntityType: "Class", EntityName: "Handler",
		RelativePath: "pkg/handle.go", Language: "go", StartLine: 5, EndLine: 8,
	},
}

// SemanticEntityContainmentFamilyOdu builds the cataloged Odù for the
// semantic_entity_containment direct-materialization family.
func SemanticEntityContainmentFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(semanticEntityContainmentFixtures)+1)
	factsForOdu = append(factsForOdu, facts.Envelope{
		ScopeID:          semanticEntityContainmentFamilyScopeID,
		GenerationID:     semanticEntityContainmentFamilyGenerationID,
		FactKind:         RepositoryFactKind,
		SchemaVersion:    "1.0.0",
		StableFactKey:    fmt.Sprintf("repository:%s", semanticEntityContainmentFamilyRepoID),
		CollectorKind:    semanticEntityContainmentFamilyCollectorKind,
		SourceConfidence: "observed",
		Payload: map[string]any{
			"repo_id":       semanticEntityContainmentFamilyRepoID,
			"source_run_id": "run-ifa-semantic-entity-containment-1",
			"local_path":    semanticEntityContainmentFamilyLocalPath,
		},
	})
	for _, fixture := range semanticEntityContainmentFixtures {
		payload := map[string]any{
			"repo_id":       semanticEntityContainmentFamilyRepoID,
			"entity_id":     fixture.EntityID,
			"entity_type":   fixture.EntityType,
			"entity_name":   fixture.EntityName,
			"relative_path": fixture.RelativePath,
			"language":      fixture.Language,
			"start_line":    fixture.StartLine,
			"end_line":      fixture.EndLine,
		}
		if len(fixture.Metadata) > 0 {
			payload["entity_metadata"] = fixture.Metadata
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          semanticEntityContainmentFamilyScopeID,
			GenerationID:     semanticEntityContainmentFamilyGenerationID,
			FactKind:         ContentEntityFactKind,
			SchemaVersion:    "1.0.0",
			StableFactKey:    fmt.Sprintf("content_entity:%s", fixture.EntityID),
			CollectorKind:    semanticEntityContainmentFamilyCollectorKind,
			SourceConfidence: "observed",
			SourceRef: facts.Ref{
				SourceURI: semanticEntityContainmentFamilyLocalPath + "/" + fixture.RelativePath,
			},
			Payload: payload,
		})
	}
	return CatalogOdu{
		Odu: Odu{
			Name:  SemanticEntityContainmentFamilyOduName,
			Facts: factsForOdu,
		},
		Detail: "semantic_entity_containment: four stamped File CONTAINS edges from the semantic subset of content_entity facts; plain function and class produce none (#8359)",
	}
}
