// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// coreAuthProviderListStore implements query.AuthProviderStore by combining
// DB-backed provider rows with env-file-registered OIDC/SAML/GitHub
// providers. Only provider_config_id and a safe display label are ever
// returned: no domain, metadata URL, entity ID, or group name.
type coreAuthProviderListStore struct {
	identity *pgstatus.IdentitySubjectStore
	// samlRuntimeEnabled reports whether the SAML login runtime is mounted.
	// A DB-backed external_saml row is never surfaced while this is false:
	// the button would 404 every time.
	samlRuntimeEnabled bool
	samlProviderIDs    []string
	// oidcProviders and githubProviders are the env-config (id, tenant)
	// pairs, surfaced only when their DB row is active for the tenant.
	oidcProviders   []query.OIDCRegisteredProvider
	githubProviders []query.GitHubRegisteredProvider
	// githubRuntimeEnabled mirrors samlRuntimeEnabled for GitHub.
	githubRuntimeEnabled bool
}

// newCoreAuthProviderListStore constructs the store. db may be nil; the
// handler then returns an empty list. query.Handler arguments may be nil when
// those runtimes are not configured.
func newCoreAuthProviderListStore(
	rawDB *sql.DB,
	samlHandler *query.SAMLHandler,
	oidcHandler *query.OIDCLoginHandler,
	githubHandler *query.GitHubLoginHandler,
) *coreAuthProviderListStore {
	var samlIDs []string
	if samlHandler != nil {
		samlIDs = samlHandler.RegisteredProviderIDs()
	}
	var oidcProviders []query.OIDCRegisteredProvider
	if oidcHandler != nil {
		oidcProviders = oidcHandler.RegisteredProviders()
	}
	var githubProviders []query.GitHubRegisteredProvider
	if githubHandler != nil {
		githubProviders = githubHandler.RegisteredProviders()
	}
	var identityStore *pgstatus.IdentitySubjectStore
	if rawDB != nil {
		identityStore = pgstatus.NewIdentitySubjectStore(db.ExecQueryer(pgstatus.SQLDB{DB: rawDB}))
	}
	return &coreAuthProviderListStore{
		identity:             identityStore,
		samlRuntimeEnabled:   samlHandler != nil,
		samlProviderIDs:      samlIDs,
		oidcProviders:        oidcProviders,
		githubProviders:      githubProviders,
		githubRuntimeEnabled: githubHandler != nil,
	}
}

// ListLoginProviders returns the active login providers for the tenant.
// Env/file-registered providers are authoritative: a DB row colliding with
// an env-registered id is excluded (env wins), and pure env-only providers
// are synthesized only when their DB row is separately confirmed active.
func (s *coreAuthProviderListStore) ListLoginProviders(
	ctx context.Context,
	tenantID string,
) ([]query.AuthProviderItem, error) {
	if s.identity == nil {
		return []query.AuthProviderItem{}, nil
	}
	dbItems, err := s.identity.ListActiveLoginProviders(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	envIDs := make(map[string]struct{}, len(s.samlProviderIDs)+len(s.oidcProviders)+len(s.githubProviders))
	for _, providerID := range s.samlProviderIDs {
		envIDs[providerID] = struct{}{}
	}
	for _, p := range s.oidcProviders {
		if p.TenantID == tenantID {
			envIDs[p.ProviderConfigID] = struct{}{}
		}
	}
	for _, p := range s.githubProviders {
		if p.TenantID == tenantID {
			envIDs[p.ProviderConfigID] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(dbItems)+len(envIDs))
	result := make([]query.AuthProviderItem, 0, len(dbItems)+len(envIDs))
	for _, item := range dbItems {
		if _, envRegistered := envIDs[item.ProviderConfigID]; envRegistered {
			continue
		}
		if item.ProviderKind == "external_saml" && !s.samlRuntimeEnabled {
			continue
		}
		if item.ProviderKind == "external_github" && !s.githubRuntimeEnabled {
			continue
		}
		label := displayLabelForCoreProviderKind(item.ProviderKind)
		if label == "" {
			continue
		}
		seen[item.ProviderConfigID] = struct{}{}
		result = append(result, query.AuthProviderItem{
			ProviderConfigID: item.ProviderConfigID,
			DisplayLabel:     label,
			ProviderKind:     canonicalCoreProviderKind(item.ProviderKind),
			IconHint:         iconHintForCoreProviderKind(item.ProviderKind),
		})
	}
	for _, providerID := range s.samlProviderIDs {
		if _, alreadySeen := seen[providerID]; alreadySeen {
			continue
		}
		active, err := s.identity.HasActiveSAMLProviderConfigForTenant(ctx, providerID, tenantID)
		if err != nil || !active {
			continue
		}
		seen[providerID] = struct{}{}
		result = append(result, query.AuthProviderItem{
			ProviderConfigID: providerID,
			DisplayLabel:     displayLabelForCoreProviderKind("external_saml"),
			ProviderKind:     "saml",
			IconHint:         iconHintForCoreProviderKind("external_saml"),
		})
	}
	for _, p := range s.oidcProviders {
		if p.TenantID != tenantID {
			continue
		}
		if _, alreadySeen := seen[p.ProviderConfigID]; alreadySeen {
			continue
		}
		active, err := s.identity.HasActiveOIDCProviderConfigForTenant(ctx, p.ProviderConfigID, tenantID)
		if err != nil || !active {
			continue
		}
		seen[p.ProviderConfigID] = struct{}{}
		result = append(result, query.AuthProviderItem{
			ProviderConfigID: p.ProviderConfigID,
			DisplayLabel:     displayLabelForCoreProviderKind("external_oidc"),
			ProviderKind:     "oidc",
			IconHint:         iconHintForCoreProviderKind("external_oidc"),
		})
	}
	for _, p := range s.githubProviders {
		if p.TenantID != tenantID {
			continue
		}
		if _, alreadySeen := seen[p.ProviderConfigID]; alreadySeen {
			continue
		}
		active, err := s.identity.HasActiveGitHubProviderConfigForTenant(ctx, p.ProviderConfigID, tenantID)
		if err != nil || !active {
			continue
		}
		seen[p.ProviderConfigID] = struct{}{}
		result = append(result, query.AuthProviderItem{
			ProviderConfigID: p.ProviderConfigID,
			DisplayLabel:     displayLabelForCoreProviderKind("external_github"),
			ProviderKind:     "github",
			IconHint:         iconHintForCoreProviderKind("external_github"),
		})
	}
	return result, nil
}

// displayLabelForCoreProviderKind returns a safe generic label. It never
// echoes a domain, metadata URL, org name, or IdP identifier.
func displayLabelForCoreProviderKind(kind string) string {
	switch kind {
	case "external_oidc", "oidc":
		return "Single sign-on (OIDC)"
	case "external_saml", "saml":
		return "Single sign-on (SAML)"
	case "external_github", "github":
		return "GitHub"
	default:
		return ""
	}
}

// canonicalCoreProviderKind normalises the DB provider_kind to the short API
// form.
func canonicalCoreProviderKind(kind string) string {
	switch kind {
	case "external_oidc":
		return "oidc"
	case "external_saml":
		return "saml"
	case "external_github":
		return "github"
	default:
		return kind
	}
}

// iconHintForCoreProviderKind returns a generic protocol-class glyph key.
func iconHintForCoreProviderKind(kind string) string {
	switch kind {
	case "external_oidc", "oidc":
		return "oidc"
	case "external_saml", "saml":
		return "saml"
	case "external_github", "github":
		return "github"
	default:
		return ""
	}
}
