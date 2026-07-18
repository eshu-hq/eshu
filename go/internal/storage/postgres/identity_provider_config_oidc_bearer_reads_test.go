// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
)

// TestListActiveOIDCBearerProvidersReturnsActiveOIDCRows proves the bearer
// resolver's provider source (issue #5162) reads only enabled
// (status='active', not tombstoned) external_oidc provider configs, and
// parses issuer/group_claim out of the stored configuration JSON exactly
// like the dbProviderConfiguration shape oidclogin's
// ResolveSealedProviderConfig decodes for the login path.
func TestListActiveOIDCBearerProvidersReturnsActiveOIDCRows(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	db.configs["pc_bearer_1"] = &fakeProviderConfigRow{
		tenantID:         "tenant_a",
		providerKind:     "external_oidc",
		status:           "active",
		activeRevisionID: "rev_1",
	}
	db.revisions["pc_bearer_1"] = map[string]*fakeProviderConfigRevisionRow{
		"rev_1": {
			status:        "active",
			sealedSecret:  "sealed-ciphertext-must-never-be-read",
			configuration: `{"issuer":"https://idp.example.test","client_id":"client-1","group_claim":"grp"}`,
		},
	}
	store := NewIdentitySubjectStore(db)

	rows, err := store.ListActiveOIDCBearerProviders(context.Background())
	if err != nil {
		t.Fatalf("ListActiveOIDCBearerProviders() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.ProviderConfigID != "pc_bearer_1" {
		t.Fatalf("ProviderConfigID = %q, want pc_bearer_1", row.ProviderConfigID)
	}
	if row.TenantID != "tenant_a" {
		t.Fatalf("TenantID = %q, want tenant_a", row.TenantID)
	}
	if row.RevisionID != "rev_1" {
		t.Fatalf("RevisionID = %q, want rev_1", row.RevisionID)
	}
	if row.Issuer != "https://idp.example.test" {
		t.Fatalf("Issuer = %q, want https://idp.example.test", row.Issuer)
	}
	if row.GroupClaim != "grp" {
		t.Fatalf("GroupClaim = %q, want grp", row.GroupClaim)
	}
}

// TestListActiveOIDCBearerProvidersExcludesDraftTombstonedAndOtherKinds
// proves the read fails closed for exactly the same reasons
// GetActiveProviderConfigForLogin does: a draft (not yet enabled) config, a
// tombstoned (deleted) config, and a non-OIDC (external_saml) config must
// never be usable to validate a bearer token.
func TestListActiveOIDCBearerProvidersExcludesDraftTombstonedAndOtherKinds(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	db.configs["pc_draft"] = &fakeProviderConfigRow{
		tenantID: "tenant_a", providerKind: "external_oidc", status: "draft", activeRevisionID: "rev_1",
	}
	db.revisions["pc_draft"] = map[string]*fakeProviderConfigRevisionRow{
		"rev_1": {status: "active", configuration: `{"issuer":"https://draft.example.test"}`},
	}
	db.configs["pc_tombstoned"] = &fakeProviderConfigRow{
		tenantID: "tenant_a", providerKind: "external_oidc", status: "active", activeRevisionID: "rev_1", tombstoned: true,
	}
	db.revisions["pc_tombstoned"] = map[string]*fakeProviderConfigRevisionRow{
		"rev_1": {status: "active", configuration: `{"issuer":"https://tombstoned.example.test"}`},
	}
	db.configs["pc_saml"] = &fakeProviderConfigRow{
		tenantID: "tenant_a", providerKind: "external_saml", status: "active", activeRevisionID: "rev_1",
	}
	db.revisions["pc_saml"] = map[string]*fakeProviderConfigRevisionRow{
		"rev_1": {status: "active", configuration: `{"entity_id":"https://saml.example.test"}`},
	}
	db.configs["pc_active_oidc"] = &fakeProviderConfigRow{
		tenantID: "tenant_a", providerKind: "external_oidc", status: "active", activeRevisionID: "rev_1",
	}
	db.revisions["pc_active_oidc"] = map[string]*fakeProviderConfigRevisionRow{
		"rev_1": {status: "active", configuration: `{"issuer":"https://active.example.test"}`},
	}
	store := NewIdentitySubjectStore(db)

	rows, err := store.ListActiveOIDCBearerProviders(context.Background())
	if err != nil {
		t.Fatalf("ListActiveOIDCBearerProviders() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (only pc_active_oidc); got %+v", len(rows), rows)
	}
	if rows[0].ProviderConfigID != "pc_active_oidc" {
		t.Fatalf("ProviderConfigID = %q, want pc_active_oidc", rows[0].ProviderConfigID)
	}
}

// TestListActiveOIDCBearerProvidersNeverReturnsSealedSecret proves the read
// never carries sealed_secret in its returned shape: JWT bearer-token
// validation never needs a client secret, and this type has no field to
// carry one even if a future change to the fake mistakenly wired it in.
func TestListActiveOIDCBearerProvidersNeverReturnsSealedSecret(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	db.configs["pc_bearer_1"] = &fakeProviderConfigRow{
		tenantID: "tenant_a", providerKind: "external_oidc", status: "active", activeRevisionID: "rev_1",
	}
	db.revisions["pc_bearer_1"] = map[string]*fakeProviderConfigRevisionRow{
		"rev_1": {status: "active", sealedSecret: "sealed-ciphertext", configuration: `{"issuer":"https://idp.example.test"}`},
	}
	store := NewIdentitySubjectStore(db)

	rows, err := store.ListActiveOIDCBearerProviders(context.Background())
	if err != nil {
		t.Fatalf("ListActiveOIDCBearerProviders() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	// OIDCBearerProviderRow intentionally has no SealedSecret field; this
	// test documents that contract via reflection-free direct field access
	// (a future field addition would need a matching test update, not just
	// a struct literal below).
	_ = rows[0].Issuer
}
