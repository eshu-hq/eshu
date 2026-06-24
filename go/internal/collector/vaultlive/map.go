// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vaultlive

import (
	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// mapAuthMount builds a vault_auth_mount source fact from metadata.
func mapAuthMount(vaultCtx secretsiam.VaultContext, sourceURI string, m AuthMount) (facts.Envelope, error) {
	return secretsiam.NewVaultAuthMountEnvelope(secretsiam.VaultAuthMountObservation{
		Context:                vaultCtx,
		MountPath:              m.Path,
		MountAccessor:          m.Accessor,
		AuthMethod:             m.Method,
		Local:                  m.Local,
		DefaultLeaseTTLSeconds: m.DefaultLeaseTTLSeconds,
		MaxLeaseTTLSeconds:     m.MaxLeaseTTLSeconds,
		SourceURI:              sourceURI,
	})
}

// mapAuthRole builds a vault_auth_role source fact from metadata.
func mapAuthRole(vaultCtx secretsiam.VaultContext, sourceURI string, r AuthRole) (facts.Envelope, error) {
	return secretsiam.NewVaultAuthRoleEnvelope(secretsiam.VaultAuthRoleObservation{
		Context:                       vaultCtx,
		MountPath:                     r.MountPath,
		RoleName:                      r.RoleName,
		AuthMethod:                    r.Method,
		KubernetesClusterID:           r.KubernetesClusterID,
		BoundServiceAccountNames:      r.BoundServiceAccountNames,
		BoundServiceAccountNamespaces: r.BoundServiceAccountNamespaces,
		TokenPolicyNames:              r.TokenPolicyNames,
		TokenTTLSeconds:               r.TokenTTLSeconds,
		SourceURI:                     sourceURI,
	})
}

// mapACLPolicy builds a vault_acl_policy source fact from metadata.
func mapACLPolicy(vaultCtx secretsiam.VaultContext, sourceURI string, p ACLPolicy) (facts.Envelope, error) {
	rules := make([]secretsiam.VaultACLPolicyRuleSummary, 0, len(p.Rules))
	for _, rule := range p.Rules {
		rules = append(rules, secretsiam.VaultACLPolicyRuleSummary{
			Path:         rule.Path,
			Capabilities: rule.Capabilities,
		})
	}
	return secretsiam.NewVaultACLPolicyEnvelope(secretsiam.VaultACLPolicyObservation{
		Context:    vaultCtx,
		PolicyName: p.PolicyName,
		PolicyHash: p.PolicyHash,
		Rules:      rules,
		SourceURI:  sourceURI,
	})
}

// mapIdentityEntity builds a vault_identity_entity source fact from metadata.
func mapIdentityEntity(vaultCtx secretsiam.VaultContext, sourceURI string, e IdentityEntity) (facts.Envelope, error) {
	return secretsiam.NewVaultIdentityEntityEnvelope(secretsiam.VaultIdentityEntityObservation{
		Context:    vaultCtx,
		EntityID:   e.EntityID,
		EntityName: e.EntityName,
		AliasCount: e.AliasCount,
		GroupCount: e.GroupCount,
		Disabled:   e.Disabled,
		SourceURI:  sourceURI,
	})
}

// mapIdentityAlias builds a vault_identity_alias source fact from metadata.
func mapIdentityAlias(vaultCtx secretsiam.VaultContext, sourceURI string, a IdentityAlias) (facts.Envelope, error) {
	return secretsiam.NewVaultIdentityAliasEnvelope(secretsiam.VaultIdentityAliasObservation{
		Context:       vaultCtx,
		AliasID:       a.AliasID,
		EntityID:      a.EntityID,
		MountPath:     a.MountPath,
		MountAccessor: a.MountAccessor,
		AliasName:     a.AliasName,
		SourceURI:     sourceURI,
	})
}

// mapKVMetadata builds a vault_kv_metadata source fact from KV v2 metadata.
func mapKVMetadata(vaultCtx secretsiam.VaultContext, sourceURI string, m KVMetadata) (facts.Envelope, error) {
	return secretsiam.NewVaultKVMetadataEnvelope(secretsiam.VaultKVMetadataObservation{
		Context:                vaultCtx,
		MountPath:              m.MountPath,
		Path:                   m.Path,
		CurrentVersion:         m.CurrentVersion,
		OldestVersion:          m.OldestVersion,
		MaxVersions:            m.MaxVersions,
		CASRequired:            m.CASRequired,
		DeleteVersionAfterSecs: m.DeleteVersionAfterSecs,
		CustomMetadataKeys:     m.CustomMetadataKeys,
		SourceURI:              sourceURI,
	})
}

// mapSecretEngineMount builds a vault_secret_engine_mount source fact.
func mapSecretEngineMount(vaultCtx secretsiam.VaultContext, sourceURI string, m SecretEngineMount) (facts.Envelope, error) {
	return secretsiam.NewVaultSecretEngineMountEnvelope(secretsiam.VaultSecretEngineMountObservation{
		Context:                vaultCtx,
		MountPath:              m.MountPath,
		MountAccessor:          m.MountAccessor,
		MountType:              m.MountType,
		KVVersion:              m.KVVersion,
		Local:                  m.Local,
		DefaultLeaseTTLSeconds: m.DefaultLeaseTTLSeconds,
		MaxLeaseTTLSeconds:     m.MaxLeaseTTLSeconds,
		SourceURI:              sourceURI,
	})
}
