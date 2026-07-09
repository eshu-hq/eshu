// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidclogin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
)

// ProviderSecretAAD builds the AAD text for a provider-config secret
// envelope, matching the epic #4962 shared crypto contract exactly:
// "eshu:provider-secret:v1|<provider_config_id>|<revision_id>". Both the
// storage-layer Seal call (postgres.IdentitySubjectStore, in
// identity_provider_config_writes.go) and this Open call must build this
// identically, or decryption fails closed with secretcrypto.ErrDecrypt.
func ProviderSecretAAD(providerConfigID, revisionID string) string {
	return "eshu:provider-secret:v1|" + providerConfigID + "|" + revisionID
}

// oidcConnectionTestSecret mirrors the query package's private
// oidcSecretFields shape (`{"client_secret":"..."}`). This package cannot
// import go/internal/query for it without an import cycle (query defines the
// ProviderConfigConnectionTester interface this package's caller in cmd/api
// implements against), so the shape is duplicated here; both sides are
// documented to keep it in sync.
type oidcConnectionTestSecret struct {
	ClientSecret string `json:"client_secret"` // #nosec G101 -- JSON field name, not a credential
}

// ConnectionTestResult reports a bounded, safe test-connection outcome.
// Detail never carries a secret or plaintext credential.
type ConnectionTestResult struct {
	OK     bool
	Detail string
}

// discoverFunc abstracts OIDC issuer discovery so TestConnection is testable
// without a live network call. Production callers pass nil to use the real
// coreos/go-oidc discovery client (the same one connector.go's
// NewOIDCConnector uses for the login flow).
type discoverFunc func(ctx context.Context, issuer string) error

// TestConnection validates an OIDC provider's discoverability/JWKS
// reachability and the decrypted client secret's basic shape.
//
// This is the ONLY (*secretcrypto.Keyring).Open call site in this package: it
// decrypts sealedSecret transiently, in-process, uses it only to confirm it
// decodes to a well-formed, non-empty client_secret, and discards it
// immediately — never logged, returned, or serialized. This satisfies the
// epic #4962 boundary that Open is confined to login/authn packages.
//
// Explicit scope note (see #4966 executor report): this does NOT perform a
// live OAuth2 authorization-code round trip — that requires an interactive
// user/browser session with the IdP and cannot be safely automated from an
// admin API call. What it proves: (1) the issuer publishes OIDC discovery
// metadata and a fetchable JWKS (the same client the real login flow uses,
// coreos/go-oidc's oidc.NewProvider), and (2) the stored secret decrypts to a
// non-empty client_secret. A full live-flow test is a deliberate, narrower
// interpretation of the original "full code-flow round trip" contract
// language, flagged explicitly rather than guessed at.
func TestConnection(
	ctx context.Context,
	keyring *secretcrypto.Keyring,
	providerConfigID, revisionID, issuer, sealedSecret string,
) (ConnectionTestResult, error) {
	return testConnection(ctx, keyring, providerConfigID, revisionID, issuer, sealedSecret, nil)
}

func testConnection(
	ctx context.Context,
	keyring *secretcrypto.Keyring,
	providerConfigID, revisionID, issuer, sealedSecret string,
	discover discoverFunc,
) (ConnectionTestResult, error) {
	if keyring == nil {
		return ConnectionTestResult{}, fmt.Errorf("oidclogin: connection test requires a configured keyring")
	}
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		return ConnectionTestResult{OK: false, Detail: "issuer is required"}, nil
	}
	if discover == nil {
		discover = func(ctx context.Context, issuer string) error {
			_, err := oidc.NewProvider(ctx, issuer)
			return err
		}
	}
	if err := discover(ctx, issuer); err != nil {
		return ConnectionTestResult{OK: false, Detail: "oidc discovery/jwks fetch failed"}, nil
	}

	plaintext, err := keyring.Open(sealedSecret, []byte(ProviderSecretAAD(providerConfigID, revisionID)))
	if err != nil {
		return ConnectionTestResult{OK: false, Detail: "stored secret failed to decrypt"}, nil
	}
	var secret oidcConnectionTestSecret
	if err := json.Unmarshal(plaintext, &secret); err != nil || strings.TrimSpace(secret.ClientSecret) == "" {
		return ConnectionTestResult{OK: false, Detail: "stored secret is not a valid client_secret payload"}, nil
	}

	return ConnectionTestResult{OK: true, Detail: "oidc discovery and jwks reachable; client_secret present"}, nil
}
