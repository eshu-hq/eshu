// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/s3grant"
)

// s3ExternalPrincipalGrantFamily is the materialized-edge family key this
// guard asserts (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const s3ExternalPrincipalGrantFamily = "s3_external_principal_grant"

// s3ExternalPrincipalGrantRelationshipType is the single relationship type
// the grant edge MERGEs: the one member of the writer's closed vocabulary
// substituted into the canonical upsert template's one %s.
//
// Read it off the template and the vocabulary, never by deriving from the
// port or family name. Duplicated here as a plain literal rather than
// imported, and it fails CLOSED: missingDirectFamilyExpectedTypes compares
// the FIXTURE against the REGISTRY, so a stale literal here yields a fixture
// whose type the registry does not contain and the guard reds instead of
// quietly asserting less.
const s3ExternalPrincipalGrantRelationshipType = "GRANTS_ACCESS_TO"

// s3ExternalPrincipalGrantResolutionMode is the writer's only resolution
// mode, stamped on every row. The extractor resolves every grant source by
// bucket-name equality against the in-memory S3 join index (the bare name,
// or the ARN tail where the collector emits no bucket_name), and every
// resolved row converges on this single mode: pinning it catches a future
// second mode silently changing what the rows claim.
const s3ExternalPrincipalGrantResolutionMode = "bucket_name"

// s3ExternalPrincipalGrantExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const s3ExternalPrincipalGrantExpectedEdgesRelPath = "go/internal/ifa/testdata/s3grant/ifa-s3-external-principal-grant-family-expected-edges.json"

// s3ExternalPrincipalGrantExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func s3ExternalPrincipalGrantExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, s3ExternalPrincipalGrantExpectedEdgesRelPath)
}

// resolveS3ExternalPrincipalGrantMaterializedEdges is
// s3_external_principal_grant's named vacuity guard (#6228).
//
// Like s3_logs_to, this family's extractor already emits exactly the rows
// the write template UNWINDs, so the rows-to-edges mapping is one-for-one
// with no routing predicate to reproduce. What the fixture proves instead is
// the extractor's resolution behaviour: one grant per projectable principal
// kind (cross-account, public, service) resolves by bucket name while a
// fourth resolves by ARN-tail fallback with a blank bucket name — while a
// grant naming an unscanned bucket, a grant with an unprojectable principal
// kind, and a grant with no principal identity each resolve to nothing
// rather than to a fabricated edge. The resolution mode is pinned on every
// row (single-mode contract), and the idle bucket proves a scanned node
// alone never gains an edge.
//
// The Odù's facts are partitioned by fact kind with the same two-case switch
// the handler's own splitS3ExternalPrincipalGrantEnvelopes uses, not a
// guard-side copy of richer logic: the production split is a pure kind
// partition (resources vs grants, everything else dropped), so reproducing
// its two cases here keeps a partition regression red in both places
// instead of letting the two disagree.
//
// A quarantined fact is fatal rather than skipped, for the same reason as every
// sibling guard: a fixture that stopped decoding against the aws_resource or
// s3_external_principal_grant contract cannot prove the set it names, and
// the surviving facts would understate its own claim.
func resolveS3ExternalPrincipalGrantMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, s3ExternalPrincipalGrantFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	var resources, grants []facts.Envelope
	for _, env := range odu.Facts {
		switch env.FactKind {
		case cloud.AWSResourceFactKind:
			resources = append(resources, env)
		case cloud.S3ExternalPrincipalGrantFactKind:
			grants = append(grants, env)
		}
	}
	rows, _, quarantined, err := s3grant.ExtractS3ExternalPrincipalGrantRows(resources, grants)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractS3ExternalPrincipalGrantRows failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the aws_resource / s3_external_principal_grant contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractS3ExternalPrincipalGrantRows produced zero GRANTS_ACCESS_TO rows; this fixture cannot prove anything", odu.Name)
	}
	for index, row := range rows {
		if got := anyToStringValue(row["relationship_type"]); got != s3ExternalPrincipalGrantRelationshipType {
			return false, fmt.Sprintf("odù %q: row %d carries relationship_type %q, want %q (the writer's single-member vocabulary)", odu.Name, index, got, s3ExternalPrincipalGrantRelationshipType)
		}
		if got := anyToStringValue(row["resolution_mode"]); got != s3ExternalPrincipalGrantResolutionMode {
			return false, fmt.Sprintf("odù %q: row %d carries resolution_mode %q, want %q (the writer's single-mode contract)", odu.Name, index, got, s3ExternalPrincipalGrantResolutionMode)
		}
	}

	actual := s3ExternalPrincipalGrantRowsToExpectedEdges(rows)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, s3ExternalPrincipalGrantFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractS3ExternalPrincipalGrantRows reproduces the expected %d-edge GRANTS_ACCESS_TO set exactly across all %d registry type(s) and %d fact envelope(s); the ghost-source, unsupported-principal, and missing-identity grants produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// s3ExternalPrincipalGrantRowsToExpectedEdges converts the extractor's rows
// one-for-one into the edge identity the write template MERGEs.
//
// source_uid is the :CloudResource bucket uid matched by the template's
// source MATCH and principal_uid is the :ExternalPrincipal uid MERGEd by
// the template, so they are the edge's source and target identity.
// grant_outcome and resolution_mode are SET after the MERGE — deliberately
// outside the relationship key, so a re-resolution converging on one edge
// rather than duplicating it — and are asserted here as mutable properties
// so a stale outcome or a second resolution mode cannot pass on edge
// identity alone.
//
// scope_id, generation_id and evidence_source are stamped by the WRITER from
// its own per-run intent arguments rather than carried on the extractor's
// rows, so they are not part of what this offline guard can or should
// assert; no static set can pin per-run values. Their stamping is covered by
// the writer unit test asserting the annotated row contents instead.
func s3ExternalPrincipalGrantRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: anyToStringValue(row["relationship_type"]),
			SourceEntityID:   anyToStringValue(row["source_uid"]),
			TargetEntityID:   anyToStringValue(row["principal_uid"]),
			Properties: map[string]string{
				"grant_outcome":   anyToStringValue(row["grant_outcome"]),
				"resolution_mode": anyToStringValue(row["resolution_mode"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}
