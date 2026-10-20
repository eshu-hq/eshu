// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/taint"
)

// codeTaintEvidenceFamily is the materialized-edge family key this guard
// asserts (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const codeTaintEvidenceFamily = "code_taint_evidence"

// codeTaintEvidenceRelationshipType is the single relationship type the
// canonical taint-evidence upsert template MERGEs: a static token in the
// template, not a per-row substitution — no row content can change the type
// this writer emits. Read it off the template and the
// CodeTaintEvidenceUpsertCypher const, never by deriving from the port or
// family name.
//
// Duplicated here as a plain literal rather than imported, and it fails CLOSED:
// missingDirectFamilyExpectedTypes compares the FIXTURE against the REGISTRY,
// so a stale literal here yields a fixture whose type the registry does not
// contain and the guard reds instead of quietly asserting less.
const codeTaintEvidenceRelationshipType = "HAS_TAINT_EVIDENCE"

// codeTaintEvidenceExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const codeTaintEvidenceExpectedEdgesRelPath = "go/internal/ifa/testdata/codetaint/ifa-code-taint-evidence-family-expected-edges.json"

// codeTaintEvidenceExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func codeTaintEvidenceExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, codeTaintEvidenceExpectedEdgesRelPath)
}

// resolveCodeTaintEvidenceMaterializedEdges is code_taint_evidence's named
// vacuity guard (#6228).
//
// This family's extractor already emits exactly the rows the write template
// UNWINDs, so the rows-to-edges mapping is one-for-one with no routing
// predicate to reproduce. What the fixture proves instead is the extractor's
// decode-and-project behaviour: two live findings on distinct functions
// project to two HAS_TAINT_EVIDENCE rows while the tombstoned finding for a
// deleted flow contributes nothing — neither a row nor a quarantine. The
// skip taxonomy itself stays covered by the reducer's own unit tests: the
// guard asserts the exact two-edge set, so a ghost edge or a dropped
// producer reds here either way.
//
// The Odù's facts pass through to the extractor unpartitioned: the
// extraction entry takes the whole envelope slice and dispatches on
// tombstone status internally before decoding each envelope through the
// typed contracts seam, so a guard-side partition would be a second copy of
// that dispatch to keep in step rather than the two-case kind split the AWS
// families reproduce.
//
// A quarantined fact is fatal rather than skipped, for the same reason as every
// sibling guard: a fixture that stopped decoding against the
// code_taint_evidence contract cannot prove the set it names, and the
// surviving facts would understate its own claim.
func resolveCodeTaintEvidenceMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, codeTaintEvidenceFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	rows, quarantined, err := taint.ExtractEvidenceRowsWithQuarantine(odu.Facts)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractEvidenceRowsWithQuarantine failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the code_taint_evidence contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractEvidenceRowsWithQuarantine produced zero HAS_TAINT_EVIDENCE rows; this fixture cannot prove anything", odu.Name)
	}

	actual := codeTaintEvidenceRowsToExpectedEdges(rows)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, codeTaintEvidenceFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractEvidenceRowsWithQuarantine reproduces the expected %d-edge HAS_TAINT_EVIDENCE set exactly across all %d registry type(s) and %d fact envelope(s); the tombstoned finding produced no spurious row",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// codeTaintEvidenceRowsToExpectedEdges converts the extractor's rows
// one-for-one into the edge identity the write template MERGEs.
//
// function_uid is the :Function uid the template MATCHes and uid the
// :CodeTaintEvidence node uid it MERGEs, so they are the edge's source and
// target identity. kind, sink_kind, and source_kind ride along as Properties
// pins: the template SETs them as plain strings from the row (unlike
// scope_id/generation_id/evidence_source, which the WRITER stamps from its
// own per-run intent arguments rather than carrying on the extractor's rows
// — no static set can pin per-run values, and their stamping is covered by
// the writer unit test asserting the annotated row contents instead). The
// live assert reads only fixture-declared property keys off each rel, so
// extra live props never collide with these pins.
func codeTaintEvidenceRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: codeTaintEvidenceRelationshipType,
			SourceEntityID:   anyToStringValue(row["function_uid"]),
			TargetEntityID:   anyToStringValue(row["uid"]),
			Properties: map[string]string{
				"kind":        anyToStringValue(row["kind"]),
				"sink_kind":   anyToStringValue(row["sink_kind"]),
				"source_kind": anyToStringValue(row["source_kind"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}
