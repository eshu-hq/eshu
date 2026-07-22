// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidclogin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

const (
	defaultStateTTL  = 10 * time.Minute
	oidcSecretBytes  = 32
	oidcSubjectClass = "external_oidc_user"
)

// Service handles backend OIDC Authorization Code login.
type Service struct {
	config           Config
	stateStore       StateStore
	grantResolver    GrantResolver
	connectorFactory ConnectorFactory
	now              func() time.Time
	newSecret        func() (string, error)
	configErr        error
	// dbProviders resolves a DB-backed provider config (#4966, epic #4962)
	// when providerConfigID does not match any env-file provider. Optional —
	// nil means OIDC login serves env-file providers only. Set via
	// WithDBProviderResolver.
	dbProviders DBProviderResolver
}

// NewService constructs an OIDC login service.
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

// RegisteredProviders returns the provider configs loaded from the config file.
// Used by the pre-auth provider discovery endpoint to surface runtime-configured
// OIDC providers that may not yet have a DB row.
func (s *Service) RegisteredProviders() []ProviderConfig {
	if s == nil {
		return nil
	}
	return append([]ProviderConfig(nil), s.config.Providers...)
}

// StartOIDCLogin stores state/nonce hashes and returns the provider redirect.
func (s *Service) StartOIDCLogin(
	ctx context.Context,
	req query.OIDCLoginStartRequest,
) (query.OIDCLoginStartResponse, error) {
	provider, err := s.provider(ctx, req.ProviderConfigID, req.TenantID, req.WorkspaceID)
	if err != nil {
		return query.OIDCLoginStartResponse{}, err
	}
	tenantID, workspaceID, err := resolveProviderContext(provider, req.TenantID, req.WorkspaceID)
	if err != nil {
		return query.OIDCLoginStartResponse{}, err
	}
	state, err := s.newSecret()
	if err != nil {
		return query.OIDCLoginStartResponse{}, fmt.Errorf("%w: create state", query.ErrOIDCLoginUnavailable)
	}
	nonce, err := s.newSecret()
	if err != nil {
		return query.OIDCLoginStartResponse{}, fmt.Errorf("%w: create nonce", query.ErrOIDCLoginUnavailable)
	}
	now := s.now().UTC()
	record := StateRecord{
		StateHash:        SHA256Hash(state),
		NonceHash:        SHA256Hash(nonce),
		ProviderConfigID: provider.ProviderConfigID,
		ProviderKeyHash:  SHA256Hash(provider.IssuerURL + "\x00" + provider.ClientID),
		IssuerHash:       SHA256Hash(provider.IssuerURL),
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
		return query.OIDCLoginStartResponse{}, fmt.Errorf("%w: persist state", query.ErrOIDCLoginUnavailable)
	}
	connector, err := s.connector(ctx, provider)
	if err != nil {
		return query.OIDCLoginStartResponse{}, err
	}
	return query.OIDCLoginStartResponse{RedirectURL: connector.AuthCodeURL(state, nonce)}, nil
}

