// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// authProviderListStore implements query.AuthProviderStore by combining:
//   - active external_oidc and external_saml rows from identity_provider_configs
//     (DB-backed; both kinds have a working login-runtime resolver — see
//     oidcDBProviderResolver and samlDBProviderResolver, #4966/#4978 — but a
//     DB-backed external_saml row is only surfaced when the SAML runtime is
//     actually mounted, see samlRuntimeEnabled below)
//   - SAML providers registered via ESHU_SAML_PROVIDERS_JSON env config that
//     are also active in identity_provider_configs (to avoid surfacing
//     env-only providers the DB hasn't provisioned)
//   - OIDC providers registered via ESHU_AUTH_OIDC_CONFIG_FILE env config that
//     are also active in identity_provider_configs for the requested tenant
//
// Only provider_config_id and a safe generic display label are returned.
// No domain, metadata URL, entity ID, client ID, org name, or group name is
// ever included — see query.AuthProviderItem for the allowed surface.
type authProviderListStore struct {
	// identity provides the DB-backed provider listing and activity check.
	identity *pgstatus.IdentitySubjectStore
	// samlRuntimeEnabled reports whether the SAML login runtime is mounted at
	// all (samlHandler != nil, i.e. ESHU_SAML_PROVIDERS_JSON is set). SAML has
	// no DB-only activation toggle — unlike OIDC's ESHU_AUTH_OIDC_ENABLED — so
	// when this is false, query.APIRouter.Mount never registers
	// /api/v0/auth/saml/providers/{id}/login (see its `if a.SAML != nil`
	// gate). A DB-backed external_saml row must never be surfaced as a login
	// option while this is false: the button would 404 every time.
	samlRuntimeEnabled bool
	// samlProviderIDs is the set of provider_config_ids from the env-config
	// SAML runtime (ESHU_SAML_PROVIDERS_JSON). Only IDs whose DB row is also
	// active are surfaced.
	samlProviderIDs []string
	// oidcProviders is the set of (provider_config_id, tenant_id) pairs from
	// the env-config OIDC runtime (ESHU_AUTH_OIDC_CONFIG_FILE). Only entries
	// whose DB row is active for the matching tenant are surfaced.
	oidcProviders []query.OIDCRegisteredProvider
}

// newAuthProviderListStore constructs the store. db may be nil in test-only
// environments without a database; the handler then returns an empty list.
// samlHandler and oidcHandler may be nil when those providers are not configured.
func newAuthProviderListStore(
	db *sql.DB,
	samlHandler *query.SAMLHandler,
	oidcHandler *query.OIDCLoginHandler,
) *authProviderListStore {
	var samlIDs []string
	if samlHandler != nil {
		samlIDs = samlHandler.RegisteredProviderIDs()
	}
	var oidcProviders []query.OIDCRegisteredProvider
	if oidcHandler != nil {
		oidcProviders = oidcHandler.RegisteredProviders()
	}
	var identityStore *pgstatus.IdentitySubjectStore
	if db != nil {
		identityStore = pgstatus.NewIdentitySubjectStore(pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db}))
	}
	return &authProviderListStore{
		identity:           identityStore,
		samlRuntimeEnabled: samlHandler != nil,
		samlProviderIDs:    samlIDs,
		oidcProviders:      oidcProviders,
	}
}

