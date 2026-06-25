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
//   - active external_oidc rows from identity_provider_configs (DB-backed)
//   - active external_saml rows from identity_provider_configs (DB-backed)
//   - SAML providers registered via ESHU_SAML_PROVIDERS_JSON env config that
//     are also active in identity_provider_configs (to avoid surfacing
//     env-only providers the DB hasn't provisioned)
//
// Only provider_config_id and a safe generic display label are returned.
// No domain, metadata URL, entity ID, client ID, org name, or group name is
// ever included — see query.AuthProviderItem for the allowed surface.
type authProviderListStore struct {
	// identity provides the DB-backed provider listing and activity check.
	identity *pgstatus.IdentitySubjectStore
	// samlProviderIDs is the set of provider_config_ids from the env-config
	// SAML runtime (ESHU_SAML_PROVIDERS_JSON). Only IDs whose DB row is also
	// active are surfaced.
	samlProviderIDs []string
}

// newAuthProviderListStore constructs the store. db may be nil in test-only
// environments without a database; the handler then returns an empty list.
// samlHandler may be nil when SAML is not configured.
func newAuthProviderListStore(
	db *sql.DB,
	samlHandler *query.SAMLHandler,
) *authProviderListStore {
	var samlIDs []string
	if samlHandler != nil {
		samlIDs = samlHandler.RegisteredProviderIDs()
	}
	var identityStore *pgstatus.IdentitySubjectStore
	if db != nil {
		identityStore = pgstatus.NewIdentitySubjectStore(pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db}))
	}
	return &authProviderListStore{
		identity:        identityStore,
		samlProviderIDs: samlIDs,
	}
}

// ListLoginProviders returns the active OIDC and SAML providers in a
// deterministic order: DB-sourced OIDC rows first (sorted by provider_config_id),
// then DB-sourced SAML rows, then env-config-only SAML entries not already
// covered by the DB rows.
func (s *authProviderListStore) ListLoginProviders(ctx context.Context) ([]query.AuthProviderItem, error) {
	if s.identity == nil {
		return []query.AuthProviderItem{}, nil
	}

	// Use the existing admin list — it already enforces tombstoned_at IS NULL
	// and returns only provider_config_id + provider_kind + status. We pass
	// an empty tenant_id because this is a cross-tenant pre-auth surface; the
	// admin store scopes by tenant_id so we call the identity-level helper
	// instead.
	dbItems, err := s.identity.ListActiveLoginProviders(ctx)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(dbItems)+len(s.samlProviderIDs))
	result := make([]query.AuthProviderItem, 0, len(dbItems)+len(s.samlProviderIDs))

	for _, item := range dbItems {
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

	// Add env-config SAML providers not already covered by the DB rows.
	// We check DB-level activity via HasActiveSAMLProviderConfig to avoid
	// surfacing a provider whose DB row has been tombstoned.
	for _, providerID := range s.samlProviderIDs {
		if _, alreadySeen := seen[providerID]; alreadySeen {
			continue
		}
		active, err := s.identity.HasActiveSAMLProviderConfig(ctx, providerID)
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
