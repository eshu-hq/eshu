// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/secretsiam"
)

// TestAssumesIAMRoleDuplicateChainsConvergeOnOneEdge proves lane A's
// duplicate delivery converges: the service account binds TWO Vault roles,
// so the production trust-chain build resolves TWO exact chains, and the
// projection extractor still emits ONE SECRETS_IAM_ASSUMES_IAM_ROLE edge
// for the account.
//
// The vacuity guard alone cannot prove this: the extractor dedups rows by
// (source, type, target) in a map, so a second chain that never resolved
// would still leave a one-edge set. Counting the chains through the same
// production build the guard runs closes that gap -- if the second Vault
// role ever stops binding, this test reds while the edge set still looks
// complete.
func TestAssumesIAMRoleDuplicateChainsConvergeOnOneEdge(t *testing.T) {
	t.Parallel()
	odu := ifa.AssumesIAMRoleFamilyOdu().Odu
	models, quarantined, err := secretsiam.BuildSecretsIAMTrustChainReadModels(odu.Facts)
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels: %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels quarantined %d fixture facts", len(quarantined))
	}
	chains := 0
	vaultRoles := map[string]bool{}
	for _, chain := range models.IdentityTrustChains {
		if chain.ServiceAccountJoinKey != "sha256:sa-assumes-app-a" {
			continue
		}
		if chain.State != secretsiam.SecretsIAMTrustChainStateExact {
			t.Fatalf("lane A chain via Vault role %q is %q, want exact", chain.VaultRoleJoinKey, chain.State)
		}
		chains++
		vaultRoles[chain.VaultRoleJoinKey] = true
	}
	if chains != 2 {
		t.Fatalf("lane A resolved %d exact chains, want 2 (one per bound Vault role)", chains)
	}
	if len(vaultRoles) != 2 {
		t.Fatalf("lane A chains span %d Vault roles, want 2 distinct roles", len(vaultRoles))
	}
	rows, problem := runSecretsIAMFamilyChain(odu)
	if problem != "" {
		t.Fatalf("runSecretsIAMFamilyChain: %s", problem)
	}
	edges := 0
	for _, row := range rows.AssumesIAMRoleEdges {
		if anyToStringValue(row["service_account_uid"]) == "sha256:sa-assumes-app-a" {
			edges++
		}
	}
	if edges != 1 {
		t.Fatalf("lane A emitted %d SECRETS_IAM_ASSUMES_IAM_ROLE rows, want 1 converged edge", edges)
	}
}
