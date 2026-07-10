// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// testSealedOIDCProviderRow seals a minimal DB-backed OIDC client_secret and
// returns the (configurationJSON, sealedSecret) pair
// GetActiveProviderConfigForLogin's row shape carries, matching
// db_provider_config_test.go's fixture shape.
func testSealedOIDCProviderRow(t *testing.T, providerConfigID, revisionID string) (configJSON, sealed string) {
	t.Helper()
	kr := testProviderSecretKeyring(t)
	sealedSecret, err := kr.Seal([]byte(`{"client_secret":"db-client-secret"}`), []byte(oidclogin.ProviderSecretAAD(providerConfigID, revisionID)))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	configJSON = `{"issuer":"https://idp.example.test","client_id":"db-client-id","redirect_url":"https://eshu.example.test/api/v0/auth/oidc/callback"}`
	return configJSON, sealedSecret
}

// TestOIDCDBProviderResolverResolvesTenantWorkspaceForSingleWorkspaceTenant
// proves the #5040 fix: a DB-backed OIDC provider config (tenant-scoped
// only — ResolveSealedProviderConfig always returns WorkspaceID == "", see
// db_provider_config.go) resolves to the tenant's one active workspace
// instead of leaving WorkspaceID blank. Before the fix, ResolveProvider
// returned the provider with WorkspaceID == "" unconditionally, which made
// login-start's identity_oidc_login_states insert fail its
// workspace_id TEXT NOT NULL constraint (oidc_login_schema.go) and every
// DB-backed provider login return 503.
func TestOIDCDBProviderResolverResolvesTenantWorkspaceForSingleWorkspaceTenant(t *testing.T) {
	t.Parallel()
	kr := testProviderSecretKeyring(t)
	configJSON, sealed := testSealedOIDCProviderRow(t, "pc_oidc_db_1", "rev_1")

	db := &samlIdentityTestDB{queryResponses: []samlIdentityTestRows{
		{rows: [][]any{{"external_oidc", "rev_1", sealed, configJSON}}}, // GetActiveProviderConfigForLogin
		{rows: [][]any{{"workspace_a"}}},                                // PrimaryWorkspaceForTenant
	}}
	resolver := &oidcDBProviderResolver{
		store:      pgstatus.NewIdentitySubjectStore(db),
		workspaces: pgstatus.NewTenantWorkspaceGrantStore(db),
		keyring:    kr,
	}

	provider, found, err := resolver.ResolveProvider(context.Background(), "pc_oidc_db_1", "tenant_a", "")
	if err != nil {
		t.Fatalf("ResolveProvider() error = %v, want the tenant's sole workspace to resolve", err)
	}
	if !found {
		t.Fatal("ResolveProvider() found = false, want true")
	}
	if provider.WorkspaceID != "workspace_a" {
		t.Fatalf("ResolveProvider() WorkspaceID = %q, want workspace_a (resolved from the tenant's sole active workspace)", provider.WorkspaceID)
	}
}

// TestOIDCDBProviderResolverHonorsExplicitWorkspaceWithoutExtraLookup proves
// that when the caller already supplies a workspace_id (e.g. a repeat
// callback resolve using the workspace persisted at login-start), the
// resolver trusts it directly rather than re-querying
// PrimaryWorkspaceForTenant — keeping the same workspace flowing from
// login-start through to callback grant resolution (#5040) with one fewer
// DB round trip.
func TestOIDCDBProviderResolverHonorsExplicitWorkspaceWithoutExtraLookup(t *testing.T) {
	t.Parallel()
	kr := testProviderSecretKeyring(t)
	configJSON, sealed := testSealedOIDCProviderRow(t, "pc_oidc_db_1", "rev_1")

	db := &samlIdentityTestDB{queryResponses: []samlIdentityTestRows{
		{rows: [][]any{{"external_oidc", "rev_1", sealed, configJSON}}}, // GetActiveProviderConfigForLogin only
	}}
	resolver := &oidcDBProviderResolver{
		store:      pgstatus.NewIdentitySubjectStore(db),
		workspaces: pgstatus.NewTenantWorkspaceGrantStore(db),
		keyring:    kr,
	}

	provider, found, err := resolver.ResolveProvider(context.Background(), "pc_oidc_db_1", "tenant_a", "workspace_b")
	if err != nil {
		t.Fatalf("ResolveProvider() error = %v, want the explicit workspace to be honored", err)
	}
	if !found || provider.WorkspaceID != "workspace_b" {
		t.Fatalf("ResolveProvider() = %+v found=%t, want WorkspaceID=workspace_b without a PrimaryWorkspaceForTenant query", provider, found)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1 (no PrimaryWorkspaceForTenant lookup when workspace_id is explicit)", len(db.queries))
	}
}

// TestOIDCDBProviderResolverRejectsAmbiguousTenantWorkspace proves a tenant
// with more than one active workspace fails closed with an
// ErrOIDCLoginInvalidRequest-style error (400, actionable) instead of either
// silently guessing a workspace or surfacing as an opaque 503.
func TestOIDCDBProviderResolverRejectsAmbiguousTenantWorkspace(t *testing.T) {
	t.Parallel()
	kr := testProviderSecretKeyring(t)
	configJSON, sealed := testSealedOIDCProviderRow(t, "pc_oidc_db_1", "rev_1")

	db := &samlIdentityTestDB{queryResponses: []samlIdentityTestRows{
		{rows: [][]any{{"external_oidc", "rev_1", sealed, configJSON}}},
		{rows: [][]any{{"workspace_a"}, {"workspace_b"}}},
	}}
	resolver := &oidcDBProviderResolver{
		store:      pgstatus.NewIdentitySubjectStore(db),
		workspaces: pgstatus.NewTenantWorkspaceGrantStore(db),
		keyring:    kr,
	}

	_, found, err := resolver.ResolveProvider(context.Background(), "pc_oidc_db_1", "tenant_a", "")
	if err == nil {
		t.Fatal("ResolveProvider() error = nil, want a clear error for an ambiguous multi-workspace tenant")
	}
	if !errors.Is(err, query.ErrOIDCLoginInvalidRequest) {
		t.Fatalf("ResolveProvider() error = %v, want errors.Is(err, query.ErrOIDCLoginInvalidRequest)", err)
	}
	if found {
		t.Fatal("ResolveProvider() found = true, want false on workspace resolution failure")
	}
}

// TestNewOIDCDBProviderResolverNilWithoutKeyring proves the constructor still
// returns nil (not a resolver that can only fail) when db or keyring is nil.
func TestNewOIDCDBProviderResolverNilWithoutKeyring(t *testing.T) {
	t.Parallel()
	if got := newOIDCDBProviderResolver(nil, testProviderSecretKeyring(t)); got != nil {
		t.Fatalf("newOIDCDBProviderResolver(nil db, keyring) = %v, want nil", got)
	}
}
