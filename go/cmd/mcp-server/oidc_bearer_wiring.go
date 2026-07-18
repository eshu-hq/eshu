// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/oidcbearer"
	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// envAuthResourceURI and envAuthOIDCConfigFile mirror cmd/api's identically
// named constants exactly (issue #5162): mcp-server has no interactive OIDC
// login route of its own, but the bearer resolver reads the SAME two env
// vars cmd/api does, so a deployment configures ESHU_AUTH_RESOURCE_URI and
// ESHU_AUTH_OIDC_CONFIG_FILE once and both processes agree.
const (
	envAuthResourceURI    = "ESHU_AUTH_RESOURCE_URI"
	envAuthOIDCConfigFile = "ESHU_AUTH_OIDC_CONFIG_FILE"
)

// newOIDCBearerResolver constructs the IdP bearer-token resolver
// (internal/oidcbearer) for cmd/mcp-server. See cmd/api's identically named
// function's doc comment for the full activation and grant-equivalence
// contract; this is a resolver only — mcp-server mounts no OIDC login
// route, and this function must not add one (wiring_router_completeness_test.go
// asserts the mounted route set).
func newOIDCBearerResolver(
	ctx context.Context,
	getenv func(string) string,
	db *sql.DB,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (query.ScopedTokenResolver, error) {
	audience := strings.TrimSpace(getenv(envAuthResourceURI))
	if audience == "" {
		return nil, nil
	}
	if db == nil {
		return nil, fmt.Errorf("oidc bearer resolver: postgres is required")
	}

	config, staticResolver, err := loadOIDCBearerEnvConfig(getenv)
	if err != nil {
		return nil, err
	}

	execQueryer := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	source := oidcbearer.ComposeProviderSources(
		oidcbearer.NewEnvProviderSource(config),
		&oidcBearerDBProviderSource{
			store:      pgstatus.NewIdentitySubjectStore(execQueryer),
			workspaces: pgstatus.NewTenantWorkspaceGrantStore(execQueryer),
			logger:     logger,
		},
	)

	resolver, err := oidcbearer.NewResolver(ctx, oidcbearer.Config{
		Source: source,
		GrantResolver: oidcBearerFallbackGrantResolver{
			primary:  &oidcBearerGrantStoreAdapter{store: pgstatus.NewOIDCLoginStore(execQueryer)},
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

// loadOIDCBearerEnvConfig loads the operator-managed OIDC config file
// (ESHU_AUTH_OIDC_CONFIG_FILE), mirroring cmd/api's identically named
// function. An empty configPath is valid: env-file providers are simply
// absent and DB-backed providers still work.
func loadOIDCBearerEnvConfig(getenv func(string) string) (oidclogin.Config, oidclogin.StaticGrantResolver, error) {
	configPath := strings.TrimSpace(getenv(envAuthOIDCConfigFile))
	if configPath == "" {
		return oidclogin.Config{}, oidclogin.StaticGrantResolver{}, nil
	}
	config, staticResolver, err := oidclogin.LoadConfigFile(configPath)
	if err != nil {
		return oidclogin.Config{}, oidclogin.StaticGrantResolver{}, fmt.Errorf("oidc bearer resolver: load %s: %w", envAuthOIDCConfigFile, err)
	}
	return config, staticResolver, nil
}

// oidcBearerGrantStoreAdapter adapts pgstatus.OIDCLoginStore to
// oidclogin.GrantResolver, mirroring cmd/api's postgresOIDCStoreAdapter
// exactly (that type lives in cmd/api's own package and cannot be imported
// from this separate main package, so the small mapping is duplicated here
// rather than shared — the two are proven equivalent by both wrapping the
// identical pgstatus.OIDCLoginStore.ResolveGroupRoleGrants call with the
// identical field mapping).
type oidcBearerGrantStoreAdapter struct {
	store *pgstatus.OIDCLoginStore
}

// ResolveGroupGrants implements oidclogin.GrantResolver.
func (a *oidcBearerGrantStoreAdapter) ResolveGroupGrants(
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

// oidcBearerFallbackGrantResolver mirrors cmd/api's fallbackOIDCGrantResolver
// exactly (same duplication reasoning as oidcBearerGrantStoreAdapter above).
type oidcBearerFallbackGrantResolver struct {
	primary  oidclogin.GrantResolver
	fallback oidclogin.GrantResolver
}

// ResolveGroupGrants implements oidclogin.GrantResolver.
func (r oidcBearerFallbackGrantResolver) ResolveGroupGrants(
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

// oidcBearerDBProviderSource mirrors cmd/api's identically named type
// exactly, including the tenant-workspace resolution reasoning in its doc
// comment; duplicated here for the same reason as the grant-resolver types
// above (separate main package, no shared wiring-glue package exists in
// this codebase).
type oidcBearerDBProviderSource struct {
	store      *pgstatus.IdentitySubjectStore
	workspaces *pgstatus.TenantWorkspaceGrantStore
	logger     *slog.Logger
}

// ActiveBearerProviders implements oidcbearer.ProviderSource.
func (s *oidcBearerDBProviderSource) ActiveBearerProviders(ctx context.Context) ([]oidcbearer.BearerProvider, error) {
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
