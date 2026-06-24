package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestResolveSAMLExternalSubjectAdminStaysAllScopeFailOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 18, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"tenant_saml",
				"workspace_saml",
				"sha256:user-subject",
				"sha256:policy",
				"user_owner",
				true, // has_admin_role: user holds owner/tenant_admin membership role
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
		t.Fatalf("auth grant = all_scopes:%t policy:%q, want all-scope admin", auth.AllScopes, auth.PolicyRevisionHash)
	}
	if auth.PermissionCatalogEnforced {
		t.Fatalf("admin auth PermissionCatalogEnforced = true, want false (must stay fail-open)")
	}
	if len(auth.AllowedPermissionFeatures) != 0 || len(auth.AllowedPermissionDataClasses) != 0 {
		t.Fatalf("admin auth carries permission grants = %#v/%#v, want empty", auth.AllowedPermissionFeatures, auth.AllowedPermissionDataClasses)
	}

	if got := len(db.queries); got != 1 {
		t.Fatalf("query count = %d, want one durable resolution query (admin short-circuits enforcement)", got)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"FROM identity_external_subjects es",
		"JOIN identity_provider_configs pc",
		"JOIN identity_users u",
		"JOIN identity_tenant_memberships m",
		"JOIN identity_membership_roles mr",
		"JOIN tenants t",
		"JOIN workspaces w",
		"pc.provider_kind = 'external_saml'",
		"es.group_claims_hash = $3",
		"AS has_admin_role",
		"mr.role_id IN ('owner', 'tenant_admin')",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("durable SAML resolution query missing %q:\n%s", want, query)
		}
	}
	for _, banned := range []string{
		"JOIN identity_role_grants rg",
		"JOIN identity_roles r\n",
		"AS has_all_scope_role",
		"rg.scope_class",
	} {
		if strings.Contains(query, banned) {
			t.Fatalf("durable SAML resolution query must not join grants or use scope_class for admin detection: found %q:\n%s", banned, query)
		}
	}
	for _, leaked := range []string{"saml-admins", "user@example.test", "raw-name-id"} {
		if fakeExecArgsContain(db.queries[0].args, leaked) {
			t.Fatalf("durable SAML resolution args leaked raw SAML value %q: %#v", leaked, db.queries[0].args)
		}
	}
}

func TestResolveSAMLExternalSubjectNonAdminEnforcesCatalog(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 18, 10, 0, 0, time.UTC)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{
			"tenant_saml",
			"workspace_saml",
			"sha256:member-subject",
			"sha256:policy",
			"user_member",
			false, // has_admin_role: no owner/tenant_admin membership role
		}}},
		{rows: [][]any{{"role_reader"}}},
		{rows: [][]any{
			{"ask_search", "ask_reasoning"},
			{"repository_content", "source_content"},
		}},
	}}
	store := NewIdentitySubjectStore(db)

	result, err := store.ResolveSAMLExternalSubject(context.Background(), SAMLExternalSubjectResolutionRequest{
		ProviderConfigID:      "provider_saml",
		ExternalSubjectIDHash: "sha256:external-subject",
		GroupClaimsHash:       "sha256:groups-current",
		Now:                   now,
	})
	if err != nil {
		t.Fatalf("ResolveSAMLExternalSubject() error = %v", err)
	}
	if !result.Resolved || !result.KnownSubject {
		t.Fatalf("ResolveSAMLExternalSubject() result = %#v, want resolved known subject", result)
	}
	auth := result.Auth
	if auth.AllScopes {
		t.Fatalf("non-admin auth AllScopes = true, want false")
	}
	if !auth.PermissionCatalogEnforced {
		t.Fatalf("non-admin auth PermissionCatalogEnforced = false, want true")
	}
	if got, want := auth.RoleIDs, []string{"role_reader"}; !equalStringSlices(got, want) {
		t.Fatalf("RoleIDs = %#v, want %#v", got, want)
	}
	if got, want := auth.AllowedPermissionFeatures, []string{"ask_search", "repository_content"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedPermissionFeatures = %#v, want %#v", got, want)
	}
	if got, want := auth.AllowedPermissionDataClasses, []string{"ask_reasoning", "source_content"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedPermissionDataClasses = %#v, want %#v", got, want)
	}
	if !fakeQueriesContain(db.queries, "FROM identity_role_grants grant") {
		t.Fatalf("non-admin SAML resolution must derive permission grants from roles: %#v", db.queries)
	}
}

