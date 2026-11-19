// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/githublogin"
	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	tenantstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/tenant"
)

// coreSAMLDBProviderResolver resolves DB-backed SAML providers through the
// sealed-secret path. It never calls query.Open itself: the samlauth resolve call
// is the single query.Open site for this path.
type coreSAMLDBProviderResolver struct {
	store   *pgstatus.IdentitySubjectStore
	keyring *secretcrypto.Keyring
}

// newCoreSAMLDBProviderResolver constructs the resolver. It returns nil when
// db or keyring is nil: without a keyring no sealed secret could ever be
// opened, so the store serves env-file providers only.
func newCoreSAMLDBProviderResolver(rawDB *sql.DB, keyring *secretcrypto.Keyring) coreSAMLProviderDBResolver {
	if rawDB == nil || keyring == nil {
		return nil
	}
	return &coreSAMLDBProviderResolver{
		store:   pgstatus.NewIdentitySubjectStore(db.ExecQueryer(pgstatus.SQLDB{DB: rawDB})),
		keyring: keyring,
	}
}

// ResolveProvider resolves a DB-backed SAML provider config.
func (r *coreSAMLDBProviderResolver) ResolveProvider(
	ctx context.Context,
	providerConfigID string,
) (query.SAMLProviderConfig, bool, error) {
	material, found, err := r.store.GetActiveSAMLProviderConfigForLogin(ctx, providerConfigID)
	if err != nil {
		return query.SAMLProviderConfig{}, false, err
	}
	if !found || material.ProviderKind != "external_saml" || material.SealedSecret == "" {
		return query.SAMLProviderConfig{}, false, nil
	}
	provider, err := samlauth.ResolveSealedProviderConfig(
		r.keyring, providerConfigID, material.RevisionID, material.Configuration, material.SealedSecret,
	)
	if err != nil {
		return query.SAMLProviderConfig{}, false, err
	}
	return query.SAMLProviderConfig{
		ProviderConfigID:                 provider.ProviderConfigID,
		ServiceProvider:                  provider.ServiceProvider,
		IdentityProviderMetadataXML:      provider.IdentityProviderMetadataXML,
		ExpectedIdentityProviderEntityID: provider.ExpectedIdentityProviderEntityID,
		GroupMapping:                     provider.GroupMapping,
		ClockSkew:                        provider.ClockSkew,
	}, true, nil
}

// coreOIDCDBProviderResolver resolves DB-backed OIDC providers through the
// sealed-secret path.
type coreOIDCDBProviderResolver struct {
	store      *pgstatus.IdentitySubjectStore
	workspaces *tenantstore.TenantWorkspaceGrantStore
	keyring    *secretcrypto.Keyring
}

// newCoreOIDCDBProviderResolver constructs the resolver. It returns nil when
// db or keyring is nil.
func newCoreOIDCDBProviderResolver(rawDB *sql.DB, keyring *secretcrypto.Keyring) oidclogin.DBProviderResolver {
	if rawDB == nil || keyring == nil {
		return nil
	}
	execQueryer := db.ExecQueryer(pgstatus.SQLDB{DB: rawDB})
	return &coreOIDCDBProviderResolver{
		store:      pgstatus.NewIdentitySubjectStore(execQueryer),
		workspaces: tenantstore.NewTenantWorkspaceGrantStore(execQueryer),
		keyring:    keyring,
	}
}

// ResolveProvider resolves a DB-backed OIDC provider config.
func (r *coreOIDCDBProviderResolver) ResolveProvider(
	ctx context.Context,
	providerConfigID, tenantID, workspaceID string,
) (oidclogin.ProviderConfig, bool, error) {
	material, found, err := r.store.GetActiveProviderConfigForLogin(ctx, providerConfigID, tenantID)
	if err != nil {
		return oidclogin.ProviderConfig{}, false, err
	}
	if !found || material.ProviderKind != "external_oidc" || material.SealedSecret == "" {
		return oidclogin.ProviderConfig{}, false, nil
	}
	provider, err := oidclogin.ResolveSealedProviderConfig(
		r.keyring, providerConfigID, material.RevisionID, tenantID, material.Configuration, material.SealedSecret,
	)
	if err != nil {
		return oidclogin.ProviderConfig{}, false, err
	}
	if provider.WorkspaceID == "" {
		resolvedWorkspaceID, err := r.resolveWorkspace(ctx, tenantID, workspaceID)
		if err != nil {
			return oidclogin.ProviderConfig{}, false, err
		}
		provider.WorkspaceID = resolvedWorkspaceID
	}
	return provider, true, nil
}

