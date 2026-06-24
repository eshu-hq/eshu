// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBootstrapDefinitionsIncludeOIDCLoginStateAndMappings(t *testing.T) {
	t.Parallel()

	var oidc Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "identity_oidc_login" {
			oidc = def
			break
		}
	}
	if oidc.Name == "" {
		t.Fatal("identity_oidc_login definition missing")
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS identity_oidc_login_states",
		"state_hash TEXT PRIMARY KEY",
		"nonce_hash TEXT NOT NULL",
		"redirect_uri_hash TEXT NOT NULL",
		"CREATE TABLE IF NOT EXISTS identity_provider_group_role_mappings",
		"external_group_hash TEXT NOT NULL",
		"role_id TEXT NOT NULL",
		"CREATE TABLE IF NOT EXISTS identity_role_scope_targets",
		"CREATE TABLE IF NOT EXISTS identity_role_repository_targets",
		"REFERENCES identity_provider_configs(provider_config_id) ON DELETE CASCADE",
		"REFERENCES identity_roles(tenant_id, role_id) ON DELETE CASCADE",
	} {
		if !strings.Contains(oidc.SQL, want) {
			t.Fatalf("OIDC login SQL missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"state TEXT",
		"nonce TEXT",
		"raw_group",
		"group_name",
		"id_token",
		"access_token",
		"client_secret",
		"email TEXT",
	} {
		if strings.Contains(strings.ToLower(oidc.SQL), strings.ToLower(forbidden)) {
			t.Fatalf("OIDC login SQL contains forbidden marker %q", forbidden)
		}
	}
}

func TestOIDCLoginStateStoreCreatesAndConsumesHashesOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 11, 0, 0, 0, time.UTC)
	expires := now.Add(10 * time.Minute)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"sha256:state",
				"sha256:nonce",
				"okta-dev",
				"tenant_a",
				"workspace_a",
				"sha256:redirect",
				"/console",
				now,
				expires,
				now,
			}},
		}},
	}
	store := NewOIDCLoginStore(db)

	if err := store.CreateState(context.Background(), OIDCLoginStateRecord{
		StateHash:        "sha256:state",
		NonceHash:        "sha256:nonce",
		ProviderConfigID: "okta-dev",
		ProviderKeyHash:  "sha256:provider-key",
		IssuerHash:       "sha256:issuer",
		ClientIDHash:     "sha256:client",
		TenantID:         "tenant_a",
		WorkspaceID:      "workspace_a",
		RedirectURIHash:  "sha256:redirect",
		ReturnToPath:     "/console",
		IssuedAt:         now,
		ExpiresAt:        expires,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("CreateState() error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	if fakeExecArgsContain(db.execs[0].args, "state-secret") ||
		fakeExecArgsContain(db.execs[0].args, "nonce-secret") ||
		fakeExecArgsContain(db.execs[0].args, "https://idp.example.test/oauth2/default") ||
		fakeExecArgsContain(db.execs[0].args, "client-id") {
		t.Fatalf("create args leaked raw state, nonce, or provider data: %#v", db.execs[0].args)
	}
	for _, want := range []string{
		"WITH provider AS",
		"INSERT INTO identity_provider_configs",
		"provider_kind",
		"provider_key_hash",
		"issuer_hash",
		"client_id_hash",
		"ON CONFLICT (provider_config_id) DO UPDATE",
		"WHERE identity_provider_configs.tombstoned_at IS NULL",
		"INSERT INTO identity_oidc_login_states",
		"FROM provider",
	} {
		if !strings.Contains(db.execs[0].query, want) {
			t.Fatalf("create query missing %q:\n%s", want, db.execs[0].query)
		}
	}

	record, ok, err := store.ConsumeState(context.Background(), "sha256:state", now)
	if err != nil {
		t.Fatalf("ConsumeState() error = %v", err)
	}
	if !ok {
		t.Fatal("ConsumeState() ok = false, want true")
	}
	if record.StateHash != "sha256:state" || record.NonceHash != "sha256:nonce" ||
		record.ProviderConfigID != "okta-dev" || record.ReturnToPath != "/console" {
		t.Fatalf("consumed record = %#v, want state metadata", record)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(db.queries))
	}
	for _, want := range []string{
		"UPDATE identity_oidc_login_states",
		"consumed_at IS NULL",
		"expires_at > $2",
		"RETURNING",
	} {
		if !strings.Contains(db.queries[0].query, want) {
			t.Fatalf("consume query missing %q:\n%s", want, db.queries[0].query)
		}
	}
}

func TestOIDCLoginStoreResolvesGroupsThroughRolesToGrants(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 11, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"developer", "sha256:policy"}}},
			{rows: [][]any{{"scope_a"}, {"scope_b"}}},
			{rows: [][]any{{"repo_a", "scope_a"}}},
			{rows: [][]any{{"ask_search", "ask_reasoning"}, {"repository_content", "source_content"}}},
		},
	}
	store := NewOIDCLoginStore(db)

	resolution, ok, err := store.ResolveGroupRoleGrants(context.Background(), OIDCGroupGrantQuery{
		ProviderConfigID:    "okta-dev",
		TenantID:            "tenant_a",
		WorkspaceID:         "workspace_a",
		ExternalGroupHashes: []string{"sha256:group"},
		AsOf:                now,
	})
	if err != nil {
		t.Fatalf("ResolveGroupRoleGrants() error = %v", err)
	}
	if !ok {
		t.Fatal("ResolveGroupRoleGrants() ok = false, want true")
	}
	if got := resolution.RoleIDs; len(got) != 1 || got[0] != "developer" {
		t.Fatalf("RoleIDs = %#v, want [developer]", got)
	}
	if got := resolution.AllowedScopeIDs; len(got) != 2 || got[0] != "scope_a" || got[1] != "scope_b" {
		t.Fatalf("AllowedScopeIDs = %#v, want scope_a/scope_b", got)
	}
	if got := resolution.AllowedRepositoryIDs; len(got) != 1 || got[0] != "repo_a" {
		t.Fatalf("AllowedRepositoryIDs = %#v, want [repo_a]", got)
	}
	if got, want := resolution.AllowedPermissionFeatures, []string{"ask_search", "repository_content"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedPermissionFeatures = %#v, want %#v", got, want)
	}
	if got, want := resolution.AllowedPermissionDataClasses, []string{"ask_reasoning", "source_content"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedPermissionDataClasses = %#v, want %#v", got, want)
	}
	if resolution.PolicyRevisionHash != "sha256:policy" {
		t.Fatalf("PolicyRevisionHash = %q, want sha256:policy", resolution.PolicyRevisionHash)
	}
	if len(db.queries) != 4 {
		t.Fatalf("query count = %d, want 4", len(db.queries))
	}
	if !strings.Contains(db.queries[3].query, "FROM identity_role_grants grant") {
		t.Fatalf("permission query missing identity_role_grants:\n%s", db.queries[3].query)
	}
	if strings.Contains(db.queries[0].query, "group_name") ||
		!strings.Contains(db.queries[0].query, "external_group_hash = ANY($5::text[])") {
		t.Fatalf("role query uses unsafe group shape:\n%s", db.queries[0].query)
	}
}

