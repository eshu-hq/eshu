// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// corePostgresOIDCStoreAdapter persists OIDC login state and resolves
// group→role grants over the Postgres OIDC login store.
type corePostgresOIDCStoreAdapter struct {
	store *pgstatus.OIDCLoginStore
}

// coreOIDCServiceAdapter wraps *oidclogin.Service so it satisfies both the
// login service and the provider lister from one value.
type coreOIDCServiceAdapter struct {
	*oidclogin.Service
}

// ListOIDCProviderIDs returns (ProviderConfigID, TenantID) pairs for every
// registered OIDC provider. No sensitive fields are included.
func (a coreOIDCServiceAdapter) ListOIDCProviderIDs() []query.OIDCRegisteredProvider {
	providers := a.RegisteredProviders()
	result := make([]query.OIDCRegisteredProvider, 0, len(providers))
	for _, p := range providers {
		result = append(result, query.OIDCRegisteredProvider{
			ProviderConfigID: p.ProviderConfigID,
			TenantID:         p.TenantID,
		})
	}
	return result
}

// coreFallbackOIDCGrantResolver tries the primary (DB) grant resolver
// first, then the static (config-file) fallback.
type coreFallbackOIDCGrantResolver struct {
	primary  oidclogin.GrantResolver
	fallback oidclogin.GrantResolver
}

func (r coreFallbackOIDCGrantResolver) ResolveGroupGrants(
	ctx context.Context,
	grantQuery oidclogin.GrantQuery,
) (oidclogin.GrantResolution, bool, error) {
	if r.primary != nil {
		resolution, ok, err := r.primary.ResolveGroupGrants(ctx, grantQuery)
		if err != nil || ok {
			return resolution, ok, err
		}
	}
	if r.fallback == nil {
		return oidclogin.GrantResolution{}, false, nil
	}
	return r.fallback.ResolveGroupGrants(ctx, grantQuery)
}

// coreOIDCLoginAdmission binds the assembled OIDC service through the
// versioned admission contract. A nil service yields an untyped nil port so
// the handler fails closed 503 instead of panicking.
func coreOIDCLoginAdmission(service *oidclogin.Service) query.OIDCLoginAdmission {
	if service == nil {
		return nil
	}
	return coreOIDCServiceAdapter{service}
}