// resolveWorkspace trusts an explicit workspaceID as-is, otherwise defaults
// to the tenant's own workspace and fails closed on ambiguity or absence.
func (r *coreOIDCDBProviderResolver) resolveWorkspace(
	ctx context.Context,
	tenantID, workspaceID string,
) (string, error) {
	if requested := strings.TrimSpace(workspaceID); requested != "" {
		return requested, nil
	}
	resolved, err := r.workspaces.PrimaryWorkspaceForTenant(ctx, tenantID)
	if err != nil {
		if errors.Is(err, tenantstore.ErrTenantWorkspaceAmbiguous) || errors.Is(err, tenantstore.ErrTenantWorkspaceNotFound) {
			return "", fmt.Errorf("%w: tenant %q has no unambiguous active workspace for a db-backed oidc provider login; specify workspace_id explicitly: %v",
				query.ErrOIDCLoginInvalidRequest, tenantID, err)
		}
		return "", err
	}
	return resolved, nil
}

// coreGitHubDBProviderResolver resolves DB-backed GitHub providers through
// the sealed-secret path, mirroring the OIDC resolver.
type coreGitHubDBProviderResolver struct {
	store      *pgstatus.IdentitySubjectStore
	workspaces *tenantstore.TenantWorkspaceGrantStore
	keyring    *secretcrypto.Keyring
}

// newCoreGitHubDBProviderResolver constructs the resolver. It returns nil
// when db or keyring is nil.
func newCoreGitHubDBProviderResolver(rawDB *sql.DB, keyring *secretcrypto.Keyring) githublogin.DBProviderResolver {
	if rawDB == nil || keyring == nil {
		return nil
	}
	execQueryer := db.ExecQueryer(pgstatus.SQLDB{DB: rawDB})
	return &coreGitHubDBProviderResolver{
		store:      pgstatus.NewIdentitySubjectStore(execQueryer),
		workspaces: tenantstore.NewTenantWorkspaceGrantStore(execQueryer),
		keyring:    keyring,
	}
}

// ResolveProvider resolves a DB-backed GitHub provider config.
func (r *coreGitHubDBProviderResolver) ResolveProvider(
	ctx context.Context,
	providerConfigID, tenantID, workspaceID string,
) (githublogin.ProviderConfig, bool, error) {
	material, found, err := r.store.GetActiveProviderConfigForLogin(ctx, providerConfigID, tenantID)
	if err != nil {
		return githublogin.ProviderConfig{}, false, err
	}
	if !found || material.ProviderKind != "external_github" || material.SealedSecret == "" {
		return githublogin.ProviderConfig{}, false, nil
	}
	provider, err := githublogin.ResolveSealedProviderConfig(
		r.keyring, providerConfigID, material.RevisionID, tenantID, material.Configuration, material.SealedSecret,
	)
	if err != nil {
		return githublogin.ProviderConfig{}, false, err
	}
	if provider.WorkspaceID == "" {
		resolvedWorkspaceID, err := r.resolveWorkspace(ctx, tenantID, workspaceID)
		if err != nil {
			return githublogin.ProviderConfig{}, false, err
		}
		provider.WorkspaceID = resolvedWorkspaceID
	}
	return provider, true, nil
}

// resolveWorkspace mirrors the OIDC resolver: explicit workspace wins,
// otherwise the tenant default, failing closed on ambiguity or absence.
func (r *coreGitHubDBProviderResolver) resolveWorkspace(
	ctx context.Context,
	tenantID, workspaceID string,
) (string, error) {
	if requested := strings.TrimSpace(workspaceID); requested != "" {
		return requested, nil
	}
	resolved, err := r.workspaces.PrimaryWorkspaceForTenant(ctx, tenantID)
	if err != nil {
		if errors.Is(err, tenantstore.ErrTenantWorkspaceAmbiguous) || errors.Is(err, tenantstore.ErrTenantWorkspaceNotFound) {
			return "", fmt.Errorf("%w: tenant %q has no unambiguous active workspace for a db-backed github provider login; specify workspace_id explicitly: %v",
				query.ErrGitHubLoginInvalidRequest, tenantID, err)
		}
		return "", err
	}
	return resolved, nil
}
