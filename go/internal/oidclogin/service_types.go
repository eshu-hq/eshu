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
	// ClientSecret is the decrypted client secret for a DB-backed provider
	// (#4966, epic #4962), held only in-process and only for the duration of
	// one connector build — see ResolveSealedProviderConfig, this package's
	// (*secretcrypto.Keyring).Open call site. Never populated from the env
	// config file (which uses ClientSecretFile instead) and never
	// JSON-marshaled (json:"-"): this field must never be logged, returned,
	// or persisted. NewOIDCConnector prefers this over ClientSecretFile when
	// both are somehow set.
	ClientSecret string `json:"-"` // #nosec G101 -- struct field name, not a credential
}

// DBProviderResolver resolves a DB-backed provider config (#4966, epic
// #4962) by provider_config_id, scoped to the caller's tenant/workspace.
// Implemented outside this package (in cmd/api, backed by storage/postgres)
// because fetching the sealed_secret ciphertext requires a database; the
// implementation must call ResolveSealedProviderConfig to decrypt it rather
// than opening the envelope itself, keeping every
// (*secretcrypto.Keyring).Open call site for provider-config secrets inside
// this package (see ResolveSealedProviderConfig's doc comment). found=false
// (with a nil error) means no active DB-backed provider matches — the
// caller's env-file lookup already ran first and also missed, so this
// becomes ErrOIDCLoginInvalidRequest at the Service.provider() call site,
// same as an unknown env-file id.
//
// ResolveProvider is also responsible for filling in ProviderConfig.WorkspaceID
// when ResolveSealedProviderConfig returns it blank (#5040): identity_provider_configs
// is tenant-scoped only, so a DB-backed provider has no workspace of its own,
// but identity_oidc_login_states.workspace_id is TEXT NOT NULL — a resolver
// that returns a blank WorkspaceID makes every login-start for that provider
// fail closed at CreateState. The workspaceID parameter carries the caller's
// already-known workspace (blank at login-start's first resolve, non-blank on
// every later resolve within the same login, since Service persists and
// replays whatever this call returned); a resolver should default a blank
// workspaceID to the provider's tenant's own workspace and fail with an
// ErrOIDCLoginInvalidRequest-wrapped error (not silently pick one) when that
// tenant has more than one active workspace. See cmd/api's
// oidcDBProviderResolver.resolveWorkspace for the reference implementation.
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
// PolicyRevisionHash is populated only by a resolver backed by a live
// authority for the workspace's current policy revision (the DB-backed
// group-mapping resolver). StaticGrantResolver (file-backed, #5038) always
// leaves it empty: the caller's session-create write defaults an empty value
// to the live workspace hash (see browser_sessions_schema.go's
// createBrowserSessionQuery COALESCE), so a stale or wrong hash hand-set in a
// static config file can never silently make every subsequent authenticated
// request 401 after a successful login.
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

// resolveProviderContext validates the request's tenant/workspace against the
// provider's own scope. tenantID is always required to match exactly
// (defaulting to the provider's when the request omits it).
//
// workspaceID is only enforced when the provider itself is workspace-scoped
// (provider.WorkspaceID != ""), which is always true for env-file providers.
// DB-backed providers (#4966, epic #4962) have no workspace column in
// identity_provider_configs — that table is tenant-scoped only —
// so ResolveSealedProviderConfig always returns WorkspaceID == "" for them.
// Service.provider()'s DBProviderResolver call fills that in before this
// function ever sees the ProviderConfig (#5040: defaulting to the tenant's
// own workspace, or failing closed when the tenant has more than one), so in
// practice provider.WorkspaceID is non-empty here for any DB-backed provider
// that successfully resolved. The actual RBAC boundary for a DB-backed
// provider remains identity_provider_group_role_mappings, which IS
// workspace-scoped and is consulted later during grant resolution
// (GrantQuery.WorkspaceID) — a login still cannot obtain access outside its
// resolved workspace's grants regardless of this function's enforcement.
func resolveProviderContext(provider ProviderConfig, tenantID string, workspaceID string) (string, string, error) {
	tenantID = defaultString(strings.TrimSpace(tenantID), provider.TenantID)
	if tenantID != provider.TenantID {
		return "", "", query.ErrOIDCLoginInvalidRequest
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if provider.WorkspaceID != "" {
		workspaceID = defaultString(workspaceID, provider.WorkspaceID)
		if workspaceID != provider.WorkspaceID {
			return "", "", query.ErrOIDCLoginInvalidRequest
		}
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
