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

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	envAuthOIDCEnabled              = "ESHU_AUTH_OIDC_ENABLED"
	envAuthOIDCConfigFile           = "ESHU_AUTH_OIDC_CONFIG_FILE"
	envAuthOIDCProviderID           = "ESHU_AUTH_OIDC_PROVIDER_ID"
	envAuthOIDCStateTTL             = "ESHU_AUTH_OIDC_STATE_TTL"
	envAuthOIDCSessionRefreshWindow = "ESHU_AUTH_OIDC_SESSION_REFRESH_WINDOW"
	envAuthOIDCLoginRatePerSec      = "ESHU_AUTH_OIDC_LOGIN_RATE_PER_SEC"
	envAuthOIDCLoginRateBurst       = "ESHU_AUTH_OIDC_LOGIN_RATE_BURST"
	envAuthOIDCLoginUserRatePerMin  = "ESHU_AUTH_OIDC_LOGIN_USER_RATE_PER_MIN"
	envAuthOIDCLoginUserBurst       = "ESHU_AUTH_OIDC_LOGIN_USER_BURST"
)

type postgresOIDCStoreAdapter struct {
	store *pgstatus.OIDCLoginStore
}

// oidcServiceAdapter wraps *oidclogin.Service so it satisfies both
// query.OIDCLoginService and query.OIDCProviderLister from a single value.
// This avoids any import cycle: oidclogin does not import query.
type oidcServiceAdapter struct {
	*oidclogin.Service
}

// ListOIDCProviderIDs implements query.OIDCProviderLister. It returns the
// (ProviderConfigID, TenantID) pairs for every OIDC provider registered in the
// config file. No sensitive fields (issuer URL, client ID, scopes, claims) are
// included. The caller deduplicates against DB rows before surfacing to clients.
func (a oidcServiceAdapter) ListOIDCProviderIDs() []query.OIDCRegisteredProvider {
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

type fallbackOIDCGrantResolver struct {
	primary  oidclogin.GrantResolver
	fallback oidclogin.GrantResolver
}

func newOIDCLoginHandler(
	getenv func(string) string,
	db *sql.DB,
	instruments *telemetry.Instruments,
) (*query.OIDCLoginHandler, error) {
	enabled := false
	if rawEnabled := strings.TrimSpace(getenv(envAuthOIDCEnabled)); rawEnabled != "" {
		parsed, err := strconv.ParseBool(rawEnabled)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", envAuthOIDCEnabled, err)
		}
		if !parsed {
			return nil, nil
		}
		enabled = true
	}
	configPath := strings.TrimSpace(getenv(envAuthOIDCConfigFile))
	if configPath == "" {
		if enabled {
			return nil, fmt.Errorf("%s is required when %s=true", envAuthOIDCConfigFile, envAuthOIDCEnabled)
		}
		return nil, nil
	}
	if db == nil {
		return nil, fmt.Errorf("postgres is required for oidc login")
	}

	config, staticResolver, err := oidclogin.LoadConfigFile(configPath)
	if err != nil {
		return nil, err
	}
	if providerID := strings.TrimSpace(getenv(envAuthOIDCProviderID)); providerID != "" {
		config.DefaultProviderID = providerID
	}
	if rawTTL := strings.TrimSpace(getenv(envAuthOIDCStateTTL)); rawTTL != "" {
		ttl, err := time.ParseDuration(rawTTL)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", envAuthOIDCStateTTL, err)
		}
		config.StateTTL = ttl
	}
	sessionRefreshWindow := query.DefaultOIDCSessionRefreshWindow
	if rawWindow := strings.TrimSpace(getenv(envAuthOIDCSessionRefreshWindow)); rawWindow != "" {
		window, err := time.ParseDuration(rawWindow)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", envAuthOIDCSessionRefreshWindow, err)
		}
		if window <= 0 {
			return nil, fmt.Errorf("%s must be positive", envAuthOIDCSessionRefreshWindow)
		}
		sessionRefreshWindow = window
	}
	normalized, err := oidclogin.ValidateConfig(config)
	if err != nil {
		return nil, fmt.Errorf("validate oidc login config: %w", err)
	}
	config = normalized

	store := newPostgresOIDCStoreAdapter(db, instruments)
	service := oidclogin.NewService(
		config,
		store,
		fallbackOIDCGrantResolver{primary: store, fallback: staticResolver},
		oidclogin.NewOIDCConnector,
	)
	return &query.OIDCLoginHandler{
		Service:              oidcServiceAdapter{service},
		SessionIssuer:        newBrowserSessionHandler(db, instruments),
		SessionRefreshWindow: sessionRefreshWindow,
	}, nil
}

