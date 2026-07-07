// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"

// DecodeKubernetesRBACRole decodes a "k8s_rbac_role" payload.
func DecodeKubernetesRBACRole(env Envelope) (secretsiamv1.KubernetesRBACRole, error) {
	return decodeLatestMajor[secretsiamv1.KubernetesRBACRole](FactKindKubernetesRBACRole, env)
}

// EncodeKubernetesRBACRole builds the direct map payload for a
// "k8s_rbac_role" fact.
func EncodeKubernetesRBACRole(role secretsiamv1.KubernetesRBACRole) (map[string]any, error) {
	payload := encodeKubernetesCommon(
		role.ClusterID,
		role.Provider,
		role.CollectorInstanceID,
		role.RedactionPolicyVersion,
	)
	payload["role_kind"] = role.RoleKind
	payload["role_scope"] = role.RoleScope
	payload["role_join_key"] = role.RoleJoinKey
	addStringPtr(payload, "namespace_fingerprint", role.NamespaceFingerprint)
	addStringPtr(payload, "role_name_fingerprint", role.RoleNameFingerprint)
	addStringPtr(payload, "uid_fingerprint", role.UIDFingerprint)
	addStringPtr(payload, "resource_version_fingerprint", role.ResourceVersionFingerprint)
	if role.Rules != nil {
		payload["rules"] = encodeKubernetesRBACRules(role.Rules)
	}
	addIntPtr(payload, "rule_count", role.RuleCount)
	return payload, nil
}

// DecodeKubernetesRBACBinding decodes a "k8s_rbac_binding" payload.
func DecodeKubernetesRBACBinding(env Envelope) (secretsiamv1.KubernetesRBACBinding, error) {
	return decodeLatestMajor[secretsiamv1.KubernetesRBACBinding](FactKindKubernetesRBACBinding, env)
}

// EncodeKubernetesRBACBinding builds the direct map payload for a
// "k8s_rbac_binding" fact.
func EncodeKubernetesRBACBinding(binding secretsiamv1.KubernetesRBACBinding) (map[string]any, error) {
	payload := encodeKubernetesCommon(
		binding.ClusterID,
		binding.Provider,
		binding.CollectorInstanceID,
		binding.RedactionPolicyVersion,
	)
	payload["binding_kind"] = binding.BindingKind
	payload["binding_scope"] = binding.BindingScope
	payload["role_ref_kind"] = binding.RoleRefKind
	payload["role_ref_join_key"] = binding.RoleRefJoinKey
	addStringPtr(payload, "namespace_fingerprint", binding.NamespaceFingerprint)
	addStringPtr(payload, "binding_name_fingerprint", binding.BindingNameFingerprint)
	addStringPtr(payload, "uid_fingerprint", binding.UIDFingerprint)
	addStringPtr(payload, "resource_version_fingerprint", binding.ResourceVersionFingerprint)
	addStringPtr(payload, "role_ref_api_group", binding.RoleRefAPIGroup)
	addStringPtr(payload, "role_ref_name_fingerprint", binding.RoleRefNameFingerprint)
	addIntPtr(payload, "subject_count", binding.SubjectCount)
	if binding.Subjects != nil {
		payload["subjects"] = encodeKubernetesRBACSubjects(binding.Subjects)
	}
	return payload, nil
}

// DecodeKubernetesServiceAccountTokenPosture decodes a
// "k8s_service_account_token_posture" payload.
func DecodeKubernetesServiceAccountTokenPosture(
	env Envelope,
) (secretsiamv1.KubernetesServiceAccountTokenPosture, error) {
	return decodeLatestMajor[secretsiamv1.KubernetesServiceAccountTokenPosture](
		FactKindKubernetesServiceAccountTokenPosture,
		env,
	)
}

// EncodeKubernetesServiceAccountTokenPosture builds the direct map payload for
// a "k8s_service_account_token_posture" fact.
func EncodeKubernetesServiceAccountTokenPosture(
	posture secretsiamv1.KubernetesServiceAccountTokenPosture,
) (map[string]any, error) {
	payload := encodeKubernetesCommon(
		posture.ClusterID,
		posture.Provider,
		posture.CollectorInstanceID,
		posture.RedactionPolicyVersion,
	)
	payload["service_account_join_key"] = posture.ServiceAccountJoinKey
	addStringPtr(payload, "namespace_fingerprint", posture.NamespaceFingerprint)
	addStringPtr(payload, "service_account_fingerprint", posture.ServiceAccountFingerprint)
	addStringPtr(payload, "service_account_uid_fingerprint", posture.ServiceAccountUIDFingerprint)
	addStringPtr(payload, "automount_token", posture.AutomountToken)
	addIntPtr(payload, "secret_ref_count", posture.SecretRefCount)
	addIntPtr(payload, "image_pull_secret_ref_count", posture.ImagePullSecretRefCount)
	return payload, nil
}

