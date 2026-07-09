// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"
)

// providerConfigIDBytes/providerConfigRevisionIDBytes size the random
// identifiers generated for a new provider config / revision. 18 raw bytes
// base64url-encodes to 24 characters — comparable entropy to the existing
// newSecret() pattern (local_identity_handler_helpers.go) used for invite
// codes and session tokens elsewhere in this package.
const (
	providerConfigIDBytes         = 18
	providerConfigRevisionIDBytes = 18
)

// newProviderConfigID generates a new provider_config_id. It is generated in
// this package (not the storage layer) so the AAD-bearing revision id below
// can be generated alongside it before either reaches the store — see
// ProviderConfigCreate's doc comment in the postgres package.
func newProviderConfigID() (string, error) {
	return newRandomID("pc_")
}

// newProviderConfigRevisionID generates a new revision_id.
func newProviderConfigRevisionID() (string, error) {
	return newRandomID("rev_")
}

func newRandomID(prefix string) (string, error) {
	buf := make([]byte, providerConfigIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// adminProviderConfigWriteRequest is the JSON body for creating or updating a
// provider config. Exactly one of the OIDC or SAML field groups is read,
// selected by ProviderKind. Every secret field (ClientSecret, SPPrivateKey,
// SPCertificate) is write-only: the handler seals it via the store and never
// echoes it back in any response.
type adminProviderConfigWriteRequest struct {
	ProviderKind string `json:"provider_kind"` // "oidc" | "saml"

	// ProviderConfigID is optional on create: when omitted, the handler
	// generates a random id. When supplied, it is used verbatim — this is how
	// an admin creates a DB-backed row that shares an id with (and is thereby
	// detected as shadowed by) an env-file-registered provider, matching the
	// existing env-vs-DB correlation-by-id pattern in this codebase
	// (HasActiveOIDCProviderConfigForTenant / HasActiveSAMLProviderConfigForTenant
	// in identity_admin_reads_providers.go) rather than re-deriving an
	// identity-key hash from env config internals the query-layer
	// OIDCRegisteredProvider/SAML RegisteredProviderIDs types deliberately do
	// not expose. Ignored on update (the path's provider_config_id wins).
	ProviderConfigID string `json:"provider_config_id,omitempty"`

	// OIDC fields.
	Issuer       string   `json:"issuer,omitempty"`
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"` // #nosec G101 -- JSON field name, not a credential
	Scopes       []string `json:"scopes,omitempty"`
	GroupClaim   string   `json:"group_claim,omitempty"`
	RedirectURL  string   `json:"redirect_url,omitempty"`

	// SAML fields.
	MetadataURL    string `json:"metadata_url,omitempty"`
	MetadataXML    string `json:"metadata_xml,omitempty"`
	EntityID       string `json:"entity_id,omitempty"`
	GroupAttribute string `json:"group_attribute,omitempty"`
	SPPrivateKey   string `json:"sp_private_key,omitempty"` // #nosec G101 -- JSON field name, not a credential
	SPCertificate  string `json:"sp_certificate,omitempty"`
}

// adminProviderConfigRevertRequest is the JSON body for reverting to a prior
// revision.
type adminProviderConfigRevertRequest struct {
	RevisionID string `json:"revision_id"`
}

// AdminProviderConfigDetail is the metadata-only admin view returned by GET
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
// AdminProviderConfigMutationStore's doc comment).
type AdminProviderConfigDetail struct {
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

// AdminProviderConfigRevisionItem is one row of a provider config's revision
// history, metadata only.
type AdminProviderConfigRevisionItem struct {
	RevisionID   string
	Status       string
	HasSecret    bool
	CreatedAt    time.Time
	ActivatedAt  time.Time
	SupersededAt time.Time
}

// AdminProviderConfigWriteResult is returned by every provider-config
// mutation.
type AdminProviderConfigWriteResult struct {
	ProviderConfigID string
	RevisionID       string
	Status           string
	Found            bool
	Changed          bool
}

// AdminProviderConfigConnectionTestResult reports a test-connection outcome.
// Detail never carries a secret or plaintext credential — only a bounded,
// operator-safe diagnostic string (e.g. an HTTP status class or a parse
// failure reason). RevisionID is the active revision that was actually
// tested; the enable path passes it back to
// AdminProviderConfigMutationStore.EnableProviderConfig as a
// compare-and-swap guard so a concurrent Update/Revert cannot slip an
// untested revision into "active" between the test and the enable call.
type AdminProviderConfigConnectionTestResult struct {
	OK         bool
	Detail     string
	RevisionID string
}

// AdminProviderConfigMutationStore is the write surface the provider-config
// admin handler uses. Every method is tenant-scoped and none accepts or
// returns a secret in plaintext; PlaintextSecret is accepted by Create/Update
// only to be sealed immediately by the implementation. No implementation of
// this interface may live in go/internal/query — see the package-level
// static-boundary test secretcrypto_open_boundary_test.go, which asserts no
// file under this package imports secretcrypto at all.
type AdminProviderConfigMutationStore interface {
	CreateProviderConfig(ctx context.Context, req AdminProviderConfigCreateRequest) (AdminProviderConfigWriteResult, error)
	UpdateProviderConfig(ctx context.Context, req AdminProviderConfigUpdateRequest) (AdminProviderConfigWriteResult, error)
	RevertProviderConfig(ctx context.Context, req AdminProviderConfigRevertRequest) (AdminProviderConfigWriteResult, error)
	// EnableProviderConfig activates a provider config. expectedActiveRevisionID
	// is REQUIRED — it must be the revision id a prior TestProviderConnection
	// call actually tested. The implementation compares it against the
	// provider config's CURRENT active revision under a row lock and rejects
	// (ErrAdminProviderConfigRevisionChanged) if a concurrent write changed it.
	EnableProviderConfig(ctx context.Context, providerConfigID, tenantID, expectedActiveRevisionID string) (AdminProviderConfigWriteResult, error)
	DisableProviderConfig(ctx context.Context, providerConfigID, tenantID string) (AdminProviderConfigWriteResult, error)
}

// AdminProviderConfigReadStore is the read surface the provider-config admin
// handler uses. No method ever returns a secret; has_secret,
// secret_fingerprint, and key_id are the only secret-adjacent fields.
type AdminProviderConfigReadStore interface {
	GetProviderConfigDetail(ctx context.Context, providerConfigID, tenantID string) (AdminProviderConfigDetail, bool, error)
	ListProviderConfigDetails(ctx context.Context, tenantID string) ([]AdminProviderConfigDetail, error)
	ListProviderConfigRevisions(ctx context.Context, providerConfigID, tenantID string) ([]AdminProviderConfigRevisionItem, error)
}

// ProviderConfigConnectionTester runs the bounded, safe portion of a
// provider's connection test (OIDC discovery + JWKS reachability and a
// secret round-trip sanity check; SAML metadata validation and a signing
// material parse check). It is implemented outside go/internal/query (in
// cmd/api, backed by oidclogin/samlauth) because opening the sealed secret
// happens inside it — see those packages' doc comments for the Open call
// sites and the explicit scope note on what "connection test" does and does
// not prove (no live browser authorization-code round trip is attempted).
type ProviderConfigConnectionTester interface {
	TestProviderConnection(ctx context.Context, providerConfigID, tenantID string) (AdminProviderConfigConnectionTestResult, error)
}

// AdminProviderConfigCreateRequest is the store-facing create request built
// by the handler from adminProviderConfigWriteRequest.
type AdminProviderConfigCreateRequest struct {
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

// AdminProviderConfigUpdateRequest is the store-facing update request.
type AdminProviderConfigUpdateRequest struct {
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

// AdminProviderConfigRevertRequest is the store-facing revert request.
// Named with a numeric suffix to avoid colliding with the JSON body type
// adminProviderConfigRevertRequest above.
type AdminProviderConfigRevertRequest struct {
	ProviderConfigID string
	TenantID         string
	TargetRevisionID string
	Now              time.Time
}
