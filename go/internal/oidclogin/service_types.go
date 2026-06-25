// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidclogin

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// Config describes the enabled OIDC provider set.
type Config struct {
	DefaultProviderID string           `json:"default_provider_id,omitempty"`
	StateTTL          time.Duration    `json:"-"`
	Providers         []ProviderConfig `json:"providers"`
}

// ProviderConfig is one backend OIDC Authorization Code provider.
type ProviderConfig struct {
	ProviderConfigID string   `json:"provider_config_id"`
	IssuerURL        string   `json:"issuer_url"`
	ClientID         string   `json:"client_id"`
	ClientSecretFile string   `json:"client_secret_file,omitempty"`
	RedirectURL      string   `json:"redirect_url"`
	Scopes           []string `json:"scopes,omitempty"`
	TenantID         string   `json:"tenant_id"`
	WorkspaceID      string   `json:"workspace_id"`
	SubjectClaim     string   `json:"subject_claim,omitempty"`
	EmailClaim       string   `json:"email_claim,omitempty"`
	GroupsClaim      string   `json:"groups_claim,omitempty"`
}

// StateRecord is one hash-only OIDC login state.
type StateRecord struct {
	StateHash        string
	NonceHash        string
	ProviderConfigID string
	ProviderKeyHash  string
	IssuerHash       string
	ClientIDHash     string
	TenantID         string
	WorkspaceID      string
	RedirectURIHash  string
	ReturnToPath     string
	IssuedAt         time.Time
	ExpiresAt        time.Time
	UpdatedAt        time.Time
}

// StateStore persists and consumes server-side OIDC state.
type StateStore interface {
	CreateState(context.Context, StateRecord) error
	ConsumeState(context.Context, string, time.Time) (StateRecord, bool, error)
}

// GrantQuery asks a resolver to map hashed external groups to Eshu grants.
//
// GroupHashes drives login-time resolution from external group claims. RoleIDs
// drives active-session refresh re-resolution from a session's already granted
// roles, which lets the refresher detect revoked role targets, tombstoned or
// expired mappings, and policy revision drift without re-querying the provider.
// A resolver should treat the two as alternative inputs, preferring GroupHashes
// when present.
type GrantQuery struct {
	ProviderConfigID string
	TenantID         string
	WorkspaceID      string
	GroupHashes      []string
	RoleIDs          []string
	AsOf             time.Time
}

// GrantResolution is the Eshu-owned role and concrete grant result.
//
// AllowedPermissionFeatures and AllowedPermissionDataClasses carry the
// permission-catalog grants for the resolved roles so the issued cookie session
// enforces identically to a scoped token for the same roles.
//
// PermissionCatalogEnforced is the resolver's explicit declaration that the
// issued session must be gated by the permission catalog. Only a resolver that
// supplies a real catalog snapshot may set it: the database resolver sets it for
// scoped (non-admin) roles, while the file-backed static resolver leaves it
// false because it carries no catalog snapshot. Enforcement must be declared,
// not inferred from AllScopes or from an empty feature set, since a legitimate
// database role may grant zero features and still require enforcement.
type GrantResolution struct {
	RoleIDs                      []string
	PolicyRevisionHash           string
	AllScopes                    bool
	PermissionCatalogEnforced    bool
	AllowedScopeIDs              []string
	AllowedRepositoryIDs         []string
	AllowedPermissionFeatures    []string
	AllowedPermissionDataClasses []string
}

// GrantResolver resolves external groups through Eshu-owned role mappings.
type GrantResolver interface {
	ResolveGroupGrants(context.Context, GrantQuery) (GrantResolution, bool, error)
}

// TokenSet carries provider tokens only long enough to verify the ID token.
type TokenSet struct {
	IDToken string
}

// VerifiedClaims is the validated, normalized ID token claim subset.
type VerifiedClaims struct {
	Subject string
	Nonce   string
	Email   string
	Groups  []string
}

// Connector performs provider-specific OAuth2 and ID token verification.
type Connector interface {
	AuthCodeURL(state string, nonce string) string
	Exchange(context.Context, string) (TokenSet, error)
	VerifyIDToken(context.Context, string) (VerifiedClaims, error)
}

// ConnectorFactory constructs an OIDC connector for one provider.
type ConnectorFactory func(context.Context, ProviderConfig) (Connector, error)

// Option customizes Service behavior for tests and wiring.
type Option func(*Service)

// ValidateConfig returns the normalized OIDC login config or an error.
func ValidateConfig(config Config) (Config, error) {
	return normalizeConfig(config)
}

// WithNow overrides the clock used for state expiry.
func WithNow(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithSecretGenerator overrides state and nonce secret generation.
func WithSecretGenerator(newSecret func() (string, error)) Option {
	return func(s *Service) {
		if newSecret != nil {
			s.newSecret = newSecret
		}
	}
}

// safeReturnPath rejects any path that is not a safe same-origin redirect.
func safeReturnPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return ""
	}
	if strings.ContainsAny(path, "\r\n\t") {
		return ""
	}
	return path
}

func resolveProviderContext(provider ProviderConfig, tenantID string, workspaceID string) (string, string, error) {
	tenantID = defaultString(strings.TrimSpace(tenantID), provider.TenantID)
	workspaceID = defaultString(strings.TrimSpace(workspaceID), provider.WorkspaceID)
	if tenantID != provider.TenantID || workspaceID != provider.WorkspaceID {
		return "", "", query.ErrOIDCLoginInvalidRequest
	}
	return tenantID, workspaceID, nil
}

func hashStrings(values []string) []string {
	hashes := make([]string, 0, len(values))
	for _, value := range cleanStrings(values) {
		hashes = append(hashes, SHA256Hash(value))
	}
	return cleanStrings(hashes)
}

func cleanStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	sort.Strings(cleaned)
	return cleaned
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
