// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

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

// providerConfigWriteRequest is the JSON body for creating or updating a
// provider config. Exactly one of the OIDC, SAML, or GitHub field groups is
// read, selected by ProviderKind. Every secret field (ClientSecret,
// SPPrivateKey, SPCertificate) is write-only: the handler seals it via the
// store and never echoes it back in any response.
type providerConfigWriteRequest struct {
	ProviderKind string `json:"provider_kind"` // "oidc" | "saml" | "github"

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

	// SAML fields. EntityID is the expected IDENTITY PROVIDER entity id
	// (validated against parsed IdP metadata — see ValidateIdentityProviderMetadata).
	// ServiceProviderEntityID/ServiceProviderACSURL are Eshu's OWN SP endpoints
	// for this provider, mirroring an env-file provider's
	// service_provider_entity_id/service_provider_acs_url (#4966 follow-up
	// #4978). Like OIDC's RedirectURL, both are optional at write/test-connection
	// time but required for the provider to resolve for login — see
	// samlauth.ResolveSealedProviderConfig.
	MetadataURL             string `json:"metadata_url,omitempty"`
	MetadataXML             string `json:"metadata_xml,omitempty"`
	EntityID                string `json:"entity_id,omitempty"`
	GroupAttribute          string `json:"group_attribute,omitempty"`
	ServiceProviderEntityID string `json:"service_provider_entity_id,omitempty"`
	ServiceProviderACSURL   string `json:"service_provider_acs_url,omitempty"`
	SPPrivateKey            string `json:"sp_private_key,omitempty"` // #nosec G101 -- JSON field name, not a credential
	SPCertificate           string `json:"sp_certificate,omitempty"`

	// GitHub fields (issue #5166, F-5). ClientID, ClientSecret, Scopes, and
	// RedirectURL above are shared with the OIDC section (same field names,
	// same meaning: an OAuth2 client id/secret/scopes/redirect). BaseURL and
	// APIBaseURL default to github.com / api.github.com when omitted — set
	// both only for a GitHub Enterprise Server instance. AllowedOrgs is
	// mandatory and non-empty: see githublogin.ProviderConfig's doc comment
	// for why a GitHub provider with no org allow-list is rejected outright.
	BaseURL     string   `json:"base_url,omitempty"`
	APIBaseURL  string   `json:"api_base_url,omitempty"`
	AllowedOrgs []string `json:"allowed_orgs,omitempty"`
}

// providerConfigRevertRequest is the JSON body for reverting to a prior
// revision.
type providerConfigRevertRequest struct {
	RevisionID string `json:"revision_id"`
}

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
// MutationStore's doc comment).
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
// tested; the enable path passes it back to
// MutationStore.EnableProviderConfig as a
// compare-and-swap guard so a concurrent Update/Revert cannot slip an
// untested revision into "active" between the test and the enable call.
type ConnectionTestResult struct {
	OK         bool
	Detail     string
	RevisionID string
}

// MutationStore is the write surface the provider-config
// admin handler uses. Every method is tenant-scoped and none accepts or
// returns a secret in plaintext; PlaintextSecret is accepted by Create/Update
// only to be sealed immediately by the implementation. No implementation of
// this interface may live in go/internal/query — see the package-level
// static-boundary test secretcrypto_open_boundary_test.go, which asserts no
// file under this package imports secretcrypto at all.
type MutationStore interface {
	CreateProviderConfig(ctx context.Context, req CreateRequest) (WriteResult, error)
	UpdateProviderConfig(ctx context.Context, req UpdateRequest) (WriteResult, error)
	RevertProviderConfig(ctx context.Context, req RevertRequest) (WriteResult, error)
	// EnableProviderConfig activates a provider config. expectedActiveRevisionID
	// is REQUIRED — it must be the revision id a prior TestProviderConnection
	// call actually tested. The implementation compares it against the
	// provider config's CURRENT active revision under a row lock and rejects
	// (ErrAdminProviderConfigRevisionChanged) if a concurrent write changed it.
	EnableProviderConfig(ctx context.Context, providerConfigID, tenantID, expectedActiveRevisionID string) (WriteResult, error)
	DisableProviderConfig(ctx context.Context, providerConfigID, tenantID string) (WriteResult, error)
}

// ReadStore is the read surface the provider-config admin
// handler uses. No method ever returns a secret; has_secret,
// secret_fingerprint, and key_id are the only secret-adjacent fields.
type ReadStore interface {
	GetProviderConfigDetail(ctx context.Context, providerConfigID, tenantID string) (Detail, bool, error)
	ListProviderConfigDetails(ctx context.Context, tenantID string) ([]Detail, error)
	ListProviderConfigRevisions(ctx context.Context, providerConfigID, tenantID string) ([]RevisionItem, error)
}

// ConnectionTester runs the bounded, safe portion of a
// provider's connection test (OIDC discovery + JWKS reachability and a
// secret round-trip sanity check; SAML metadata validation and a signing
// material parse check). It is implemented outside go/internal/query (in
// cmd/api, backed by oidclogin/samlauth) because opening the sealed secret
// happens inside it — see those packages' doc comments for the Open call
// sites and the explicit scope note on what "connection test" does and does
// not prove (no live browser authorization-code round trip is attempted).
type ConnectionTester interface {
	TestProviderConnection(ctx context.Context, providerConfigID, tenantID string) (ConnectionTestResult, error)
}

// CreateRequest is the store-facing create request built
// by the handler from providerConfigWriteRequest.
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
// Named with a numeric suffix to avoid colliding with the JSON body type
// providerConfigRevertRequest above.
type RevertRequest struct {
	ProviderConfigID string
	TenantID         string
	TargetRevisionID string
	Now              time.Time
}
