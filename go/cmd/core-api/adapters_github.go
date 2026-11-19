// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/githublogin"
	"github.com/eshu-hq/eshu/go/internal/oidcbearer"
	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	tenantstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/tenant"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// corePostgresGitHubStoreAdapter persists GitHub login state.
type corePostgresGitHubStoreAdapter struct {
	store *pgstatus.GitHubLoginStore
}

// coreGitHubServiceAdapter wraps *githublogin.Service for the service and
// lister contracts.
type coreGitHubServiceAdapter struct {
	*githublogin.Service
}

// ListGitHubProviderIDs returns registered GitHub providers without secrets.
func (a coreGitHubServiceAdapter) ListGitHubProviderIDs() []query.GitHubRegisteredProvider {
	providers := a.RegisteredProviders()
	result := make([]query.GitHubRegisteredProvider, 0, len(providers))
	for _, p := range providers {
		result = append(result, query.GitHubRegisteredProvider{
			ProviderConfigID: p.ProviderConfigID,
			TenantID:         p.TenantID,
		})
	}
	return result
}

// coreGitHubLoginAdmission binds the GitHub service through the admission
// contract. A nil service yields an untyped nil port (503, never panic).
func coreGitHubLoginAdmission(service *githublogin.Service) query.GitHubLoginAdmission {
	if service == nil {
		return nil
	}
	return coreGitHubServiceAdapter{service}
}

// coreGitHubGrantResolverAdapter reuses the OIDC login store's group→role
// grant resolution for GitHub (one mapping table, keyed on
// provider_config_id alone).
type coreGitHubGrantResolverAdapter struct {
	oidcStore *pgstatus.OIDCLoginStore
	fallback  githublogin.GrantResolver
}

func (r coreGitHubGrantResolverAdapter) ResolveGroupGrants(
	ctx context.Context,
	grantQuery githublogin.GrantQuery,
) (githublogin.GrantResolution, bool, error) {
	if r.oidcStore != nil {
		resolution, ok, err := r.oidcStore.ResolveGroupRoleGrants(ctx, pgstatus.OIDCGroupGrantQuery{
			ProviderConfigID:    grantQuery.ProviderConfigID,
			TenantID:            grantQuery.TenantID,
			WorkspaceID:         grantQuery.WorkspaceID,
			ExternalGroupHashes: append([]string(nil), grantQuery.GroupHashes...),
			AsOf:                grantQuery.AsOf,
		})
		if err != nil || ok {
			return githublogin.GrantResolution{
				RoleIDs:                      append([]string(nil), resolution.RoleIDs...),
				PolicyRevisionHash:           resolution.PolicyRevisionHash,
				PermissionCatalogEnforced:    true,
				AllowedScopeIDs:              append([]string(nil), resolution.AllowedScopeIDs...),
				AllowedRepositoryIDs:         append([]string(nil), resolution.AllowedRepositoryIDs...),
				AllowedPermissionFeatures:    append([]string(nil), resolution.AllowedPermissionFeatures...),
				AllowedPermissionDataClasses: append([]string(nil), resolution.AllowedPermissionDataClasses...),
			}, ok, err
		}
	}
	if r.fallback == nil {
		return githublogin.GrantResolution{}, false, nil
	}
	return r.fallback.ResolveGroupGrants(ctx, grantQuery)
}

