// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/secretsiam"
)

// Shared offline-chain runner for the three secrets/IAM vault
// direct-materialization families (#6228 slice A): authenticates_vault_role,
// uses_vault_policy, and grants_secret_read.
//
// All three families share one production chain (collector facts ->
// trust-chain read models -> graph-projection rows through
// secretsiam.ProjectSecretsIAMGraphRows), so their guards share one runner
// and differ only in which edge rows they compare. The runner enforces the
// single-scope-generation shape the live cells replay: the trust-chain
// handler loads one scope generation's source facts, so a fixture spanning
// scopes or generations would not mirror any real intent.

// runSecretsIAMFamilyChain runs the production offline chain over the Odù's
// facts and returns the graph-projection rows. A fact-less Odù, a
// multi-scope or multi-generation Odù, a chain error, or any quarantined
// fact fails closed with the reason: a fixture that stopped decoding
// against its contracts cannot prove the set it names, and the surviving
// facts would understate their own claim.
func runSecretsIAMFamilyChain(odu ifa.Odu) (secretsiam.SecretsIAMGraphRows, string) {
	if len(odu.Facts) == 0 {
		return secretsiam.SecretsIAMGraphRows{}, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	scopeID := odu.Facts[0].ScopeID
	generationID := odu.Facts[0].GenerationID
	envelopes := make([]facts.Envelope, 0, len(odu.Facts))
	for _, env := range odu.Facts {
		if env.ScopeID != scopeID || env.GenerationID != generationID {
			return secretsiam.SecretsIAMGraphRows{}, fmt.Sprintf(
				"odù %q: facts span scopes/generations %q/%q and %q/%q; one reducer intent loads one scope generation",
				odu.Name, scopeID, generationID, env.ScopeID, env.GenerationID,
			)
		}
		if env.IsTombstone {
			continue
		}
		envelopes = append(envelopes, env)
	}
	rows, quarantined, err := secretsiam.ProjectSecretsIAMGraphRows(envelopes, scopeID, generationID)
	if err != nil {
		return secretsiam.SecretsIAMGraphRows{}, fmt.Sprintf("odù %q: ProjectSecretsIAMGraphRows failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return secretsiam.SecretsIAMGraphRows{}, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against its contracts, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	return rows, ""
}