func newPostgresOIDCStoreAdapter(
	db *sql.DB,
	instruments *telemetry.Instruments,
) *postgresOIDCStoreAdapter {
	oidcDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	if instruments != nil {
		oidcDB = &pgstatus.InstrumentedDB{
			Inner:       oidcDB,
			Tracer:      otel.Tracer("eshu-api"),
			Instruments: instruments,
			StoreName:   "identity_oidc_login",
		}
	}
	return &postgresOIDCStoreAdapter{store: pgstatus.NewOIDCLoginStore(oidcDB)}
}

func (a *postgresOIDCStoreAdapter) CreateState(ctx context.Context, record oidclogin.StateRecord) error {
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

func (a *postgresOIDCStoreAdapter) ConsumeState(
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

func (a *postgresOIDCStoreAdapter) ResolveGroupGrants(
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
		RoleIDs:            append([]string(nil), resolution.RoleIDs...),
		PolicyRevisionHash: resolution.PolicyRevisionHash,
		// Database-resolved OIDC roles are always scoped (non-admin) and carry a
		// real catalog snapshot, so the issued session enforces the catalog
		// identically to a scoped token for the same roles.
		PermissionCatalogEnforced:    true,
		AllowedScopeIDs:              append([]string(nil), resolution.AllowedScopeIDs...),
		AllowedRepositoryIDs:         append([]string(nil), resolution.AllowedRepositoryIDs...),
		AllowedPermissionFeatures:    append([]string(nil), resolution.AllowedPermissionFeatures...),
		AllowedPermissionDataClasses: append([]string(nil), resolution.AllowedPermissionDataClasses...),
	}, true, nil
}

func (r fallbackOIDCGrantResolver) ResolveGroupGrants(
	ctx context.Context,
	query oidclogin.GrantQuery,
) (oidclogin.GrantResolution, bool, error) {
	if r.primary != nil {
		resolution, ok, err := r.primary.ResolveGroupGrants(ctx, query)
		if err != nil || ok {
			return resolution, ok, err
		}
	}
	if r.fallback == nil {
		return oidclogin.GrantResolution{}, false, nil
	}
	return r.fallback.ResolveGroupGrants(ctx, query)
}

// newOIDCRateLimiter creates an OIDC login rate limiter from env vars. Returns
// nil when OIDC is disabled or rate limiting is configured at zero.
func newOIDCRateLimiter(getenv func(string) string, instruments *telemetry.Instruments) *query.OIDCRateLimiter {
	enabled := false
	if raw := strings.TrimSpace(getenv(envAuthOIDCEnabled)); raw != "" {
		enabled, _ = strconv.ParseBool(raw)
	}
	if !enabled {
		return nil
	}
	ipRate := query.DefaultOIDCLoginRatePerSec
	if v := strings.TrimSpace(getenv(envAuthOIDCLoginRatePerSec)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ipRate = n
		}
	}
	ipBurst := query.DefaultOIDCLoginBurst
	if v := strings.TrimSpace(getenv(envAuthOIDCLoginRateBurst)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ipBurst = n
		}
	}
	userRate := query.DefaultOIDCLoginUserRatePerMin
	if v := strings.TrimSpace(getenv(envAuthOIDCLoginUserRatePerMin)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			userRate = n
		}
	}
	userBurst := query.DefaultOIDCLoginUserBurst
	if v := strings.TrimSpace(getenv(envAuthOIDCLoginUserBurst)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			userBurst = n
		}
	}
	if ipRate <= 0 && userRate <= 0 {
		return nil
	}
	return query.NewOIDCRateLimiter(float64(ipRate), ipBurst, float64(userRate), userBurst, instruments)
}
