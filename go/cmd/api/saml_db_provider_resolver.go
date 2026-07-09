// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// samlProviderDBResolver resolves a DB-backed SAML provider config
// (#4966, epic #4962; completes #4978) into the query-layer view the SAML
// login runtime needs. It is a narrow interface — not the concrete
// samlDBProviderResolver type — so postgresSAMLStore's unit tests can
// substitute a fake instead of standing up real Postgres, mirroring how
// oidclogin.DBProviderResolver is a package-level interface for the same
// reason.
type samlProviderDBResolver interface {
	ResolveProvider(ctx context.Context, providerConfigID string) (query.SAMLProviderConfig, bool, error)
}

// samlDBProviderResolver implements samlProviderDBResolver. It reads a
// DB-backed provider config's sealed_secret ciphertext from Postgres
// (GetActiveSAMLProviderConfigForLogin never calls Open, and requires
// status='active' — a draft or disabled provider can never authenticate a
// login) and hands the ciphertext to samlauth.ResolveSealedProviderConfig,
// which is the actual (*secretcrypto.Keyring).Open call site for the SAML
// login path — mirrors oidcDBProviderResolver exactly. This type itself
// never calls Open.
type samlDBProviderResolver struct {
	store   *pgstatus.IdentitySubjectStore
	keyring *secretcrypto.Keyring
}

// newSAMLDBProviderResolver constructs the resolver. Returns nil when db or
// keyring is nil: without a keyring no sealed secret could ever be opened, so
// wiring a resolver that can only fail is pointless — postgresSAMLStore
// simply serves env-file providers only in that case (dbProviders stays nil).
func newSAMLDBProviderResolver(db *sql.DB, keyring *secretcrypto.Keyring) samlProviderDBResolver {
	if db == nil || keyring == nil {
		return nil
	}
	return &samlDBProviderResolver{
		store:   pgstatus.NewIdentitySubjectStore(pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})),
		keyring: keyring,
	}
}

func (r *samlDBProviderResolver) ResolveProvider(
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
