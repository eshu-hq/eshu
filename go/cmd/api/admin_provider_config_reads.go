// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// decodeProviderConfiguration parses the stored non-secret configuration JSON
// text into a generic map for the API response. A malformed or empty value
// decodes to nil rather than erroring the whole list/detail read — the
// configuration column is never secret, so a decode failure here is a data
// quality signal for the admin, not a security concern.
func decodeProviderConfiguration(configurationJSON string) map[string]any {
	if configurationJSON == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(configurationJSON), &out); err != nil {
		return nil
	}
	return out
}

// newAdminProviderConfigReadHandler wires the DB-backed provider-config admin
// read endpoints (#4966). The handler is nil-safe: a nil database yields a
// handler whose store is nil, so each route returns 503 rather than
// panicking.
func newAdminProviderConfigReadHandler(
	db *sql.DB,
	oidcLoginHandler *query.OIDCLoginHandler,
	samlHandler *query.SAMLHandler,
) *query.AdminProviderConfigReadHandler {
	handler := &query.AdminProviderConfigReadHandler{}
	if store := newProviderConfigReadAdapter(db, oidcLoginHandler, samlHandler); store != nil {
		handler.Store = store
	}
	return handler
}

// providerConfigReadAdapter implements query.AdminProviderConfigReadStore by
// combining the Postgres-backed CRUD provider configs with the env-file
// -registered OIDC/SAML providers (ESHU_AUTH_OIDC_CONFIG_FILE,
// ESHU_SAML_PROVIDERS_JSON): env/file providers are authoritative, and a
// DB row whose provider_config_id matches a registered env provider is
// returned read-only with ShadowedByEnvironment=true and ManagedBy="environment"
// — its sealed_secret is never consulted for that determination (env config
// wins by construction: this adapter still surfaces the DB row's has_secret
// metadata for admin visibility, but the login/authn path (oidclogin,
// samlauth) never sources the DB row's secret when it is shadowed; see
// wiring.go). A pure env-file-only OIDC provider — one with NO DB row at
// all — is additionally synthesized into ListProviderConfigDetails so it is
// visible to the admin console at all, also with ManagedBy="environment".
//
// SAML env-only providers are NOT synthesized: query.SAMLProviderIDLister
// exposes provider_config_id only, with no tenant attribution (unlike
// OIDCRegisteredProvider, which carries TenantID) — the pre-auth discovery
// list works around this by checking a DB row's tenant via
// HasActiveSAMLProviderConfigForTenant, which by definition does not exist
// for a pure env-only provider. A pure env-only SAML provider therefore
// remains invisible on this admin list until it gains a colliding DB row
// (at which point it surfaces as a normal shadowed entry). Flagged as a
// known gap rather than guessed at — see the #4966 executor report.
type providerConfigReadAdapter struct {
	store            *pgstatus.IdentitySubjectStore
	envProviderIDs   map[string]struct{}
	envOIDCProviders []query.OIDCRegisteredProvider
}

func newProviderConfigReadAdapter(
	db *sql.DB,
	oidcLoginHandler *query.OIDCLoginHandler,
	samlHandler *query.SAMLHandler,
) *providerConfigReadAdapter {
	if db == nil {
		return nil
	}
	var envOIDCProviders []query.OIDCRegisteredProvider
	if oidcLoginHandler != nil {
		envOIDCProviders = oidcLoginHandler.RegisteredProviders()
	}
	return &providerConfigReadAdapter{
		store:            pgstatus.NewIdentitySubjectStore(pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})),
		envProviderIDs:   envRegisteredProviderIDs(oidcLoginHandler, samlHandler),
		envOIDCProviders: envOIDCProviders,
	}
}