func TestOIDCLoginStoreReturnsNotMappedWhenNoGroupRoleRowsMatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: nil}},
	}
	store := NewOIDCLoginStore(db)

	resolution, ok, err := store.ResolveGroupRoleGrants(context.Background(), OIDCGroupGrantQuery{
		ProviderConfigID:    "okta-dev",
		TenantID:            "tenant_a",
		WorkspaceID:         "workspace_a",
		ExternalGroupHashes: []string{"sha256:unmapped"},
		AsOf:                now,
	})
	if err != nil {
		t.Fatalf("ResolveGroupRoleGrants() error = %v, want nil", err)
	}
	if ok {
		t.Fatal("ResolveGroupRoleGrants() ok = true, want false")
	}
	if len(resolution.RoleIDs) != 0 || resolution.PolicyRevisionHash != "" ||
		len(resolution.AllowedScopeIDs) != 0 || len(resolution.AllowedRepositoryIDs) != 0 {
		t.Fatalf("resolution = %#v, want empty not-mapped result", resolution)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want only role resolution before not-mapped result", len(db.queries))
	}
}

func TestOIDCLoginStoreRejectsMixedPolicyRevisionRoleMappings(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 11, 45, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{
				{"developer", "sha256:policy-a"},
				{"auditor", "sha256:policy-b"},
			},
		}},
	}
	store := NewOIDCLoginStore(db)

	_, ok, err := store.ResolveGroupRoleGrants(context.Background(), OIDCGroupGrantQuery{
		ProviderConfigID:    "okta-dev",
		TenantID:            "tenant_a",
		WorkspaceID:         "workspace_a",
		ExternalGroupHashes: []string{"sha256:group"},
		AsOf:                now,
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one policy revision") {
		t.Fatalf("ResolveGroupRoleGrants() error = %v, want policy revision mismatch", err)
	}
	if ok {
		t.Fatal("ResolveGroupRoleGrants() ok = true, want false")
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want only role resolution before failure", len(db.queries))
	}
}
