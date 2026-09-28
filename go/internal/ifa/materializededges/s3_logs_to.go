// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/s3logsto"
)

// s3LogsToFamily is the materialized-edge family key this guard asserts
// (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const s3LogsToFamily = "s3_logs_to"

// s3LogsToRelationshipType is the single relationship type the canonical
// S3 logs-to upsert template MERGEs: the one member of the writer's closed
// vocabulary substituted into the template's one %s.
//
// Read it off the template and the s3LogsToRelationshipType const, never by
// deriving from the port or family name — "S3_LOGS_TO" is statement
// metadata carried beside the query, not a graph relationship type: taking
// the type from the port or family name is the same name-derived mistake
// one level above, and it would make this guard assert an always-empty
// population.
//
// Duplicated here as a plain literal rather than imported, and it fails CLOSED:
// missingDirectFamilyExpectedTypes compares the FIXTURE against the REGISTRY,
// so a stale literal here yields a fixture whose type the registry does not
// contain and the guard reds instead of quietly asserting less.
const s3LogsToRelationshipType = "LOGS_TO"

// s3LogsToResolutionMode is the writer's only resolution mode, stamped on
// every row. The extractor resolves both endpoints by bucket-name equality
// against the in-memory S3 join index (the posture's own name, or the ARN
// tail where the collector emits no bucket_name), and every resolved row
// converges on this single mode: pinning it catches a future second mode
// silently changing what the rows claim.
const s3LogsToResolutionMode = "name"

// s3LogsToExpectedEdgesRelPath is the family's hand-derived expected-edge
// fixture, repoRoot-anchored.
const s3LogsToExpectedEdgesRelPath = "go/internal/ifa/testdata/s3logsto/ifa-s3-logs-to-family-expected-edges.json"

// s3LogsToExpectedEdgesPath joins repoRoot onto the family's expected-edge
// fixture.
func s3LogsToExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, s3LogsToExpectedEdgesRelPath)
}

// resolveS3LogsToMaterializedEdges is s3_logs_to's named vacuity guard
// (#6228).
//
// Like ec2_uses_profile, this family's extractor already emits exactly the
// rows the write template UNWINDs, so the rows-to-edges mapping is
// one-for-one with no routing predicate to reproduce. What the fixture
// proves instead is the extractor's resolution behaviour: two buckets
// resolve by name against scanned nodes while a third resolves by ARN-tail
// fallback with a blank bucket name — while a bucket with logging disabled,
// a bucket naming an unscanned log bucket, and an orphan posture with no
// scanned node each resolve to nothing rather than to a fabricated edge.
// The resolution mode is pinned on every row (single-mode contract), and
// the skip taxonomy itself stays covered by the reducer's own unit tests:
// the guard asserts the exact three-edge set, so a ghost edge or a dropped
// producer reds here either way.
//
// The Odù's facts are partitioned by fact kind with the same two-case switch
// the handler's own split uses, not a guard-side copy of richer logic: the
// production split is a pure kind partition (resources vs postures,
// everything else dropped), so reproducing its two cases here keeps a
// partition regression red in both places instead of letting the two
// disagree. Unlike ec2_uses_profile there is no tombstone member to carry:
// the s3 extractor has no tombstone filter (a deleted bucket's posture
// simply stops being emitted), so none is claimed here.
//
// A quarantined fact is fatal rather than skipped, for the same reason as every
// sibling guard: a fixture that stopped decoding against the aws_resource or
// s3_bucket_posture contract cannot prove the set it names, and the surviving
// facts would understate its own claim.
func resolveS3LogsToMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, s3LogsToFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	var resources, postures []facts.Envelope
	for _, env := range odu.Facts {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case facts.S3BucketPostureFactKind:
			postures = append(postures, env)
		}
	}
	rows, _, quarantined, err := s3logsto.ExtractS3LogsToEdgeRows(resources, postures)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractS3LogsToEdgeRows failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the aws_resource / s3_bucket_posture contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractS3LogsToEdgeRows produced zero LOGS_TO rows; this fixture cannot prove anything", odu.Name)
	}
	for index, row := range rows {
		if got := anyToStringValue(row["resolution_mode"]); got != s3LogsToResolutionMode {
			return false, fmt.Sprintf("odù %q: row %d carries resolution_mode %q, want %q (the writer's single-mode contract)", odu.Name, index, got, s3LogsToResolutionMode)
		}
	}

	actual := s3LogsToRowsToExpectedEdges(rows)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, s3LogsToFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractS3LogsToEdgeRows reproduces the expected %d-edge LOGS_TO set exactly across all %d registry type(s) and %d fact envelope(s); the disabled, ghost-target, and orphan postures produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// s3LogsToRowsToExpectedEdges converts the extractor's rows one-for-one
// into the edge identity the write template MERGEs.
//
// source_uid and target_uid are both :CloudResource uids, matched by the
// template's two MATCH clauses, so they are the edge's source and target
// identity. resolution_mode rides along as a Properties pin: the template
// SETs it as a plain string from the row (unlike scope_id/generation_id/
// evidence_source, which the WRITER stamps from its own per-run intent
// arguments rather than carrying on the extractor's rows — no static set
// can pin per-run values, and their stamping is covered by the writer unit
// test asserting the annotated row contents instead). The live assert reads
// only fixture-declared property keys off each rel, so extra live props
// never collide with this pin.
func s3LogsToRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: s3LogsToRelationshipType,
			SourceEntityID:   anyToStringValue(row["source_uid"]),
			TargetEntityID:   anyToStringValue(row["target_uid"]),
			Properties: map[string]string{
				"resolution_mode": anyToStringValue(row["resolution_mode"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}
