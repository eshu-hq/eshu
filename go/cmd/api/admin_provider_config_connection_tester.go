// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/githublogin"
	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// providerConfigConnectionTester implements query.ProviderConfigConnectionTester.
// It is the orchestration point that reads a provider config's sealed_secret
// ciphertext from Postgres (never plaintext — GetProviderConfigConnectionTestMaterial
// never calls Open) and hands it to oidclogin.TestConnection or
// samlauth.TestConnection. Those are two of this codebase's four
// (*secretcrypto.Keyring).Open call sites for provider-config secrets — the
// other two are the login-path resolvers, oidclogin.ResolveSealedProviderConfig
// and samlauth.ResolveSealedProviderConfig (#4966, epic #4962; completes
// #4978), invoked from cmd/api's oidcDBProviderResolver and
// samlDBProviderResolver respectively, not from this type. This type itself
// never calls Open.
type providerConfigConnectionTester struct {
	store   *pgstatus.IdentitySubjectStore
	keyring *secretcrypto.Keyring
}

func newProviderConfigConnectionTester(db *sql.DB, keyring *secretcrypto.Keyring) query.ProviderConfigConnectionTester {
	if db == nil {
		return nil
	}
	return &providerConfigConnectionTester{
		store:   pgstatus.NewIdentitySubjectStore(pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})),
		keyring: keyring,
	}
}

func (t *providerConfigConnectionTester) TestProviderConnection(
	ctx context.Context,
	providerConfigID, tenantID string,
) (query.AdminProviderConfigConnectionTestResult, error) {
	material, found, err := t.store.GetProviderConfigConnectionTestMaterial(ctx, providerConfigID, tenantID)
	if err != nil {
		return query.AdminProviderConfigConnectionTestResult{}, err
	}
	if !found || material.SealedSecret == "" {
		return query.AdminProviderConfigConnectionTestResult{OK: false, Detail: "provider config has no active secret to test"}, nil
	}
	if t.keyring == nil {
		return query.AdminProviderConfigConnectionTestResult{}, fmt.Errorf("provider connection test: encryption key is not configured")
	}

	switch material.ProviderKind {
	case "external_oidc":
		var cfg struct {
			Issuer string `json:"issuer"`
		}
		_ = json.Unmarshal([]byte(material.Configuration), &cfg)
		result, err := oidclogin.TestConnection(ctx, t.keyring, providerConfigID, material.RevisionID, cfg.Issuer, material.SealedSecret)
		if err != nil {
			return query.AdminProviderConfigConnectionTestResult{}, err
		}
		// RevisionID is always the tested revision, on both pass and fail: the
		// enable path only ever calls EnableProviderConfig after OK is true,
		// but reporting it unconditionally keeps this result self-describing.
		return query.AdminProviderConfigConnectionTestResult{OK: result.OK, Detail: result.Detail, RevisionID: material.RevisionID}, nil
	case "external_saml":
		var cfg struct {
			EntityID    string `json:"entity_id"`
			MetadataURL string `json:"metadata_url"`
			MetadataXML string `json:"metadata_xml"`
		}
		_ = json.Unmarshal([]byte(material.Configuration), &cfg)
		result, err := samlauth.TestConnection(ctx, t.keyring, providerConfigID, material.RevisionID, cfg.EntityID, cfg.MetadataURL, cfg.MetadataXML, material.SealedSecret)
		if err != nil {
			return query.AdminProviderConfigConnectionTestResult{}, err
		}
		return query.AdminProviderConfigConnectionTestResult{OK: result.OK, Detail: result.Detail, RevisionID: material.RevisionID}, nil
	case "external_github":
		var cfg struct {
			APIBaseURL string `json:"api_base_url"`
		}
		_ = json.Unmarshal([]byte(material.Configuration), &cfg)
		result, err := githublogin.TestConnection(ctx, t.keyring, providerConfigID, material.RevisionID, cfg.APIBaseURL, material.SealedSecret)
		if err != nil {
			return query.AdminProviderConfigConnectionTestResult{}, err
		}
		return query.AdminProviderConfigConnectionTestResult{OK: result.OK, Detail: result.Detail, RevisionID: material.RevisionID}, nil
	default:
		return query.AdminProviderConfigConnectionTestResult{OK: false, Detail: "unknown provider kind"}, nil
	}
}
