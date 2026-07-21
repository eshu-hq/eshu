// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package githublogin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

const githubSecretBytes = 32

// Service handles backend GitHub Authorization Code (plain OAuth2) login.
// See doc.go for why this cannot ride oidclogin's discovery-based path.
type Service struct {
	config           Config
	stateStore       StateStore
	grantResolver    GrantResolver
	connectorFactory ConnectorFactory
	now              func() time.Time
	newSecret        func() (string, error)
	configErr        error
	// dbProviders resolves a DB-backed provider config when providerConfigID
	// does not match any env-file provider. Optional — nil means GitHub
	// login serves env-file providers only. Set via WithDBProviderResolver.
	dbProviders DBProviderResolver
}

// NewService constructs a GitHub login service.
func NewService(
	config Config,
	stateStore StateStore,
	grantResolver GrantResolver,
	connectorFactory ConnectorFactory,
	options ...Option,
) *Service {
	normalized, err := ValidateConfig(config)
	service := &Service{
		config:           normalized,
		stateStore:       stateStore,
		grantResolver:    grantResolver,
		connectorFactory: connectorFactory,
		now:              func() time.Time { return time.Now().UTC() },
		newSecret:        randomSecret,
		configErr:        err,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

// RegisteredProviders returns the provider configs loaded from the config
// file, mirroring oidclogin.Service.RegisteredProviders.
func (s *Service) RegisteredProviders() []ProviderConfig {
	if s == nil {
		return nil
	}
	return append([]ProviderConfig(nil), s.config.Providers...)
}

// StartGitHubLogin stores a state hash and returns the provider redirect.
func (s *Service) StartGitHubLogin(ctx context.Context, req StartRequest) (StartResponse, error) {
	provider, err := s.provider(ctx, req.ProviderConfigID, req.TenantID, req.WorkspaceID)
	if err != nil {
		return StartResponse{}, err
	}
	tenantID, workspaceID, err := resolveProviderContext(provider, req.TenantID, req.WorkspaceID)
	if err != nil {
		return StartResponse{}, err
	}
	state, err := s.newSecret()
	if err != nil {
		return StartResponse{}, fmt.Errorf("%w: create state", ErrGitHubLoginUnavailable)
	}
	now := s.now().UTC()
	record := StateRecord{
		StateHash:        SHA256Hash(state),
		ProviderConfigID: provider.ProviderConfigID,
		ProviderKeyHash:  SHA256Hash(provider.BaseURL + "\x00" + provider.ClientID),
		BaseURLHash:      SHA256Hash(provider.BaseURL),
		ClientIDHash:     SHA256Hash(provider.ClientID),
		TenantID:         tenantID,
		WorkspaceID:      workspaceID,
		RedirectURIHash:  SHA256Hash(provider.RedirectURL),
		ReturnToPath:     safeReturnPath(req.ReturnToPath),
		IssuedAt:         now,
		ExpiresAt:        now.Add(s.stateTTL()),
		UpdatedAt:        now,
	}
	if err := s.stateStore.CreateState(ctx, record); err != nil {
		return StartResponse{}, fmt.Errorf("%w: persist state", ErrGitHubLoginUnavailable)
	}
	connector, err := s.connector(ctx, provider)
	if err != nil {
		return StartResponse{}, err
	}
	return StartResponse{RedirectURL: connector.AuthCodeURL(state)}, nil
}

// CompleteGitHubLogin validates the callback and returns a browser-session
// auth context. Enforcement order: state replay/expiry, code exchange,
// verified-email presence, allowed-org membership, then team→role grant
// resolution — each is a distinct fail-closed reason logged for audit
// (issue #5166 acceptance: "user outside allowed orgs is rejected with an
// audited denied reason; no session created").
func (s *Service) CompleteGitHubLogin(ctx context.Context, req CompleteRequest) (CompleteResponse, error) {
	if err := s.ready(); err != nil {
		return CompleteResponse{}, err
	}
	state := strings.TrimSpace(req.State)
	code := strings.TrimSpace(req.Code)
	if state == "" || code == "" {
		return CompleteResponse{}, ErrGitHubLoginInvalidRequest
	}
	now := s.now().UTC()
	record, ok, err := s.stateStore.ConsumeState(ctx, SHA256Hash(state), now)
	if err != nil {
		return CompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: ErrGitHubLoginUnavailable, Reason: "state_store_unavailable"}
	}
	if !ok {
		return CompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied, Reason: "state_invalid"}
	}
	provider, err := s.provider(ctx, record.ProviderConfigID, record.TenantID, record.WorkspaceID)
	if err != nil {
		return CompleteResponse{}, err
	}
	if record.RedirectURIHash != SHA256Hash(provider.RedirectURL) {
		return CompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied, Reason: "redirect_mismatch"}
	}
	connector, err := s.connector(ctx, provider)
	if err != nil {
		return CompleteResponse{}, err
	}
	tokens, err := connector.Exchange(ctx, code)
	if err != nil {
		slog.WarnContext(ctx, "github login denied: code exchange failed",
			"provider_config_id", provider.ProviderConfigID, "reason", "code_exchange_failed")
		return CompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied, Reason: "code_exchange_failed"}
	}
	identity, err := connector.FetchIdentity(ctx, strings.TrimSpace(tokens.AccessToken), provider.AllowedOrgs)
	if err != nil {
		slog.WarnContext(ctx, "github login denied: identity fetch failed",
			"provider_config_id", provider.ProviderConfigID, "reason", "identity_fetch_failed")
		return CompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied, Reason: "identity_fetch_failed"}
	}
	if strings.TrimSpace(identity.Subject) == "" {
		return CompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied, Reason: "subject_missing"}
	}
	if strings.TrimSpace(identity.Email) == "" {
		slog.WarnContext(ctx, "github login denied: no verified primary email",
			"provider_config_id", provider.ProviderConfigID, "reason", "email_not_verified")
		return CompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied, Reason: "email_not_verified"}
	}
	if !anyOrgAllowed(identity.ActiveOrgs, provider.AllowedOrgs) {
		slog.WarnContext(ctx, "github login denied: no active membership in an allowed org",
			"provider_config_id", provider.ProviderConfigID, "reason", "org_not_allowed")
		return CompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied, Reason: "org_not_allowed"}
	}
	groupHashes := hashTeamHandles(identity.TeamHandles, provider.AllowedOrgs)
	if len(groupHashes) == 0 {
		slog.WarnContext(ctx, "github login denied: no team membership maps to a role",
			"provider_config_id", provider.ProviderConfigID, "reason", "no_team_role_mapping")
		return CompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied, Reason: "no_team_role_mapping"}
	}
	grants, ok, err := s.grantResolver.ResolveGroupGrants(ctx, GrantQuery{
		ProviderConfigID: provider.ProviderConfigID,
		TenantID:         record.TenantID,
		WorkspaceID:      record.WorkspaceID,
		GroupHashes:      groupHashes,
		AsOf:             now,
	})
	if err != nil {
		return CompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: ErrGitHubLoginUnavailable, Reason: "grant_resolution_unavailable"}
	}
	if !ok || len(grants.RoleIDs) == 0 {
		slog.WarnContext(ctx, "github login denied: team hashes resolved to no active role grant",
			"provider_config_id", provider.ProviderConfigID, "reason", "no_grants")
		return CompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: ErrGitHubLoginDenied, Reason: "no_grants"}
	}
	subjectIDHash := SHA256Hash(provider.ProviderConfigID + ":" + strings.TrimSpace(identity.Subject))
	return CompleteResponse{
		Auth: query.AuthContext{
			Mode:                         query.AuthModeScoped,
			TenantID:                     record.TenantID,
			WorkspaceID:                  record.WorkspaceID,
			SubjectClass:                 githubSubjectClass,
			SubjectIDHash:                subjectIDHash,
			PolicyRevisionHash:           grants.PolicyRevisionHash,
			RoleIDs:                      append([]string(nil), grants.RoleIDs...),
			AllScopes:                    grants.AllScopes,
			PermissionCatalogEnforced:    grants.PermissionCatalogEnforced,
			AllowedScopeIDs:              append([]string(nil), grants.AllowedScopeIDs...),
			AllowedRepositoryIDs:         append([]string(nil), grants.AllowedRepositoryIDs...),
			AllowedPermissionFeatures:    append([]string(nil), grants.AllowedPermissionFeatures...),
			AllowedPermissionDataClasses: append([]string(nil), grants.AllowedPermissionDataClasses...),
		},
		ProviderConfigID:    provider.ProviderConfigID,
		ProviderSubjectID:   subjectIDHash,
		ProviderGroupHashes: append([]string(nil), groupHashes...),
		ProviderProofAt:     now,
		ReturnToPath:        safeReturnPath(record.ReturnToPath),
	}, nil
}

