// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
)

// authenticatesVaultRoleFamily is the materialized-edge family key this guard
// asserts (#6228 slice A), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const authenticatesVaultRoleFamily = "authenticates_vault_role"

// authenticatesVaultRoleRelationshipType is the single relationship type
// secretsIAMAuthenticatesVaultRoleEdgeUpsertCypher MERGEs. There is no %s to
// fill: the token is static in the template.
//
// Duplicated here as a plain literal rather than imported, and it fails
// CLOSED: missingDirectFamilyExpectedTypes compares the FIXTURE against the
// REGISTRY, so a stale literal here yields a fixture whose type the registry
// does not contain and the guard reds instead of quietly asserting less.
const authenticatesVaultRoleRelationshipType = "SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE"

// authenticatesVaultRoleExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const authenticatesVaultRoleExpectedEdgesRelPath = "go/internal/ifa/testdata/authenticatesvaultrole/ifa-authenticates-vault-role-family-expected-edges.json"

// authenticatesVaultRoleExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func authenticatesVaultRoleExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, authenticatesVaultRoleExpectedEdgesRelPath)
}

// resolveAuthenticatesVaultRoleMaterializedEdges is authenticates_vault_role's
// named vacuity guard (#6228 slice A).
//
// It runs the full production offline chain over the Odù's collector facts
// (trust-chain build -> read-model payloads -> graph-projection extractor)
// and compares the SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE rows one-for-one
// against the hand-derived fixture. What the fixture proves is the chain's
// Vault-hop resolution: two exact chains across both identity mechanisms
// (IRSA web-identity and EKS Pod Identity) each yield one edge keyed by the
// (service account, Vault role) pair, while a chain without a Vault hop, a
// ghost-bound role, a wildcard trust, a wildcard Vault selector, and a Deny
// each resolve to nothing rather than to a fabricated edge.
//
// The Odù's roles carry no token policies on purpose: an exact chain needs
// no policy to authenticate, and the uses_vault_policy Odù owns the
// policy-fan-out proof. The template SETs no plain-string property after the
// MERGE that is stable offline -- scope_id, generation_id, confidence, and
// evidence_fact_ids are per-run or data-carried values, and evidence_source
// is the writer's stamp covered by its unit test -- so the offline edge
// carries no Properties.
//
// A quarantined fact is fatal rather than skipped, for the same reason as
// every sibling guard: a fixture that stopped decoding against its contracts
// cannot prove the set it names, and the surviving facts would understate
// its own claim.
func resolveAuthenticatesVaultRoleMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, authenticatesVaultRoleFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}
	rows, problem := runSecretsIAMFamilyChain(odu)
	if problem != "" {
		return false, problem
	}
	if len(rows.AuthenticatesVaultRoleEdges) == 0 {
		return false, fmt.Sprintf("odù %q: the chain produced zero SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE rows; this fixture cannot prove anything", odu.Name)
	}
	actual := authenticatesVaultRoleRowsToExpectedEdges(rows.AuthenticatesVaultRoleEdges)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, authenticatesVaultRoleFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: the chain reproduces the expected %d-edge SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE set exactly across all %d registry type(s) and %d fact envelope(s); the missing-Vault-hop, ghost-bound-role, wildcard-trust, wildcard-Vault-selector, and Deny lanes produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// authenticatesVaultRoleRowsToExpectedEdges converts the chain's
// authenticates rows one-for-one into the edge identity the write template
// MERGEs.
//
// service_account_uid and vault_auth_role_uid are both reducer-owned
// SecretsIAM* node uids, matched by the template's two MATCH clauses, so
// they are the edge's source and target identity. The template SETs no
// plain-string property that is stable offline, so the offline edge carries
// no Properties.
func authenticatesVaultRoleRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: authenticatesVaultRoleRelationshipType,
			SourceEntityID:   anyToStringValue(row["service_account_uid"]),
			TargetEntityID:   anyToStringValue(row["vault_auth_role_uid"]),
		}
		edges = append(edges, edge)
	}
	return edges
}