// CompleteOIDCLogin validates the callback and returns a browser-session auth context.
func (s *Service) CompleteOIDCLogin(
	ctx context.Context,
	req query.OIDCLoginCompleteRequest,
) (query.OIDCLoginCompleteResponse, error) {
	if err := s.ready(); err != nil {
		return query.OIDCLoginCompleteResponse{}, err
	}
	state := strings.TrimSpace(req.State)
	code := strings.TrimSpace(req.Code)
	if state == "" || code == "" {
		return query.OIDCLoginCompleteResponse{}, query.ErrOIDCLoginInvalidRequest
	}
	now := s.now().UTC()
	record, ok, err := s.stateStore.ConsumeState(ctx, SHA256Hash(state), now)
	if err != nil {
		return query.OIDCLoginCompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: query.ErrOIDCLoginUnavailable, Reason: "state_store_unavailable"}
	}
	if !ok {
		return query.OIDCLoginCompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: query.ErrOIDCLoginDenied, Reason: "state_invalid"}
	}
	provider, err := s.provider(ctx, record.ProviderConfigID, record.TenantID, record.WorkspaceID)
	if err != nil {
		return query.OIDCLoginCompleteResponse{}, err
	}
	if record.RedirectURIHash != SHA256Hash(provider.RedirectURL) {
		return query.OIDCLoginCompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: query.ErrOIDCLoginDenied, Reason: "redirect_mismatch"}
	}
	connector, err := s.connector(ctx, provider)
	if err != nil {
		return query.OIDCLoginCompleteResponse{}, err
	}
	tokens, err := connector.Exchange(ctx, code)
	if err != nil {
		return query.OIDCLoginCompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: query.ErrOIDCLoginDenied, Reason: "code_exchange_failed"}
	}
	claims, err := connector.VerifyIDToken(ctx, strings.TrimSpace(tokens.IDToken))
	if err != nil {
		return query.OIDCLoginCompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: query.ErrOIDCLoginDenied, Reason: "id_token_invalid"}
	}
	if SHA256Hash(claims.Nonce) != record.NonceHash {
		return query.OIDCLoginCompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: query.ErrOIDCLoginDenied, Reason: "nonce_mismatch"}
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return query.OIDCLoginCompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: query.ErrOIDCLoginDenied, Reason: "subject_missing"}
	}
	groupHashes := hashStrings(claims.Groups)
	if len(groupHashes) == 0 {
		return query.OIDCLoginCompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: query.ErrOIDCLoginDenied, Reason: "no_group_claim"}
	}
	grants, ok, err := s.grantResolver.ResolveGroupGrants(ctx, GrantQuery{
		ProviderConfigID: provider.ProviderConfigID,
		TenantID:         record.TenantID,
		WorkspaceID:      record.WorkspaceID,
		GroupHashes:      groupHashes,
		AsOf:             now,
	})
	if err != nil {
		return query.OIDCLoginCompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: query.ErrOIDCLoginUnavailable, Reason: "grant_resolution_unavailable"}
	}
	if !ok || len(grants.RoleIDs) == 0 {
		return query.OIDCLoginCompleteResponse{}, &query.SSOLoginDeniedError{Sentinel: query.ErrOIDCLoginDenied, Reason: "no_grants"}
	}
	subjectIDHash := SHA256Hash(provider.ProviderConfigID + ":" + strings.TrimSpace(claims.Subject))
	return query.OIDCLoginCompleteResponse{
		Auth: query.AuthContext{
			Mode:                         query.AuthModeScoped,
			TenantID:                     record.TenantID,
			WorkspaceID:                  record.WorkspaceID,
			SubjectClass:                 oidcSubjectClass,
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

// provider resolves a provider_config_id to its full ProviderConfig,
// preferring the env-file provider set (unchanged, tenant-agnostic lookup by
// id) and falling back to a DB-backed provider (#4966, epic #4962) via
// dbProviders when the id is not found there. Env config always wins on a
// ambiguous/colliding id — the DB fallback only runs when no env provider
// matched — matching the same env-authoritative precedence enforced at the
// pre-auth discovery list (see cmd/api/auth_providers.go's
// ListLoginProviders doc comment) and the admin read surface's
// shadowed_by_environment derivation. The DB fallback requires a non-empty
// tenantID (DB rows are tenant-scoped) and is a no-op when dbProviders is nil
// (env-only deployment).
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
			// A resolver that fails closed with an actionable, caller-facing
			// reason (e.g. #5040: a tenant-scoped DB-backed provider whose
			// tenant has no unambiguous workspace) surfaces as that error
			// directly — ErrOIDCLoginInvalidRequest maps to 400, not the
			// opaque 503 every other resolver failure (DB unavailable,
			// decrypt failure, malformed configuration) maps to below.
			if errors.Is(err, query.ErrOIDCLoginInvalidRequest) {
				return ProviderConfig{}, err
			}
			return ProviderConfig{}, fmt.Errorf("%w: resolve db provider", query.ErrOIDCLoginUnavailable)
		}
		if found {
			return dbProvider, nil
		}
	}
	return ProviderConfig{}, query.ErrOIDCLoginInvalidRequest
}

func (s *Service) ready() error {
	if s == nil || s.stateStore == nil || s.grantResolver == nil || s.connectorFactory == nil {
		return query.ErrOIDCLoginUnavailable
	}
	if s.configErr != nil {
		return fmt.Errorf("%w: %v", query.ErrOIDCLoginInvalidRequest, s.configErr)
	}
	return nil
}

func (s *Service) connector(ctx context.Context, provider ProviderConfig) (Connector, error) {
	connector, err := s.connectorFactory(ctx, provider)
	if err != nil {
		return nil, fmt.Errorf("%w: connect provider", query.ErrOIDCLoginUnavailable)
	}
	return connector, nil
}

func (s *Service) stateTTL() time.Duration {
	if s.config.StateTTL > 0 {
		return s.config.StateTTL
	}
	return defaultStateTTL
}

// SHA256Hash returns the stable sha256: audit hash for a transient value.
func SHA256Hash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func randomSecret() (string, error) {
	var bytes [oidcSecretBytes]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
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
	// Zero env-file providers is valid: OIDC login can run purely on
	// DB-backed provider configs resolved at request time via
	// Service.dbProviders (see WithDBProviderResolver and provider()) — a
	// deployment need not maintain an env config file at all once #4966
	// provider-config CRUD is in use. A caller with neither env-file nor
	// DB-backed providers configured simply has no usable provider, which
	// surfaces as ErrOIDCLoginInvalidRequest at request time, not here.
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
	provider.IssuerURL = strings.TrimSpace(provider.IssuerURL)
	provider.ClientID = strings.TrimSpace(provider.ClientID)
	provider.ClientSecretFile = strings.TrimSpace(provider.ClientSecretFile)
	provider.RedirectURL = strings.TrimSpace(provider.RedirectURL)
	provider.TenantID = strings.TrimSpace(provider.TenantID)
	provider.WorkspaceID = strings.TrimSpace(provider.WorkspaceID)
	provider.SubjectClaim = defaultString(strings.TrimSpace(provider.SubjectClaim), "sub")
	provider.EmailClaim = defaultString(strings.TrimSpace(provider.EmailClaim), "email")
	provider.GroupsClaim = defaultString(strings.TrimSpace(provider.GroupsClaim), "groups")
	provider.Scopes = cleanStrings(provider.Scopes)
	if len(provider.Scopes) == 0 {
		provider.Scopes = []string{"openid", "profile", "email", "groups"}
	}
	if provider.ProviderConfigID == "" || provider.IssuerURL == "" ||
		provider.ClientID == "" || provider.RedirectURL == "" ||
		provider.TenantID == "" || provider.WorkspaceID == "" {
		return ProviderConfig{}, errors.New("provider id, issuer, client id, redirect url, tenant, and workspace are required")
	}
	return provider, nil
}
