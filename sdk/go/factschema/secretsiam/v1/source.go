// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// KubernetesCommon carries the redaction-safe common payload fields shared by
// Kubernetes secrets_iam source facts.
type KubernetesCommon struct {
	ClusterID              string `json:"cluster_id"`
	Provider               string `json:"provider"`
	CollectorInstanceID    string `json:"collector_instance_id"`
	RedactionPolicyVersion string `json:"redaction_policy_version"`
}

// KubernetesRBACRule is the typed nested payload for one redacted RBAC rule
// summary inside a k8s_rbac_role fact.
type KubernetesRBACRule struct {
	Verbs                  []string `json:"verbs,omitempty"`
	APIGroups              []string `json:"api_groups,omitempty"`
	Resources              []string `json:"resources,omitempty"`
	ResourceNameCount      *int     `json:"resource_name_count,omitempty"`
	ResourceNamesPresent   *bool    `json:"resource_names_present,omitempty"`
	NonResourceURLCount    *int     `json:"non_resource_url_count,omitempty"`
	NonResourceURLsPresent *bool    `json:"non_resource_urls_present,omitempty"`
}

// KubernetesRBACSubject is the typed nested payload for one redacted RBAC
// binding subject.
type KubernetesRBACSubject struct {
	Kind                  *string `json:"kind,omitempty"`
	APIGroup              *string `json:"api_group,omitempty"`
	NamespaceFingerprint  *string `json:"namespace_fingerprint,omitempty"`
	NameFingerprint       *string `json:"name_fingerprint,omitempty"`
	ServiceAccountJoinKey *string `json:"service_account_join_key,omitempty"`
}

// KubernetesRBACRole is the schema-version-1 payload for a "k8s_rbac_role"
// source fact.
type KubernetesRBACRole struct {
	ClusterID                  string               `json:"cluster_id"`
	Provider                   string               `json:"provider"`
	CollectorInstanceID        string               `json:"collector_instance_id"`
	RedactionPolicyVersion     string               `json:"redaction_policy_version"`
	RoleKind                   string               `json:"role_kind"`
	RoleScope                  string               `json:"role_scope"`
	RoleJoinKey                string               `json:"role_join_key"`
	NamespaceFingerprint       *string              `json:"namespace_fingerprint,omitempty"`
	RoleNameFingerprint        *string              `json:"role_name_fingerprint,omitempty"`
	UIDFingerprint             *string              `json:"uid_fingerprint,omitempty"`
	ResourceVersionFingerprint *string              `json:"resource_version_fingerprint,omitempty"`
	Rules                      []KubernetesRBACRule `json:"rules,omitempty"`
	RuleCount                  *int                 `json:"rule_count,omitempty"`
}

// KubernetesRBACBinding is the schema-version-1 payload for a
// "k8s_rbac_binding" source fact.
type KubernetesRBACBinding struct {
	ClusterID                  string                  `json:"cluster_id"`
	Provider                   string                  `json:"provider"`
	CollectorInstanceID        string                  `json:"collector_instance_id"`
	RedactionPolicyVersion     string                  `json:"redaction_policy_version"`
	BindingKind                string                  `json:"binding_kind"`
	BindingScope               string                  `json:"binding_scope"`
	RoleRefKind                string                  `json:"role_ref_kind"`
	RoleRefJoinKey             string                  `json:"role_ref_join_key"`
	NamespaceFingerprint       *string                 `json:"namespace_fingerprint,omitempty"`
	BindingNameFingerprint     *string                 `json:"binding_name_fingerprint,omitempty"`
	UIDFingerprint             *string                 `json:"uid_fingerprint,omitempty"`
	ResourceVersionFingerprint *string                 `json:"resource_version_fingerprint,omitempty"`
	RoleRefAPIGroup            *string                 `json:"role_ref_api_group,omitempty"`
	RoleRefNameFingerprint     *string                 `json:"role_ref_name_fingerprint,omitempty"`
	SubjectCount               *int                    `json:"subject_count,omitempty"`
	Subjects                   []KubernetesRBACSubject `json:"subjects,omitempty"`
}

// KubernetesServiceAccountTokenPosture is the schema-version-1 payload for a
// "k8s_service_account_token_posture" source fact.
type KubernetesServiceAccountTokenPosture struct {
	ClusterID                    string  `json:"cluster_id"`
	Provider                     string  `json:"provider"`
	CollectorInstanceID          string  `json:"collector_instance_id"`
	RedactionPolicyVersion       string  `json:"redaction_policy_version"`
	ServiceAccountJoinKey        string  `json:"service_account_join_key"`
	NamespaceFingerprint         *string `json:"namespace_fingerprint,omitempty"`
	ServiceAccountFingerprint    *string `json:"service_account_fingerprint,omitempty"`
	ServiceAccountUIDFingerprint *string `json:"service_account_uid_fingerprint,omitempty"`
	AutomountToken               *string `json:"automount_token,omitempty"`
	SecretRefCount               *int    `json:"secret_ref_count,omitempty"`
	ImagePullSecretRefCount      *int    `json:"image_pull_secret_ref_count,omitempty"`
}

// VaultCommon carries the redaction-safe common payload fields shared by Vault
// secrets_iam source facts.
type VaultCommon struct {
	VaultClusterID         string  `json:"vault_cluster_id"`
	NamespaceFingerprint   *string `json:"namespace_fingerprint,omitempty"`
	Provider               string  `json:"provider"`
	CollectorInstanceID    string  `json:"collector_instance_id"`
	RedactionPolicyVersion string  `json:"redaction_policy_version"`
}

