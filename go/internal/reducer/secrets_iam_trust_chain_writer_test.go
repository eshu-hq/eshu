// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

func TestPostgresSecretsIAMTrustChainWriterKeysFactsByScopeGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresSecretsIAMTrustChainWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	for _, write := range []SecretsIAMTrustChainWrite{
		secretsIAMTrustChainWriteForScope("scope-a", "generation-a"),
		secretsIAMTrustChainWriteForScope("scope-b", "generation-b"),
	} {
		if _, err := writer.WriteSecretsIAMTrustChainReadModels(context.Background(), write); err != nil {
			t.Fatalf("WriteSecretsIAMTrustChainReadModels() error = %v, want nil", err)
		}
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	if db.execs[0].args[0] == db.execs[1].args[0] {
		t.Fatalf("fact_id collided across scope/generation: %v", db.execs[0].args[0])
	}
	if db.execs[0].args[4] == db.execs[1].args[4] {
		t.Fatalf("stable_fact_key collided across scope/generation: %v", db.execs[0].args[4])
	}
}

func secretsIAMTrustChainWriteForScope(scopeID, generationID string) SecretsIAMTrustChainWrite {
	return SecretsIAMTrustChainWrite{
		IntentID:     "intent-" + scopeID,
		ScopeID:      scopeID,
		GenerationID: generationID,
		SourceSystem: "secrets_iam_posture",
		Models: SecretsIAMTrustChainReadModels{
			IdentityTrustChains: []SecretsIAMIdentityTrustChain{{
				ChainID:               "identity_trust_chain:same-model",
				State:                 SecretsIAMTrustChainStateExact,
				Confidence:            "exact",
				ServiceAccountJoinKey: "sha256:service-account",
				WorkloadObjectID:      "workload-object",
				IAMRoleFingerprint:    "sha256:iam-role",
				VaultRoleJoinKey:      "sha256:vault-role",
				EvidenceFactIDs:       []string{"fact-1"},
			}},
		},
	}
}