// newCoreGitHubLoginHandler builds the GitHub login handler. It returns nil
// when neither a config file nor ESHU_AUTH_GITHUB_ENABLED=true activates it.
func newCoreGitHubLoginHandler(
	getenv func(string) string,
	database *sql.DB,
	instruments *telemetry.Instruments,
	providerSecretKeyring *secretcrypto.Keyring,
) (*query.GitHubLoginHandler, error) {
	enabled := false
	if rawEnabled := coreEnvOrEmpty(getenv, coreEnvAuthGitHubEnabled); rawEnabled != "" {
		parsed, err := strconv.ParseBool(rawEnabled)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", coreEnvAuthGitHubEnabled, err)
		}
		if !parsed {
			return nil, nil
		}
		enabled = true
	}
	configPath := coreEnvOrEmpty(getenv, coreEnvAuthGitHubConfigFile)
	if configPath == "" && !enabled {
		return nil, nil
	}
	if database == nil {
		return nil, fmt.Errorf("postgres is required for github login")
	}

	var config githublogin.Config
	staticResolver := githublogin.StaticGrantResolver{}
	if configPath != "" {
		loaded, resolver, err := githublogin.LoadConfigFile(configPath)
		if err != nil {
			return nil, err
		}
		config, staticResolver = loaded, resolver
	}
	if providerID := coreEnvOrEmpty(getenv, coreEnvAuthGitHubProviderID); providerID != "" {
		config.DefaultProviderID = providerID
	}
	if rawTTL := coreEnvOrEmpty(getenv, coreEnvAuthGitHubStateTTL); rawTTL != "" {
		ttl, err := time.ParseDuration(rawTTL)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", coreEnvAuthGitHubStateTTL, err)
		}
		config.StateTTL = ttl
	}
	sessionRefreshWindow := query.DefaultGitHubSessionRefreshWindow
	if rawWindow := coreEnvOrEmpty(getenv, coreEnvAuthGitHubSessionRefreshWindow); rawWindow != "" {
		window, err := time.ParseDuration(rawWindow)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", coreEnvAuthGitHubSessionRefreshWindow, err)
		}
		if window <= 0 {
			return nil, fmt.Errorf("%s must be positive", coreEnvAuthGitHubSessionRefreshWindow)
		}
		sessionRefreshWindow = window
	}
	normalized, err := githublogin.ValidateConfig(config)
	if err != nil {
		return nil, fmt.Errorf("validate github login config: %w", err)
	}
	config = normalized

	store := newCorePostgresGitHubStoreAdapter(database, instruments)
	oidcStoreForGrants := newCorePostgresOIDCStoreAdapter(database, instruments).store
	var serviceOptions []githublogin.Option
	if resolver := newCoreGitHubDBProviderResolver(database, providerSecretKeyring); resolver != nil {
		serviceOptions = append(serviceOptions, githublogin.WithDBProviderResolver(resolver))
	}
	service := githublogin.NewService(
		config,
		store,
		coreGitHubGrantResolverAdapter{oidcStore: oidcStoreForGrants, fallback: staticResolver},
		githublogin.NewGitHubConnector,
		serviceOptions...,
	)
	cookieSecureMode, err := query.ValidateCookieSecureMode(coreEnvOrEmpty(getenv, query.CookieSecureModeEnv))
	if err != nil {
		return nil, fmt.Errorf("configure cookie secure mode: %w", err)
	}
	return &query.GitHubLoginHandler{
		Service:              coreGitHubLoginAdmission(service),
		SessionIssuer:        newCoreBrowserSessionHandler(database, instruments, cookieSecureMode),
		SessionRefreshWindow: sessionRefreshWindow,
	}, nil
}

// newCorePostgresGitHubStoreAdapter builds the GitHub login store adapter.
func newCorePostgresGitHubStoreAdapter(
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
) *corePostgresGitHubStoreAdapter {
	return &corePostgresGitHubStoreAdapter{
		store: pgstatus.NewGitHubLoginStore(coreInstrumentedDB(rawDB, instruments, "identity_github_login")),
	}
}

// CreateState persists one GitHub login state record.
func (a *corePostgresGitHubStoreAdapter) CreateState(ctx context.Context, record githublogin.StateRecord) error {
	return a.store.CreateState(ctx, pgstatus.GitHubLoginStateRecord{
		StateHash:        record.StateHash,
		ProviderConfigID: record.ProviderConfigID,
		TenantID:         record.TenantID,
		WorkspaceID:      record.WorkspaceID,
		RedirectURIHash:  record.RedirectURIHash,
		ReturnToPath:     record.ReturnToPath,
		IssuedAt:         record.IssuedAt,
		ExpiresAt:        record.ExpiresAt,
		UpdatedAt:        record.UpdatedAt,
	})
}

// ConsumeState consumes one GitHub login state exactly once.
func (a *corePostgresGitHubStoreAdapter) ConsumeState(
	ctx context.Context,
	stateHash string,
	consumedAt time.Time,
) (githublogin.StateRecord, bool, error) {
	record, ok, err := a.store.ConsumeState(ctx, stateHash, consumedAt)
	if err != nil || !ok {
		return githublogin.StateRecord{}, ok, err
	}
	return githublogin.StateRecord{
		StateHash:        record.StateHash,
		ProviderConfigID: record.ProviderConfigID,
		TenantID:         record.TenantID,
		WorkspaceID:      record.WorkspaceID,
		RedirectURIHash:  record.RedirectURIHash,
		ReturnToPath:     record.ReturnToPath,
		IssuedAt:         record.IssuedAt,
		ExpiresAt:        record.ExpiresAt,
		UpdatedAt:        record.UpdatedAt,
	}, true, nil
}

