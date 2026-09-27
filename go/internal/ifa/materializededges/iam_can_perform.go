// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
)

// iamCanPerformFamily is the materialized-edge family key this guard asserts
// (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const iamCanPerformFamily = "iam_can_perform"

// iamCanPerformRelationshipType is the single relationship type
// CanonicalIAMCanPerformEdgeUpsertCypher MERGEs. There is no %s to fill: the
// granted action set lives in rel.actions, never in the relationship type.
//
// It is NOT iamCanPerformEdgeLabel ("IAM_CAN_PERFORM"), which the writer
// attaches as statement metadata beside the query and which never reaches
// the graph. Taking the type from that label is the same name-derived
// mistake at one level below the port name, and it would make this guard
// assert an always-empty population.
//
// Duplicated here as a plain literal rather than imported, and it fails CLOSED:
// missingDirectFamilyExpectedTypes compares the FIXTURE against the REGISTRY,
// so a stale literal here yields a fixture whose type the registry does not
// contain and the guard reds instead of quietly asserting less.
const iamCanPerformRelationshipType = "CAN_PERFORM"

// iamCanPerformExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const iamCanPerformExpectedEdgesRelPath = "go/internal/ifa/testdata/iamcanperform/ifa-iam-can-perform-family-expected-edges.json"

// iamCanPerformExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func iamCanPerformExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, iamCanPerformExpectedEdgesRelPath)
}

