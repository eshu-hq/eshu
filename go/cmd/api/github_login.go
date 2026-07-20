// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/githublogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	envAuthGitHubEnabled              = "ESHU_AUTH_GITHUB_ENABLED"
	envAuthGitHubConfigFile           = "ESHU_AUTH_GITHUB_CONFIG_FILE"
	envAuthGitHubProviderID           = "ESHU_AUTH_GITHUB_PROVIDER_ID"
	envAuthGitHubStateTTL             = "ESHU_AUTH_GITHUB_STATE_TTL"
	envAuthGitHubSessionRefreshWindow = "ESHU_AUTH_GITHUB_SESSION_REFRESH_WINDOW"
)

type postgresGitHubStoreAdapter struct {
	store *pgstatus.GitHubLoginStore
}

// githubServiceAdapter wraps *githublogin.Service so it satisfies both
// query.GitHubLoginService and query.GitHubProviderLister from a single
// value, mirroring oidcServiceAdapter. No import cycle: githublogin does not
// import cmd/api.
type githubServiceAdapter struct {
	*githublogin.Service
}

// ListGitHubProviderIDs implements query.GitHubProviderLister. No sensitive
// fields (client id, secret, base URL, allowed orgs) are included.
func (a githubServiceAdapter) ListGitHubProviderIDs() []query.GitHubRegisteredProvider {
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

// githubGrantResolverAdapter adapts pgstatus.OIDCLoginStore's group→role
// grant resolution for githublogin.Service. This is deliberately the SAME
// OIDCLoginStore instance the OIDC path uses, not a parallel
// implementation: identity_provider_group_role_mappings has no
// provider_kind column (see go/internal/githublogin/doc.go), so the
// existing OIDC grant-resolution SQL already resolves GitHub team hashes
// correctly keyed on provider_config_id alone.
type githubGrantResolverAdapter struct {
	oidcStore *pgstatus.OIDCLoginStore
	fallback  githublogin.GrantResolver
}

func (r githubGrantResolverAdapter) ResolveGroupGrants(
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

func newGitHubLoginHandler(
	getenv func(string) string,
	db *sql.DB,
	instruments *telemetry.Instruments,
	providerSecretKeyring *secretcrypto.Keyring,
) (*query.GitHubLoginHandler, error) {
	enabled := false
	if rawEnabled := strings.TrimSpace(getenv(envAuthGitHubEnabled)); rawEnabled != "" {
		parsed, err := strconv.ParseBool(rawEnabled)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", envAuthGitHubEnabled, err)
		}
		if !parsed {
			return nil, nil
		}
		enabled = true
	}
	configPath := strings.TrimSpace(getenv(envAuthGitHubConfigFile))
	// Mirrors OIDC's activation precedence (#4966): an env config file is
	// one valid activation path, ESHU_AUTH_GITHUB_ENABLED=true with no
	// config file is the DB-only provider path (admin-registered GitHub
	// providers only).
	if configPath == "" && !enabled {
		return nil, nil
	}
	if db == nil {
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
	if providerID := strings.TrimSpace(getenv(envAuthGitHubProviderID)); providerID != "" {
		config.DefaultProviderID = providerID
	}
	if rawTTL := strings.TrimSpace(getenv(envAuthGitHubStateTTL)); rawTTL != "" {
		ttl, err := time.ParseDuration(rawTTL)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", envAuthGitHubStateTTL, err)
		}
		config.StateTTL = ttl
	}
	sessionRefreshWindow := query.DefaultGitHubSessionRefreshWindow
	if rawWindow := strings.TrimSpace(getenv(envAuthGitHubSessionRefreshWindow)); rawWindow != "" {
		window, err := time.ParseDuration(rawWindow)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", envAuthGitHubSessionRefreshWindow, err)
		}
		if window <= 0 {
			return nil, fmt.Errorf("%s must be positive", envAuthGitHubSessionRefreshWindow)
		}
		sessionRefreshWindow = window
	}
	normalized, err := githublogin.ValidateConfig(config)
	if err != nil {
		return nil, fmt.Errorf("validate github login config: %w", err)
	}
	config = normalized

	store := newPostgresGitHubStoreAdapter(db, instruments)
	oidcStoreForGrants := newPostgresOIDCStoreAdapter(db, instruments).store
	var serviceOptions []githublogin.Option
	if resolver := newGitHubDBProviderResolver(db, providerSecretKeyring); resolver != nil {
		serviceOptions = append(serviceOptions, githublogin.WithDBProviderResolver(resolver))
	}
	service := githublogin.NewService(
		config,
		store,
		githubGrantResolverAdapter{oidcStore: oidcStoreForGrants, fallback: staticResolver},
		githublogin.NewGitHubConnector,
		serviceOptions...,
	)
	cookieSecureMode, err := query.ValidateCookieSecureMode(getenv(query.CookieSecureModeEnv))
	if err != nil {
		return nil, fmt.Errorf("configure cookie secure mode: %w", err)
	}
	return &query.GitHubLoginHandler{
		Service:              githubServiceAdapter{service},
		SessionIssuer:        newBrowserSessionHandler(db, instruments, cookieSecureMode),
		SessionRefreshWindow: sessionRefreshWindow,
	}, nil
}

func newPostgresGitHubStoreAdapter(
	db *sql.DB,
	instruments *telemetry.Instruments,
) *postgresGitHubStoreAdapter {
	githubDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	if instruments != nil {
		githubDB = &pgstatus.InstrumentedDB{
			Inner:       githubDB,
			Tracer:      otel.Tracer(telemetry.DefaultSignalName),
			Instruments: instruments,
			StoreName:   "identity_github_login",
		}
	}
	return &postgresGitHubStoreAdapter{store: pgstatus.NewGitHubLoginStore(githubDB)}
}

func (a *postgresGitHubStoreAdapter) CreateState(ctx context.Context, record githublogin.StateRecord) error {
	return a.store.CreateState(ctx, pgstatus.GitHubLoginStateRecord{
		StateHash:        record.StateHash,
		ProviderConfigID: record.ProviderConfigID,
		ProviderKeyHash:  record.ProviderKeyHash,
		IssuerHash:       record.BaseURLHash,
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

func (a *postgresGitHubStoreAdapter) ConsumeState(
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