// newCoreOIDCLoginHandler builds the OIDC login handler. It returns nil when
// neither a config file nor ESHU_AUTH_OIDC_ENABLED=true activates it.
func newCoreOIDCLoginHandler(
	getenv func(string) string,
	database *sql.DB,
	instruments *telemetry.Instruments,
	providerSecretKeyring *secretcrypto.Keyring,
	logger *slog.Logger,
) (*query.OIDCLoginHandler, error) {
	enabled := false
	if rawEnabled := coreEnvOrEmpty(getenv, coreEnvAuthOIDCEnabled); rawEnabled != "" {
		parsed, err := strconv.ParseBool(rawEnabled)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", coreEnvAuthOIDCEnabled, err)
		}
		if !parsed {
			return nil, nil
		}
		enabled = true
	}
	configPath := coreEnvOrEmpty(getenv, coreEnvAuthOIDCConfigFile)
	if configPath == "" && !enabled {
		return nil, nil
	}
	if database == nil {
		return nil, fmt.Errorf("postgres is required for oidc login")
	}

	var config oidclogin.Config
	staticResolver := oidclogin.StaticGrantResolver{}
	if configPath != "" {
		loaded, resolver, err := oidclogin.LoadConfigFile(configPath)
		if err != nil {
			return nil, err
		}
		config, staticResolver = loaded, resolver
	}
	if logger != nil && staticResolver.HasIgnoredPolicyRevisionHash() {
		logger.Warn(
			"static OIDC config role_grants[].policy_revision_hash is deprecated and ignored",
			telemetry.EventAttr("auth.oidc.static_config.policy_revision_hash_ignored"),
		)
	}
	if providerID := coreEnvOrEmpty(getenv, coreEnvAuthOIDCProviderID); providerID != "" {
		config.DefaultProviderID = providerID
	}
	if rawTTL := coreEnvOrEmpty(getenv, coreEnvAuthOIDCStateTTL); rawTTL != "" {
		ttl, err := time.ParseDuration(rawTTL)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", coreEnvAuthOIDCStateTTL, err)
		}
		config.StateTTL = ttl
	}
	sessionRefreshWindow := query.DefaultOIDCSessionRefreshWindow
	if rawWindow := coreEnvOrEmpty(getenv, coreEnvAuthOIDCSessionRefreshWindow); rawWindow != "" {
		window, err := time.ParseDuration(rawWindow)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", coreEnvAuthOIDCSessionRefreshWindow, err)
		}
		if window <= 0 {
			return nil, fmt.Errorf("%s must be positive", coreEnvAuthOIDCSessionRefreshWindow)
		}
		sessionRefreshWindow = window
	}
	normalized, err := oidclogin.ValidateConfig(config)
	if err != nil {
		return nil, fmt.Errorf("validate oidc login config: %w", err)
	}
	config = normalized

	store := newCorePostgresOIDCStoreAdapter(database, instruments)
	var serviceOptions []oidclogin.Option
	if resolver := newCoreOIDCDBProviderResolver(database, providerSecretKeyring); resolver != nil {
		serviceOptions = append(serviceOptions, oidclogin.WithDBProviderResolver(resolver))
	}
	service := oidclogin.NewService(
		config,
		store,
		coreFallbackOIDCGrantResolver{primary: store, fallback: staticResolver},
		oidclogin.NewOIDCConnector,
		serviceOptions...,
	)
	cookieSecureMode, err := query.ValidateCookieSecureMode(coreEnvOrEmpty(getenv, query.CookieSecureModeEnv))
	if err != nil {
		return nil, fmt.Errorf("configure cookie secure mode: %w", err)
	}
	return &query.OIDCLoginHandler{
		Service:              coreOIDCLoginAdmission(service),
		SessionIssuer:        newCoreBrowserSessionHandler(database, instruments, cookieSecureMode),
		SessionRefreshWindow: sessionRefreshWindow,
	}, nil
}

// newCorePostgresOIDCStoreAdapter builds the OIDC login store adapter.
func newCorePostgresOIDCStoreAdapter(
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
) *corePostgresOIDCStoreAdapter {
	return &corePostgresOIDCStoreAdapter{
		store: pgstatus.NewOIDCLoginStore(coreInstrumentedDB(rawDB, instruments, "identity_oidc_login")),
	}
}

// CreateState persists one login state record.
func (a *corePostgresOIDCStoreAdapter) CreateState(ctx context.Context, record oidclogin.StateRecord) error {
	return a.store.CreateState(ctx, pgstatus.OIDCLoginStateRecord{
		StateHash:        record.StateHash,
		NonceHash:        record.NonceHash,
		ProviderConfigID: record.ProviderConfigID,
		ProviderKeyHash:  record.ProviderKeyHash,
		IssuerHash:       record.IssuerHash,
		ClientIDHash:     record.ClientIDHash,
		TenantID:         record.TenantID,
		WorkspaceID:      record.WorkspaceID,
		RedirectURIHash:  record.RedirectURIHash,
		ReturnToPath:     record.ReturnToPath,
		IssuedAt:         record.IssuedAt,
		ExpiresAt:        record.ExpiresAt,
		UpdatedAt:        record.UpdatedAt,
	})
}

