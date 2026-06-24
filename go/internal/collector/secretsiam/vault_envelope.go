// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewVaultAuthMountEnvelope builds a vault_auth_mount source fact.
func NewVaultAuthMountEnvelope(observation VaultAuthMountObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	mountKey, err := vaultMountJoinKey(observation.Context, observation.MountPath)
	if err != nil {
		return facts.Envelope{}, err
	}
	authMethod := strings.TrimSpace(observation.AuthMethod)
	if authMethod == "" {
		return facts.Envelope{}, fmt.Errorf("vault auth mount observation requires auth_method")
	}
	stableKey := facts.StableID(facts.VaultAuthMountFactKind, map[string]any{
		"mount_join_key":   mountKey,
		"vault_cluster_id": observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["auth_method"] = authMethod
	payload["mount_join_key"] = mountKey
	payload["mount_path_fingerprint"] = fingerprintVaultMountPath(observation.Context, observation.MountPath)
	payload["mount_path_depth"] = vaultPathDepth(observation.MountPath)
	payload["mount_accessor_fingerprint"] = fingerprintVaultValue(observation.Context, "mount_accessor", observation.MountAccessor)
	payload["local"] = observation.Local
	payload["default_lease_ttl_seconds"] = observation.DefaultLeaseTTLSeconds
	payload["max_lease_ttl_seconds"] = observation.MaxLeaseTTLSeconds
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultAuthMountFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultAuthRoleEnvelope builds a vault_auth_role source fact.
func NewVaultAuthRoleEnvelope(observation VaultAuthRoleObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	authMethod := strings.TrimSpace(observation.AuthMethod)
	if authMethod == "" {
		return facts.Envelope{}, fmt.Errorf("vault auth role observation requires auth_method")
	}
	roleKey, err := vaultRoleJoinKey(observation.Context, observation.MountPath, observation.RoleName)
	if err != nil {
		return facts.Envelope{}, err
	}
	mountKey, err := vaultMountJoinKey(observation.Context, observation.MountPath)
	if err != nil {
		return facts.Envelope{}, err
	}
	policyKeys := vaultPolicyJoinKeys(observation.Context, observation.TokenPolicyNames)
	serviceAccountJoinKeys := vaultBoundServiceAccountJoinKeys(
		observation.Context,
		observation.KubernetesClusterID,
		observation.BoundServiceAccountNamespaces,
		observation.BoundServiceAccountNames,
	)
	selectorWildcard := hasWildcard(observation.BoundServiceAccountNames) ||
		hasWildcard(observation.BoundServiceAccountNamespaces)
	stableKey := facts.StableID(facts.VaultAuthRoleFactKind, map[string]any{
		"role_join_key":    roleKey,
		"vault_cluster_id": observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["auth_method"] = authMethod
	payload["mount_join_key"] = mountKey
	payload["role_join_key"] = roleKey
	payload["role_name_fingerprint"] = fingerprintVaultValue(observation.Context, "auth_role", observation.RoleName)
	payload["bound_service_account_name_count"] = len(normalizeKeyList(observation.BoundServiceAccountNames))
	payload["bound_service_account_namespace_count"] = len(normalizeKeyList(observation.BoundServiceAccountNamespaces))
	payload["bound_service_account_name_fingerprints"] = fingerprintVaultValues(observation.Context, "service_account", observation.BoundServiceAccountNames)
	payload["bound_service_account_namespace_fingerprints"] = fingerprintVaultValues(observation.Context, "namespace", observation.BoundServiceAccountNamespaces)
	if len(serviceAccountJoinKeys) > 0 {
		payload["bound_service_account_join_keys"] = serviceAccountJoinKeys
	}
	payload["bound_service_account_selector_wildcard"] = selectorWildcard
	payload["token_policy_count"] = len(policyKeys)
	payload["token_policy_join_keys"] = policyKeys
	payload["token_policy_name_fingerprints"] = fingerprintVaultValues(observation.Context, "policy_name", observation.TokenPolicyNames)
	payload["token_ttl_seconds"] = observation.TokenTTLSeconds
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultAuthRoleFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultACLPolicyEnvelope builds a vault_acl_policy source fact.
func NewVaultACLPolicyEnvelope(observation VaultACLPolicyObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	policyKey, err := vaultPolicyJoinKey(observation.Context, observation.PolicyName)
	if err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.VaultACLPolicyFactKind, map[string]any{
		"policy_join_key":  policyKey,
		"vault_cluster_id": observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["policy_join_key"] = policyKey
	payload["policy_name_fingerprint"] = fingerprintVaultValue(observation.Context, "policy_name", observation.PolicyName)
	payload["policy_hash_fingerprint"] = fingerprintVaultValue(observation.Context, "policy_hash", observation.PolicyHash)
	payload["rules"] = vaultPolicyRulePayloads(observation.Context, observation.Rules)
	payload["rule_count"] = len(observation.Rules)
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultACLPolicyFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultIdentityEntityEnvelope builds a vault_identity_entity source fact.
func NewVaultIdentityEntityEnvelope(observation VaultIdentityEntityObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	entityKey, err := vaultEntityJoinKey(observation.Context, observation.EntityID)
	if err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.VaultIdentityEntityFactKind, map[string]any{
		"entity_join_key":  entityKey,
		"vault_cluster_id": observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["entity_join_key"] = entityKey
	payload["entity_id_fingerprint"] = fingerprintVaultValue(observation.Context, "entity_id", observation.EntityID)
	payload["entity_name_fingerprint"] = fingerprintVaultValue(observation.Context, "entity_name", observation.EntityName)
	payload["alias_count"] = observation.AliasCount
	payload["group_count"] = observation.GroupCount
	payload["disabled"] = observation.Disabled
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultIdentityEntityFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultIdentityAliasEnvelope builds a vault_identity_alias source fact.
func NewVaultIdentityAliasEnvelope(observation VaultIdentityAliasObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	aliasID := strings.TrimSpace(observation.AliasID)
	if aliasID == "" {
		return facts.Envelope{}, fmt.Errorf("vault identity alias observation requires alias_id")
	}
	mountKey, err := vaultMountJoinKey(observation.Context, observation.MountPath)
	if err != nil {
		return facts.Envelope{}, err
	}
	entityKey, err := vaultEntityJoinKey(observation.Context, observation.EntityID)
	if err != nil {
		return facts.Envelope{}, err
	}
	stableKey := facts.StableID(facts.VaultIdentityAliasFactKind, map[string]any{
		"alias_id":         fingerprintVaultValue(observation.Context, "alias_id", aliasID),
		"vault_cluster_id": observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["alias_id_fingerprint"] = fingerprintVaultValue(observation.Context, "alias_id", aliasID)
	payload["alias_name_fingerprint"] = fingerprintVaultValue(observation.Context, "alias_name", observation.AliasName)
	payload["entity_join_key"] = entityKey
	payload["mount_join_key"] = mountKey
	payload["mount_accessor_fingerprint"] = fingerprintVaultValue(observation.Context, "mount_accessor", observation.MountAccessor)
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultIdentityAliasFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultKVMetadataEnvelope builds a vault_kv_metadata source fact.
func NewVaultKVMetadataEnvelope(observation VaultKVMetadataObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	mountKey, err := vaultMountJoinKey(observation.Context, observation.MountPath)
	if err != nil {
		return facts.Envelope{}, err
	}
	pathFingerprint := fingerprintVaultPath(observation.Context, observation.Path)
	if pathFingerprint == "" {
		return facts.Envelope{}, fmt.Errorf("vault kv metadata observation requires path")
	}
	stableKey := facts.StableID(facts.VaultKVMetadataFactKind, map[string]any{
		"kv_path_fingerprint": pathFingerprint,
		"mount_join_key":      mountKey,
		"vault_cluster_id":    observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["mount_join_key"] = mountKey
	payload["mount_path_fingerprint"] = fingerprintVaultMountPath(observation.Context, observation.MountPath)
	payload["kv_path_fingerprint"] = pathFingerprint
	payload["path_depth"] = vaultPathDepth(observation.Path)
	payload["current_version"] = observation.CurrentVersion
	payload["oldest_version"] = observation.OldestVersion
	payload["max_versions"] = observation.MaxVersions
	payload["cas_required"] = observation.CASRequired
	payload["delete_version_after_seconds"] = observation.DeleteVersionAfterSecs
	payload["custom_metadata_key_count"] = len(normalizeKeyList(observation.CustomMetadataKeys))
	payload["custom_metadata_key_fingerprints"] = fingerprintVaultValues(observation.Context, "custom_metadata_key", observation.CustomMetadataKeys)
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultKVMetadataFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultSecretEngineMountEnvelope builds a vault_secret_engine_mount source
// fact.
func NewVaultSecretEngineMountEnvelope(observation VaultSecretEngineMountObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	mountKey, err := vaultMountJoinKey(observation.Context, observation.MountPath)
	if err != nil {
		return facts.Envelope{}, err
	}
	mountType := strings.TrimSpace(observation.MountType)
	if mountType == "" {
		return facts.Envelope{}, fmt.Errorf("vault secret engine mount observation requires mount_type")
	}
	stableKey := facts.StableID(facts.VaultSecretEngineMountFactKind, map[string]any{
		"mount_join_key":   mountKey,
		"vault_cluster_id": observation.Context.VaultClusterID,
	})
	payload := vaultPayload(observation.Context)
	payload["mount_join_key"] = mountKey
	payload["mount_path_fingerprint"] = fingerprintVaultMountPath(observation.Context, observation.MountPath)
	payload["mount_path_depth"] = vaultPathDepth(observation.MountPath)
	payload["mount_accessor_fingerprint"] = fingerprintVaultValue(observation.Context, "mount_accessor", observation.MountAccessor)
	payload["mount_type"] = mountType
	payload["kv_version"] = strings.TrimSpace(observation.KVVersion)
	payload["local"] = observation.Local
	payload["default_lease_ttl_seconds"] = observation.DefaultLeaseTTLSeconds
	payload["max_lease_ttl_seconds"] = observation.MaxLeaseTTLSeconds
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.VaultSecretEngineMountFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewVaultCoverageWarningEnvelope builds secrets_iam_coverage_warning evidence
// for partial, hidden, unsupported, rate-limited, or stale Vault source reads.
func NewVaultCoverageWarningEnvelope(observation VaultCoverageWarningObservation) (facts.Envelope, error) {
	if err := validateVaultContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	warningKind := strings.TrimSpace(observation.WarningKind)
	sourceState := strings.TrimSpace(observation.SourceState)
	resourceScope := strings.TrimSpace(observation.ResourceScope)
	if warningKind == "" || sourceState == "" || resourceScope == "" {
		return facts.Envelope{}, fmt.Errorf("vault coverage warning requires warning_kind, source_state, and resource_scope")
	}
	stableKey := facts.StableID(facts.SecretsIAMCoverageWarningFactKind, map[string]any{
		"generation":       observation.Context.GenerationID,
		"resource_scope":   resourceScope,
		"source_state":     sourceState,
		"vault_cluster_id": observation.Context.VaultClusterID,
		"warning_kind":     warningKind,
	})
	payload := vaultPayload(observation.Context)
	payload["warning_kind"] = warningKind
	payload["source_state"] = sourceState
	payload["resource_scope"] = resourceScope
	payload["error_class"] = strings.TrimSpace(observation.ErrorClass)
	payload["message_present"] = strings.TrimSpace(observation.Message) != ""
	payload["message_fingerprint"] = fingerprintVaultValue(observation.Context, "warning_message", observation.Message)
	payload["attribute_count"] = len(observation.Attributes)
	payload["attribute_key_fingerprints"] = fingerprintVaultValues(observation.Context, "attribute_key", mapKeys(observation.Attributes))
	return newEnvelope(
		vaultEnvelopeContext(observation.Context),
		facts.SecretsIAMCoverageWarningFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, stableKey),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

func validateVaultContext(ctx VaultContext) error {
	switch {
	case strings.TrimSpace(ctx.VaultClusterID) == "":
		return fmt.Errorf("vault secrets iam observation requires vault_cluster_id")
	case strings.TrimSpace(ctx.ScopeID) == "":
		return fmt.Errorf("vault secrets iam observation requires scope_id")
	case strings.TrimSpace(ctx.GenerationID) == "":
		return fmt.Errorf("vault secrets iam observation requires generation_id")
	case strings.TrimSpace(ctx.CollectorInstanceID) == "":
		return fmt.Errorf("vault secrets iam observation requires collector_instance_id")
	case ctx.FencingToken <= 0:
		return fmt.Errorf("vault secrets iam observation fencing_token must be positive")
	case ctx.RedactionKey.IsZero():
		return fmt.Errorf("vault secrets iam observation requires redaction key")
	default:
		return nil
	}
}