// anyOrgAllowed reports whether the user has an active membership (per
// activeOrgs, already lowercased by the connector) in at least one org from
// allowedOrgs (lowercased here for a case-insensitive compare).
func anyOrgAllowed(activeOrgs []string, allowedOrgs []string) bool {
	allowed := make(map[string]struct{}, len(allowedOrgs))
	for _, org := range allowedOrgs {
		allowed[strings.ToLower(strings.TrimSpace(org))] = struct{}{}
	}
	for _, org := range activeOrgs {
		if _, ok := allowed[strings.ToLower(strings.TrimSpace(org))]; ok {
			return true
		}
	}
	return false
}

// hashTeamHandles hashes only the team handles ("org/team-slug") whose org
// segment is in allowedOrgs, so a team in an org outside the connector's
// tenant boundary can never map to a role even if a mapping row somehow
// exists for it.
func hashTeamHandles(teamHandles []string, allowedOrgs []string) []string {
	allowed := make(map[string]struct{}, len(allowedOrgs))
	for _, org := range allowedOrgs {
		allowed[strings.ToLower(strings.TrimSpace(org))] = struct{}{}
	}
	hashes := make([]string, 0, len(teamHandles))
	for _, handle := range cleanLowerStrings(teamHandles) {
		org, _, found := strings.Cut(handle, "/")
		if !found {
			continue
		}
		if _, ok := allowed[org]; !ok {
			continue
		}
		hashes = append(hashes, SHA256Hash(handle))
	}
	sort.Strings(hashes)
	return hashes
}