// ConsumeState consumes one login state exactly once.
func (a *corePostgresOIDCStoreAdapter) ConsumeState(
	ctx context.Context,
	stateHash string,
	consumedAt time.Time,
) (oidclogin.StateRecord, bool, error) {
	record, ok, err := a.store.ConsumeState(ctx, stateHash, consumedAt)
	if err != nil || !ok {
		return oidclogin.StateRecord{}, ok, err
	}
	return oidclogin.StateRecord{
		StateHash:        record.StateHash,
		NonceHash:        record.NonceHash,
		ProviderConfigID: record.ProviderConfigID,
		ProviderKeyHash:  record.ProviderKeyHash,
		IssuerHash:       record.IssuerHash,
		ClientIDHash:     record.ClientIDHash,
		TenantID:         record.TenantID,
		WorkspaceID:      record.WorkspaceID,
		RedirectURIHash:  record.RedirectURIHash,
		ReturnToPath:     record.ReturnToPath,
		IssuedAt:         record.IssuedAt,
		ExpiresAt:        record.ExpiresAt,
		UpdatedAt:        record.UpdatedAt,
	}, true, nil
}

// ResolveGroupGrants resolves IdP group hashes to roles.
func (a *corePostgresOIDCStoreAdapter) ResolveGroupGrants(
	ctx context.Context,
	grantQuery oidclogin.GrantQuery,
) (oidclogin.GrantResolution, bool, error) {
	resolution, ok, err := a.store.ResolveGroupRoleGrants(ctx, pgstatus.OIDCGroupGrantQuery{
		ProviderConfigID:    grantQuery.ProviderConfigID,
		TenantID:            grantQuery.TenantID,
		WorkspaceID:         grantQuery.WorkspaceID,
		ExternalGroupHashes: append([]string(nil), grantQuery.GroupHashes...),
		AsOf:                grantQuery.AsOf,
	})
	if err != nil || !ok {
		return oidclogin.GrantResolution{}, ok, err
	}
	return oidclogin.GrantResolution{
		RoleIDs:                      append([]string(nil), resolution.RoleIDs...),
		PolicyRevisionHash:           resolution.PolicyRevisionHash,
		PermissionCatalogEnforced:    true,
		AllowedScopeIDs:              append([]string(nil), resolution.AllowedScopeIDs...),
		AllowedRepositoryIDs:         append([]string(nil), resolution.AllowedRepositoryIDs...),
		AllowedPermissionFeatures:    append([]string(nil), resolution.AllowedPermissionFeatures...),
		AllowedPermissionDataClasses: append([]string(nil), resolution.AllowedPermissionDataClasses...),
	}, true, nil
}

// CoreOIDCRateLimiter creates an OIDC login rate limiter from env vars.
// It returns nil when OIDC is disabled or rate limiting is configured at
// zero. Main wires it as middleware ahead of the auth chain.
func CoreOIDCRateLimiter(getenv func(string) string, instruments *telemetry.Instruments) *query.OIDCRateLimiter {
	enabled, _ := strconv.ParseBool(coreEnvOrEmpty(getenv, coreEnvAuthOIDCEnabled))
	if !enabled {
		return nil
	}
	ipRate := query.DefaultOIDCLoginRatePerSec
	if v := coreEnvOrEmpty(getenv, coreEnvAuthOIDCLoginRatePerSec); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			ipRate = n
		}
	}
	ipBurst := query.DefaultOIDCLoginBurst
	if v := coreEnvOrEmpty(getenv, coreEnvAuthOIDCLoginRateBurst); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			ipBurst = n
		}
	}
	providerRate := query.DefaultOIDCLoginProviderRatePerMin
	if v := coreEnvOrEmpty(getenv, coreEnvAuthOIDCLoginProviderRatePerMin); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			providerRate = n
		}
	}
	providerBurst := query.DefaultOIDCLoginProviderBurst
	if v := coreEnvOrEmpty(getenv, coreEnvAuthOIDCLoginProviderBurst); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			providerBurst = n
		}
	}
	return query.NewOIDCRateLimiter(float64(ipRate), ipBurst, float64(providerRate), providerBurst, instruments)
}