// VaultAuthMount is the schema-version-1 payload for a "vault_auth_mount"
// source fact.
type VaultAuthMount struct {
	VaultClusterID           string  `json:"vault_cluster_id"`
	Provider                 string  `json:"provider"`
	CollectorInstanceID      string  `json:"collector_instance_id"`
	RedactionPolicyVersion   string  `json:"redaction_policy_version"`
	AuthMethod               string  `json:"auth_method"`
	MountJoinKey             string  `json:"mount_join_key"`
	NamespaceFingerprint     *string `json:"namespace_fingerprint,omitempty"`
	MountPathFingerprint     *string `json:"mount_path_fingerprint,omitempty"`
	MountPathDepth           *int    `json:"mount_path_depth,omitempty"`
	MountAccessorFingerprint *string `json:"mount_accessor_fingerprint,omitempty"`
	Local                    *bool   `json:"local,omitempty"`
	DefaultLeaseTTLSeconds   *int    `json:"default_lease_ttl_seconds,omitempty"`
	MaxLeaseTTLSeconds       *int    `json:"max_lease_ttl_seconds,omitempty"`
}

// VaultIdentityEntity is the schema-version-1 payload for a
// "vault_identity_entity" source fact.
type VaultIdentityEntity struct {
	VaultClusterID         string  `json:"vault_cluster_id"`
	Provider               string  `json:"provider"`
	CollectorInstanceID    string  `json:"collector_instance_id"`
	RedactionPolicyVersion string  `json:"redaction_policy_version"`
	EntityJoinKey          string  `json:"entity_join_key"`
	NamespaceFingerprint   *string `json:"namespace_fingerprint,omitempty"`
	EntityIDFingerprint    *string `json:"entity_id_fingerprint,omitempty"`
	EntityNameFingerprint  *string `json:"entity_name_fingerprint,omitempty"`
	AliasCount             *int    `json:"alias_count,omitempty"`
	GroupCount             *int    `json:"group_count,omitempty"`
	Disabled               *bool   `json:"disabled,omitempty"`
}

// VaultIdentityAlias is the schema-version-1 payload for a
// "vault_identity_alias" source fact.
type VaultIdentityAlias struct {
	VaultClusterID           string  `json:"vault_cluster_id"`
	Provider                 string  `json:"provider"`
	CollectorInstanceID      string  `json:"collector_instance_id"`
	RedactionPolicyVersion   string  `json:"redaction_policy_version"`
	AliasIDFingerprint       string  `json:"alias_id_fingerprint"`
	EntityJoinKey            string  `json:"entity_join_key"`
	MountJoinKey             string  `json:"mount_join_key"`
	NamespaceFingerprint     *string `json:"namespace_fingerprint,omitempty"`
	AliasNameFingerprint     *string `json:"alias_name_fingerprint,omitempty"`
	MountAccessorFingerprint *string `json:"mount_accessor_fingerprint,omitempty"`
}

// VaultSecretEngineMount is the schema-version-1 payload for a
// "vault_secret_engine_mount" source fact.
type VaultSecretEngineMount struct {
	VaultClusterID           string  `json:"vault_cluster_id"`
	Provider                 string  `json:"provider"`
	CollectorInstanceID      string  `json:"collector_instance_id"`
	RedactionPolicyVersion   string  `json:"redaction_policy_version"`
	MountJoinKey             string  `json:"mount_join_key"`
	MountType                string  `json:"mount_type"`
	NamespaceFingerprint     *string `json:"namespace_fingerprint,omitempty"`
	MountPathFingerprint     *string `json:"mount_path_fingerprint,omitempty"`
	MountPathDepth           *int    `json:"mount_path_depth,omitempty"`
	MountAccessorFingerprint *string `json:"mount_accessor_fingerprint,omitempty"`
	KVVersion                *string `json:"kv_version,omitempty"`
	Local                    *bool   `json:"local,omitempty"`
	DefaultLeaseTTLSeconds   *int    `json:"default_lease_ttl_seconds,omitempty"`
	MaxLeaseTTLSeconds       *int    `json:"max_lease_ttl_seconds,omitempty"`
}

// CoverageWarning is the schema-version-1 payload for a
// "secrets_iam_coverage_warning" source fact.
type CoverageWarning struct {
	Provider                 string         `json:"provider"`
	CollectorInstanceID      string         `json:"collector_instance_id"`
	RedactionPolicyVersion   string         `json:"redaction_policy_version"`
	WarningKind              string         `json:"warning_kind"`
	SourceState              string         `json:"source_state"`
	AccountID                *string        `json:"account_id,omitempty"`
	Region                   *string        `json:"region,omitempty"`
	ClusterID                *string        `json:"cluster_id,omitempty"`
	VaultClusterID           *string        `json:"vault_cluster_id,omitempty"`
	NamespaceFingerprint     *string        `json:"namespace_fingerprint,omitempty"`
	ResourceScope            *string        `json:"resource_scope,omitempty"`
	ErrorClass               *string        `json:"error_class,omitempty"`
	Message                  *string        `json:"message,omitempty"`
	Attributes               map[string]any `json:"attributes,omitempty"`
	MessagePresent           *bool          `json:"message_present,omitempty"`
	MessageFingerprint       *string        `json:"message_fingerprint,omitempty"`
	AttributeCount           *int           `json:"attribute_count,omitempty"`
	AttributeKeyFingerprints []string       `json:"attribute_key_fingerprints,omitempty"`
}
