// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
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
type oidcDBProviderResolver struct {
	store   *pgstatus.IdentitySubjectStore
	keyring *secretcrypto.Keyring
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
	return &oidcDBProviderResolver{
		store:   pgstatus.NewIdentitySubjectStore(pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})),
		keyring: keyring,
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
	return provider, true, nil
}
