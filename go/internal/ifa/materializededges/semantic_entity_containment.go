// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/semantic/entity"
)

// semanticEntityContainmentFamily is the materialized-edge family key this
// guard asserts (#8359), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies.
const semanticEntityContainmentFamily = "semantic_entity_containment"

// semanticEntityContainmentRelationshipType is the single relationship
// type the semantic entity writer MERGEs. It is shared with the canonical
// node writer, so the live assert fences this family's population by
// endpoint labels plus the rel evidence_source stamp (see endpoints.go);
// the guard's job is only the extractor-to-edge-set derivation. Read off
// the writer templates (`MERGE (f)-[rel:CONTAINS]->(n)`), never derived
// from the port name WriteSemanticEntities.
//
// Duplicated as a plain literal rather than imported, and it fails CLOSED:
// loadDirectFamilyExpectedEdges compares the fixture against the
// registry, so a stale literal here reds instead of asserting less.
const semanticEntityContainmentRelationshipType = "CONTAINS"

// semanticEntityContainmentExpectedEdgesRelPath is the family's
// hand-derived expected-edge fixture, repoRoot-anchored.
const semanticEntityContainmentExpectedEdgesRelPath = "go/internal/ifa/testdata/semanticentitycontainment/ifa-semantic-entity-containment-family-expected-edges.json"

func semanticEntityContainmentExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, semanticEntityContainmentExpectedEdgesRelPath)
}

// resolveSemanticEntityContainmentMaterializedEdges is
// semantic_entity_containment's named vacuity guard (#8359).
//
// The extractor (semantic.ExtractEntityRowsForRepo) is the admission
// contract: it keeps only the semantic subset of content_entity facts.
// The fixture proves that subset in both directions — four producers
// admitted through three different paths (always-semantic Annotation, Go
// Function with semantic metadata, Python async Function, Elixir
// module-attribute Variable) and two non-producers in the same files (a
// plain Python Function with no marker, a Class) that must contribute no
// edge. Asserting the exact set is what makes either direction red: a
// ghost edge from a widened subset and a dropped producer both mismatch.
//
// Rows map one-for-one to edges: each row's template MATCHes its File and
// MERGEs one CONTAINS edge. The File endpoint identity is the canonical
// writer's File uid derivation, RepoID + ":" + RelativePath
// (canonical_node_writer_phases.go), reproduced here because the live
// assert resolves File endpoints by uid; if that derivation moves, the
// live cell reds against this fixture rather than passing on a stale
// identity. Rust ImplBlock ownership edges are absent by fixture design
// (see the family Odù header): no ImplBlock rows, so no ownership
// derivation is reproduced here.
//
// The evidence_source property pin is the writer's own stamp constant,
// imported rather than copied: unlike the relationship type, no registry
// comparison would catch a stale literal, and the stamp is the fence the
// live population is asserted by.
func resolveSemanticEntityContainmentMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, semanticEntityContainmentFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}
	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	_, rows := semantic.ExtractEntityRowsForRepo(odu.Facts, "")
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractEntityRowsForRepo produced zero semantic rows; this fixture cannot prove anything", odu.Name)
	}

	actual := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		actual = append(actual, ExpectedEdge{
			RelationshipType: semanticEntityContainmentRelationshipType,
			SourceEntityID:   row.RepoID + ":" + row.RelativePath,
			TargetEntityID:   row.EntityID,
			Properties: map[string]string{
				"evidence_source": entity.SemanticEntityEvidenceSource,
			},
		})
	}
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, semanticEntityContainmentFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractEntityRowsForRepo reproduces the expected %d-edge CONTAINS set exactly across all %d registry type(s) and %d fact envelope(s); the plain function and class produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}
