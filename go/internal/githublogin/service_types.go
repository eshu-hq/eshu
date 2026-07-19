// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package githublogin

import (
	"context"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
)

const (
	defaultStateTTL = 10 * time.Minute
	// defaultBaseURL is the github.com web/OAuth base used when a provider
	// does not set BaseURL (the common case: no GitHub Enterprise Server).
	defaultBaseURL = "https://github.com"
	// defaultAPIBaseURL is the github.com REST API base used when a provider
	// does not set APIBaseURL.
	defaultAPIBaseURL = "https://api.github.com"
	// githubSubjectClass identifies a GitHub-backed browser session's subject
	// class, mirroring oidclogin's "external_oidc_user" for the OIDC path.
	githubSubjectClass = "external_github_user"
)

// Sign-in errors are query.ErrGitHubLogin{Unavailable,InvalidRequest,Denied}
// (aliased below), not a package-local set: mirrors oidclogin, which uses
// query.ErrOIDCLogin* directly rather than defining its own, so
// query.GitHubLoginHandler's error mapping and this package's Service agree
// on one canonical set of sentinels.
var (
	ErrGitHubLoginUnavailable    = query.ErrGitHubLoginUnavailable
	ErrGitHubLoginInvalidRequest = query.ErrGitHubLoginInvalidRequest
	ErrGitHubLoginDenied         = query.ErrGitHubLoginDenied
)

// Config describes the enabled GitHub provider set for the env-file
// activation path (mirrors oidclogin.Config).
type Config struct {
	DefaultProviderID string           `json:"default_provider_id,omitempty"`
	StateTTL          time.Duration    `json:"-"`
	Providers         []ProviderConfig `json:"providers"`
}

// ProviderConfig is one backend GitHub Authorization Code (plain OAuth2, not
// OIDC) provider. AllowedOrgs is mandatory and non-empty: unlike an OIDC
// provider (which trusts the IdP's own tenant boundary), a GitHub OAuth App
// can authenticate ANY github.com (or GitHub Enterprise Server) account, so
// the org allow-list is the only tenant boundary this connector has —
// ValidateConfig rejects a provider with no allowed orgs rather than
// silently letting any GitHub user sign in.
type ProviderConfig struct {
	ProviderConfigID string `json:"provider_config_id"`
	// BaseURL is the GitHub web/OAuth host, e.g. "https://github.com" for
	// github.com or "https://github.example.com" for a GitHub Enterprise
	// Server instance. Defaults to "https://github.com".
	BaseURL string `json:"base_url,omitempty"`
	// APIBaseURL is the GitHub REST API host, e.g. "https://api.github.com"
	// for github.com or "https://github.example.com/api/v3" for GHES.
	// Defaults to "https://api.github.com" when BaseURL is also the
	// github.com default, otherwise defaults to BaseURL + "/api/v3".
	APIBaseURL       string   `json:"api_base_url,omitempty"`
	ClientID         string   `json:"client_id"`
	ClientSecretFile string   `json:"client_secret_file,omitempty"`
	RedirectURL      string   `json:"redirect_url"`
	Scopes           []string `json:"scopes,omitempty"`
	TenantID         string   `json:"tenant_id"`
	WorkspaceID      string   `json:"workspace_id"`
	// AllowedOrgs is the mandatory, non-empty allow-list of GitHub org logins
	// (case-insensitive) a user must have an active membership in for login
	// to succeed. This is the connector's tenant boundary.
	AllowedOrgs []string `json:"allowed_orgs"`
	// ClientSecret is the decrypted client secret for a DB-backed provider,
	// held only in-process for the duration of one connector build. Never
	// populated from the env config file (which uses ClientSecretFile
	// instead) and never JSON-marshaled — this field must never be logged,
	// returned, or persisted.
	ClientSecret string `json:"-"` // #nosec G101 -- struct field name, not a credential
}

// DBProviderResolver resolves a DB-backed GitHub provider config by
// provider_config_id, scoped to the caller's tenant, mirroring
// oidclogin.DBProviderResolver. Implemented outside this package (in
// cmd/api, backed by storage/postgres) because fetching the sealed_secret
// ciphertext requires a database.
type DBProviderResolver interface {
	ResolveProvider(ctx context.Context, providerConfigID, tenantID, workspaceID string) (ProviderConfig, bool, error)
}

// WithDBProviderResolver wires a DB-backed provider fallback into the
// service. When unset, Service.provider() only resolves env-file providers.
func WithDBProviderResolver(resolver DBProviderResolver) Option {
	return func(s *Service) {
		s.dbProviders = resolver
	}
}

// StateRecord is one hash-only GitHub OAuth2 state row. Unlike
// oidclogin.StateRecord, there is no NonceHash: plain OAuth2 has no ID token
// and no nonce concept — the `state` parameter is the only CSRF control.
type StateRecord struct {
	StateHash        string
	ProviderConfigID string
	ProviderKeyHash  string
	BaseURLHash      string
	ClientIDHash     string
	TenantID         string
	WorkspaceID      string
	RedirectURIHash  string
	ReturnToPath     string
	IssuedAt         time.Time
	ExpiresAt        time.Time
	UpdatedAt        time.Time
}

