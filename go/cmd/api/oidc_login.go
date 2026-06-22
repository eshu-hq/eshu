package main

import (
	"context"
	"database/sql"
	"fmt"
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
)

type postgresOIDCStoreAdapter struct {
	store *pgstatus.OIDCLoginStore
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
	enabled := strings.EqualFold(strings.TrimSpace(getenv(envAuthOIDCEnabled)), "true")
	if strings.EqualFold(strings.TrimSpace(getenv(envAuthOIDCEnabled)), "false") {
		return nil, nil
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
		Service:              service,
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
		RoleIDs:              append([]string(nil), resolution.RoleIDs...),
		PolicyRevisionHash:   resolution.PolicyRevisionHash,
		AllowedScopeIDs:      append([]string(nil), resolution.AllowedScopeIDs...),
		AllowedRepositoryIDs: append([]string(nil), resolution.AllowedRepositoryIDs...),
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