// TestResolveSAMLExternalSubjectNonAdminWithAllScopeGrantStillEnforcesCatalog
// is the exact Codex P1 priv-esc scenario: a user holds a non-admin membership
// role (e.g. "role_reader") whose grant carries scope_class='all' (tenant-wide
// for a specific feature). scope_class='all' on a grant means the grant applies
// across the whole tenant for that feature — it does NOT mean admin. The
// resolution query derives admin status from mr.role_id IN ('owner','tenant_admin'),
// not from grant scope. The user must resolve with PermissionCatalogEnforced=true
// and AllScopes=false, never AllScopes=true.
func TestResolveSAMLExternalSubjectNonAdminWithAllScopeGrantStillEnforcesCatalog(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 18, 15, 0, 0, time.UTC)
	// has_admin_role=false: role_reader is not owner/tenant_admin even if its
	// grant carries scope_class='all'.
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{
			"tenant_saml",
			"workspace_saml",
			"sha256:reader-subject",
			"sha256:policy",
			"user_reader",
			false, // has_admin_role: role_reader is NOT an admin role
		}}},
		{rows: [][]any{{"role_reader"}}},
		{rows: [][]any{
			{"ask_search", "ask_reasoning"},
		}},
	}}
	store := NewIdentitySubjectStore(db)

	result, err := store.ResolveSAMLExternalSubject(context.Background(), SAMLExternalSubjectResolutionRequest{
		ProviderConfigID:      "provider_saml",
		ExternalSubjectIDHash: "sha256:reader-external",
		GroupClaimsHash:       "sha256:reader-groups",
		Now:                   now,
	})
	if err != nil {
		t.Fatalf("ResolveSAMLExternalSubject() error = %v", err)
	}
	if !result.Resolved || !result.KnownSubject {
		t.Fatalf("ResolveSAMLExternalSubject() result = %#v, want resolved known subject", result)
	}
	auth := result.Auth
	// Core priv-esc assertion: scope_class='all' on a grant must NOT elevate
	// a non-admin role to AllScopes. Admin is determined solely by role membership.
	if auth.AllScopes {
		t.Fatalf("non-admin role with tenant-wide grant: AllScopes = true, want false (scope_class='all' is NOT admin)")
	}
	if !auth.PermissionCatalogEnforced {
		t.Fatalf("non-admin role with tenant-wide grant: PermissionCatalogEnforced = false, want true")
	}
	if got, want := auth.RoleIDs, []string{"role_reader"}; !equalStringSlices(got, want) {
		t.Fatalf("RoleIDs = %#v, want %#v", got, want)
	}
}

// TestResolveSAMLExternalSubjectParityWithScopedTokenForSameRole proves that a
// resolved non-admin SAML session authorizes identically to a scoped token for
// the same role: same allowed features, same data classes, catalog enforced,
// not all-scope. Both call resolvePermissionGrantsForRoles, the single source of
// truth, so the two derive identical grants from the same role set.
func TestResolveSAMLExternalSubjectParityWithScopedTokenForSameRole(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 18, 20, 0, 0, time.UTC)
	grantRows := [][]any{
		{"ask_search", "ask_reasoning"},
		{"repository_content", "source_content"},
	}

	samlDB := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{
			"tenant_saml",
			"workspace_saml",
			"sha256:member-subject",
			"sha256:policy",
			"user_member",
			false, // has_admin_role: no owner/tenant_admin membership role
		}}},
		{rows: [][]any{{"role_reader"}}},
		{rows: append([][]any(nil), grantRows...)},
	}}
	samlResult, err := NewIdentitySubjectStore(samlDB).ResolveSAMLExternalSubject(
		context.Background(),
		SAMLExternalSubjectResolutionRequest{
			ProviderConfigID:      "provider_saml",
			ExternalSubjectIDHash: "sha256:external-subject",
			GroupClaimsHash:       "sha256:groups-current",
			Now:                   now,
		},
	)
	if err != nil {
		t.Fatalf("ResolveSAMLExternalSubject() error = %v", err)
	}

	tokenDB := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: append([][]any(nil), grantRows...)},
	}}
	tokenFeatures, tokenDataClasses, err := NewScopedAPITokenStore(tokenDB).ResolvePermissionGrantsForRoles(
		context.Background(),
		"tenant_saml",
		[]string{"role_reader"},
		now,
	)
	if err != nil {
		t.Fatalf("ResolvePermissionGrantsForRoles() error = %v", err)
	}

	if samlResult.Auth.AllScopes {
		t.Fatalf("SAML auth AllScopes = true, want false for parity with scoped token")
	}
	if !samlResult.Auth.PermissionCatalogEnforced {
		t.Fatalf("SAML auth PermissionCatalogEnforced = false, want true for parity with scoped token")
	}
	if !equalStringSlices(samlResult.Auth.AllowedPermissionFeatures, tokenFeatures) {
		t.Fatalf("feature parity mismatch: saml %#v vs token %#v", samlResult.Auth.AllowedPermissionFeatures, tokenFeatures)
	}
	if !equalStringSlices(samlResult.Auth.AllowedPermissionDataClasses, tokenDataClasses) {
		t.Fatalf("data-class parity mismatch: saml %#v vs token %#v", samlResult.Auth.AllowedPermissionDataClasses, tokenDataClasses)
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
