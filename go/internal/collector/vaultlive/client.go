// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vaultlive

import "context"

// AuthMount is the metadata-only view of one Vault auth method mount. It carries
// no secret value, token, or credential material; the path and accessor are
// fingerprinted downstream by the secretsiam envelope builders.
type AuthMount struct {
	Path                   string
	Accessor               string
	Method                 string
	Local                  bool
	DefaultLeaseTTLSeconds int
	MaxLeaseTTLSeconds     int
}

// AuthRole is the metadata-only view of one Vault auth role. For Kubernetes auth
// it carries the bound ServiceAccount names/namespaces that anchor the IAM-Vault
// join; it never carries a token, JWT, or AppRole secret_id.
type AuthRole struct {
	MountPath                     string
	RoleName                      string
	Method                        string
	KubernetesClusterID           string
	BoundServiceAccountNames      []string
	BoundServiceAccountNamespaces []string
	TokenPolicyNames              []string
	TokenTTLSeconds               int
}

// ACLRule is one metadata-only Vault ACL rule: a path and its capabilities. The
// path is fingerprinted downstream; the raw path text never leaves this lane.
type ACLRule struct {
	Path         string
	Capabilities []string
}

// ACLPolicy is the metadata-only view of one Vault ACL policy. It carries a
// policy-name and content hash plus per-rule capability summaries, never the
// raw policy body.
type ACLPolicy struct {
	PolicyName string
	PolicyHash string
	Rules      []ACLRule
}

// IdentityEntity is the metadata-only view of one Vault identity entity.
type IdentityEntity struct {
	EntityID   string
	EntityName string
	AliasCount int
	GroupCount int
	Disabled   bool
}

// IdentityAlias is the metadata-only view of one Vault identity alias and its
// mount/entity join anchors.
type IdentityAlias struct {
	AliasID       string
	EntityID      string
	MountPath     string
	MountAccessor string
	AliasName     string
}

// KVMetadata is the metadata-only view of one Vault KV v2 metadata path. It is
// sourced from the KV v2 metadata endpoint only — never /data — and carries
// version counters and custom-metadata key names, never values or secret data.
type KVMetadata struct {
	MountPath              string
	Path                   string
	CurrentVersion         int
	OldestVersion          int
	MaxVersions            int
	CASRequired            bool
	DeleteVersionAfterSecs int
	CustomMetadataKeys     []string
}

// SecretEngineMount is the metadata-only view of one Vault secret-engine mount.
type SecretEngineMount struct {
	MountPath              string
	MountAccessor          string
	MountType              string
	KVVersion              string
	Local                  bool
	DefaultLeaseTTLSeconds int
	MaxLeaseTTLSeconds     int
}

// Client is the narrow, read-only Vault metadata surface the source lane needs.
//
// It is metadata-only by construction: there is deliberately no method that
// reads a KV /data value, a token, an AppRole secret_id, or any other secret
// material. New methods added here must preserve that invariant — list and
// describe metadata only, never read values. The invariant is guarded by
// TestClientSurfaceIsMetadataOnly.
type Client interface {
	// ListAuthMounts returns auth method mount metadata (sys/auth).
	ListAuthMounts(ctx context.Context) ([]AuthMount, error)
	// ListAuthRoles returns auth role metadata, including Kubernetes-auth bound
	// ServiceAccount selectors used as the IAM-Vault join anchor.
	ListAuthRoles(ctx context.Context) ([]AuthRole, error)
	// ListACLPolicies returns ACL policy metadata (sys/policies/acl) as name,
	// content hash, and per-rule capability summaries — never the raw body.
	ListACLPolicies(ctx context.Context) ([]ACLPolicy, error)
	// ListIdentityEntities returns identity entity metadata.
	ListIdentityEntities(ctx context.Context) ([]IdentityEntity, error)
	// ListIdentityAliases returns identity alias metadata and mount/entity
	// join anchors.
	ListIdentityAliases(ctx context.Context) ([]IdentityAlias, error)
	// ListKVMetadata returns KV v2 metadata-path descriptions from the metadata
	// endpoint only. It must never read a KV /data value.
	ListKVMetadata(ctx context.Context) ([]KVMetadata, error)
	// ListSecretEngineMounts returns secret-engine mount metadata (sys/mounts).
	ListSecretEngineMounts(ctx context.Context) ([]SecretEngineMount, error)
}
