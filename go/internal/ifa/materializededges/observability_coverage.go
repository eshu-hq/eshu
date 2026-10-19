// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/obscoverage"
)

// observabilityCoverageFamily is the materialized-edge family key this
// guard asserts (#6228), matching the key registered in
// cypher.multiTypeDirectMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const observabilityCoverageFamily = "observability_coverage"

// observabilityCoverageExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const observabilityCoverageExpectedEdgesRelPath = "go/internal/ifa/testdata/obscoverage/ifa-observability-coverage-family-expected-edges.json"

// observabilityCoverageExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func observabilityCoverageExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, observabilityCoverageExpectedEdgesRelPath)
}

// resolveObservabilityCoverageMaterializedEdges is observability_coverage's
// named vacuity guard (#6228).
//
// Unlike the single-type families this guard carries no relationship-type
// literal: the writer derives its AWS_COVERS_<signal> token per row from the
// closed signal vocabulary, so the rows-to-edges mapping derives the same
// token by the same label-plus-signal rule and the registry half of the
// check (missingDirectFamilyExpectedTypes, comparing the FIXTURE against the
// REGISTRY) is what fails closed on a new signal with no matching expected
// entry. A literal here would either duplicate the vocabulary (a second
// source of truth for the closed set) or name one member while asserting
// five.
//
// What the fixture proves is the classifier's exact-promotion behaviour:
// five observability objects (one per signal) resolve by ARN or bare-id
// dimension to five distinct scanned targets while a metric-name-only alarm,
// a service-name X-Ray match, and an alarm naming an unscanned target each
// resolve to nothing rather than to a fabricated edge. The resolution mode
// rides each row from the identity that actually matched (arn or bare_id),
// and the skip taxonomy itself stays covered by the reducer's own unit
// tests: the guard asserts the exact five-edge set, so a ghost edge or a
// dropped producer reds here either way.
//
// The Odù's facts pass through to the extractor unpartitioned: the
// extraction entry takes the whole envelope slice and partitions by fact
// kind internally (resources to the object/target index, relationships to
// the coverage-relationship index), so a guard-side partition would be a
// second copy of that dispatch to keep in step rather than the two-case kind
// split the s3 guard reproduces.
//
// A quarantined fact is fatal rather than skipped, for the same reason as every
// sibling guard: a fixture that stopped decoding against the aws_resource /
// aws_relationship contract cannot prove the set it names, and the surviving
// facts would understate its own claim.
func resolveObservabilityCoverageMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, observabilityCoverageFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	rows, _, quarantined, err := obscoverage.ExtractObservabilityCoverageEdgeRows(odu.Facts)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractObservabilityCoverageEdgeRows failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the aws_resource / aws_relationship contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractObservabilityCoverageEdgeRows produced zero AWS_COVERS_* rows; this fixture cannot prove anything", odu.Name)
	}

	actual := observabilityCoverageRowsToExpectedEdges(rows)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, observabilityCoverageFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractObservabilityCoverageEdgeRows reproduces the expected %d-edge AWS_COVERS_* set exactly across all %d registry type(s) and %d fact envelope(s); the metric-name-only, service-name, and ghost-target restraints produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// observabilityCoverageRowsToExpectedEdges converts the extractor's rows
// one-for-one into the edge identity the write template MERGEs.
//
// observability_uid and target_uid are both :CloudResource uids, matched by
// the template's two MATCH clauses, so they are the edge's source and target
// identity. The relationship type follows the write path's own token rule
// (observabilityCoverageEdgeLabel + "_" + coverage_signal): the template
// interpolates the same static token per signal group, so deriving it here
// by the same rule keeps the guard's claim identical to what the writer
// MERGEs. coverage_signal and resolution_mode ride along as Properties pins:
// the template SETs them as plain strings from the row (unlike
// scope_id/generation_id/evidence_source, which the WRITER stamps from its
// own per-run intent arguments rather than carrying on the extractor's rows
// — no static set can pin per-run values, and their stamping is covered by
// the writer unit test asserting the annotated row contents instead). The
// live assert reads only fixture-declared property keys off each rel, so
// extra live props never collide with these pins.
func observabilityCoverageRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		signal := anyToStringValue(row["coverage_signal"])
		edge := ExpectedEdge{
			RelationshipType: "AWS_COVERS_" + signal,
			SourceEntityID:   anyToStringValue(row["observability_uid"]),
			TargetEntityID:   anyToStringValue(row["target_uid"]),
			Properties: map[string]string{
				"coverage_signal": signal,
				"resolution_mode": anyToStringValue(row["resolution_mode"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}
