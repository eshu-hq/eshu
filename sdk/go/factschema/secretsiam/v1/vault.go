// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// VaultAuthRole is the schema-version-1 typed payload for the
// "vault_auth_role" fact kind: one Vault Kubernetes (or other) auth role,
// carrying the bound service-account selector and token policy references as
// redaction-safe join keys.
//
// RoleJoinKey is required: the collector emitter
// (secretsiam.NewVaultAuthRoleEnvelope) always derives it from MountPath and
// RoleName and it is the reducer's sole index key
// (index.vaultRoles/vaultAuthRoles in buildSecretsIAMIndex). A payload
// missing this key could never join to a service account or resolve any
// vault-role identity in a trust chain, so it dead-letters as input_invalid
// rather than silently vanishing from the index the way the pre-typing
// addByKey blank-key guard did.
//
// BoundServiceAccountJoinKeys is optional: the emitter omits the payload key
// entirely when it resolves zero bound service-account join keys
// (`if len(serviceAccountJoinKeys) > 0`), so an absent value is a valid "no
// resolvable bound service account" observation, not a decode failure. The
// reducer's read side already tolerates this (payloadStrings returns an empty
// slice for an absent key); this struct preserves that tolerance.
type VaultAuthRole struct {
	// RoleJoinKey is the collector-derived stable join key for this auth
	// role (mount + role name fingerprint). Required -- the reducer's sole
	// index key for a Vault auth role.
	RoleJoinKey string `json:"role_join_key"`

	// MountJoinKey is the collector-derived stable join key for the auth
	// mount this role belongs to. Optional: always emitted by the collector,
	// but not itself a reducer join anchor the way RoleJoinKey is.
	MountJoinKey *string `json:"mount_join_key,omitempty"`

	// BoundServiceAccountJoinKeys lists the service-account join keys this
	// role's Kubernetes auth-method selector resolves to. Optional: absent
	// when the collector resolves zero bound service accounts (a wildcard
	// selector or an unresolvable binding), matching
	// BoundServiceAccountSelectorWildcard's own semantics.
	BoundServiceAccountJoinKeys []string `json:"bound_service_account_join_keys,omitempty"`

	// BoundServiceAccountSelectorWildcard reports whether the role's bound
	// service-account or bound-namespace selector contains a wildcard.
	// Optional: the collector always emits this key, but it is modeled as
	// optional so an older payload lacking it still decodes (defaults to
	// false / "not a wildcard" on the reducer's read side, matching
	// payloadBool's pre-typing zero-value behavior).
	BoundServiceAccountSelectorWildcard *bool `json:"bound_service_account_selector_wildcard,omitempty"`

	// TokenPolicyJoinKeys lists the join keys for the Vault ACL policies this
	// role's issued tokens carry. Optional: the collector always emits this
	// key (even as an empty slice when the role has zero token policies), so
	// an absent value and an empty slice both mean "no token policies."
	TokenPolicyJoinKeys []string `json:"token_policy_join_keys,omitempty"`
}

// VaultACLPolicyRule is the schema-version-1 typed payload for one rule
// entry inside a vault_acl_policy fact's "rules" array. It is a fully typed
// nested struct, not a map[string]any pass-through: the collector emitter
// (secretsiam.vaultPolicyRulePayloads) always emits exactly this
// {path_fingerprint, path_depth, capabilities} shape per rule.
type VaultACLPolicyRule struct {
	// PathFingerprint is the redaction-safe fingerprint of the Vault path
	// this rule governs. Optional: the collector always emits a
	// fingerprint, but the field stays a pointer so an absent or malformed
	// rule entry in a heterogeneous historical payload does not force a
	// decode failure for the OTHER rules in the same array (the reducer's
	// secret-access-path resolution simply skips a rule whose
	// PathFingerprint does not join to any vault_kv_metadata fact, its
	// existing tolerant behavior).
	PathFingerprint *string `json:"path_fingerprint,omitempty"`

	// PathDepth is the Vault path's segment depth. Optional: emitted by the
	// collector but not read by any reducer trust-chain logic today.
	PathDepth *int `json:"path_depth,omitempty"`

	// Capabilities lists the Vault ACL capabilities this rule grants (for
	// example "read", "list"). Optional: the collector always emits a
	// (possibly empty) slice.
	Capabilities []string `json:"capabilities,omitempty"`
}

// VaultACLPolicy is the schema-version-1 typed payload for the
// "vault_acl_policy" fact kind: one Vault ACL policy's redacted rule summary,
// without the raw policy body.
//
// PolicyJoinKey is required: the collector emitter
// (secretsiam.NewVaultACLPolicyEnvelope) always derives it from PolicyName
// and it is the reducer's sole index key (index.vaultPolicies in
// buildSecretsIAMIndex, joined from VaultAuthRole.TokenPolicyJoinKeys). A
// payload missing this key could never join back to any auth role's token
// policy reference.
type VaultACLPolicy struct {
	// PolicyJoinKey is the collector-derived stable join key for this ACL
	// policy. Required -- the reducer's sole index key for a Vault ACL
	// policy.
	PolicyJoinKey string `json:"policy_join_key"`

	// Rules lists this policy's redacted rule summaries. Optional: the
	// collector always emits a (possibly empty) slice
	// (secretsiam.vaultPolicyRulePayloads returns []map[string]any{} for
	// zero rules, never nil), but the field stays omitempty so an absent key
	// on an older payload still decodes to nil rather than a decode
	// failure -- the reducer's vaultPolicyRules already treats a missing
	// "rules" key as zero rules.
	Rules []VaultACLPolicyRule `json:"rules,omitempty"`
}

// VaultKVMetadata is the schema-version-1 typed payload for the
// "vault_kv_metadata" fact kind: one Vault KV v2 metadata path, without
// carrying key names, path text, or secret data.
//
// MountJoinKey and KVPathFingerprint are both required: the collector
// emitter (secretsiam.NewVaultKVMetadataEnvelope) always derives both, and
// together they are the reducer's join key back to a VaultACLPolicyRule's
// PathFingerprint (index.vaultKV in buildSecretsIAMIndex, keyed solely by
// KVPathFingerprint). A payload missing either could never join to the
// secret-access-path resolution the way a real collector-emitted fact always
// can.
type VaultKVMetadata struct {
	// MountJoinKey is the collector-derived stable join key for the KV v2
	// mount this metadata path belongs to. Required -- always emitted by the
	// collector.
	MountJoinKey string `json:"mount_join_key"`

	// KVPathFingerprint is the redaction-safe fingerprint of the KV path.
	// Required -- the reducer's sole index key for a Vault KV metadata
	// fact (index.vaultKV), joined from a VaultACLPolicyRule's
	// PathFingerprint.
	KVPathFingerprint string `json:"kv_path_fingerprint"`

	// PathDepth is the KV path's segment depth. Optional: emitted by the
	// collector but not read by any reducer trust-chain logic today.
	PathDepth *int `json:"path_depth,omitempty"`
}
