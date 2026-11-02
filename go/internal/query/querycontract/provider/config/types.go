// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import "time"

// Detail is the metadata-only admin view returned by GET
// routes. It never carries a secret; HasSecret/SecretFingerprint/SecretKeyID
// are derived, non-reversible metadata. ShadowedByEnvironment is derived at
// read time (never stored) — true when an env-file-backed provider already
// occupies this identity key, making the DB row read-only and its
// sealed_secret never consulted for login. ManagedBy is "environment" for
// both a pure env-file-only provider (no DB row at all — synthesized by the
// read adapter, see ListProviderConfigDetails) and a colliding, shadowed DB
// row (ShadowedByEnvironment=true); it is "database" for a normal,
// admin-editable DB-backed provider. Every "environment" provider — whether
// synthesized or shadowed — rejects edit/delete with a clear error (see
// the MutationStore port in the provider-config family package).
type Detail struct {
	ProviderConfigID      string
	ProviderKind          string
	Status                string
	ActiveRevisionID      string
	Configuration         map[string]any
	HasSecret             bool
	SecretFingerprint     string
	SecretKeyID           string
	ShadowedByEnvironment bool
	ManagedBy             string // "database" | "environment"
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// RevisionItem is one row of a provider config's revision
// history, metadata only.
type RevisionItem struct {
	RevisionID   string
	Status       string
	HasSecret    bool
	CreatedAt    time.Time
	ActivatedAt  time.Time
	SupersededAt time.Time
}

// WriteResult is returned by every provider-config
// mutation.
type WriteResult struct {
	ProviderConfigID string
	RevisionID       string
	Status           string
	Found            bool
	Changed          bool
}

// ConnectionTestResult reports a test-connection outcome.
// Detail never carries a secret or plaintext credential — only a bounded,
// operator-safe diagnostic string (e.g. an HTTP status class or a parse
// failure reason). RevisionID is the active revision that was actually
// tested; the enable path passes it back to the MutationStore port's
// EnableProviderConfig as a compare-and-swap guard so a concurrent
// Update/Revert cannot slip an untested revision into "active" between the
// test and the enable call.
type ConnectionTestResult struct {
	OK         bool
	Detail     string
	RevisionID string
}

// CreateRequest is the store-facing create request built
// by the handler from the family's providerConfigWriteRequest body.
type CreateRequest struct {
	ProviderConfigID  string
	TenantID          string
	ProviderKind      string
	ProviderKeyHash   string
	IssuerHash        string
	ClientIDHash      string
	MetadataURLHash   string
	EntityIDHash      string
	RevisionID        string
	Configuration     string
	ConfigurationHash string
	MetadataHash      string
	PlaintextSecret   string
	Now               time.Time
}

// UpdateRequest is the store-facing update request.
type UpdateRequest struct {
	ProviderConfigID string
	TenantID         string
	// ProviderKind must match the existing provider config's immutable kind;
	// the store rejects a mismatch with ErrAdminProviderConfigKindMismatch.
	ProviderKind      string
	RevisionID        string
	Configuration     string
	ConfigurationHash string
	MetadataHash      string
	PlaintextSecret   string
	Now               time.Time
}

// RevertRequest is the store-facing revert request.
// Named with a numeric suffix relative to the JSON body type
// providerConfigRevertRequest in the family package to avoid collision.
type RevertRequest struct {
	ProviderConfigID string
	TenantID         string
	TargetRevisionID string
	Now              time.Time
}
