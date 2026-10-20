// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/awscloud"
)

// cloudResourceContainerImageFamily is the materialized-edge family key this
// guard asserts (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const cloudResourceContainerImageFamily = "cloud_resource_container_image"

// cloudResourceContainerImageRelationshipType is the graph relationship type
// the writer interpolates into its upsert template's one %s: the fixed,
// AWS_-namespaced token for the single raw verb the writer accepts. It is
// NOT cloudResourceContainerImageEdgeLabel ("AWS_LAMBDA_FUNCTION_USES_IMAGE"),
// which the writer attaches as statement metadata beside the query and which
// never reaches the graph — the same #6181-shaped trap one level below the
// port name that iam_instance_profile_role documents for HAS_ROLE.
//
// Duplicated here as a plain literal rather than imported, and it fails
// CLOSED: missingDirectFamilyExpectedTypes compares the FIXTURE against the
// REGISTRY, so a stale literal here yields a fixture whose type the registry
// does not contain and the guard reds instead of quietly asserting less.
const cloudResourceContainerImageRelationshipType = "AWS_lambda_function_uses_image"

// cloudResourceContainerImageRawRelationshipType is the raw aws_relationship
// verb the extractor stamps onto each row and the writer screens against its
// closed single-member vocabulary before mapping it to the graph token above.
// Pinning it here binds the third copy — this guard's literal — to the row
// value and the writer vocabulary, so a family that started emitting a
// different verb cannot pass by having its fixture updated to match.
const cloudResourceContainerImageRawRelationshipType = "lambda_function_uses_image"

// cloudResourceContainerImageResolutionMode is the writer's only resolution
// mode, stamped on every row: the two-MATCH-MERGE target is always an exact
// registry+repository@digest reference, never a derived or tag-resolved one.
// Pinning the single mode catches a future second mode silently changing
// what the rows claim.
const cloudResourceContainerImageResolutionMode = "container_image_digest"

// cloudResourceContainerImageExpectedEdgesRelPath is the family's
// hand-derived expected-edge fixture, repoRoot-anchored.
const cloudResourceContainerImageExpectedEdgesRelPath = "go/internal/ifa/testdata/cloudresourcecontainerimage/ifa-cloud-resource-container-image-family-expected-edges.json"

// cloudResourceContainerImageExpectedEdgesPath joins repoRoot onto the
// family's expected-edge fixture.
func cloudResourceContainerImageExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, cloudResourceContainerImageExpectedEdgesRelPath)
}

// resolveCloudResourceContainerImageMaterializedEdges is
// cloud_resource_container_image's named vacuity guard (#6228).
//
// Like s3_logs_to, this family's extractor already emits exactly the rows
// the write template UNWINDs, so the rows-to-edges mapping is one-for-one
// with no routing predicate to reproduce. What the fixture proves instead
// is the extractor's resolution behaviour: two Lambda functions resolve the
// same exact digest reference against scanned nodes — fanning out to one
// edge per function since the dedup key is (source, target) — while a
// tag-only ECS reference (EXACT-ONLY policy), a digest-less zip function, an
// unparseable tag-only ref, an unscanned ghost function, and a foreign verb
// each resolve to nothing rather than to a fabricated edge. The raw verb
// and the resolution mode are pinned on every row (closed-vocabulary and
// single-mode contracts).
//
// The Odù's facts are partitioned by fact kind with the same two-case split
// the handler's own load uses (resources vs relationships, everything else
// dropped), not a guard-side copy of richer logic. A quarantined fact is
// fatal rather than skipped, for the same reason as every sibling guard: a
// fixture that stopped decoding against the aws_resource / aws_relationship
// contract cannot prove the set it names, and the surviving facts would
// understate its own claim.
func resolveCloudResourceContainerImageMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, cloudResourceContainerImageFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	var resources, relationships []facts.Envelope
	for _, env := range odu.Facts {
		switch env.FactKind {
		case cloud.AWSResourceFactKind:
			resources = append(resources, env)
		case cloud.AWSRelationshipFactKind:
			relationships = append(relationships, env)
		}
	}
	rows, _, quarantined, err := awscloud.ExtractAWSCloudImageEdgeRows(resources, relationships)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractAWSCloudImageEdgeRows failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the aws_resource / aws_relationship contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractAWSCloudImageEdgeRows produced zero AWS_lambda_function_uses_image rows; this fixture cannot prove anything", odu.Name)
	}
	for index, row := range rows {
		if got := anyToStringValue(row["relationship_type"]); got != cloudResourceContainerImageRawRelationshipType {
			return false, fmt.Sprintf("odù %q: row %d carries relationship_type %q, want %q; the writer screens this value against a closed single-member vocabulary, so a different token would be rejected at write time rather than materialized", odu.Name, index, got, cloudResourceContainerImageRawRelationshipType)
		}
		if got := anyToStringValue(row["resolution_mode"]); got != cloudResourceContainerImageResolutionMode {
			return false, fmt.Sprintf("odù %q: row %d carries resolution_mode %q, want %q (the writer's single-mode contract)", odu.Name, index, got, cloudResourceContainerImageResolutionMode)
		}
	}

	actual := cloudResourceContainerImageRowsToExpectedEdges(rows)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, cloudResourceContainerImageFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractAWSCloudImageEdgeRows reproduces the expected %d-edge AWS_lambda_function_uses_image set exactly across all %d registry type(s) and %d fact envelope(s); the tag-only, digest-less, unparseable, ghost-source and foreign-verb relationships produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// cloudResourceContainerImageRowsToExpectedEdges converts the extractor's
// rows one-for-one into the edge identity the write template MERGEs.
//
// source_uid and target_uid are the :CloudResource and :ContainerImage uids
// the template's two MATCH clauses bind, so they are the edge's source and
// target identity. The relationship type is the graph token the writer
// interpolates (not the row's raw verb, which is pinned above and never
// reaches the graph). resolution_mode is SET after the MERGE — deliberately
// outside the relationship key — and is asserted here as a mutable pin.
// scope_id, generation_id and evidence_source are stamped by the WRITER from
// its own arguments rather than carried on the extractor's rows, so they
// are outside what this guard can or should assert. The live assert reads
// only fixture-declared property keys off each rel, so extra live props
// never collide with this pin.
func cloudResourceContainerImageRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: cloudResourceContainerImageRelationshipType,
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