// ListLoginProviders returns the active OIDC and SAML providers for the
// supplied tenant. Env/file-registered providers are authoritative: when a
// provider_config_id is registered via both ESHU_SAML_PROVIDERS_JSON /
// ESHU_AUTH_OIDC_CONFIG_FILE and a DB row, the env-config entry wins and the
// colliding DB row is excluded from this list (this must agree with the
// admin read surface's shadowed_by_environment derivation — see
// providerConfigReadAdapter.toAdminDetail in admin_provider_config_reads.go,
// which flags the same collision for the same reason: env config is the
// source of truth for login, so the DB row's own secret must never be
// consulted for it here). Order: non-colliding DB-sourced rows (as returned
// by ListActiveLoginProviders), then env-config SAML entries, then env-config
// OIDC entries — each only when their DB row is separately confirmed active
// for the tenant (env config supplies identity/auth material; the DB row
// still gates whether the provider is turned on).
//
// A non-colliding DB-sourced external_saml row is surfaced only when
// s.samlRuntimeEnabled is true (see that field's doc comment): the SAML
// login runtime has a working DB resolver now (#4966, epic #4962; completes
// #4978), but its routes are never mounted at all unless
// ESHU_SAML_PROVIDERS_JSON is set — SAML has no DB-only activation toggle
// (unlike OIDC's ESHU_AUTH_OIDC_ENABLED). Surfacing a DB-only external_saml
// row while the runtime is disabled would produce a login button whose route
// 404s every time. external_oidc rows are unaffected by this gate.
// tenantID must be non-empty; callers must not invoke this method with an empty
// tenantID — the handler returns an empty list in that case without calling here.
func (s *authProviderListStore) ListLoginProviders(ctx context.Context, tenantID string) ([]query.AuthProviderItem, error) {
	if s.identity == nil {
		return []query.AuthProviderItem{}, nil
	}

	dbItems, err := s.identity.ListActiveLoginProviders(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Build the env-registered id set first so DB rows that collide with an
	// env-registered provider are excluded below — env wins, not the DB row.
	envIDs := make(map[string]struct{}, len(s.samlProviderIDs)+len(s.oidcProviders))
	for _, providerID := range s.samlProviderIDs {
		envIDs[providerID] = struct{}{}
	}
	for _, p := range s.oidcProviders {
		if p.TenantID == tenantID {
			envIDs[p.ProviderConfigID] = struct{}{}
		}
	}

	seen := make(map[string]struct{}, len(dbItems)+len(envIDs))
	result := make([]query.AuthProviderItem, 0, len(dbItems)+len(envIDs))

	for _, item := range dbItems {
		if _, envRegistered := envIDs[item.ProviderConfigID]; envRegistered {
			// Env config is authoritative for this id; the DB-sourced entry
			// is intentionally skipped so the env-sourced entry below (once
			// its own active check passes) represents it instead.
			continue
		}
		if item.ProviderKind == "external_saml" && !s.samlRuntimeEnabled {
			// The SAML runtime is not mounted at all (see samlRuntimeEnabled's
			// doc comment) — surfacing this row would offer a login button
			// whose route always 404s.
			continue
		}
		label := displayLabelForKind(item.ProviderKind)
		if label == "" {
			// Not a login-facing provider kind (e.g. "local"); skip.
			continue
		}
		seen[item.ProviderConfigID] = struct{}{}
		result = append(result, query.AuthProviderItem{
			ProviderConfigID: item.ProviderConfigID,
			DisplayLabel:     label,
			ProviderKind:     canonicalKind(item.ProviderKind),
		})
	}

	// Add env-config SAML providers. Use the tenant-scoped check to prevent a
	// SAML provider active for a different tenant from leaking into this
	// tenant's provider list.
	for _, providerID := range s.samlProviderIDs {
		if _, alreadySeen := seen[providerID]; alreadySeen {
			continue
		}
		active, err := s.identity.HasActiveSAMLProviderConfigForTenant(ctx, providerID, tenantID)
		if err != nil {
			// Non-fatal: skip this provider rather than failing the whole list.
			continue
		}
		if !active {
			continue
		}
		seen[providerID] = struct{}{}
		result = append(result, query.AuthProviderItem{
			ProviderConfigID: providerID,
			DisplayLabel:     displayLabelForKind("external_saml"),
			ProviderKind:     "saml",
		})
	}

	// Add env-config OIDC providers. Only include providers whose config-file
	// tenant_id matches the requested tenant to prevent cross-tenant provider
	// enumeration.
	for _, p := range s.oidcProviders {
		if p.TenantID != tenantID {
			continue
		}
		if _, alreadySeen := seen[p.ProviderConfigID]; alreadySeen {
			continue
		}
		active, err := s.identity.HasActiveOIDCProviderConfigForTenant(ctx, p.ProviderConfigID, tenantID)
		if err != nil {
			// Non-fatal: skip this provider rather than failing the whole list.
			continue
		}
		if !active {
			continue
		}
		seen[p.ProviderConfigID] = struct{}{}
		result = append(result, query.AuthProviderItem{
			ProviderConfigID: p.ProviderConfigID,
			DisplayLabel:     displayLabelForKind("external_oidc"),
			ProviderKind:     "oidc",
		})
	}

	return result, nil
}

// displayLabelForKind returns a safe generic label for a provider_kind value.
// NEVER echoes a domain, metadata URL, org name, or IdP identifier.
// Returns "" for unknown or non-login-facing kinds (e.g. "local").
func displayLabelForKind(kind string) string {
	switch kind {
	case "external_oidc", "oidc":
		return "Single sign-on (OIDC)"
	case "external_saml", "saml":
		return "Single sign-on (SAML)"
	default:
		return ""
	}
}

// canonicalKind normalises the DB provider_kind to the short form used in the
// API response ("oidc", "saml").
func canonicalKind(kind string) string {
	switch kind {
	case "external_oidc":
		return "oidc"
	case "external_saml":
		return "saml"
	default:
		return kind
	}
}
