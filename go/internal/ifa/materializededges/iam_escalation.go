// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamescalation"
)

// iamEscalationFamily is the materialized-edge family key this guard asserts
// (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const iamEscalationFamily = "iam_escalation"

// iamEscalationRelationshipType is the single relationship type
// CanonicalIAMEscalationEdgeUpsertCypher MERGEs. There is no %s to fill: the
// merged primitive set lives in rel.primitives, never in the relationship
// type.
//
// Read it off the template and the iamEscalationEdgeLabel const, which IS
// "CAN_ESCALATE_TO" (the const doubles as the relationship type and the
// statement-metadata tag). "IAM_ESCALATION" appears nowhere in code: taking
// the type from the port or family name is the same name-derived mistake one
// level above, and it would make this guard assert an always-empty
// population.
//
// Duplicated here as a plain literal rather than imported, and it fails CLOSED:
// missingDirectFamilyExpectedTypes compares the FIXTURE against the REGISTRY,
// so a stale literal here yields a fixture whose type the registry does not
// contain and the guard reds instead of quietly asserting less.
const iamEscalationRelationshipType = "CAN_ESCALATE_TO"

// iamEscalationExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const iamEscalationExpectedEdgesRelPath = "go/internal/ifa/testdata/iamescalation/ifa-iam-escalation-family-expected-edges.json"

// iamEscalationExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func iamEscalationExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, iamEscalationExpectedEdgesRelPath)
}

