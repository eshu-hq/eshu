// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
)

// TestGetActiveSAMLProviderConfigForLoginReturnsActiveExternalSAMLRow proves
// the login-facing read returns the active revision's sealed_secret and
// configuration for an enabled (status='active') external_saml provider,
// scoped by kind exactly like GetActiveProviderConfigForLogin is scoped for
// OIDC — but WITHOUT a tenant_id argument, since SAML login routes carry none
// (#4966, epic #4962; completes #4978).
func TestGetActiveSAMLProviderConfigForLoginReturnsActiveExternalSAMLRow(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	db.configs["pc_saml_1"] = &fakeProviderConfigRow{
		tenantID:         "tenant_a",
		providerKind:     "external_saml",
		status:           "active",
		activeRevisionID: "rev_1",
	}
	db.revisions["pc_saml_1"] = map[string]*fakeProviderConfigRevisionRow{
		"rev_1": {status: "active", sealedSecret: "sealed-ciphertext", configuration: `{"entity_id":"https://idp.example.test"}`},
	}
	store := NewIdentitySubjectStore(db)

	material, found, err := store.GetActiveSAMLProviderConfigForLogin(context.Background(), "pc_saml_1")
	if err != nil {
		t.Fatalf("GetActiveSAMLProviderConfigForLogin() error = %v", err)
	}
	if !found {
		t.Fatal("GetActiveSAMLProviderConfigForLogin() found = false, want true for an active external_saml row")
	}
	if material.ProviderKind != "external_saml" || material.RevisionID != "rev_1" {
		t.Fatalf("material kind/revision = (%q, %q), want (external_saml, rev_1)", material.ProviderKind, material.RevisionID)
	}
	if material.SealedSecret != "sealed-ciphertext" {
		t.Fatalf("SealedSecret = %q, want the stored ciphertext (never opened here)", material.SealedSecret)
	}
	if material.Configuration != `{"entity_id":"https://idp.example.test"}` {
		t.Fatalf("Configuration = %q, want the stored non-secret configuration", material.Configuration)
	}
}

// TestGetActiveSAMLProviderConfigForLoginRejectsInactiveOrWrongKind proves
// draft status and non-SAML provider kind both fail closed (found=false),
// matching Enable's "never usable to authenticate until tested+enabled"
// contract and the kind-scoping selectActiveSAMLProviderConfigForLoginQuery
// enforces server-side.
func TestGetActiveSAMLProviderConfigForLoginRejectsInactiveOrWrongKind(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	db.configs["pc_saml_draft"] = &fakeProviderConfigRow{
		tenantID:         "tenant_a",
		providerKind:     "external_saml",
		status:           "draft",
		activeRevisionID: "rev_1",
	}
	db.revisions["pc_saml_draft"] = map[string]*fakeProviderConfigRevisionRow{
		"rev_1": {status: "active", sealedSecret: "sealed", configuration: "{}"},
	}
	db.configs["pc_oidc_active"] = &fakeProviderConfigRow{
		tenantID:         "tenant_a",
		providerKind:     "external_oidc",
		status:           "active",
		activeRevisionID: "rev_1",
	}
	db.revisions["pc_oidc_active"] = map[string]*fakeProviderConfigRevisionRow{
		"rev_1": {status: "active", sealedSecret: "sealed", configuration: "{}"},
	}
	store := NewIdentitySubjectStore(db)

	if _, found, err := store.GetActiveSAMLProviderConfigForLogin(context.Background(), "pc_saml_draft"); err != nil || found {
		t.Fatalf("draft provider: found = %v, err = %v, want (false, nil)", found, err)
	}
	if _, found, err := store.GetActiveSAMLProviderConfigForLogin(context.Background(), "pc_oidc_active"); err != nil || found {
		t.Fatalf("external_oidc provider: found = %v, err = %v, want (false, nil) — must never cross kinds", found, err)
	}
	if _, found, err := store.GetActiveSAMLProviderConfigForLogin(context.Background(), "pc_missing"); err != nil || found {
		t.Fatalf("missing provider: found = %v, err = %v, want (false, nil)", found, err)
	}
}

// TestGetActiveSAMLProviderConfigForLoginRequiresProviderConfigID proves the
// method rejects an empty provider_config_id rather than issuing an
// unbounded query.
func TestGetActiveSAMLProviderConfigForLoginRequiresProviderConfigID(t *testing.T) {
	t.Parallel()
	store := NewIdentitySubjectStore(newProviderConfigFakeDB())
	if _, _, err := store.GetActiveSAMLProviderConfigForLogin(context.Background(), "   "); err == nil {
		t.Fatal("GetActiveSAMLProviderConfigForLogin() error = nil, want error for blank provider_config_id")
	}
}
