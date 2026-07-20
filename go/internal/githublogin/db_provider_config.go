// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package githublogin

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
)

// dbProviderConfiguration mirrors the query package's private
// githubConfigurationFields shape. This package cannot import
// go/internal/query for it without an import cycle, so the shape is
// duplicated here (matching oidclogin.dbProviderConfiguration's own
// documented duplication for the same reason); both sides are documented to
// keep it in sync.
type dbProviderConfiguration struct {
	ClientID    string   `json:"client_id"`
	BaseURL     string   `json:"base_url,omitempty"`
	APIBaseURL  string   `json:"api_base_url,omitempty"`
	RedirectURL string   `json:"redirect_url,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	AllowedOrgs []string `json:"allowed_orgs"`
}

// dbProviderSecret mirrors the query package's private githubSecretFields
// shape.
type dbProviderSecret struct {
	ClientSecret string `json:"client_secret"` // #nosec G101 -- JSON field name, not a credential
}

// ProviderSecretAAD is oidclogin's shared provider-secret AAD builder, reused
// unchanged: the epic #4962 crypto contract
// ("eshu:provider-secret:v1|<provider_config_id>|<revision_id>") is
// per-envelope, not per-provider-kind, so a GitHub provider's sealed secret
// uses the identical AAD scheme an OIDC or SAML provider's does.
var ProviderSecretAAD = oidclogin.ProviderSecretAAD

// ResolveSealedProviderConfig decrypts a DB-backed GitHub provider's sealed
// secret and combines it with its non-secret configuration to build a usable
// ProviderConfig for the login runtime, mirroring
// oidclogin.ResolveSealedProviderConfig. This is one of this package's two
// (*secretcrypto.Keyring).Open call sites for provider-config secrets — the
// other is TestConnection (provider_connection_test_probe.go). The decrypted
// ClientSecret is held only in the returned ProviderConfig for the duration
// of building one OAuth2 connector; it is never logged, returned to a caller
// outside this package, or persisted.
//
// As with oidclogin's equivalent, this does NOT run the result through
// normalizeProvider — identity_provider_configs is tenant-scoped only, so
// the returned ProviderConfig always has WorkspaceID == "", which the
// caller (cmd/api's githubDBProviderResolver) fills in the same way
// oidcDBProviderResolver does.
func ResolveSealedProviderConfig(
	keyring *secretcrypto.Keyring,
	providerConfigID, revisionID, tenantID string,
	configurationJSON, sealedSecret string,
) (ProviderConfig, error) {
	if keyring == nil {
		return ProviderConfig{}, fmt.Errorf("githublogin: resolve db provider requires a configured keyring")
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	revisionID = strings.TrimSpace(revisionID)
	tenantID = strings.TrimSpace(tenantID)
	if providerConfigID == "" || revisionID == "" || tenantID == "" {
		return ProviderConfig{}, fmt.Errorf("githublogin: resolve db provider requires provider_config_id, revision_id, and tenant_id")
	}

	var cfg dbProviderConfiguration
	if err := json.Unmarshal([]byte(configurationJSON), &cfg); err != nil {
		return ProviderConfig{}, fmt.Errorf("githublogin: decode db provider configuration: %w", err)
	}
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.RedirectURL = strings.TrimSpace(cfg.RedirectURL)
	if cfg.ClientID == "" || cfg.RedirectURL == "" {
		return ProviderConfig{}, fmt.Errorf("githublogin: db provider configuration is missing client_id or redirect_url")
	}
	if len(cleanLowerStrings(cfg.AllowedOrgs)) == 0 {
		return ProviderConfig{}, fmt.Errorf("githublogin: db provider configuration is missing allowed_orgs")
	}

	plaintext, err := keyring.Open(sealedSecret, []byte(ProviderSecretAAD(providerConfigID, revisionID)))
	if err != nil {
		return ProviderConfig{}, fmt.Errorf("githublogin: open db provider secret: %w", err)
	}
	var secret dbProviderSecret
	if err := json.Unmarshal(plaintext, &secret); err != nil {
		return ProviderConfig{}, fmt.Errorf("githublogin: decode db provider secret: %w", err)
	}
	secret.ClientSecret = strings.TrimSpace(secret.ClientSecret)
	if secret.ClientSecret == "" {
		return ProviderConfig{}, fmt.Errorf("githublogin: db provider secret has no client_secret")
	}

	baseURL := defaultString(strings.TrimSpace(cfg.BaseURL), defaultBaseURL)
	// EffectiveAPIBaseURL applies the same defaulting the admin connection
	// tester uses, so the login endpoint and the test-connection probe target
	// the identical host (issue #5166, F-5).
	apiBaseURL := EffectiveAPIBaseURL(cfg.BaseURL, cfg.APIBaseURL)
	scopes := cleanLowerStrings(cfg.Scopes)
	if len(scopes) == 0 {
		scopes = []string{"read:org", "user:email"}
	}

	return ProviderConfig{
		ProviderConfigID: providerConfigID,
		BaseURL:          baseURL,
		APIBaseURL:       apiBaseURL,
		ClientID:         cfg.ClientID,
		ClientSecret:     secret.ClientSecret,
		RedirectURL:      cfg.RedirectURL,
		Scopes:           scopes,
		TenantID:         tenantID,
		WorkspaceID:      "", // see doc comment: DB providers are not workspace-scoped
		AllowedOrgs:      cleanLowerStrings(cfg.AllowedOrgs),
	}, nil
}
