// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidclogin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
)

func testDBProviderKeyring(t *testing.T) *secretcrypto.Keyring {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 7)
	}
	kr, err := secretcrypto.NewKeyring("k1", map[secretcrypto.KeyID][]byte{"k1": key})
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return kr
}

// TestResolveSealedProviderConfigDecryptsSecret proves ResolveSealedProviderConfig
// round-trips a sealed client_secret correctly and builds a usable
// ProviderConfig from the non-secret configuration JSON.
func TestResolveSealedProviderConfigDecryptsSecret(t *testing.T) {
	t.Parallel()
	kr := testDBProviderKeyring(t)
	const plaintext = "correct-horse-redaction-canary"
	sealed, err := kr.Seal([]byte(`{"client_secret":"`+plaintext+`"}`), []byte(ProviderSecretAAD("pc_1", "rev_1")))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	configJSON := `{"issuer":"https://idp.example.test","client_id":"client-1","scopes":["openid","email"],"group_claim":"groups","redirect_url":"https://eshu.example.test/api/v0/auth/oidc/callback"}`

	provider, err := ResolveSealedProviderConfig(kr, "pc_1", "rev_1", "tenant_a", configJSON, sealed)
	if err != nil {
		t.Fatalf("ResolveSealedProviderConfig() error = %v", err)
	}
	if provider.ClientSecret != plaintext {
		t.Fatalf("ClientSecret = %q, want %q", provider.ClientSecret, plaintext)
	}
	if provider.IssuerURL != "https://idp.example.test" || provider.ClientID != "client-1" {
		t.Fatalf("provider = %+v, want issuer/client_id from configuration JSON", provider)
	}
	if provider.RedirectURL != "https://eshu.example.test/api/v0/auth/oidc/callback" {
		t.Fatalf("RedirectURL = %q, want the configured callback", provider.RedirectURL)
	}
	if provider.TenantID != "tenant_a" || provider.WorkspaceID != "" {
		t.Fatalf("provider tenant/workspace = (%q, %q), want (tenant_a, \"\") — DB providers are not workspace-scoped", provider.TenantID, provider.WorkspaceID)
	}
	if provider.GroupsClaim != "groups" {
		t.Fatalf("GroupsClaim = %q, want groups", provider.GroupsClaim)
	}
}

// TestResolveSealedProviderConfigFailsClosedOnAADMismatch proves that
// resolving with the wrong provider_config_id or revision_id (which changes
// the AAD) fails closed rather than returning a wrong or partial secret.
func TestResolveSealedProviderConfigFailsClosedOnAADMismatch(t *testing.T) {
	t.Parallel()
	kr := testDBProviderKeyring(t)
	sealed, err := kr.Seal([]byte(`{"client_secret":"s"}`), []byte(ProviderSecretAAD("pc_1", "rev_1")))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	configJSON := `{"issuer":"https://idp.example.test","client_id":"client-1","redirect_url":"https://eshu.example.test/callback"}`

	if _, err := ResolveSealedProviderConfig(kr, "pc_1", "rev_WRONG", "tenant_a", configJSON, sealed); err == nil {
		t.Fatal("ResolveSealedProviderConfig() with wrong revision_id error = nil, want ErrDecrypt-wrapped failure")
	}
	if _, err := ResolveSealedProviderConfig(kr, "pc_OTHER", "rev_1", "tenant_a", configJSON, sealed); err == nil {
		t.Fatal("ResolveSealedProviderConfig() with wrong provider_config_id error = nil, want ErrDecrypt-wrapped failure")
	}
}

// TestResolveSealedProviderConfigRequiresConfiguration proves missing
// issuer/client_id/redirect_url in the configuration JSON is rejected rather
// than silently building an unusable provider.
func TestResolveSealedProviderConfigRequiresConfiguration(t *testing.T) {
	t.Parallel()
	kr := testDBProviderKeyring(t)
	sealed, err := kr.Seal([]byte(`{"client_secret":"s"}`), []byte(ProviderSecretAAD("pc_1", "rev_1")))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := ResolveSealedProviderConfig(kr, "pc_1", "rev_1", "tenant_a", `{"issuer":"https://idp.example.test"}`, sealed); err == nil {
		t.Fatal("ResolveSealedProviderConfig() with missing client_id/redirect_url error = nil, want error")
	}
}