// envRegisteredProviderIDs collects every provider_config_id registered via
// env/file config, across OIDC and SAML, for the shadow-detection check. This
// is a global (not tenant-scoped) id set: provider_config_id is already
// globally unique by construction (identity_provider_configs primary key),
// so no tenant filter is needed here — the tenant scoping for what a caller
// may even see happens in the DB query (WHERE tenant_id = $1).
func envRegisteredProviderIDs(oidcLoginHandler *query.OIDCLoginHandler, samlHandler *query.SAMLHandler) map[string]struct{} {
	ids := make(map[string]struct{})
	if oidcLoginHandler != nil {
		for _, p := range oidcLoginHandler.RegisteredProviders() {
			ids[p.ProviderConfigID] = struct{}{}
		}
	}
	if samlHandler != nil {
		for _, id := range samlHandler.RegisteredProviderIDs() {
			ids[id] = struct{}{}
		}
	}
	return ids
}

func (a *providerConfigReadAdapter) GetProviderConfigDetail(
	ctx context.Context,
	providerConfigID, tenantID string,
) (query.AdminProviderConfigDetail, bool, error) {
	detail, found, err := a.store.GetProviderConfigDetail(ctx, providerConfigID, tenantID)
	if err != nil || !found {
		return query.AdminProviderConfigDetail{}, found, err
	}
	return a.toAdminDetail(detail), true, nil
}

func (a *providerConfigReadAdapter) ListProviderConfigDetails(
	ctx context.Context,
	tenantID string,
) ([]query.AdminProviderConfigDetail, error) {
	items, err := a.store.ListProviderConfigs(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]query.AdminProviderConfigDetail, 0, len(items))
	for _, item := range items {
		seen[item.ProviderConfigID] = struct{}{}
		out = append(out, a.toAdminDetail(item))
	}
	// Synthesize entries for pure env-file-only OIDC providers (no DB row at
	// all) so they are visible to the admin console — see the package doc
	// comment on providerConfigReadAdapter for why SAML env-only providers
	// cannot be synthesized the same way.
	for _, p := range a.envOIDCProviders {
		if p.TenantID != tenantID {
			continue
		}
		if _, alreadyListed := seen[p.ProviderConfigID]; alreadyListed {
			continue
		}
		seen[p.ProviderConfigID] = struct{}{}
		out = append(out, query.AdminProviderConfigDetail{
			ProviderConfigID: p.ProviderConfigID,
			ProviderKind:     "oidc",
			Status:           "active",
			ManagedBy:        "environment",
		})
	}
	return out, nil
}

func (a *providerConfigReadAdapter) ListProviderConfigRevisions(
	ctx context.Context,
	providerConfigID, tenantID string,
) ([]query.AdminProviderConfigRevisionItem, error) {
	items, err := a.store.ListProviderConfigRevisions(ctx, providerConfigID, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]query.AdminProviderConfigRevisionItem, 0, len(items))
	for _, item := range items {
		out = append(out, query.AdminProviderConfigRevisionItem{
			RevisionID:   item.RevisionID,
			Status:       item.Status,
			HasSecret:    item.HasSecret,
			CreatedAt:    item.CreatedAt,
			ActivatedAt:  item.ActivatedAt,
			SupersededAt: item.SupersededAt,
		})
	}
	return out, nil
}

func (a *providerConfigReadAdapter) toAdminDetail(detail pgstatus.ProviderConfigDetail) query.AdminProviderConfigDetail {
	_, shadowed := a.envProviderIDs[detail.ProviderConfigID]
	managedBy := "database"
	if shadowed {
		managedBy = "environment"
	}
	return query.AdminProviderConfigDetail{
		ProviderConfigID:      detail.ProviderConfigID,
		ProviderKind:          detail.ProviderKind,
		Status:                detail.Status,
		ActiveRevisionID:      detail.ActiveRevisionID,
		Configuration:         decodeProviderConfiguration(detail.Configuration),
		HasSecret:             detail.HasSecret,
		SecretFingerprint:     detail.SecretFingerprint,
		SecretKeyID:           detail.SecretKeyID,
		ShadowedByEnvironment: shadowed,
		ManagedBy:             managedBy,
		CreatedAt:             detail.CreatedAt,
		UpdatedAt:             detail.UpdatedAt,
	}
}
