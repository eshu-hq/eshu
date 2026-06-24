// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestNewVaultKVMetadataEnvelopeRedactsPathAndCustomMetadata(t *testing.T) {
	ctx := testVaultContext()
	env, err := NewVaultKVMetadataEnvelope(VaultKVMetadataObservation{
		Context:                ctx,
		MountPath:              "secret/",
		Path:                   "payments/prod/api-key",
		CurrentVersion:         7,
		OldestVersion:          2,
		MaxVersions:            10,
		CASRequired:            true,
		DeleteVersionAfterSecs: 3600,
		CustomMetadataKeys:     []string{"owner", "private_url"},
	})
	if err != nil {
		t.Fatalf("NewVaultKVMetadataEnvelope() error = %v", err)
	}
	assertFact(t, env, facts.VaultKVMetadataFactKind)
	assertPayloadString(t, env.Payload, "provider", ProviderVault)
	if got, _ := env.Payload["path_depth"].(int); got != 3 {
		t.Fatalf("path_depth = %d, want 3", got)
	}
	if got, _ := env.Payload["custom_metadata_key_count"].(int); got != 2 {
		t.Fatalf("custom_metadata_key_count = %d, want 2", got)
	}
	if payloadContains(env.Payload, "payments") ||
		payloadContains(env.Payload, "api-key") ||
		payloadContains(env.Payload, "owner") ||
		payloadContains(env.Payload, "private_url") {
		t.Fatalf("Vault KV metadata payload leaked raw path or custom metadata keys: %#v", env.Payload)
	}
	if env.Payload["kv_path_fingerprint"] == "" || env.Payload["mount_join_key"] == "" {
		t.Fatalf("Vault KV metadata payload missing fingerprints or join keys: %#v", env.Payload)
	}
	if got, _ := env.Payload["kv_path_fingerprint"].(string); !strings.HasPrefix(got, "redacted:hmac-sha256:") {
		t.Fatalf("kv_path_fingerprint = %q, want keyed HMAC marker", got)
	}
}

func TestNewVaultKVMetadataEnvelopeStableAcrossRepeatedObservation(t *testing.T) {
	firstCtx := testVaultContext()
	first, err := NewVaultKVMetadataEnvelope(VaultKVMetadataObservation{
		Context:        firstCtx,
		MountPath:      "secret/",
		Path:           "payments/prod/api-key",
		CurrentVersion: 7,
	})
	if err != nil {
		t.Fatalf("NewVaultKVMetadataEnvelope(first) error = %v", err)
	}
	nextCtx := firstCtx
	nextCtx.GenerationID = "vault:vault-prod:admin-payments:secret:2"
	next, err := NewVaultKVMetadataEnvelope(VaultKVMetadataObservation{
		Context:        nextCtx,
		MountPath:      "secret/",
		Path:           "payments/prod/api-key",
		CurrentVersion: 8,
	})
	if err != nil {
		t.Fatalf("NewVaultKVMetadataEnvelope(next) error = %v", err)
	}
	if first.StableFactKey != next.StableFactKey {
		t.Fatalf("StableFactKey changed across repeated observation: %q != %q", first.StableFactKey, next.StableFactKey)
	}
	if first.FactID == next.FactID {
		t.Fatalf("FactID did not include generation boundary: %q", first.FactID)
	}
}

func TestVaultMountJoinKeysNormalizeEquivalentMountPaths(t *testing.T) {
	ctx := testVaultContext()
	first, err := NewVaultAuthMountEnvelope(VaultAuthMountObservation{
		Context:    ctx,
		MountPath:  "auth/kubernetes",
		AuthMethod: VaultAuthMethodKubernetes,
	})
	if err != nil {
		t.Fatalf("NewVaultAuthMountEnvelope(first) error = %v", err)
	}
	next, err := NewVaultAuthMountEnvelope(VaultAuthMountObservation{
		Context:    ctx,
		MountPath:  "auth/kubernetes/",
		AuthMethod: VaultAuthMethodKubernetes,
	})
	if err != nil {
		t.Fatalf("NewVaultAuthMountEnvelope(next) error = %v", err)
	}
	if first.StableFactKey != next.StableFactKey {
		t.Fatalf("StableFactKey changed for equivalent mount paths: %q != %q",
			first.StableFactKey, next.StableFactKey)
	}
	if first.Payload["mount_join_key"] != next.Payload["mount_join_key"] {
		t.Fatalf("mount_join_key changed for equivalent mount paths: %q != %q",
			first.Payload["mount_join_key"], next.Payload["mount_join_key"])
	}
}

