// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/code"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/taint"
)

// codeInterprocEvidenceFamily is the materialized-edge family key this
// guard asserts (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const codeInterprocEvidenceFamily = "code_interproc_evidence"

// codeInterprocEvidenceRelationshipType is the single relationship type the
// canonical interproc-evidence upsert template MERGEs: a static token in the
// template, not a per-row substitution — no row content can change the type
// this writer emits. Read it off the template and the
// CodeInterprocEvidenceUpsertCypher const, never by deriving from the port
// or family name.
//
// Duplicated here as a plain literal rather than imported, and it fails CLOSED:
// missingDirectFamilyExpectedTypes compares the FIXTURE against the REGISTRY,
// so a stale literal here yields a fixture whose type the registry does not
// contain and the guard reds instead of quietly asserting less.
const codeInterprocEvidenceRelationshipType = "TAINT_FLOWS_TO"

// codeInterprocEvidenceExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const codeInterprocEvidenceExpectedEdgesRelPath = "go/internal/ifa/testdata/codeinterproc/ifa-code-interproc-evidence-family-expected-edges.json"

// codeInterprocEvidenceExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func codeInterprocEvidenceExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, codeInterprocEvidenceExpectedEdgesRelPath)
}

// resolveCodeInterprocEvidenceMaterializedEdges is code_interproc_evidence's
// named vacuity guard (#6228).
//
// This family's extractor already emits exactly the rows the write template
// UNWINDs, so the rows-to-edges mapping is one-for-one with no routing
// predicate to reproduce. What the fixture proves instead is the extractor's
// decode-and-project behaviour: two live findings on distinct endpoint pairs
// project to two TAINT_FLOWS_TO rows (one carrying the optional why-trail
// keys) while the tombstoned finding for a deleted flow contributes
// nothing — neither a row nor a quarantine. The skip taxonomy itself stays
// covered by the reducer's own unit tests: the guard asserts the exact
// two-edge set, so a ghost edge or a dropped producer reds here either way.
//
// The Odù's node-substrate facts (repository, file, content_entity) never
// reach the extractor: the guard partitions to code_interproc_evidence facts
// first, exactly what the handler's fact loader (ListFactsByKind) feeds the
// extractor in production. That kind partition is the production contract,
// not a second copy of dispatch: the tombstone/valid split the old comment
// warned about stays inside the extractor, untouched.
//
// A quarantined fact is fatal rather than skipped, for the same reason as every
// sibling guard: a fixture that stopped decoding against the
// code_interproc_evidence contract cannot prove the set it names, and the
// surviving facts would understate its own claim.
func resolveCodeInterprocEvidenceMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, codeInterprocEvidenceFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	interprocFacts := make([]facts.Envelope, 0, len(odu.Facts))
	for _, env := range odu.Facts {
		if env.FactKind != code.InterprocEvidenceFactKind {
			continue
		}
		interprocFacts = append(interprocFacts, env)
	}
	rows, quarantined, err := taint.ExtractInterprocEvidenceRowsWithQuarantine(interprocFacts)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractInterprocEvidenceRowsWithQuarantine failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the code_interproc_evidence contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractInterprocEvidenceRowsWithQuarantine produced zero TAINT_FLOWS_TO rows; this fixture cannot prove anything", odu.Name)
	}

	actual := codeInterprocEvidenceRowsToExpectedEdges(rows)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, codeInterprocEvidenceFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractInterprocEvidenceRowsWithQuarantine reproduces the expected %d-edge TAINT_FLOWS_TO set exactly across all %d registry type(s) and %d fact envelope(s); the tombstoned finding produced no spurious row",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// codeInterprocEvidenceRowsToExpectedEdges converts the extractor's rows
// one-for-one into the edge identity the write template MERGEs.
//
// source_function_uid and sink_function_uid are the :Function uids the
// template's two MATCH clauses bind, so they are the edge's source and
// target identity. uid rides along as the Identity pin: the template MERGEs
// the relationship on (endpoints plus evidence_uid), so the fixture must
// declare exactly that property as identity -- LoadExpectedEdges rejects
// any other identity shape against the family's registration. sink_kind and
// source_kind ride along as Properties pins: the template SETs them as
// plain strings from the row (unlike scope_id/generation_id/
// evidence_source, which the WRITER stamps from its own per-run intent
// arguments rather than carrying on the extractor's rows — no static set
// can pin per-run values, and their stamping is covered by the writer unit
// test asserting the annotated row contents instead). The live assert reads
// only fixture-declared property keys off each rel, so extra live props
// never collide with these pins.
func codeInterprocEvidenceRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: codeInterprocEvidenceRelationshipType,
			SourceEntityID:   anyToStringValue(row["source_function_uid"]),
			TargetEntityID:   anyToStringValue(row["sink_function_uid"]),
			Identity: map[string]string{
				"evidence_uid": anyToStringValue(row["uid"]),
			},
			Properties: map[string]string{
				"sink_kind":   anyToStringValue(row["sink_kind"]),
				"source_kind": anyToStringValue(row["source_kind"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}
