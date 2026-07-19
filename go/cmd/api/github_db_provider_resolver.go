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
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// githubDBProviderResolver implements githublogin.DBProviderResolver,
// mirroring oidcDBProviderResolver exactly (including the #5040 workspace
// resolution it fixed): identity_provider_configs is tenant-scoped only, so
// a DB-backed GitHub provider has no workspace of its own, and a resolver
// that returns a blank WorkspaceID would fail every login-start the same
// way an unfixed OIDC resolver once did.
type githubDBProviderResolver struct {
	store      *pgstatus.IdentitySubjectStore
	workspaces *pgstatus.TenantWorkspaceGrantStore
	keyring    *secretcrypto.Keyring
}

// newGitHubDBProviderResolver constructs the resolver. Returns nil when db
// or keyring is nil, matching newOIDCDBProviderResolver's convention.
func newGitHubDBProviderResolver(db *sql.DB, keyring *secretcrypto.Keyring) githublogin.DBProviderResolver {
	if db == nil || keyring == nil {
		return nil
	}
	execQueryer := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	return &githubDBProviderResolver{
		store:      pgstatus.NewIdentitySubjectStore(execQueryer),
		workspaces: pgstatus.NewTenantWorkspaceGrantStore(execQueryer),
		keyring:    keyring,
	}
}

func (r *githubDBProviderResolver) ResolveProvider(
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

// resolveWorkspace mirrors oidcDBProviderResolver.resolveWorkspace exactly
// (#5040): trusts an explicit workspaceID as-is, otherwise defaults to the
// tenant's own workspace and fails closed
// (query.ErrGitHubLoginInvalidRequest, a 400) on ambiguity or absence
// rather than guessing.
func (r *githubDBProviderResolver) resolveWorkspace(ctx context.Context, tenantID, workspaceID string) (string, error) {
	if requested := strings.TrimSpace(workspaceID); requested != "" {
		return requested, nil
	}
	resolved, err := r.workspaces.PrimaryWorkspaceForTenant(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgstatus.ErrTenantWorkspaceAmbiguous) || errors.Is(err, pgstatus.ErrTenantWorkspaceNotFound) {
			return "", fmt.Errorf("%w: tenant %q has no unambiguous active workspace for a db-backed github provider login; specify workspace_id explicitly: %v",
				query.ErrGitHubLoginInvalidRequest, tenantID, err)
		}
		return "", err
	}
	return resolved, nil
}