// provider resolves a provider_config_id to its full ProviderConfig,
// mirroring oidclogin.Service.provider: env-file providers are authoritative
// (matching the pre-auth discovery list's env-authoritative precedence),
// falling back to a DB-backed provider only when no env provider matched.
func (s *Service) provider(ctx context.Context, providerConfigID, tenantID, workspaceID string) (ProviderConfig, error) {
	if err := s.ready(); err != nil {
		return ProviderConfig{}, err
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	if providerConfigID == "" {
		providerConfigID = s.config.DefaultProviderID
	}
	for _, provider := range s.config.Providers {
		if provider.ProviderConfigID == providerConfigID {
			return provider, nil
		}
	}
	tenantID = strings.TrimSpace(tenantID)
	if s.dbProviders != nil && providerConfigID != "" && tenantID != "" {
		dbProvider, found, err := s.dbProviders.ResolveProvider(ctx, providerConfigID, tenantID, strings.TrimSpace(workspaceID))
		if err != nil {
			if errors.Is(err, ErrGitHubLoginInvalidRequest) {
				return ProviderConfig{}, fmt.Errorf("resolve db provider: %w", err)
			}
			return ProviderConfig{}, fmt.Errorf("%w: resolve db provider", ErrGitHubLoginUnavailable)
		}
		if found {
			return dbProvider, nil
		}
	}
	return ProviderConfig{}, ErrGitHubLoginInvalidRequest
}

func (s *Service) ready() error {
	if s == nil || s.stateStore == nil || s.grantResolver == nil || s.connectorFactory == nil {
		return ErrGitHubLoginUnavailable
	}
	if s.configErr != nil {
		return fmt.Errorf("%w: %v", ErrGitHubLoginInvalidRequest, s.configErr)
	}
	return nil
}

func (s *Service) connector(ctx context.Context, provider ProviderConfig) (Connector, error) {
	connector, err := s.connectorFactory(ctx, provider)
	if err != nil {
		return nil, fmt.Errorf("%w: connect provider", ErrGitHubLoginUnavailable)
	}
	return connector, nil
}

func (s *Service) stateTTL() time.Duration {
	if s.config.StateTTL > 0 {
		return s.config.StateTTL
	}
	return defaultStateTTL
}

func randomSecret() (string, error) {
	var bytes [githubSecretBytes]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate random secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

// resolveProviderContext validates the request's tenant/workspace against
// the provider's own scope, mirroring oidclogin's resolveProviderContext.
func resolveProviderContext(provider ProviderConfig, tenantID string, workspaceID string) (string, string, error) {
	tenantID = defaultString(strings.TrimSpace(tenantID), provider.TenantID)
	if tenantID != provider.TenantID {
		return "", "", ErrGitHubLoginInvalidRequest
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if provider.WorkspaceID != "" {
		workspaceID = defaultString(workspaceID, provider.WorkspaceID)
		if workspaceID != provider.WorkspaceID {
			return "", "", ErrGitHubLoginInvalidRequest
		}
	}
	return tenantID, workspaceID, nil
}

// ValidateConfig returns the normalized GitHub login config or an error.
func ValidateConfig(config Config) (Config, error) {
	return normalizeConfig(config)
}

func normalizeConfig(config Config) (Config, error) {
	config.DefaultProviderID = strings.TrimSpace(config.DefaultProviderID)
	if config.StateTTL < 0 {
		return Config{}, errors.New("state ttl must be non-negative")
	}
	providers := make([]ProviderConfig, 0, len(config.Providers))
	seen := map[string]struct{}{}
	for _, provider := range config.Providers {
		normalized, err := normalizeProvider(provider)
		if err != nil {
			return Config{}, err
		}
		if _, exists := seen[normalized.ProviderConfigID]; exists {
			return Config{}, errors.New("provider_config_id must be unique")
		}
		seen[normalized.ProviderConfigID] = struct{}{}
		providers = append(providers, normalized)
	}
	// Zero env-file providers is valid: GitHub login can run purely on
	// DB-backed provider configs resolved at request time, matching
	// oidclogin's normalizeConfig.
	if len(providers) == 0 {
		config.Providers = providers
		config.DefaultProviderID = ""
		return config, nil
	}
	if config.DefaultProviderID == "" {
		config.DefaultProviderID = providers[0].ProviderConfigID
	}
	if _, ok := seen[config.DefaultProviderID]; !ok {
		return Config{}, errors.New("default provider must reference a configured provider")
	}
	config.Providers = providers
	return config, nil
}

func normalizeProvider(provider ProviderConfig) (ProviderConfig, error) {
	provider.ProviderConfigID = strings.TrimSpace(provider.ProviderConfigID)
	provider.BaseURL = defaultString(strings.TrimSpace(provider.BaseURL), defaultBaseURL)
	provider.APIBaseURL = EffectiveAPIBaseURL(provider.BaseURL, provider.APIBaseURL)
	provider.ClientID = strings.TrimSpace(provider.ClientID)
	provider.ClientSecretFile = strings.TrimSpace(provider.ClientSecretFile)
	provider.RedirectURL = strings.TrimSpace(provider.RedirectURL)
	provider.TenantID = strings.TrimSpace(provider.TenantID)
	provider.WorkspaceID = strings.TrimSpace(provider.WorkspaceID)
	provider.Scopes = cleanLowerStrings(provider.Scopes)
	if len(provider.Scopes) == 0 {
		provider.Scopes = []string{"read:org", "user:email"}
	}
	provider.AllowedOrgs = cleanLowerStrings(provider.AllowedOrgs)
	if provider.ProviderConfigID == "" || provider.ClientID == "" || provider.RedirectURL == "" ||
		provider.TenantID == "" || provider.WorkspaceID == "" {
		return ProviderConfig{}, errors.New("provider id, client id, redirect url, tenant, and workspace are required")
	}
	if len(provider.AllowedOrgs) == 0 {
		return ProviderConfig{}, errors.New("allowed_orgs must be non-empty: a github provider with no org allow-list would let any github account sign in")
	}
	return provider, nil
}

// EffectiveAPIBaseURL resolves the REST API base URL a GitHub provider's
// login flow will actually call, given its (possibly blank) base_url and
// api_base_url configuration. It applies the identical defaulting the login
// path uses (see ResolveSealedProviderConfig / normalizeProvider): a blank
// base_url means github.com; an explicit api_base_url wins; otherwise
// github.com resolves to https://api.github.com and any GitHub Enterprise
// Server host to <base_url>/api/v3.
//
// The admin connection tester (cmd/api) derives its probe URL through this so
// it exercises exactly the endpoint login will reach — never a different
// default. Without it, a GHES provider with base_url set but api_base_url
// omitted would be probed at api.github.com while login uses the GHES host,
// producing a false-green test that enables an unreachable provider
// (issue #5166, F-5).
func EffectiveAPIBaseURL(baseURL, apiBaseURL string) string {
	return defaultAPIBaseURLFor(defaultString(strings.TrimSpace(baseURL), defaultBaseURL), strings.TrimSpace(apiBaseURL))
}

// defaultAPIBaseURLFor derives the REST API base URL for baseURL when
// apiBaseURL is not explicitly set: github.com uses api.github.com, any
// other (GitHub Enterprise Server) host uses baseURL + "/api/v3". baseURL is
// assumed already defaulted (non-empty); callers with a possibly-blank
// base_url should use EffectiveAPIBaseURL, which defaults it first.
func defaultAPIBaseURLFor(baseURL string, apiBaseURL string) string {
	if apiBaseURL != "" {
		return apiBaseURL
	}
	if baseURL == defaultBaseURL {
		return defaultAPIBaseURL
	}
	return strings.TrimRight(baseURL, "/") + "/api/v3"
}
