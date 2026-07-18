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

// envAuthResourceURI is the canonical Eshu API/MCP resource identifier
// (RFC 8707) an IdP bearer access token's aud claim must carry (issue
// #5162). Unset disables IdP bearer-token validation entirely: wireAPI must
// not construct a Resolver at all in that case (see newOIDCBearerResolver's
// doc comment), leaving the shared/scoped/file-registry token chain
// unaffected.
const envAuthResourceURI = "ESHU_AUTH_RESOURCE_URI"

// newOIDCBearerResolver constructs the IdP bearer-token resolver
// (internal/oidcbearer) for cmd/api, or returns (nil, nil) when
// ESHU_AUTH_RESOURCE_URI is unset — the resolver is disabled entirely in
// that case, not constructed-and-always-erroring.
//
// The grant resolver composition mirrors newOIDCLoginHandler's
// fallbackOIDCGrantResolver{primary: <postgres-backed>, fallback:
// <static env-file>} exactly (AC #3: bearer tokens must produce identical
// grants to an interactive login for the same user), but is built
// independently here because bearer-token validation activates on
// ESHU_AUTH_RESOURCE_URI alone — it must work even when interactive OIDC
// login itself (ESHU_AUTH_OIDC_ENABLED / ESHU_AUTH_OIDC_CONFIG_FILE) is
// off, since a token-only IdP integration has no need for the browser
// login flow at all.
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
		Source:        source,
		GrantResolver: fallbackOIDCGrantResolver{primary: newPostgresOIDCStoreAdapter(db, instruments), fallback: staticResolver},
		Audience:      audience,
		Instruments:   instruments,
		Logger:        logger,
	})
	if err != nil {
		return nil, fmt.Errorf("oidc bearer resolver: %w", err)
	}
	return resolver, nil
}

// loadOIDCBearerEnvConfig loads the same operator-managed OIDC config file
// (ESHU_AUTH_OIDC_CONFIG_FILE) interactive login reads, independently of
// whether interactive login itself is enabled — an empty configPath is
// valid (env-file providers are simply absent; DB-backed providers still
// work).
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

// oidcBearerDBProviderSource adapts DB-backed OIDC provider configs
// (pgstatus.IdentitySubjectStore.ListActiveOIDCBearerProviders) into
// oidcbearer.ProviderSource entries. identity_provider_configs is
// tenant-scoped only — it has no workspace_id column — so this type
// resolves each provider's workspace the same way cmd/api's
// oidcDBProviderResolver.resolveWorkspace does for interactive login: the
// tenant's one active workspace, or exclude the provider entirely (fail
// closed, logged) when the tenant has none or more than one. A bearer token
// carries no explicit workspace_id override the way a login-start request
// can, so "default to primary, fail closed on ambiguity" is the only choice
// available here.
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