// resolveIAMEscalationMaterializedEdges is iam_escalation's named vacuity
// guard (#6228).
//
// Like iam_can_perform, this family's extractor already emits exactly the rows
// the write template UNWINDs, so the rows-to-edges mapping is one-for-one
// with no routing predicate to reproduce. What the fixture proves instead is
// the extractor's resolution behaviour: six Allows fan out one edge per
// DISTINCT resolved (principal, target) pair — a policy-target primitive,
// three primitives from two statements converging on a single
// merged-primitives role edge, a group-target primitive, a PassRole-family
// multi-action primitive, and a second principal resolving a user target —
// while a self-loop, a Deny, a conditioned statement, a NotAction, a
// wildcard, an unscanned target ARN, an unscanned principal, a deferred
// sts:AssumeRole, and a user-action/wrong-role-target pair each resolve to
// nothing rather than to a fabricated edge. The merged primitive sets
// themselves are pinned per pair in checkIAMEscalationMergedPrimitives, not
// in the fixture: the writer stores them as a SET LIST property and the
// shared fixture (which the live cell also reads) can only carry strings.
//
// The Odù's facts are partitioned by fact kind with the same two-case switch
// the handler's own splitIAMEscalationEnvelopes uses, not a guard-side copy
// of richer logic: the production split is a pure kind partition (resource
// vs permission, everything else dropped), so reproducing its two cases here
// keeps a partition regression red in both places instead of letting the two
// disagree. Tombstoned envelopes are dropped on both sides alike.
//
// A quarantined fact is fatal rather than skipped, for the same reason as every
// sibling guard: a fixture that stopped decoding against the aws_resource or
// aws_iam_permission contract cannot prove the set it names, and the surviving
// facts would understate its own claim.
func resolveIAMEscalationMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, iamEscalationFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	var resources, permissions []facts.Envelope
	for _, env := range odu.Facts {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case facts.AWSIAMPermissionFactKind:
			permissions = append(permissions, env)
		}
	}
	result, err := iamescalation.ExtractIAMEscalationEdges(resources, permissions)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractIAMEscalationEdges failed: %v", odu.Name, err)
	}
	if len(result.Quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the aws_resource / aws_iam_permission contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(result.Quarantined))
	}
	if len(result.Edges) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractIAMEscalationEdges produced zero CAN_ESCALATE_TO rows; this fixture cannot prove anything", odu.Name)
	}

	actual := iamEscalationRowsToExpectedEdges(result.Edges)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, iamEscalationFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	if problem := checkIAMEscalationMergedPrimitives(odu.Name, result.Edges); problem != "" {
		return false, problem
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractIAMEscalationEdges reproduces the expected %d-edge CAN_ESCALATE_TO set exactly across all %d registry type(s) and %d fact envelope(s), with the merged primitive sets pinned per pair; the self-loop, deny, conditioned, NotAction, wildcard, unscanned-target, unscanned-principal, sts:AssumeRole-deferral, and wrong-target statements produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// iamEscalationExpectedMergedPrimitives pins each expected edge's merged,
// sorted primitive token set, keyed by the endpoint-uid pair. Hand-derived
// from the fixture statements and verified against the extractor's own
// output, never read back out of the rows under test: the extractor merges
// every primitive resolving to the same (principal, target) pair into one
// row's sorted set, so the exec-role pair carries three tokens while every
// other pair carries one. All five pairs resolve through inline Allow
// identity statements.
//
// This check exists because the shared expected-edge fixture cannot carry
// primitives: the writer stores them as a SET LIST property
// (rel.primitives) and the live assert-edges comparison round-trips strings
// only (readExpectedProperties, go/cmd/ifa/assert_edges_scan.go), so those
// keys would red the live cell on every run. primitive_count is the row's
// own length and needs no separate pin beyond the set it counts.
func iamEscalationExpectedMergedPrimitives() map[[2]string]string {
	return map[[2]string]string{
		// attacker -> team policy: the policy-target primitive.
		{
			"b70b554fc0787a73bda123c00d0d5c3df4bca48d41a7ab8e810bbdcd69fcc4db",
			"7f53d2fda5a780a0bd3e302f2c12ffed33757e587bf151cd61e85f3203382867",
		}: "iam_create_policy_version",
		// attacker -> exec role: three primitives, one edge.
		{
			"b70b554fc0787a73bda123c00d0d5c3df4bca48d41a7ab8e810bbdcd69fcc4db",
			"e1cec07643adc9533e38d2889b7e3545322957e42f14a28ee96d9ea8a20edb07",
		}: "iam_attach_role_policy,iam_put_role_policy,iam_update_assume_role_policy",
		// attacker -> admins group: the group-target primitive.
		{
			"b70b554fc0787a73bda123c00d0d5c3df4bca48d41a7ab8e810bbdcd69fcc4db",
			"a5388d1d4ba3ed4cd90097cdca050b7f91d38fdbb3a6b2b239b82786b6ef78d7",
		}: "iam_add_user_to_group",
		// attacker -> deploy role: the PassRole-family multi-action primitive.
		{
			"b70b554fc0787a73bda123c00d0d5c3df4bca48d41a7ab8e810bbdcd69fcc4db",
			"0547d48134b3ea7004f4aa65c9fc7fd11cbe3d84d1f56a10967323b719d8d43e",
		}: "passrole_lambda",
		// victim -> attacker: the second principal resolving a user target.
		{
			"f23d56ffebe7cb11479a5d92f49052066ec824e77ec4e7044db2f39a17ba5fa7",
			"b70b554fc0787a73bda123c00d0d5c3df4bca48d41a7ab8e810bbdcd69fcc4db",
		}: "iam_create_access_key",
	}
}

// checkIAMEscalationMergedPrimitives pins the merged primitive set per edge
// pair. A regression that emitted one edge per PRIMITIVE instead of one per
// resolved pair would still reproduce the endpoint pairs and pass the
// fixture comparison while tripling the exec-role edge; a regression that
// dropped the merge sort would pass on set membership. Both fail here.
func checkIAMEscalationMergedPrimitives(oduName string, rows []map[string]any) string {
	want := iamEscalationExpectedMergedPrimitives()
	if len(rows) != len(want) {
		return fmt.Sprintf("odù %q: extractor produced %d CAN_ESCALATE_TO rows, want %d; the merged-primitives table cannot pin a different-sized set", oduName, len(rows), len(want))
	}
	for index, row := range rows {
		pair := [2]string{anyToStringValue(row["principal_uid"]), anyToStringValue(row["target_uid"])}
		pinned, ok := want[pair]
		if !ok {
			return fmt.Sprintf("odù %q: extractor row %d names endpoint pair %v no hand derivation sanctions -- a fabricated endpoint or a dropped negative control", oduName, index, pair)
		}
		if got := joinEscalationAnyValues(row["primitives"]); got != pinned {
			return fmt.Sprintf("odù %q: endpoint pair %v merges primitives %q, want %q; one edge per resolved pair with the sorted merged set, not one edge per primitive", oduName, pair, got, pinned)
		}
	}
	return ""
}

// iamEscalationRowsToExpectedEdges converts the extractor's rows one-for-one
// into the edge identity the write template MERGEs.
//
// principal_uid and target_uid are both :CloudResource uids, matched by the
// template's two MATCH clauses, so they are the edge's source and target
// identity. The template SETs no plain-string property after the MERGE —
// primitives is a LIST, primitive_count an int, and scope_id/generation_id/
// evidence_source are stamped by the WRITER from its own per-run intent
// arguments (and its reducer-owned evidence-source constant) rather than
// carried on the extractor's rows — so the offline edge carries no
// Properties: no static set can pin per-run values, and the lists are pinned
// in checkIAMEscalationMergedPrimitives instead. Their stamping is covered
// by the writer unit test asserting the annotated row contents instead.
func iamEscalationRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: iamEscalationRelationshipType,
			SourceEntityID:   anyToStringValue(row["principal_uid"]),
			TargetEntityID:   anyToStringValue(row["target_uid"]),
		}
		edges = append(edges, edge)
	}
	return edges
}

// joinEscalationAnyValues renders an extractor set-valued row field
// (primitives) as the comma-joined string the merged-primitives table pins.
// The extractor already sorts the set, so no second sort here — sorting
// would mask an extractor ordering regression this comparison should catch.
// Rows carry []string (sortedPrimitiveTokens); []any is accepted too so a
// row-shape change fails on values, not on types.
func joinEscalationAnyValues(value any) string {
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
