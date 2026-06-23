package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOIDCLoginStoreResolvesActiveRoleGrantsForRefresh(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// Active role rows for the persisted role ids.
			{rows: [][]any{{"developer", "sha256:policy"}}},
			// Scope targets.
			{rows: [][]any{{"scope_a"}}},
			// Repository targets.
			{rows: [][]any{{"repo_a", "scope_a"}}},
		},
	}
	store := NewOIDCLoginStore(db)

	resolution, ok, err := store.ResolveActiveRoleGrants(context.Background(), OIDCRoleGrantQuery{
		ProviderConfigID: "okta-dev",
		TenantID:         "tenant_a",
		WorkspaceID:      "workspace_a",
		RoleIDs:          []string{"developer"},
		AsOf:             now,
	})
	if err != nil {
		t.Fatalf("ResolveActiveRoleGrants() error = %v", err)
	}
	if !ok {
		t.Fatal("ResolveActiveRoleGrants() ok = false, want true")
	}
	if got := resolution.RoleIDs; len(got) != 1 || got[0] != "developer" {
		t.Fatalf("RoleIDs = %#v, want [developer]", got)
	}
	if resolution.PolicyRevisionHash != "sha256:policy" {
		t.Fatalf("PolicyRevisionHash = %q, want sha256:policy", resolution.PolicyRevisionHash)
	}
	if got := resolution.AllowedScopeIDs; len(got) != 1 || got[0] != "scope_a" {
		t.Fatalf("AllowedScopeIDs = %#v, want [scope_a]", got)
	}
	if len(db.queries) != 3 {
		t.Fatalf("query count = %d, want 3", len(db.queries))
	}
	roleQuery := db.queries[0].query
	for _, want := range []string{
		"identity_roles",
		"role_id = ANY($5::text[])",
		"status = 'active'",
		"tombstoned_at IS NULL",
	} {
		if !strings.Contains(roleQuery, want) {
			t.Fatalf("active-role query missing %q:\n%s", want, roleQuery)
		}
	}
}

func TestOIDCLoginStoreResolveActiveRoleGrantsDeniesWhenRoleTombstoned(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 12, 10, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		// No active role rows returned: every persisted role target is revoked or
		// tombstoned, so refresh must deny (ok = false) and not query targets.
		queryResponses: []queueFakeRows{{rows: nil}},
	}
	store := NewOIDCLoginStore(db)

	_, ok, err := store.ResolveActiveRoleGrants(context.Background(), OIDCRoleGrantQuery{
		ProviderConfigID: "okta-dev",
		TenantID:         "tenant_a",
		WorkspaceID:      "workspace_a",
		RoleIDs:          []string{"developer"},
		AsOf:             now,
	})
	if err != nil {
		t.Fatalf("ResolveActiveRoleGrants() error = %v", err)
	}
	if ok {
		t.Fatal("ResolveActiveRoleGrants() ok = true, want false when roles tombstoned")
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want only role validation before deny", len(db.queries))
	}
}

func TestOIDCLoginStoreResolveActiveRoleGrantsRequiresRoleIDs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 12, 15, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewOIDCLoginStore(db)

	_, _, err := store.ResolveActiveRoleGrants(context.Background(), OIDCRoleGrantQuery{
		ProviderConfigID: "okta-dev",
		TenantID:         "tenant_a",
		WorkspaceID:      "workspace_a",
		RoleIDs:          nil,
		AsOf:             now,
	})
	if err == nil {
		t.Fatal("ResolveActiveRoleGrants() with no role ids must return error")
	}
	if len(db.queries) != 0 {
		t.Fatalf("query count = %d, want 0 when validation fails", len(db.queries))
	}
}

func TestOIDCLoginStoreExternalSubjectActiveTrue(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{true}}}},
	}
	store := NewOIDCLoginStore(db)

	active, err := store.ExternalSubjectActive(context.Background(), "okta-dev", "sha256:subject")
	if err != nil {
		t.Fatalf("ExternalSubjectActive() error = %v", err)
	}
	if !active {
		t.Fatal("ExternalSubjectActive() = false, want true")
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(db.queries))
	}
	for _, want := range []string{
		"external_subject_id_hash",
		"provider_config_id",
		"status = 'active'",
		"disabled_at IS NULL",
		"tombstoned_at IS NULL",
	} {
		if !strings.Contains(db.queries[0].query, want) {
			t.Fatalf("subject-active query missing %q:\n%s", want, db.queries[0].query)
		}
	}
}

func TestOIDCLoginStoreExternalSubjectActiveFalseWhenNoRow(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: nil}},
	}
	store := NewOIDCLoginStore(db)

	active, err := store.ExternalSubjectActive(context.Background(), "okta-dev", "sha256:subject")
	if err != nil {
		t.Fatalf("ExternalSubjectActive() error = %v", err)
	}
	if active {
		t.Fatal("ExternalSubjectActive() = true, want false when subject disabled or unknown")
	}
}

func TestOIDCLoginStoreExternalSubjectActiveRequiresIdentifiers(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewOIDCLoginStore(db)

	if _, err := store.ExternalSubjectActive(context.Background(), "", "sha256:subject"); err == nil {
		t.Fatal("ExternalSubjectActive() with empty provider must error")
	}
	if _, err := store.ExternalSubjectActive(context.Background(), "okta-dev", ""); err == nil {
		t.Fatal("ExternalSubjectActive() with empty subject must error")
	}
	if len(db.queries) != 0 {
		t.Fatalf("query count = %d, want 0 when identifiers missing", len(db.queries))
	}
}