// DecodeVaultAuthMount decodes a "vault_auth_mount" payload.
func DecodeVaultAuthMount(env Envelope) (secretsiamv1.VaultAuthMount, error) {
	return decodeLatestMajor[secretsiamv1.VaultAuthMount](FactKindVaultAuthMount, env)
}

// EncodeVaultAuthMount builds the direct map payload for a "vault_auth_mount"
// fact.
func EncodeVaultAuthMount(mount secretsiamv1.VaultAuthMount) (map[string]any, error) {
	payload := encodeVaultCommon(
		mount.VaultClusterID,
		mount.Provider,
		mount.CollectorInstanceID,
		mount.RedactionPolicyVersion,
		mount.NamespaceFingerprint,
	)
	payload["auth_method"] = mount.AuthMethod
	payload["mount_join_key"] = mount.MountJoinKey
	addStringPtr(payload, "mount_path_fingerprint", mount.MountPathFingerprint)
	addIntPtr(payload, "mount_path_depth", mount.MountPathDepth)
	addStringPtr(payload, "mount_accessor_fingerprint", mount.MountAccessorFingerprint)
	addBoolPtr(payload, "local", mount.Local)
	addIntPtr(payload, "default_lease_ttl_seconds", mount.DefaultLeaseTTLSeconds)
	addIntPtr(payload, "max_lease_ttl_seconds", mount.MaxLeaseTTLSeconds)
	return payload, nil
}

// DecodeVaultIdentityEntity decodes a "vault_identity_entity" payload.
func DecodeVaultIdentityEntity(env Envelope) (secretsiamv1.VaultIdentityEntity, error) {
	return decodeLatestMajor[secretsiamv1.VaultIdentityEntity](FactKindVaultIdentityEntity, env)
}

// EncodeVaultIdentityEntity builds the direct map payload for a
// "vault_identity_entity" fact.
func EncodeVaultIdentityEntity(entity secretsiamv1.VaultIdentityEntity) (map[string]any, error) {
	payload := encodeVaultCommon(
		entity.VaultClusterID,
		entity.Provider,
		entity.CollectorInstanceID,
		entity.RedactionPolicyVersion,
		entity.NamespaceFingerprint,
	)
	payload["entity_join_key"] = entity.EntityJoinKey
	addStringPtr(payload, "entity_id_fingerprint", entity.EntityIDFingerprint)
	addStringPtr(payload, "entity_name_fingerprint", entity.EntityNameFingerprint)
	addIntPtr(payload, "alias_count", entity.AliasCount)
	addIntPtr(payload, "group_count", entity.GroupCount)
	addBoolPtr(payload, "disabled", entity.Disabled)
	return payload, nil
}

// DecodeVaultIdentityAlias decodes a "vault_identity_alias" payload.
func DecodeVaultIdentityAlias(env Envelope) (secretsiamv1.VaultIdentityAlias, error) {
	return decodeLatestMajor[secretsiamv1.VaultIdentityAlias](FactKindVaultIdentityAlias, env)
}

// EncodeVaultIdentityAlias builds the direct map payload for a
// "vault_identity_alias" fact.
func EncodeVaultIdentityAlias(alias secretsiamv1.VaultIdentityAlias) (map[string]any, error) {
	payload := encodeVaultCommon(
		alias.VaultClusterID,
		alias.Provider,
		alias.CollectorInstanceID,
		alias.RedactionPolicyVersion,
		alias.NamespaceFingerprint,
	)
	payload["alias_id_fingerprint"] = alias.AliasIDFingerprint
	payload["entity_join_key"] = alias.EntityJoinKey
	payload["mount_join_key"] = alias.MountJoinKey
	addStringPtr(payload, "alias_name_fingerprint", alias.AliasNameFingerprint)
	addStringPtr(payload, "mount_accessor_fingerprint", alias.MountAccessorFingerprint)
	return payload, nil
}

// DecodeVaultSecretEngineMount decodes a "vault_secret_engine_mount" payload.
func DecodeVaultSecretEngineMount(env Envelope) (secretsiamv1.VaultSecretEngineMount, error) {
	return decodeLatestMajor[secretsiamv1.VaultSecretEngineMount](FactKindVaultSecretEngineMount, env)
}