// CoreOIDCBearerResolver constructs the IdP bearer-token resolver, or
// returns (nil, nil) when ESHU_AUTH_RESOURCE_URI is unset: bearer
// validation is disabled entirely in that case, leaving the
// shared/scoped/file chain unaffected. The grant resolver mirrors the
// interactive OIDC login composition (DB-backed primary, static env-file
// fallback) so bearer tokens produce identical grants.
func CoreOIDCBearerResolver(
	ctx context.Context,
	getenv func(string) string,
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (query.ScopedTokenResolver, error) {
	audience := coreEnvOrEmpty(getenv, "ESHU_AUTH_RESOURCE_URI")
	if audience == "" {
		return nil, nil
	}
	if rawDB == nil {
		return nil, errors.New("oidc bearer resolver: postgres is required")
	}
	envConfig := oidclogin.Config{}
	staticResolver := oidclogin.StaticGrantResolver{}
	if configPath := coreEnvOrEmpty(getenv, coreEnvAuthOIDCConfigFile); configPath != "" {
		loaded, resolver, err := oidclogin.LoadConfigFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("oidc bearer resolver: load %s: %w", coreEnvAuthOIDCConfigFile, err)
		}
		envConfig, staticResolver = loaded, resolver
	}
	execQueryer := db.ExecQueryer(pgstatus.SQLDB{DB: rawDB})
	source := oidcbearer.ComposeProviderSources(
		oidcbearer.NewEnvProviderSource(envConfig),
		&coreOIDCBearerDBProviderSource{
			store:      pgstatus.NewIdentitySubjectStore(execQueryer),
			workspaces: tenantstore.NewTenantWorkspaceGrantStore(execQueryer),
			logger:     logger,
		},
	)
	resolver, err := oidcbearer.NewResolver(ctx, oidcbearer.Config{
		Source: source,
		GrantResolver: coreFallbackOIDCGrantResolver{
			primary:  newCorePostgresOIDCStoreAdapter(rawDB, instruments),
			fallback: staticResolver,
		},
		Audience:    audience,
		Instruments: instruments,
		Logger:      logger,
	})
	if err != nil {
		return nil, fmt.Errorf("oidc bearer resolver: %w", err)
	}
	return resolver, nil
}

// coreOIDCBearerDBProviderSource adapts DB-backed OIDC provider configs
// into bearer provider entries, excluding providers whose tenant has no
// unambiguous workspace (fail closed, logged).
type coreOIDCBearerDBProviderSource struct {
	store      *pgstatus.IdentitySubjectStore
	workspaces *tenantstore.TenantWorkspaceGrantStore
	logger     *slog.Logger
}

// ActiveBearerProviders implements oidcbearer.ProviderSource.
func (s *coreOIDCBearerDBProviderSource) ActiveBearerProviders(
	ctx context.Context,
) ([]oidcbearer.BearerProvider, error) {
	rows, err := s.store.ListActiveOIDCBearerProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("oidc bearer db provider source: %w", err)
	}
	providers := make([]oidcbearer.BearerProvider, 0, len(rows))
	for _, row := range rows {
		workspaceID, err := s.workspaces.PrimaryWorkspaceForTenant(ctx, row.TenantID)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn(
					"oidc bearer db provider has no unambiguous tenant workspace; excluded from this snapshot",
					"provider_config_id", row.ProviderConfigID, "tenant_id", row.TenantID, "error", err,
				)
			}
			continue
		}
		providers = append(providers, oidcbearer.BearerProvider{
			ProviderConfigID: row.ProviderConfigID,
			IssuerURL:        row.Issuer,
			TenantID:         row.TenantID,
			WorkspaceID:      workspaceID,
			GroupsClaim:      row.GroupClaim,
			SubjectClaim:     "sub",
			RevisionID:       row.RevisionID,
		})
	}
	return providers, nil
}
