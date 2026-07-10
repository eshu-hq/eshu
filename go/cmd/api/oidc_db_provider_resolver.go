// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// oidcDBProviderResolver implements oidclogin.DBProviderResolver (#4966,
// epic #4962). It reads a DB-backed provider config's sealed_secret
// ciphertext from Postgres (GetActiveProviderConfigForLogin never calls
// Open, and requires status='active' — a draft or disabled provider can
// never authenticate a login) and hands the ciphertext to
// oidclogin.ResolveSealedProviderConfig, which is the actual
// (*secretcrypto.Keyring).Open call site for the login path. This type
// itself never calls Open.
//
// It also resolves the tenant's workspace for a DB-backed provider (#5040):
// ResolveSealedProviderConfig always returns ProviderConfig.WorkspaceID ==
// "" because identity_provider_configs is tenant-scoped only (see that
// function's doc comment). Left blank, that WorkspaceID flows into
// identity_oidc_login_states.workspace_id, a TEXT NOT NULL column with a
// (tenant_id, workspace_id) FK into workspaces — the insert fails and every
// DB-backed provider login-start returned 503. See resolveWorkspace.
type oidcDBProviderResolver struct {
	store      *pgstatus.IdentitySubjectStore
	workspaces *pgstatus.TenantWorkspaceGrantStore
	keyring    *secretcrypto.Keyring
}

// newOIDCDBProviderResolver constructs the resolver. Returns nil when db or
// keyring is nil: without a keyring no sealed secret could ever be opened, so
// wiring a resolver that can only fail is pointless — oidclogin.Service
// simply serves env-file providers only in that case (dbProviders stays
// nil, see WithDBProviderResolver).
func newOIDCDBProviderResolver(db *sql.DB, keyring *secretcrypto.Keyring) oidclogin.DBProviderResolver {
	if db == nil || keyring == nil {
		return nil
	}
	execQueryer := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	return &oidcDBProviderResolver{
		store:      pgstatus.NewIdentitySubjectStore(execQueryer),
		workspaces: pgstatus.NewTenantWorkspaceGrantStore(execQueryer),
		keyring:    keyring,
	}
}

func (r *oidcDBProviderResolver) ResolveProvider(
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

// resolveWorkspace fills in the workspace a tenant-scoped DB-backed
// provider's login must use (#5040). It trusts an explicit workspaceID from
// the caller as-is when present — this keeps the workspace persisted at
// login-start (identity_oidc_login_states.workspace_id) flowing unchanged
// into the callback's grant resolution (GrantQuery.WorkspaceID) without a
// second lookup, and lets a caller that already knows which workspace it
// wants disambiguate a multi-workspace tenant itself. Only when the caller
// supplies no workspace does it default to the tenant's own workspace via
// PrimaryWorkspaceForTenant, which fails closed
// (pgstatus.ErrTenantWorkspaceAmbiguous / ErrTenantWorkspaceNotFound) rather
// than guessing. Ambiguity and absence are mapped to
// query.ErrOIDCLoginInvalidRequest (a 400 the caller can act on — specify a
// workspace_id) instead of the opaque 503 an unmapped error would produce at
// Service.provider()'s call site.
func (r *oidcDBProviderResolver) resolveWorkspace(ctx context.Context, tenantID, workspaceID string) (string, error) {
	if requested := strings.TrimSpace(workspaceID); requested != "" {
		return requested, nil
	}
	resolved, err := r.workspaces.PrimaryWorkspaceForTenant(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgstatus.ErrTenantWorkspaceAmbiguous) || errors.Is(err, pgstatus.ErrTenantWorkspaceNotFound) {
			return "", fmt.Errorf("%w: tenant %q has no unambiguous active workspace for a db-backed oidc provider login; specify workspace_id explicitly: %v",
				query.ErrOIDCLoginInvalidRequest, tenantID, err)
		}
		return "", err
	}
	return resolved, nil
}