// fakeDBProviderResolver is a programmable oidclogin.DBProviderResolver for
// Service-level tests.
type fakeDBProviderResolver struct {
	called    bool
	gotID     string
	gotTenant string
	provider  ProviderConfig
	found     bool
	err       error
}

func (f *fakeDBProviderResolver) ResolveProvider(_ context.Context, providerConfigID, tenantID, _ string) (ProviderConfig, bool, error) {
	f.called = true
	f.gotID = providerConfigID
	f.gotTenant = tenantID
	if f.err != nil {
		return ProviderConfig{}, false, f.err
	}
	return f.provider, f.found, nil
}

// TestServiceFallsBackToDBProviderWhenNotInEnvConfig proves Service.provider()
// (exercised via a full StartOIDCLogin + CompleteOIDCLogin round trip) falls
// back to a DBProviderResolver when the requested provider_config_id is not
// in the env-file provider set, and that the resulting login succeeds end to
// end — the #4966 "register→test→enable→working SSO login" contract.
func TestServiceFallsBackToDBProviderWhenNotInEnvConfig(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	dbProvider := ProviderConfig{
		ProviderConfigID: "db-provider-1",
		IssuerURL:        "https://idp.example.test/oauth2/default",
		ClientID:         "db-client-id",
		ClientSecret:     "db-client-secret",
		RedirectURL:      "https://eshu.example.test/api/v0/auth/oidc/callback",
		Scopes:           []string{"openid", "email", "groups"},
		TenantID:         "tenant_a",
		WorkspaceID:      "",
		SubjectClaim:     "sub",
		EmailClaim:       "email",
		GroupsClaim:      "groups",
	}
	resolver := &fakeDBProviderResolver{provider: dbProvider, found: true}
	store := &fakeStateStore{}

	// Config has ZERO env-file providers — proves the #4966 claim that OIDC
	// login can run purely on DB-backed providers, no config file needed.
	service := NewService(Config{StateTTL: 10 * time.Minute}, store, StaticGrantResolver{}, fakeConnectorFactory(t),
		WithNow(func() time.Time { return now }),
		WithSecretGenerator(sequenceOIDCSecrets("state-secret", "nonce-secret")),
		WithDBProviderResolver(resolver))

	start, err := service.StartOIDCLogin(context.Background(), query.OIDCLoginStartRequest{
		ProviderConfigID: "db-provider-1",
		TenantID:         "tenant_a",
		WorkspaceID:      "workspace_a",
	})
	if err != nil {
		t.Fatalf("StartOIDCLogin() error = %v, want the DB-backed provider to resolve", err)
	}
	if !resolver.called || resolver.gotID != "db-provider-1" || resolver.gotTenant != "tenant_a" {
		t.Fatalf("DBProviderResolver not called with expected args: called=%v id=%q tenant=%q", resolver.called, resolver.gotID, resolver.gotTenant)
	}
	if !strings.Contains(start.RedirectURL, "state=state-secret") {
		t.Fatalf("StartOIDCLogin() redirect = %q, want the authorization redirect", start.RedirectURL)
	}
	if len(store.created) != 1 {
		t.Fatalf("created states = %d, want 1", len(store.created))
	}

	// Complete the round trip: the callback must also resolve the DB
	// provider (via record.ProviderConfigID/TenantID persisted at Start).
	store.consume = store.created[0]
	store.consumeOK = true
	connector := &fakeConnector{claims: VerifiedClaims{Subject: "ext-subject", Nonce: "nonce-secret", Groups: []string{"g1"}}}
	service2 := NewService(Config{StateTTL: 10 * time.Minute}, store, StaticGrantResolver{
		GroupRoleMappings: []GroupRoleMapping{{Group: "g1", RoleIDs: []string{"developer"}}},
		RoleGrants: []RoleGrant{{
			RoleID: "developer", PolicyRevisionHash: "sha256:policy",
			AllowedScopeIDs: []string{"scope_a"}, AllowedRepositoryIDs: []string{"repo_a"},
		}},
	}, func(context.Context, ProviderConfig) (Connector, error) { return connector, nil },
		WithNow(func() time.Time { return now }), WithDBProviderResolver(resolver))

	complete, err := service2.CompleteOIDCLogin(context.Background(), query.OIDCLoginCompleteRequest{
		State: "state-secret", Code: "auth-code",
	})
	if err != nil {
		t.Fatalf("CompleteOIDCLogin() error = %v, want the DB-backed provider login to succeed end to end", err)
	}
	if complete.ProviderConfigID != "db-provider-1" || len(complete.Auth.RoleIDs) == 0 {
		t.Fatalf("CompleteOIDCLogin() result = %+v, want a resolved auth context for the DB provider", complete)
	}
}

