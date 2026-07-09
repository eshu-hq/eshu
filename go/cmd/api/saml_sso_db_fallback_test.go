// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fakeSAMLDBProviderResolver is a programmable samlProviderDBResolver for
// postgresSAMLStore-level tests (mirrors oidclogin's fakeDBProviderResolver).
type fakeSAMLDBProviderResolver struct {
	called   bool
	gotID    string
	provider query.SAMLProviderConfig
	found    bool
	err      error
}

func (f *fakeSAMLDBProviderResolver) ResolveProvider(_ context.Context, providerConfigID string) (query.SAMLProviderConfig, bool, error) {
	f.called = true
	f.gotID = providerConfigID
	if f.err != nil {
		return query.SAMLProviderConfig{}, false, f.err
	}
	return f.provider, f.found, nil
}

// TestGetSAMLProviderFallsBackToDBProviderWhenNotInEnvConfig proves
// GetSAMLProvider falls back to dbProviders (#4966, epic #4962; completes
// #4978) when the requested provider_config_id is not in the env-file
// provider set.
func TestGetSAMLProviderFallsBackToDBProviderWhenNotInEnvConfig(t *testing.T) {
	t.Parallel()

	dbProvider := query.SAMLProviderConfig{
		ProviderConfigID: "db-provider-1",
		ServiceProvider: samlauth.ServiceProviderConfig{
			EntityID: "https://eshu.example.test/api/v0/auth/saml/providers/db-provider-1/metadata",
			ACSURL:   "https://eshu.example.test/api/v0/auth/saml/providers/db-provider-1/acs",
		},
	}
	resolver := &fakeSAMLDBProviderResolver{provider: dbProvider, found: true}
	store := &postgresSAMLStore{dbProviders: resolver}

	cfg, ok, err := store.GetSAMLProvider(context.Background(), "db-provider-1")
	if err != nil {
		t.Fatalf("GetSAMLProvider() error = %v, want the DB-backed provider to resolve", err)
	}
	if !ok || cfg.ProviderConfigID != "db-provider-1" {
		t.Fatalf("GetSAMLProvider() = %+v, ok = %t, want the DB-backed provider", cfg, ok)
	}
	if !resolver.called || resolver.gotID != "db-provider-1" {
		t.Fatalf("samlProviderDBResolver not called with expected id: called=%v id=%q", resolver.called, resolver.gotID)
	}
}

// TestGetSAMLProviderPrefersEnvProviderOverDBResolver proves that when a
// provider_config_id matches BOTH an env-file provider and would also match a
// samlProviderDBResolver, the env-file provider wins and the DB resolver is
// never consulted — env config is authoritative, matching OIDC's
// Service.provider() precedence and auth_providers.go's ListLoginProviders.
func TestGetSAMLProviderPrefersEnvProviderOverDBResolver(t *testing.T) {
	t.Parallel()

	providers, err := loadSAMLProviderConfigs(samlTestGetenv())
	if err != nil {
		t.Fatalf("loadSAMLProviderConfigs() error = %v", err)
	}
	db := &samlIdentityTestDB{queryResponses: []samlIdentityTestRows{{
		rows: [][]any{{"provider_a"}},
	}}}
	resolver := &fakeSAMLDBProviderResolver{found: true, provider: query.SAMLProviderConfig{ProviderConfigID: "provider_a"}}
	store := &postgresSAMLStore{
		identity:    pgstatus.NewIdentitySubjectStore(db),
		providers:   providers,
		dbProviders: resolver,
	}

	cfg, ok, err := store.GetSAMLProvider(context.Background(), "provider_a")
	if err != nil {
		t.Fatalf("GetSAMLProvider() error = %v", err)
	}
	if !ok || cfg.ProviderConfigID != "provider_a" || string(cfg.IdentityProviderMetadataXML) != samlTestMetadataXML {
		t.Fatalf("GetSAMLProvider() = %+v, ok = %t, want the env-file provider (with its metadata XML)", cfg, ok)
	}
	if resolver.called {
		t.Fatal("samlProviderDBResolver was consulted despite an env-file provider matching the same id — env must win without even calling the DB resolver")
	}
}

// TestResolveSAMLPrincipalFallsBackToDBProviderExistenceCheck proves
// ResolveSAMLPrincipal resolves a DB-only provider (not in s.providers) via
// the lightweight HasActiveSAMLProviderConfig existence/active check —
// closing the gap where a DB-backed provider could pass GetSAMLProvider /
// assertion verification earlier in the ACS flow, then fail principal
// resolution because it was never registered in the env map.
func TestResolveSAMLPrincipalFallsBackToDBProviderExistenceCheck(t *testing.T) {
	t.Parallel()

	db := &samlIdentityTestDB{queryResponses: []samlIdentityTestRows{
		{rows: [][]any{{"db-provider-1"}}}, // HasActiveSAMLProviderConfig
		{rows: [][]any{{ // resolveActiveSAMLExternalSubject
			"tenant_durable", "workspace_durable", "sha256:user-subject", "sha256:policy-durable", "user_durable", true,
		}}},
	}}
	resolver := &fakeSAMLDBProviderResolver{found: true}
	store := &postgresSAMLStore{
		identity:    pgstatus.NewIdentitySubjectStore(db),
		dbProviders: resolver,
	}

	auth, ok, err := store.ResolveSAMLPrincipal(context.Background(), "db-provider-1", samlauth.Principal{
		ExternalSubjectHash: "sha256:external-subject",
		GroupClaimHash:      "sha256:groups-current",
	}, time.Date(2026, 6, 22, 19, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ResolveSAMLPrincipal() error = %v", err)
	}
	if !ok || auth.TenantID != "tenant_durable" {
		t.Fatalf("ResolveSAMLPrincipal() auth = %+v ok = %t, want durable resolution for the DB-backed provider", auth, ok)
	}
	if resolver.called {
		t.Fatal("samlProviderDBResolver.ResolveProvider was called — ResolveSAMLPrincipal must use the lightweight active check, not open the sealed secret")
	}
	if got := len(db.queries); got != 2 {
		t.Fatalf("durable identity query count = %d, want 2 (active check + resolution)", got)
	}
}

// TestResolveSAMLPrincipalDeniesUnknownDBProvider proves an unregistered
// provider_config_id (not in env map, and inactive/absent in the DB) is
// denied without ever calling ResolveSAMLExternalSubject.
func TestResolveSAMLPrincipalDeniesUnknownDBProvider(t *testing.T) {
	t.Parallel()

	db := &samlIdentityTestDB{queryResponses: []samlIdentityTestRows{{}}} // HasActiveSAMLProviderConfig: no rows
	resolver := &fakeSAMLDBProviderResolver{found: true}
	store := &postgresSAMLStore{
		identity:    pgstatus.NewIdentitySubjectStore(db),
		dbProviders: resolver,
	}

	auth, ok, err := store.ResolveSAMLPrincipal(context.Background(), "unknown-provider", samlauth.Principal{
		ExternalSubjectHash: "sha256:external-subject",
		GroupClaimHash:      "sha256:groups-current",
	}, time.Date(2026, 6, 22, 19, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ResolveSAMLPrincipal() error = %v", err)
	}
	if ok || auth.TenantID != "" {
		t.Fatalf("ResolveSAMLPrincipal() auth = %+v ok = %t, want denial for an inactive/unknown provider", auth, ok)
	}
	if got := len(db.queries); got != 1 {
		t.Fatalf("durable identity query count = %d, want 1 (active check only)", got)
	}
}