// StateStore persists and consumes server-side GitHub OAuth2 state.
type StateStore interface {
	CreateState(context.Context, StateRecord) error
	ConsumeState(context.Context, string, time.Time) (StateRecord, bool, error)
}

// TokenSet carries the GitHub OAuth2 access token only long enough to fetch
// identity. GitHub access tokens are opaque bearer credentials — there is no
// ID token to verify.
type TokenSet struct {
	AccessToken string
}

// Identity is the normalized, verified GitHub identity fetched from the
// REST API after token exchange. Login and Email are held only in-process
// for the duration of one login (used to compute hashes and the audit log
// line) and are never persisted raw, matching the OIDC connector's
// VerifiedClaims convention.
type Identity struct {
	// Subject is the GitHub numeric user id (stable across username
	// renames), as a decimal string. This is the identity anchor — never
	// the mutable Login.
	Subject string
	Login   string
	// Email is the user's verified primary email address. Empty when the
	// account has no verified primary email; CompleteGitHubLogin then fails
	// closed (ErrGitHubLoginDenied).
	Email string
	// ActiveOrgs is the set of GitHub org logins (lowercased) the user has
	// an active membership in, restricted to orgs the caller asked about
	// (see Connector.FetchIdentity's doc comment) — the connector itself
	// enforces no allow-list; Service does.
	ActiveOrgs []string
	// TeamHandles is "org/team-slug" for every team membership the user has
	// within ActiveOrgs, lowercased. These feed GrantQuery.GroupHashes
	// exactly like an OIDC group claim feeds it, through the identical
	// oidclogin.GrantResolver seam.
	TeamHandles []string
}

// Connector performs GitHub-specific OAuth2 code exchange and REST identity
// lookup. Unlike oidclogin.Connector there is no ID token to verify — GitHub
// user login is plain OAuth2 (see package doc.go) — identity comes entirely
// from calling the REST API with the exchanged access token.
type Connector interface {
	AuthCodeURL(state string) string
	Exchange(ctx context.Context, code string) (TokenSet, error)
	// FetchIdentity resolves the verified identity for accessToken, scoped
	// to allowedOrgs: implementations should only resolve org-membership and
	// team-membership detail for orgs in allowedOrgs (case-insensitive),
	// both to bound the number of GitHub API calls and to avoid holding
	// membership detail for orgs outside the connector's tenant boundary.
	FetchIdentity(ctx context.Context, accessToken string, allowedOrgs []string) (Identity, error)
}

// ConnectorFactory constructs a GitHub connector for one provider.
type ConnectorFactory func(context.Context, ProviderConfig) (Connector, error)

// Option customizes Service behavior for tests and wiring.
type Option func(*Service)

// WithNow overrides the clock used for state expiry.
func WithNow(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithSecretGenerator overrides state secret generation.
func WithSecretGenerator(newSecret func() (string, error)) Option {
	return func(s *Service) {
		if newSecret != nil {
			s.newSecret = newSecret
		}
	}
}

// GrantResolver, GrantQuery, GrantResolution, and StaticGrantResolver are the
// SAME group/team → role mapping seam oidclogin uses for OIDC group claims
// (issue #5166 requires team→role mapping "proven equivalent to OIDC
// group→role for the same grants fixture") — this package intentionally
// does not redefine them. A GitHub team handle ("org/team-slug") is hashed
// with SHA256Hash and fed into GrantQuery.GroupHashes exactly like an OIDC
// group claim value is.
type (
	GrantResolver       = oidclogin.GrantResolver
	GrantQuery          = oidclogin.GrantQuery
	GrantResolution     = oidclogin.GrantResolution
	StaticGrantResolver = oidclogin.StaticGrantResolver
	GroupRoleMapping    = oidclogin.GroupRoleMapping
	RoleGrant           = oidclogin.RoleGrant
)

// SHA256Hash is oidclogin's stable "sha256:" audit hash, reused unchanged so
// a GitHub team hash and an OIDC group hash are computed identically (the
// DB-backed identity_provider_group_role_mappings table has no provider_kind
// column and is keyed only on provider_config_id + external_group_hash — the
// same mapping rows and the same resolver code path serve both providers).
var SHA256Hash = oidclogin.SHA256Hash

// StartRequest, StartResponse, CompleteRequest, and CompleteResponse are the
// SAME types query.GitHubLoginHandler uses (mirrors oidclogin's use of
// query.OIDCLoginStartRequest et al. directly as its own method
// signatures) — Service.StartGitHubLogin/CompleteGitHubLogin satisfy
// query.GitHubLoginService without any field-mapping adapter in cmd/api.
type (
	StartRequest     = query.GitHubLoginStartRequest
	StartResponse    = query.GitHubLoginStartResponse
	CompleteRequest  = query.GitHubLoginCompleteRequest
	CompleteResponse = query.GitHubLoginCompleteResponse
)

// DefaultSessionRefreshWindow bounds external-proof staleness for
// already-issued GitHub browser sessions. GitHub access tokens cannot be
// silently re-validated the way an OIDC ID token can (there is no JWKS to
// re-check against) — a stale GitHub-backed session is refreshed by
// re-resolving the session's already-granted roles through the same
// GrantResolver, not by re-contacting GitHub.
const DefaultSessionRefreshWindow = query.DefaultGitHubSessionRefreshWindow

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

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func cleanLowerStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}