func TestNewVaultAuthRoleAndPolicyEnvelopesRedactNamesAndPaths(t *testing.T) {
	ctx := testVaultContext()
	role, err := NewVaultAuthRoleEnvelope(VaultAuthRoleObservation{
		Context:                       ctx,
		MountPath:                     "auth/kubernetes",
		RoleName:                      "payments-api",
		AuthMethod:                    VaultAuthMethodKubernetes,
		BoundServiceAccountNames:      []string{"checkout-sa"},
		BoundServiceAccountNamespaces: []string{"payments"},
		TokenPolicyNames:              []string{"prod-payments-read"},
		TokenTTLSeconds:               900,
	})
	if err != nil {
		t.Fatalf("NewVaultAuthRoleEnvelope() error = %v", err)
	}
	assertFact(t, role, facts.VaultAuthRoleFactKind)
	assertPayloadString(t, role.Payload, "auth_method", VaultAuthMethodKubernetes)
	if payloadContains(role.Payload, "payments-api") ||
		payloadContains(role.Payload, "checkout-sa") ||
		payloadContains(role.Payload, "prod-payments-read") {
		t.Fatalf("Vault auth role payload leaked raw role, ServiceAccount, or policy names: %#v", role.Payload)
	}
	if role.Payload["role_join_key"] == "" || role.Payload["mount_join_key"] == "" {
		t.Fatalf("Vault auth role payload missing join keys: %#v", role.Payload)
	}

	policy, err := NewVaultACLPolicyEnvelope(VaultACLPolicyObservation{
		Context:    ctx,
		PolicyName: "prod-payments-read",
		PolicyHash: "sha256:policy-body-hash",
		Rules: []VaultACLPolicyRuleSummary{{
			Path:         "secret/data/payments/prod/api-key",
			Capabilities: []string{"read", "list", "read"},
		}},
	})
	if err != nil {
		t.Fatalf("NewVaultACLPolicyEnvelope() error = %v", err)
	}
	assertFact(t, policy, facts.VaultACLPolicyFactKind)
	if payloadContains(policy.Payload, "prod-payments-read") ||
		payloadContains(policy.Payload, "payments") ||
		payloadContains(policy.Payload, "api-key") ||
		payloadContains(policy.Payload, "policy-body-hash") {
		t.Fatalf("Vault policy payload leaked raw policy name, path, or hash: %#v", policy.Payload)
	}
	rules, ok := policy.Payload["rules"].([]map[string]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("rules = %#v, want one rule payload", policy.Payload["rules"])
	}
	capabilities, ok := rules[0]["capabilities"].([]string)
	if !ok || len(capabilities) != 2 || capabilities[0] != "list" || capabilities[1] != "read" {
		t.Fatalf("capabilities = %#v, want [list read]", rules[0]["capabilities"])
	}
}

func TestNewVaultAuthRoleEnvelopeRequiresAuthMethod(t *testing.T) {
	_, err := NewVaultAuthRoleEnvelope(VaultAuthRoleObservation{
		Context:   testVaultContext(),
		MountPath: "auth/kubernetes",
		RoleName:  "payments-api",
	})
	if err == nil {
		t.Fatalf("NewVaultAuthRoleEnvelope() error = nil, want non-nil")
	}
}

func TestNewVaultACLPolicyEnvelopeKeepsEmptyRulesArray(t *testing.T) {
	env, err := NewVaultACLPolicyEnvelope(VaultACLPolicyObservation{
		Context:    testVaultContext(),
		PolicyName: "prod-payments-read",
	})
	if err != nil {
		t.Fatalf("NewVaultACLPolicyEnvelope() error = %v", err)
	}
	rules, ok := env.Payload["rules"].([]map[string]any)
	if !ok {
		t.Fatalf("rules = %#v, want []map[string]any", env.Payload["rules"])
	}
	if rules == nil {
		t.Fatalf("rules = nil, want empty non-nil slice")
	}
	if len(rules) != 0 {
		t.Fatalf("len(rules) = %d, want 0", len(rules))
	}
}

