// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestResolveSAMLExternalSubjectRequiresActiveIdentityMembershipAndGrant(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 18, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"tenant_saml",
				"workspace_saml",
				"sha256:user-subject",
				"sha256:policy",
			}},
		}},
	}
	store := NewIdentitySubjectStore(db)

	result, err := store.ResolveSAMLExternalSubject(context.Background(), SAMLExternalSubjectResolutionRequest{
		ProviderConfigID:      " provider_saml ",
		ExternalSubjectIDHash: " sha256:external-subject ",
		GroupClaimsHash:       " sha256:groups-current ",
		Now:                   now,
	})
	if err != nil {
		t.Fatalf("ResolveSAMLExternalSubject() error = %v", err)
	}
	if !result.Resolved || !result.KnownSubject {
		t.Fatalf("ResolveSAMLExternalSubject() result = %#v, want resolved known subject", result)
	}
	auth := result.Auth
	if auth.TenantID != "tenant_saml" || auth.WorkspaceID != "workspace_saml" {
		t.Fatalf("auth tenant/workspace = %q/%q, want durable membership", auth.TenantID, auth.WorkspaceID)
	}
	if auth.SubjectClass != "external_saml" || auth.SubjectIDHash != "sha256:user-subject" {
		t.Fatalf("auth subject = %q/%q, want mapped durable user", auth.SubjectClass, auth.SubjectIDHash)
	}
	if !auth.AllScopes || auth.PolicyRevisionHash != "sha256:policy" {
		t.Fatalf("auth grant = all_scopes:%t policy:%q, want active all-scope grant", auth.AllScopes, auth.PolicyRevisionHash)
	}

	if got := len(db.queries); got != 1 {
		t.Fatalf("query count = %d, want one successful durable resolution query", got)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"FROM identity_external_subjects es",
		"JOIN identity_provider_configs pc",
		"JOIN identity_users u",
		"JOIN identity_tenant_memberships m",
		"JOIN identity_membership_roles mr",
		"JOIN identity_roles r",
		"JOIN identity_role_grants rg",
		"JOIN tenants t",
		"JOIN workspaces w",
		"pc.provider_kind = 'external_saml'",
		"es.group_claims_hash = $3",
		"rg.scope_class = 'all'",
		"rg.status = 'active'",
		"rg.tombstoned_at IS NULL",
		"ORDER BY m.effective_at DESC, mr.effective_at DESC, rg.effective_at DESC",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("durable SAML resolution query missing %q:\n%s", want, query)
		}
	}
	for _, leaked := range []string{"saml-admins", "user@example.test", "raw-name-id"} {
		if fakeExecArgsContain(db.queries[0].args, leaked) {
			t.Fatalf("durable SAML resolution args leaked raw SAML value %q: %#v", leaked, db.queries[0].args)
		}
	}
}

func TestResolveSAMLExternalSubjectMarksKnownSubjectWhenGroupHashNoLongerMatches(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{},
			{rows: [][]any{{"external_identity_saml"}}},
		},
	}
	store := NewIdentitySubjectStore(db)

	result, err := store.ResolveSAMLExternalSubject(context.Background(), SAMLExternalSubjectResolutionRequest{
		ProviderConfigID:      "provider_saml",
		ExternalSubjectIDHash: "sha256:external-subject",
		GroupClaimsHash:       "sha256:groups-new",
		Now:                   time.Date(2026, 6, 22, 18, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ResolveSAMLExternalSubject() error = %v", err)
	}
	if result.Resolved || !result.KnownSubject {
		t.Fatalf("ResolveSAMLExternalSubject() result = %#v, want denied known subject", result)
	}
	if got := len(db.queries); got != 2 {
		t.Fatalf("query count = %d, want resolution plus known-subject check", got)
	}
	if !strings.Contains(db.queries[0].query, "es.group_claims_hash = $3") {
		t.Fatalf("resolution query did not require current group hash:\n%s", db.queries[0].query)
	}
	if strings.Contains(db.queries[1].query, "group_claims_hash =") {
		t.Fatalf("known-subject query must not treat stale group hash as unknown subject:\n%s", db.queries[1].query)
	}
}

func TestHasActiveSAMLProviderConfigRequiresExternalSAMLProviderRow(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{"provider_saml"}}}},
	}
	store := NewIdentitySubjectStore(db)

	active, err := store.HasActiveSAMLProviderConfig(context.Background(), " provider_saml ")
	if err != nil {
		t.Fatalf("HasActiveSAMLProviderConfig() error = %v", err)
	}
	if !active {
		t.Fatal("HasActiveSAMLProviderConfig() active = false, want true")
	}
	query := db.queries[0].query
	for _, want := range []string{
		"FROM identity_provider_configs pc",
		"JOIN tenants t",
		"pc.provider_config_id = $1",
		"pc.provider_kind = 'external_saml'",
		"pc.status = 'active'",
		"pc.tombstoned_at IS NULL",
		"t.status = 'active'",
		"t.tombstoned_at IS NULL",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("active SAML provider query missing %q:\n%s", want, query)
		}
	}
}
