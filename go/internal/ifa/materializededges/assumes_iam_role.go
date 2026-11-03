// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
)

// assumesIAMRoleFamily is the materialized-edge family key this guard
// asserts (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const assumesIAMRoleFamily = "assumes_iam_role"

// assumesIAMRoleRelationshipType is the single relationship type
// SecretsIAMAssumesIAMRoleEdgeUpsertCypher MERGEs. There is no %s to
// fill: the token is static in the template.
//
// Duplicated here as a plain literal rather than imported, and it fails
// CLOSED: missingDirectFamilyExpectedTypes compares the FIXTURE against the
// REGISTRY, so a stale literal here yields a fixture whose type the registry
// does not contain and the guard reds instead of quietly asserting less.
const assumesIAMRoleRelationshipType = "SECRETS_IAM_ASSUMES_IAM_ROLE"

// assumesIAMRoleExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const assumesIAMRoleExpectedEdgesRelPath = "go/internal/ifa/testdata/assumesiamrole/ifa-assumes-iam-role-family-expected-edges.json"

// assumesIAMRoleExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func assumesIAMRoleExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, assumesIAMRoleExpectedEdgesRelPath)
}

// resolveAssumesIAMRoleMaterializedEdges is assumes_iam_role's named
// vacuity guard (#6228).
//
// It runs the full production offline chain over the Odù's collector facts
// (trust-chain build -> read-model payloads -> graph-projection extractor)
// and compares the SECRETS_IAM_ASSUMES_IAM_ROLE rows one-for-one against
// the hand-derived fixture. What the fixture proves is the chain's IAM-role
// endpoint resolution: two exact chains across both identity mechanisms
// (IRSA web-identity and EKS Pod Identity) each yield one edge keyed by the
// (service account, IAM-role CloudResource) pair with the bounded assume
// mode the evidence kind classifies, while a fingerprint-only annotation, a
// partial chain, a Deny, and a wildcard trust each resolve to nothing rather
// than to a fabricated edge. Lane A's second bound Vault role resolves a
// second chain whose row converges on the same edge -- the duplicate
// delivery the extractor's (source, type, target) dedup absorbs.
//
// The Odù's roles carry no token policies on purpose: an exact chain needs
// no policy to assume, and the uses_vault_policy Odù owns the
// policy-fan-out proof. The template SETs rel.assume_mode as a bounded,
// offline-stable classification of the evidence kind, so the offline edge
// carries it as properties; scope_id, generation_id, confidence, and
// evidence_fact_ids are per-run or data-carried values, and
// evidence_source is the writer's stamp covered by its unit test.
//
// A quarantined fact is fatal rather than skipped, for the same reason as
// every sibling guard: a fixture that stopped decoding against its contracts
// cannot prove the set it names, and the surviving facts would understate
// its own claim.
func resolveAssumesIAMRoleMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, assumesIAMRoleFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}
	rows, problem := runSecretsIAMFamilyChain(odu)
	if problem != "" {
		return false, problem
	}
	if len(rows.AssumesIAMRoleEdges) == 0 {
		return false, fmt.Sprintf("odù %q: the chain produced zero SECRETS_IAM_ASSUMES_IAM_ROLE rows; this fixture cannot prove anything", odu.Name)
	}
	actual := assumesIAMRoleRowsToExpectedEdges(rows.AssumesIAMRoleEdges)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, assumesIAMRoleFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: the chain reproduces the expected %d-edge SECRETS_IAM_ASSUMES_IAM_ROLE set exactly across all %d registry type(s) and %d fact envelope(s); the fingerprint-only, partial-chain, Deny, and wildcard-trust lanes produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// assumesIAMRoleRowsToExpectedEdges converts the chain's assumes rows
// one-for-one into the edge identity the write template MERGEs.
//
// service_account_uid is the reducer-owned SecretsIAMServiceAccount node
// uid and cloud_resource_uid the existing IAM-role CloudResource node uid,
// each matched by the template's two MATCH clauses, so they are the edge's
// source and target identity. assume_mode is the template's only stable
// plain-string SET property: the bounded evidence-kind classification the
// extractor derives, carried here as properties so the guard pins the mode
// per lane rather than asserting endpoints alone.
func assumesIAMRoleRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: assumesIAMRoleRelationshipType,
			SourceEntityID:   anyToStringValue(row["service_account_uid"]),
			TargetEntityID:   anyToStringValue(row["cloud_resource_uid"]),
			Properties:       map[string]string{"assume_mode": anyToStringValue(row["assume_mode"])},
		}
		edges = append(edges, edge)
	}
	return edges
}