func TestNewVaultIdentityAndMountEnvelopesRedactIdentifiers(t *testing.T) {
	ctx := testVaultContext()
	mount, err := NewVaultAuthMountEnvelope(VaultAuthMountObservation{
		Context:       ctx,
		MountPath:     "auth/kubernetes",
		MountAccessor: "auth_kubernetes_123456",
		AuthMethod:    VaultAuthMethodKubernetes,
		Local:         true,
	})
	if err != nil {
		t.Fatalf("NewVaultAuthMountEnvelope() error = %v", err)
	}
	assertFact(t, mount, facts.VaultAuthMountFactKind)
	if payloadContains(mount.Payload, "auth/kubernetes") || payloadContains(mount.Payload, "auth_kubernetes_123456") {
		t.Fatalf("Vault auth mount payload leaked raw mount path or accessor: %#v", mount.Payload)
	}

	engine, err := NewVaultSecretEngineMountEnvelope(VaultSecretEngineMountObservation{
		Context:       ctx,
		MountPath:     "secret/",
		MountAccessor: "kv_secret_123456",
		MountType:     VaultSecretEngineKVV2,
		KVVersion:     "2",
	})
	if err != nil {
		t.Fatalf("NewVaultSecretEngineMountEnvelope() error = %v", err)
	}
	assertFact(t, engine, facts.VaultSecretEngineMountFactKind)
	assertPayloadString(t, engine.Payload, "mount_type", VaultSecretEngineKVV2)
	if payloadContains(engine.Payload, "secret/") || payloadContains(engine.Payload, "kv_secret_123456") {
		t.Fatalf("Vault secret engine payload leaked raw mount path or accessor: %#v", engine.Payload)
	}

	entity, err := NewVaultIdentityEntityEnvelope(VaultIdentityEntityObservation{
		Context:    ctx,
		EntityID:   "entity-payments-prod",
		EntityName: "payments-prod",
		AliasCount: 2,
		GroupCount: 1,
		Disabled:   false,
	})
	if err != nil {
		t.Fatalf("NewVaultIdentityEntityEnvelope() error = %v", err)
	}
	assertFact(t, entity, facts.VaultIdentityEntityFactKind)
	if payloadContains(entity.Payload, "entity-payments-prod") || payloadContains(entity.Payload, "payments-prod") {
		t.Fatalf("Vault identity entity payload leaked raw entity identifier: %#v", entity.Payload)
	}

	alias, err := NewVaultIdentityAliasEnvelope(VaultIdentityAliasObservation{
		Context:       ctx,
		AliasID:       "alias-checkout-sa",
		EntityID:      "entity-payments-prod",
		MountPath:     "auth/kubernetes",
		MountAccessor: "auth_kubernetes_123456",
		AliasName:     "system:serviceaccount:payments:checkout-sa",
	})
	if err != nil {
		t.Fatalf("NewVaultIdentityAliasEnvelope() error = %v", err)
	}
	assertFact(t, alias, facts.VaultIdentityAliasFactKind)
	if payloadContains(alias.Payload, "checkout-sa") ||
		payloadContains(alias.Payload, "payments") ||
		payloadContains(alias.Payload, "alias-checkout-sa") {
		t.Fatalf("Vault identity alias payload leaked raw alias identity: %#v", alias.Payload)
	}
}

func TestNewVaultCoverageWarningEnvelopeRedactsMessage(t *testing.T) {
	ctx := testVaultContext()
	env, err := NewVaultCoverageWarningEnvelope(VaultCoverageWarningObservation{
		Context:       ctx,
		WarningKind:   "permission_hidden",
		SourceState:   SourceStatePermissionHidden,
		ResourceScope: "kv_metadata",
		ErrorClass:    "permission_denied",
		Message:       "permission denied for /v1/secret/data/payments/prod/api-key",
	})
	if err != nil {
		t.Fatalf("NewVaultCoverageWarningEnvelope() error = %v", err)
	}
	assertFact(t, env, facts.SecretsIAMCoverageWarningFactKind)
	assertPayloadString(t, env.Payload, "source_state", SourceStatePermissionHidden)
	if _, ok := env.Payload["message"]; ok {
		t.Fatalf("Vault warning payload stored raw message: %#v", env.Payload)
	}
	if env.Payload["message_fingerprint"] == "" {
		t.Fatalf("Vault warning payload missing message fingerprint: %#v", env.Payload)
	}
	if payloadContains(env.Payload, "payments") || payloadContains(env.Payload, "api-key") {
		t.Fatalf("Vault warning payload leaked raw path from message: %#v", env.Payload)
	}
}

func TestNewVaultCoverageWarningEnvelopeRequiresResourceScope(t *testing.T) {
	_, err := NewVaultCoverageWarningEnvelope(VaultCoverageWarningObservation{
		Context:     testVaultContext(),
		WarningKind: "permission_hidden",
		SourceState: SourceStatePermissionHidden,
	})
	if err == nil {
		t.Fatalf("NewVaultCoverageWarningEnvelope() error = nil, want non-nil")
	}
}

func testVaultContext() VaultContext {
	return VaultContext{
		VaultClusterID:      "vault-prod",
		Namespace:           "admin/payments",
		ScopeID:             "vault:vault-prod:admin-payments:secret",
		GenerationID:        "vault:vault-prod:admin-payments:secret:1",
		CollectorInstanceID: "vault-prod",
		FencingToken:        11,
		ObservedAt:          time.Date(2026, 6, 2, 14, 0, 0, 0, time.UTC),
		RedactionKey:        testSecretsIAMRedactionKey(),
	}
}

func testSecretsIAMRedactionKey() redact.Key {
	key, err := redact.NewKey([]byte("secrets-iam-test-redaction-key"))
	if err != nil {
		panic(err)
	}
	return key
}

func payloadContains(payload map[string]any, needle string) bool {
	for key, value := range payload {
		if strings.Contains(key, needle) || anyPayloadValueContains(value, needle) {
			return true
		}
	}
	return false
}

func anyPayloadValueContains(value any, needle string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, needle)
	case []string:
		for _, item := range typed {
			if strings.Contains(item, needle) {
				return true
			}
		}
	case map[string]any:
		return payloadContains(typed, needle)
	case []map[string]any:
		for _, item := range typed {
			if payloadContains(item, needle) {
				return true
			}
		}
	}
	return false
}
