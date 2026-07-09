// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidclogin

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
)

// dbProviderConfiguration mirrors the query package's private
// oidcConfigurationFields shape (`{"issuer":"...","client_id":"...",...}`).
// This package cannot import go/internal/query for it without an import
// cycle (query's ProviderConfigConnectionTester interface is implemented by
// callers of THIS package), so the shape is duplicated here; both sides are
// documented to keep it in sync.
type dbProviderConfiguration struct {
	Issuer      string   `json:"issuer"`
	ClientID    string   `json:"client_id"`
	Scopes      []string `json:"scopes,omitempty"`
	GroupClaim  string   `json:"group_claim,omitempty"`
	RedirectURL string   `json:"redirect_url,omitempty"`
}

// dbProviderSecret mirrors the query package's private oidcSecretFields
// shape (`{"client_secret":"..."}`).
type dbProviderSecret struct {
	ClientSecret string `json:"client_secret"` // #nosec G101 -- JSON field name, not a credential
}

// ResolveSealedProviderConfig decrypts a DB-backed OIDC provider's sealed
// secret and combines it with its non-secret configuration to build a usable
// ProviderConfig for the login runtime (#4966, epic #4962).
//
// This is one of exactly two (*secretcrypto.Keyring).Open call sites in this
// codebase for provider-config secrets — the other is
// oidclogin.TestConnection (provider_connection_test_probe.go), used by the
// admin test-connection endpoint. Both are confined to this package per the
// epic #4962 boundary (go/internal/query never imports secretcrypto — see
// secretcrypto_open_boundary_test.go). The decrypted ClientSecret is held
// only in the returned ProviderConfig for the duration of building one OAuth2
// connector (NewOIDCConnector); it is never logged, returned to a caller
// outside this package, or persisted.
//
// providerConfigID and revisionID must be exactly the values the caller read
// the sealed_secret envelope for — they reconstruct the AAD Seal bound the
// envelope to (see postgres.providerSecretAAD / identity_provider_config_writes.go);
// a mismatch fails closed with secretcrypto.ErrDecrypt.
//
// This does NOT run the returned ProviderConfig through normalizeProvider:
// that function requires a non-empty WorkspaceID, which env-file providers
// always have but a DB-backed provider never does — identity_provider_configs
// is tenant-scoped only, with no workspace_id column. The resulting
// ProviderConfig always has WorkspaceID == "", which resolveProviderContext
// treats as "not workspace-scoped" (see its doc comment): the actual
// workspace RBAC boundary for a DB-backed provider is
// identity_provider_group_role_mappings, applied later during grant
// resolution, not at provider resolution.
func ResolveSealedProviderConfig(
	keyring *secretcrypto.Keyring,
	providerConfigID, revisionID, tenantID string,
	configurationJSON, sealedSecret string,
) (ProviderConfig, error) {
	if keyring == nil {
		return ProviderConfig{}, fmt.Errorf("oidclogin: resolve db provider requires a configured keyring")
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	revisionID = strings.TrimSpace(revisionID)
	tenantID = strings.TrimSpace(tenantID)
	if providerConfigID == "" || revisionID == "" || tenantID == "" {
		return ProviderConfig{}, fmt.Errorf("oidclogin: resolve db provider requires provider_config_id, revision_id, and tenant_id")
	}

	var cfg dbProviderConfiguration
	if err := json.Unmarshal([]byte(configurationJSON), &cfg); err != nil {
		return ProviderConfig{}, fmt.Errorf("oidclogin: decode db provider configuration: %w", err)
	}
	cfg.Issuer = strings.TrimSpace(cfg.Issuer)
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.RedirectURL = strings.TrimSpace(cfg.RedirectURL)
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.RedirectURL == "" {
		return ProviderConfig{}, fmt.Errorf("oidclogin: db provider configuration is missing issuer, client_id, or redirect_url")
	}

	plaintext, err := keyring.Open(sealedSecret, []byte(ProviderSecretAAD(providerConfigID, revisionID)))
	if err != nil {
		return ProviderConfig{}, fmt.Errorf("oidclogin: open db provider secret: %w", err)
	}
	var secret dbProviderSecret
	if err := json.Unmarshal(plaintext, &secret); err != nil {
		return ProviderConfig{}, fmt.Errorf("oidclogin: decode db provider secret: %w", err)
	}
	secret.ClientSecret = strings.TrimSpace(secret.ClientSecret)
	if secret.ClientSecret == "" {
		return ProviderConfig{}, fmt.Errorf("oidclogin: db provider secret has no client_secret")
	}

	scopes := cleanStrings(cfg.Scopes)
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email", "groups"}
	}
	groupsClaim := defaultString(strings.TrimSpace(cfg.GroupClaim), "groups")

	return ProviderConfig{
		ProviderConfigID: providerConfigID,
		IssuerURL:        cfg.Issuer,
		ClientID:         cfg.ClientID,
		ClientSecret:     secret.ClientSecret,
		RedirectURL:      cfg.RedirectURL,
		Scopes:           scopes,
		TenantID:         tenantID,
		WorkspaceID:      "", // see doc comment: DB providers are not workspace-scoped
		SubjectClaim:     "sub",
		EmailClaim:       "email",
		GroupsClaim:      groupsClaim,
	}, nil
}