// TestServicePrefersEnvProviderOverDBResolver proves that when a
// provider_config_id matches BOTH an env-file provider and would also match a
// DBProviderResolver, the env-file provider wins and the DB resolver is never
// consulted — env config is authoritative (see auth_providers.go's
// ListLoginProviders doc comment for the same precedence at the discovery
// list layer, and Service.provider()'s doc comment for this layer).
func TestServicePrefersEnvProviderOverDBResolver(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	resolver := &fakeDBProviderResolver{found: true, provider: ProviderConfig{ProviderConfigID: "shared-id"}}
	store := &fakeStateStore{}
	service := NewService(Config{
		DefaultProviderID: "shared-id",
		Providers: []ProviderConfig{{
			ProviderConfigID: "shared-id",
			IssuerURL:        "https://idp.example.test/oauth2/default",
			ClientID:         "env-client-id",
			RedirectURL:      "https://eshu.example.test/api/v0/auth/oidc/callback",
			TenantID:         "tenant_a",
			WorkspaceID:      "workspace_a",
		}},
	}, store, StaticGrantResolver{}, fakeConnectorFactory(t),
		WithNow(func() time.Time { return now }),
		WithSecretGenerator(sequenceOIDCSecrets("state-secret", "nonce-secret")),
		WithDBProviderResolver(resolver))

	if _, err := service.StartOIDCLogin(context.Background(), query.OIDCLoginStartRequest{
		ProviderConfigID: "shared-id", TenantID: "tenant_a", WorkspaceID: "workspace_a",
	}); err != nil {
		t.Fatalf("StartOIDCLogin() error = %v", err)
	}
	if resolver.called {
		t.Fatal("DBProviderResolver was consulted despite an env-file provider matching the same id — env must win without even calling the DB resolver")
	}
}

// TestServicePropagatesDBResolverInvalidRequestInsteadOf503 proves Service.provider()
// (#5040) surfaces a DBProviderResolver failure that is already an
// ErrOIDCLoginInvalidRequest-style error (e.g. a tenant-scoped DB-backed
// provider whose tenant has no unambiguous workspace to default to) as that
// same 400-mapped error, rather than collapsing every resolver failure into
// the opaque 503 ErrOIDCLoginUnavailable. A resolver failure with no such
// wrapping (a genuine DB/decrypt failure) must still map to 503 — proven by
// the second case below.
func TestServicePropagatesDBResolverInvalidRequestInsteadOf503(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	store := &fakeStateStore{}

	t.Run("invalid request passes through", func(t *testing.T) {
		resolver := &fakeDBProviderResolver{err: query.ErrOIDCLoginInvalidRequest}
		service := NewService(Config{StateTTL: 10 * time.Minute}, store, StaticGrantResolver{}, fakeConnectorFactory(t),
			WithNow(func() time.Time { return now }), WithDBProviderResolver(resolver))

		_, err := service.StartOIDCLogin(context.Background(), query.OIDCLoginStartRequest{
			ProviderConfigID: "db-provider-1", TenantID: "tenant_a",
		})
		if !errors.Is(err, query.ErrOIDCLoginInvalidRequest) {
			t.Fatalf("StartOIDCLogin() error = %v, want errors.Is(err, query.ErrOIDCLoginInvalidRequest)", err)
		}
		if errors.Is(err, query.ErrOIDCLoginUnavailable) {
			t.Fatalf("StartOIDCLogin() error = %v, want it NOT mapped to ErrOIDCLoginUnavailable", err)
		}
	})

	t.Run("other resolver failure still maps to 503", func(t *testing.T) {
		resolver := &fakeDBProviderResolver{err: errors.New("connection refused")}
		service := NewService(Config{StateTTL: 10 * time.Minute}, store, StaticGrantResolver{}, fakeConnectorFactory(t),
			WithNow(func() time.Time { return now }), WithDBProviderResolver(resolver))

		_, err := service.StartOIDCLogin(context.Background(), query.OIDCLoginStartRequest{
			ProviderConfigID: "db-provider-1", TenantID: "tenant_a",
		})
		if !errors.Is(err, query.ErrOIDCLoginUnavailable) {
			t.Fatalf("StartOIDCLogin() error = %v, want errors.Is(err, query.ErrOIDCLoginUnavailable)", err)
		}
	})
}
