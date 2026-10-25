// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/runtime/declaration"
)

// runtimeDeclarationFamily is the materialized-edge family key this guard
// asserts (#8816), matching the key the runtime_declaration entry registers in
// materialized.singleTypeMaterializedEdgeFamilies.
const runtimeDeclarationFamily = "runtime_declaration"

// runtimeDeclarationRelationshipType is the single relationship type the
// RuntimeDeclarationUpsertCypher template MERGEs, a static token in the
// template. Duplicated as a literal and failing closed for the same reason as
// codeTaintEvidenceRelationshipType: the fixture is compared against the
// registry, so a stale literal reds instead of asserting less.
const runtimeDeclarationRelationshipType = "DECLARES_RUNTIME"

// runtimeDeclarationExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const runtimeDeclarationExpectedEdgesRelPath = "go/internal/ifa/testdata/runtimedeclaration/ifa-runtime-declaration-family-expected-edges.json"

// runtimeDeclarationExpectedEdgesPath joins repoRoot onto the fixture.
func runtimeDeclarationExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, runtimeDeclarationExpectedEdgesRelPath)
}

// resolveRuntimeDeclarationMaterializedEdges is runtime_declaration's named
// vacuity guard (#8816). It runs the Odù's facts through
// declaration.ExtractRows, the pure seam the production handler feeds the
// writer, and requires the projected DECLARES_RUNTIME set to equal the
// hand-derived fixture exactly: the tombstoned fact, the blank-runtime fact and
// the same-key replay must add nothing, and the alias must still project.
//
// The target identity is the node's five-part MERGE key joined by
// runtimeDeclarationEntityID, because RuntimeDeclaration carries no uid or id.
// The live assert-edges endpoint rule for this label is part of the live
// enrollment (#6228), not of this offline guard.
//
// A quarantined fact is fatal, as in every sibling guard: a fixture that no
// longer decodes against the runtime_declaration.version contract cannot prove
// the set it names.
func resolveRuntimeDeclarationMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(expectedEdgesPath, runtimeDeclarationFamily, odu.Name)
	if problem != "" {
		return false, problem
	}
	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	rows, quarantined, err := declaration.ExtractRows(odu.Facts)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractRows failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the runtime_declaration.version contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractRows produced zero DECLARES_RUNTIME rows; this fixture cannot prove anything", odu.Name)
	}
	actual := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		actual = append(actual, ExpectedEdge{
			RelationshipType: runtimeDeclarationRelationshipType,
			SourceEntityID:   anyToStringValue(row["repo_id"]),
			TargetEntityID:   runtimeDeclarationEntityID(row),
			Properties: map[string]string{
				"raw_spec":  anyToStringValue(row["raw_spec"]),
				"spec_kind": anyToStringValue(row["spec_kind"]),
			},
		})
	}
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, runtimeDeclarationFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractRows reproduces the expected %d-edge DECLARES_RUNTIME set exactly across all %d registry type(s) and %d fact envelope(s); the tombstoned, blank-runtime and replayed facts added nothing",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// runtimeDeclarationEntityID renders a declaration row's five-part MERGE key
// (repo_id, relative_path, line_number, runtime, order_index) as one string.
func runtimeDeclarationEntityID(row map[string]any) string {
	return fmt.Sprintf("%s|%s|%v|%s|%v", row["repo_id"], row["relative_path"], row["line_number"], row["runtime"], row["order_index"])
}