// EncodeVaultSecretEngineMount builds the direct map payload for a
// "vault_secret_engine_mount" fact.
func EncodeVaultSecretEngineMount(mount secretsiamv1.VaultSecretEngineMount) (map[string]any, error) {
	payload := encodeVaultCommon(
		mount.VaultClusterID,
		mount.Provider,
		mount.CollectorInstanceID,
		mount.RedactionPolicyVersion,
		mount.NamespaceFingerprint,
	)
	payload["mount_join_key"] = mount.MountJoinKey
	payload["mount_type"] = mount.MountType
	addStringPtr(payload, "mount_path_fingerprint", mount.MountPathFingerprint)
	addIntPtr(payload, "mount_path_depth", mount.MountPathDepth)
	addStringPtr(payload, "mount_accessor_fingerprint", mount.MountAccessorFingerprint)
	addStringPtr(payload, "kv_version", mount.KVVersion)
	addBoolPtr(payload, "local", mount.Local)
	addIntPtr(payload, "default_lease_ttl_seconds", mount.DefaultLeaseTTLSeconds)
	addIntPtr(payload, "max_lease_ttl_seconds", mount.MaxLeaseTTLSeconds)
	return payload, nil
}

// DecodeSecretsIAMCoverageWarning decodes a "secrets_iam_coverage_warning"
// payload.
func DecodeSecretsIAMCoverageWarning(env Envelope) (secretsiamv1.CoverageWarning, error) {
	return decodeLatestMajor[secretsiamv1.CoverageWarning](FactKindSecretsIAMCoverageWarning, env)
}

// EncodeSecretsIAMCoverageWarning builds the direct map payload for a
// "secrets_iam_coverage_warning" fact.
func EncodeSecretsIAMCoverageWarning(warning secretsiamv1.CoverageWarning) (map[string]any, error) {
	payload := map[string]any{
		"provider":                 warning.Provider,
		"collector_instance_id":    warning.CollectorInstanceID,
		"redaction_policy_version": warning.RedactionPolicyVersion,
		"warning_kind":             warning.WarningKind,
		"source_state":             warning.SourceState,
	}
	addStringPtr(payload, "account_id", warning.AccountID)
	addStringPtr(payload, "region", warning.Region)
	addStringPtr(payload, "cluster_id", warning.ClusterID)
	addStringPtr(payload, "vault_cluster_id", warning.VaultClusterID)
	addStringPtr(payload, "namespace_fingerprint", warning.NamespaceFingerprint)
	addStringPtr(payload, "resource_scope", warning.ResourceScope)
	addStringPtr(payload, "error_class", warning.ErrorClass)
	addStringPtr(payload, "message", warning.Message)
	addAnyMap(payload, "attributes", warning.Attributes)
	addBoolPtr(payload, "message_present", warning.MessagePresent)
	addStringPtr(payload, "message_fingerprint", warning.MessageFingerprint)
	addIntPtr(payload, "attribute_count", warning.AttributeCount)
	addStringSlice(payload, "attribute_key_fingerprints", warning.AttributeKeyFingerprints)
	return payload, nil
}

func encodeKubernetesCommon(clusterID, provider, collectorInstanceID, redactionPolicyVersion string) map[string]any {
	return map[string]any{
		"cluster_id":               clusterID,
		"provider":                 provider,
		"collector_instance_id":    collectorInstanceID,
		"redaction_policy_version": redactionPolicyVersion,
	}
}

func encodeVaultCommon(
	vaultClusterID string,
	provider string,
	collectorInstanceID string,
	redactionPolicyVersion string,
	namespaceFingerprint *string,
) map[string]any {
	payload := map[string]any{
		"vault_cluster_id":         vaultClusterID,
		"provider":                 provider,
		"collector_instance_id":    collectorInstanceID,
		"redaction_policy_version": redactionPolicyVersion,
	}
	addStringPtr(payload, "namespace_fingerprint", namespaceFingerprint)
	return payload
}

func encodeKubernetesRBACRules(rules []secretsiamv1.KubernetesRBACRule) []map[string]any {
	output := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		item := map[string]any{}
		addStringSlice(item, "verbs", rule.Verbs)
		addStringSlice(item, "api_groups", rule.APIGroups)
		addStringSlice(item, "resources", rule.Resources)
		addIntPtr(item, "resource_name_count", rule.ResourceNameCount)
		addBoolPtr(item, "resource_names_present", rule.ResourceNamesPresent)
		addIntPtr(item, "non_resource_url_count", rule.NonResourceURLCount)
		addBoolPtr(item, "non_resource_urls_present", rule.NonResourceURLsPresent)
		output = append(output, item)
	}
	return output
}

func encodeKubernetesRBACSubjects(subjects []secretsiamv1.KubernetesRBACSubject) []map[string]any {
	output := make([]map[string]any, 0, len(subjects))
	for _, subject := range subjects {
		item := map[string]any{}
		addStringPtr(item, "kind", subject.Kind)
		addStringPtr(item, "api_group", subject.APIGroup)
		addStringPtr(item, "namespace_fingerprint", subject.NamespaceFingerprint)
		addStringPtr(item, "name_fingerprint", subject.NameFingerprint)
		addStringPtr(item, "service_account_join_key", subject.ServiceAccountJoinKey)
		output = append(output, item)
	}
	return output
}
