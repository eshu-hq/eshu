// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package posture

import (
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

// TestVaultAuthRolePayloadPassesSDKRedactionGate is the seeded-violation
// regression for eshu-hq/eshu#8869: the production vault_auth_role envelope
// (integer TTL duration, integer policy count, HMAC policy-name
// fingerprints) must validate through the pinned SDK's host-side
// ValidateResult walk — the same walk that ended collection runs with
// invalid_result before the allow-list — instead of failing closed.
func TestVaultAuthRolePayloadPassesSDKRedactionGate(t *testing.T) {
	t.Parallel()

	ctx := testVaultContext()
	env, err := NewVaultAuthRoleEnvelope(VaultAuthRoleObservation{
		Context:                       ctx,
		MountPath:                     "auth/kubernetes",
		RoleName:                      "payments-api",
		AuthMethod:                    VaultAuthMethodKubernetes,
		BoundServiceAccountNames:      []string{"checkout-sa"},
		BoundServiceAccountNamespaces: []string{"payments"},
		TokenPolicyNames:              []string{"prod-payments-read"},
		TokenTTLSeconds:               900,
		SourceURI:                     "https://vault.example.test/v1/auth/kubernetes/role/payments-api",
		SourceRecordID:                "auth/kubernetes/role/payments-api",
	})
	if err != nil {
		t.Fatalf("NewVaultAuthRoleEnvelope() error = %v", err)
	}

	// The three keys that ended QA collection with invalid_result. If the
	// producer stops emitting any of them, this test must fail loudly
	// rather than silently stop exercising the gate.
	for _, key := range []string{"token_ttl_seconds", "token_policy_count", "token_policy_name_fingerprints"} {
		if _, ok := env.Payload[key]; !ok {
			t.Fatalf("auth-role payload missing %q: the test no longer exercises the #8869 gate", key)
		}
	}

	contract := sdkcollector.Contract{Facts: []sdkcollector.FactDeclaration{{
		Kind:             env.FactKind,
		SchemaVersions:   []string{env.SchemaVersion},
		SourceConfidence: []sdkcollector.SourceConfidence{sdkcollector.SourceConfidence(env.SourceConfidence)},
	}}}
	claim := sdkcollector.Claim{
		ComponentID:   "dev.eshu.collector.vault-live",
		InstanceID:    "vault-live-test-1",
		CollectorKind: "vault_live",
		SourceSystem:  env.SourceRef.SourceSystem,
		Scope:         sdkcollector.Scope{ID: env.SourceRef.ScopeID, Kind: "vault"},
		SourceRunID:   "test-run",
		GenerationID:  env.GenerationID,
		WorkItemID:    "test-work-item",
		FencingToken:  "11",
		ConfigHandle:  "test-config",
		Attempt:       1,
		Deadline:      time.Date(2026, 6, 2, 15, 0, 0, 0, time.UTC),
	}
	result := sdkcollector.Result{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		State:           sdkcollector.ResultComplete,
		Claim:           claim,
		Generation:      sdkcollector.Generation{ID: env.GenerationID, ObservedAt: time.Date(2026, 6, 2, 14, 0, 0, 0, time.UTC)},
		Facts: []sdkcollector.Fact{{
			Kind:             env.FactKind,
			SchemaVersion:    env.SchemaVersion,
			StableKey:        env.StableFactKey,
			SourceConfidence: sdkcollector.SourceConfidence(env.SourceConfidence),
			ObservedAt:       env.ObservedAt,
			SourceRef: sdkcollector.SourceRef{
				SourceSystem: env.SourceRef.SourceSystem,
				ScopeID:      env.SourceRef.ScopeID,
				GenerationID: env.SourceRef.GenerationID,
				FactKey:      env.SourceRef.FactKey,
				URI:          env.SourceRef.SourceURI,
				RecordID:     env.SourceRef.SourceRecordID,
			},
			Payload: env.Payload,
		}},
	}
	if _, err := sdkcollector.NewValidator(contract).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult(vault_auth_role) = %v, want nil (#8869)", err)
	}
}
