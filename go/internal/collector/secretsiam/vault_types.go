// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

const (
	// ProviderVault identifies Vault metadata source evidence in the
	// secrets/IAM posture family.
	ProviderVault = "vault"

	// VaultAuthMethodKubernetes identifies a Vault Kubernetes auth mount or
	// role.
	VaultAuthMethodKubernetes = "kubernetes"
	// VaultAuthMethodAppRole identifies a Vault AppRole auth mount or role.
	VaultAuthMethodAppRole = "approle"
	// VaultSecretEngineKVV2 identifies a Vault KV v2 secret engine.
	VaultSecretEngineKVV2 = "kv-v2"
)

// VaultContext carries source scope, generation, claim, and observation fields
// for Vault secrets/IAM posture source facts.
type VaultContext struct {
	VaultClusterID      string
	Namespace           string
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
	RedactionKey        redact.Key
}

// VaultAuthMountObservation describes one Vault auth method mount. Mount paths
// and accessors are fingerprinted by envelope construction.
type VaultAuthMountObservation struct {
	Context                VaultContext
	MountPath              string
	MountAccessor          string
	AuthMethod             string
	Local                  bool
	DefaultLeaseTTLSeconds int
	MaxLeaseTTLSeconds     int
	SourceURI              string
	SourceRecordID         string
}

// VaultAuthRoleObservation describes one Vault auth role with selector and
// policy references redacted to fingerprints and counts.
type VaultAuthRoleObservation struct {
	Context                       VaultContext
	MountPath                     string
	RoleName                      string
	AuthMethod                    string
	KubernetesClusterID           string
	BoundServiceAccountNames      []string
	BoundServiceAccountNamespaces []string
	TokenPolicyNames              []string
	TokenTTLSeconds               int
	SourceURI                     string
	SourceRecordID                string
}

// VaultACLPolicyRuleSummary carries one redacted Vault ACL rule summary. Path
// values are fingerprinted by the envelope builder.
type VaultACLPolicyRuleSummary struct {
	Path         string
	Capabilities []string
}

// VaultACLPolicyObservation describes one Vault ACL policy without carrying
// the raw policy body.
type VaultACLPolicyObservation struct {
	Context        VaultContext
	PolicyName     string
	PolicyHash     string
	Rules          []VaultACLPolicyRuleSummary
	SourceURI      string
	SourceRecordID string
}

// VaultIdentityEntityObservation describes one Vault identity entity with
// identifiers redacted by envelope construction.
type VaultIdentityEntityObservation struct {
	Context        VaultContext
	EntityID       string
	EntityName     string
	AliasCount     int
	GroupCount     int
	Disabled       bool
	SourceURI      string
	SourceRecordID string
}

// VaultIdentityAliasObservation describes one Vault identity alias and its
// mount/entity join anchors.
type VaultIdentityAliasObservation struct {
	Context        VaultContext
	AliasID        string
	EntityID       string
	MountPath      string
	MountAccessor  string
	AliasName      string
	SourceURI      string
	SourceRecordID string
}

// VaultKVMetadataObservation describes one Vault KV v2 metadata path without
// carrying key names, path text, custom metadata values, or secret data.
type VaultKVMetadataObservation struct {
	Context                VaultContext
	MountPath              string
	Path                   string
	CurrentVersion         int
	OldestVersion          int
	MaxVersions            int
	CASRequired            bool
	DeleteVersionAfterSecs int
	CustomMetadataKeys     []string
	SourceURI              string
	SourceRecordID         string
}

// VaultSecretEngineMountObservation describes one Vault secret engine mount.
// Mount paths and accessors are fingerprinted by envelope construction.
type VaultSecretEngineMountObservation struct {
	Context                VaultContext
	MountPath              string
	MountAccessor          string
	MountType              string
	KVVersion              string
	Local                  bool
	DefaultLeaseTTLSeconds int
	MaxLeaseTTLSeconds     int
	SourceURI              string
	SourceRecordID         string
}

// VaultCoverageWarningObservation describes partial, hidden, unsupported,
// rate-limited, or stale Vault source coverage.
type VaultCoverageWarningObservation struct {
	Context        VaultContext
	WarningKind    string
	SourceState    string
	ResourceScope  string
	ErrorClass     string
	Message        string
	Attributes     map[string]any
	SourceURI      string
	SourceRecordID string
}
