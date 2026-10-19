// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
)

// usesVaultPolicyFamily is the materialized-edge family key this guard
// asserts (#6228 slice A), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const usesVaultPolicyFamily = "uses_vault_policy"

// usesVaultPolicyRelationshipType is the single relationship type
// SecretsIAMUsesVaultPolicyEdgeUpsertCypher MERGEs. There is no %s to
// fill: the token is static in the template.
//
// Duplicated here as a plain literal rather than imported, and it fails
// CLOSED: missingDirectFamilyExpectedTypes compares the FIXTURE against the
// REGISTRY, so a stale literal here yields a fixture whose type the registry
// does not contain and the guard reds instead of quietly asserting less.
const usesVaultPolicyRelationshipType = "SECRETS_IAM_USES_VAULT_POLICY"

// usesVaultPolicyExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const usesVaultPolicyExpectedEdgesRelPath = "go/internal/ifa/testdata/usesvaultpolicy/ifa-uses-vault-policy-family-expected-edges.json"

// usesVaultPolicyExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func usesVaultPolicyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, usesVaultPolicyExpectedEdgesRelPath)
}

// resolveUsesVaultPolicyMaterializedEdges is uses_vault_policy's named
// vacuity guard (#6228 slice A).
//
// It runs the full production offline chain over the Odù's collector facts
// (trust-chain build -> read-model payloads -> graph-projection extractor)
// and compares the SECRETS_IAM_USES_VAULT_POLICY rows one-for-one against
// the hand-derived fixture. What the fixture proves is the chain's
// policy-fan-out: the IRSA lane's role carries two token policies and the
// Pod Identity lane's role carries one, each yielding one edge keyed by
// the (Vault role, Vault policy) pair, while a chain without a Vault hop,
// a ghost-bound role, a wildcard trust, a wildcard Vault selector, and a
// Deny each resolve to nothing rather than to a fabricated edge. Roles
// outside the two exact lanes carry no token policies, so the fan-out
// cannot leak past the lanes that own it.
//
// A quarantined fact is fatal rather than skipped, for the same reason as
// every sibling guard: a fixture that stopped decoding against its contracts
// cannot prove the set it names, and the surviving facts would understate
// its own claim.
func resolveUsesVaultPolicyMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, usesVaultPolicyFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}
	rows, problem := runSecretsIAMFamilyChain(odu)
	if problem != "" {
		return false, problem
	}
	if len(rows.UsesVaultPolicyEdges) == 0 {
		return false, fmt.Sprintf("odù %q: the chain produced zero SECRETS_IAM_USES_VAULT_POLICY rows; this fixture cannot prove anything", odu.Name)
	}
	actual := usesVaultPolicyRowsToExpectedEdges(rows.UsesVaultPolicyEdges)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, usesVaultPolicyFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: the chain reproduces the expected %d-edge SECRETS_IAM_USES_VAULT_POLICY set exactly across all %d registry type(s) and %d fact envelope(s); the missing-Vault-hop, ghost-bound-role, wildcard-trust, wildcard-Vault-selector, and Deny lanes produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// usesVaultPolicyRowsToExpectedEdges converts the chain's uses rows
// one-for-one into the edge identity the write template MERGEs.
//
// vault_auth_role_uid and vault_policy_uid are both reducer-owned
// SecretsIAM* node uids, matched by the template's two MATCH clauses, so
// they are the edge's source and target identity. The template SETs no
// plain-string property that is stable offline, so the offline edge carries
// no Properties.
func usesVaultPolicyRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: usesVaultPolicyRelationshipType,
			SourceEntityID:   anyToStringValue(row["vault_auth_role_uid"]),
			TargetEntityID:   anyToStringValue(row["vault_policy_uid"]),
		}
		edges = append(edges, edge)
	}
	return edges
}
