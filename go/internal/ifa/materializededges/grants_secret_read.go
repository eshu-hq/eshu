// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
)

// grantsSecretReadFamily is the materialized-edge family key this guard
// asserts (#6228 remainder), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const grantsSecretReadFamily = "grants_secret_read"

// grantsSecretReadRelationshipType is the single relationship type
// secretsIAMGrantsSecretReadEdgeUpsertCypher MERGEs. There is no %s to
// fill: the token is static in the template.
//
// Duplicated here as a plain literal rather than imported, and it fails
// CLOSED: missingDirectFamilyExpectedTypes compares the FIXTURE against the
// REGISTRY, so a stale literal here yields a fixture whose type the registry
// does not contain and the guard reds instead of quietly asserting less.
const grantsSecretReadRelationshipType = "SECRETS_IAM_GRANTS_SECRET_READ"

// grantsSecretReadExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const grantsSecretReadExpectedEdgesRelPath = "go/internal/ifa/testdata/grantssecretread/ifa-grants-secret-read-family-expected-edges.json"

// grantsSecretReadExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func grantsSecretReadExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, grantsSecretReadExpectedEdgesRelPath)
}

// resolveGrantsSecretReadMaterializedEdges is grants_secret_read's named
// vacuity guard (#6228 remainder).
//
// It runs the full production offline chain over the Odù's collector facts
// (trust-chain build -> read-model payloads -> graph-projection extractor)
// and compares the SECRETS_IAM_GRANTS_SECRET_READ rows one-for-one against
// the hand-derived fixture. What the fixture proves is the chain's
// policy-to-path fan-out: the IRSA lane's role carries two token policies
// with one read rule each and the Pod Identity lane's role carries one,
// each yielding one edge keyed by the (Vault policy, secret path) pair,
// while a chain without a Vault hop, a ghost-bound role, a wildcard trust,
// a wildcard Vault selector, a Deny, a list-only rule, and a read rule
// with no matching KV metadata each resolve to nothing rather than to a
// fabricated edge. The two further exact lanes carry policies that yield
// nothing (a list-only rule, a read rule with no KV metadata), so the
// fan-out cannot leak past read rules with matching paths.
//
// A quarantined fact is fatal rather than skipped, for the same reason as
// every sibling guard: a fixture that stopped decoding against its contracts
// cannot prove the set it names, and the surviving facts would understate
// its own claim.
func resolveGrantsSecretReadMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, grantsSecretReadFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}
	rows, problem := runSecretsIAMFamilyChain(odu)
	if problem != "" {
		return false, problem
	}
	if len(rows.GrantsSecretReadEdges) == 0 {
		return false, fmt.Sprintf("odù %q: the chain produced zero SECRETS_IAM_GRANTS_SECRET_READ rows; this fixture cannot prove anything", odu.Name)
	}
	actual := grantsSecretReadRowsToExpectedEdges(rows.GrantsSecretReadEdges)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, grantsSecretReadFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: the chain reproduces the expected %d-edge SECRETS_IAM_GRANTS_SECRET_READ set exactly across all %d registry type(s) and %d fact envelope(s); the missing-Vault-hop, ghost-bound-role, wildcard-trust, wildcard-Vault-selector, Deny, list-only-capability, and missing-KV-metadata lanes produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// grantsSecretReadRowsToExpectedEdges converts the chain's grants rows
// one-for-one into the edge identity the write template MERGEs.
//
// vault_policy_uid and secret_path_uid are both reducer-owned SecretsIAM*
// node uids, matched by the template's two MATCH clauses, so they are the
// edge's source and target identity. The template SETs capabilities as a
// row-carried list the writer's unit test covers, and no plain-string SET
// property is stable offline, so the offline edge carries no Properties:
// lane 7 proves the capability gate by the absence of its edge.
func grantsSecretReadRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: grantsSecretReadRelationshipType,
			SourceEntityID:   anyToStringValue(row["vault_policy_uid"]),
			TargetEntityID:   anyToStringValue(row["secret_path_uid"]),
		}
		edges = append(edges, edge)
	}
	return edges
}