// resolveIAMCanPerformMaterializedEdges is iam_can_perform's named vacuity
// guard (#6228).
//
// Like iam_can_assume, this family's extractor already emits exactly the rows
// the write template UNWINDs, so the rows-to-edges mapping is one-for-one
// with no routing predicate to reproduce. What the fixture proves instead is
// the extractor's resolution behaviour: three Allows fan out one edge per
// DISTINCT resolved (principal, resource) pair — two catalog actions on one
// bucket converging on a single merged-actions edge, a KMS action on a second
// service family, a DynamoDB action on a role principal — while a
// type-mismatched ARN, a Deny, a conditioned statement, a NotAction, an
// uncatalogued action, a wildcard, an unscanned target ARN, a non-identity
// source, an unscanned principal, and a catalogued-action/wrong-target pair
// each resolve to nothing rather than to a fabricated edge. The merged action
// sets themselves are pinned per pair in checkIAMCanPerformMergedActions,
// not in the fixture: the writer stores them as SET LIST properties and the
// shared fixture (which the live cell also reads) can only carry strings.
//
// The Odù's facts are partitioned with the production
// iamcan.SplitIAMCanPerformEnvelopes the handler itself calls, not a guard-side
// copy of it, so a partition regression reds the handler and this guard
// together instead of letting the two disagree. Boundary and resource-policy
// partitions must be empty here: this Odù exercises the identity-policy path
// only, and a stray fact of either kind would assert an evaluation_scope the
// identity path never stamps.
//
// A quarantined fact is fatal rather than skipped, for the same reason as every
// sibling guard: a fixture that stopped decoding against the aws_resource or
// aws_iam_permission contract cannot prove the set it names, and the surviving
// facts would understate its own claim.
func resolveIAMCanPerformMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, iamCanPerformFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	resources, permissions, boundaries, resourcePolicies := iamcan.SplitIAMCanPerformEnvelopes(odu.Facts)
	if len(boundaries) > 0 {
		return false, fmt.Sprintf("odù %q: carries %d permission-boundary fact(s); this Odù exercises the identity-policy path only", odu.Name, len(boundaries))
	}
	if len(resourcePolicies) > 0 {
		return false, fmt.Sprintf("odù %q: carries %d resource-policy fact(s); this Odù exercises the identity-policy path only", odu.Name, len(resourcePolicies))
	}
	result, err := iamcan.ExtractIAMCanPerformEdges(resources, permissions)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractIAMCanPerformEdges failed: %v", odu.Name, err)
	}
	if len(result.Quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the aws_resource / aws_iam_permission contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(result.Quarantined))
	}
	if len(result.Edges) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractIAMCanPerformEdges produced zero CAN_PERFORM rows; this fixture cannot prove anything", odu.Name)
	}

	actual := iamCanPerformRowsToExpectedEdges(result.Edges)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, iamCanPerformFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	if problem := checkIAMCanPerformMergedActions(odu.Name, result.Edges); problem != "" {
		return false, problem
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractIAMCanPerformEdges reproduces the expected %d-edge CAN_PERFORM set exactly across all %d registry type(s) and %d fact envelope(s), with the merged action sets pinned per pair; the type-mismatch, deny, conditioned, NotAction, uncatalogued, wildcard, unscanned-target, non-identity-source, unscanned-principal, and wrong-target statements produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// iamCanPerformExpectedMergedActions pins each expected edge's merged,
// sorted granted action set and grant sources, keyed by the endpoint-uid
// pair. Hand-derived from the fixture statements, never read back out of the
// rows under test: the extractor merges every catalog action resolving to
// the same (principal, resource) pair into one row's sorted set, so the S3
// pair carries s3:getobject,s3:putobject while the KMS and DynamoDB pairs
// each carry their single action. All three pairs resolve through
// identity-policy statements only.
//
// This check exists because the shared expected-edge fixture cannot carry
// actions or grant_sources: the writer stores both as SET LIST properties
// and the live assert-edges comparison round-trips strings only
// (readExpectedProperties, go/cmd/ifa/assert_edges_scan.go), so those keys
// would red the live cell on every run. evaluation_scope is a plain string
// and stays in the fixture; the lists are pinned here, over the extractor
// rows directly, plus the writer unit test pinning the SET list shape.
func iamCanPerformExpectedMergedActions() map[[2]string][2]string {
	return map[[2]string][2]string{
		// deployer -> reports bucket: two catalog actions, one edge.
		{
			"8327c4a5dcbfe218637938fb9b0659cb4fd859465121a14dbdf32f4446d5babf",
			"f8ce91bd1613a10b45e8bf8df8c30978948794888d62eba449afe3a151c9793c",
		}: {"s3:getobject,s3:putobject", "identity_policy"},
		// deployer -> vault key: second service family, same principal.
		{
			"8327c4a5dcbfe218637938fb9b0659cb4fd859465121a14dbdf32f4446d5babf",
			"0af6fe0dd74e9868f5dcc4751078f5d0371d82b96f118e61c87361c8af3ef453",
		}: {"kms:decrypt", "identity_policy"},
		// ci-role -> orders table: role principals resolve too.
		{
			"9d0fe620dfc642b2ba92d822ecab2032f6018f6093815fd029402287d0a5a5a1",
			"0f71ce5432e24475cbe8883c1b5029bf98102516bbc5abb1cd03a60164518181",
		}: {"dynamodb:getitem", "identity_policy"},
	}
}

// checkIAMCanPerformMergedActions pins the merged action set and grant
// sources per edge pair. A regression that emitted one edge per ACTION
// instead of one per resolved pair would still reproduce the endpoint pairs
// and pass the fixture comparison while doubling the S3 edge; a regression
// that dropped the merge sort would pass on set membership. Both fail here.
func checkIAMCanPerformMergedActions(oduName string, rows []map[string]any) string {
	want := iamCanPerformExpectedMergedActions()
	if len(rows) != len(want) {
		return fmt.Sprintf("odù %q: extractor produced %d CAN_PERFORM rows, want %d; the merged-actions table cannot pin a different-sized set", oduName, len(rows), len(want))
	}
	for index, row := range rows {
		pair := [2]string{anyToStringValue(row["principal_uid"]), anyToStringValue(row["resource_uid"])}
		pinned, ok := want[pair]
		if !ok {
			return fmt.Sprintf("odù %q: extractor row %d names endpoint pair %v no hand derivation sanctions -- a fabricated endpoint or a dropped negative control", oduName, index, pair)
		}
		if got := joinCanPerformAnyValues(row["actions"]); got != pinned[0] {
			return fmt.Sprintf("odù %q: endpoint pair %v merges actions %q, want %q; one edge per resolved pair with the sorted merged set, not one edge per action", oduName, pair, got, pinned[0])
		}
		if got := joinCanPerformAnyValues(row["grant_sources"]); got != pinned[1] {
			return fmt.Sprintf("odù %q: endpoint pair %v carries grant sources %q, want %q", oduName, pair, got, pinned[1])
		}
	}
	return ""
}

// iamCanPerformRowsToExpectedEdges converts the extractor's rows one-for-one
// into the edge identity the write template MERGEs.
//
// principal_uid and resource_uid are both :CloudResource uids, matched by the
// template's two MATCH clauses, so they are the edge's source and target
// identity. evaluation_scope is SET after the MERGE — deliberately outside
// the relationship key, so a re-resolution converges on one edge rather than
// duplicating it — and is asserted here as a mutable property so a stale
// scope cannot pass on edge identity alone. actions and grant_sources are
// also SET after the MERGE but as LIST properties, which the shared fixture
// cannot carry (see checkIAMCanPerformMergedActions); they are pinned there,
// not here.
//
// scope_id, generation_id and evidence_source are stamped by the WRITER from
// its own per-run intent arguments (and its reducer-owned evidence-source
// constant) rather than carried on the extractor's rows, so they are not part
// of what this offline guard can or should assert; no static set can pin
// per-run values. Their stamping is covered by the writer unit test asserting
// the annotated row contents instead.
func iamCanPerformRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: iamCanPerformRelationshipType,
			SourceEntityID:   anyToStringValue(row["principal_uid"]),
			TargetEntityID:   anyToStringValue(row["resource_uid"]),
			Properties: map[string]string{
				"evaluation_scope": anyToStringValue(row["evaluation_scope"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}

// joinCanPerformAnyValues renders an extractor set-valued row field (actions,
// grant_sources) as the comma-joined string the expected-edge fixture pins.
// The extractor already sorts both sets, so no second sort here — sorting
// would mask an extractor ordering regression this comparison should catch.
// Rows carry []string (sortedCanPerformActions / sortedCanPerformStringSet);
// []any is accepted too so a row-shape change fails on values, not on types.
func joinCanPerformAnyValues(value any) string {
	switch items := value.(type) {
	case []string:
		return strings.Join(items, ",")
	case []any:
		rendered := make([]string, 0, len(items))
		for _, item := range items {
			rendered = append(rendered, anyToStringValue(item))
		}
		return strings.Join(rendered, ",")
	default:
		return anyToStringValue(value)
	}
}
