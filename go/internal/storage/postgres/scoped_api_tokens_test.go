// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestBootstrapDefinitionsIncludeScopedAPITokens(t *testing.T) {
	t.Parallel()

	var tokens Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "scoped_api_tokens" {
			tokens = def
			break
		}
	}
	if tokens.Name == "" {
		t.Fatal("scoped_api_tokens definition missing")
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS scoped_api_tokens",
		"token_hash TEXT PRIMARY KEY",
		"subject_id_hash TEXT NOT NULL",
		"FOREIGN KEY (tenant_id, workspace_id)",
		"REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE",
		"scoped_api_tokens_active_idx",
	} {
		if !strings.Contains(tokens.SQL, want) {
			t.Fatalf("scoped api token SQL missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"token_value",
		"raw_token",
		"bearer_token",
		"secret_value",
		"tenant_name",
		"workspace_name",
	} {
		if strings.Contains(strings.ToLower(tokens.SQL), forbidden) {
			t.Fatalf("scoped api token SQL contains forbidden marker %q", forbidden)
		}
	}
}

func TestScopedAPITokenStoreUpsertsHashOnlyRecord(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 9, 16, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewScopedAPITokenStore(db)

	err := store.UpsertToken(context.Background(), ScopedAPITokenRecord{
		TokenHash:          "sha256:token",
		TenantID:           "tenant_a",
		WorkspaceID:        "workspace_a",
		SubjectIDHash:      "sha256:subject",
		SubjectClass:       "team",
		Status:             "active",
		PolicyRevisionHash: "sha256:policy",
		IssuedAt:           now,
		UpdatedAt:          now,
	})
	if err != nil {
		t.Fatalf("UpsertToken() error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	if !strings.Contains(db.execs[0].query, "ON CONFLICT (token_hash) DO UPDATE") {
		t.Fatalf("upsert query missing conflict clause:\n%s", db.execs[0].query)
	}
	if fakeExecArgsContain(db.execs[0].args, "raw-secret-token") {
		t.Fatalf("upsert args leaked raw token: %#v", db.execs[0].args)
	}
}

func TestScopedAPITokenStoreResolvesOnlyActiveTenantWorkspaceToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 9, 17, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"sha256:token",
				"tenant_a",
				"workspace_a",
				"sha256:subject",
				"team",
				"active",
				"sha256:policy",
				now.Add(-time.Hour),
				sql.NullTime{},
				sql.NullTime{},
				sql.NullTime{},
			}},
		}},
	}
	store := NewScopedAPITokenStore(db)

	token, ok, err := store.ResolveTokenHash(context.Background(), "sha256:token", now)
	if err != nil {
		t.Fatalf("ResolveTokenHash() error = %v", err)
	}
	if !ok {
		t.Fatal("ResolveTokenHash() ok = false, want true")
	}
	if token.TenantID != "tenant_a" || token.WorkspaceID != "workspace_a" ||
		token.SubjectClass != "team" || token.SubjectIDHash != "sha256:subject" {
		t.Fatalf("resolved token = %#v, want tenant/workspace subject", token)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"JOIN tenants ten",
		"JOIN workspaces ws",
		"tok.status = 'active'",
		"ten.status = 'active'",
		"ws.status = 'active'",
		"tok.revoked_at IS NULL",
		"(tok.expires_at IS NULL OR tok.expires_at > $2)",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("resolve query missing %q:\n%s", want, query)
		}
	}
}

func TestScopedAPITokenStoreRejectsBlankHash(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewScopedAPITokenStore(db)
	_, ok, err := store.ResolveTokenHash(context.Background(), "", time.Now())
	if err == nil {
		t.Fatal("ResolveTokenHash() error = nil, want validation error")
	}
	if ok {
		t.Fatal("ResolveTokenHash() ok = true, want false")
	}
	if len(db.queries) != 0 {
		t.Fatalf("query count = %d, want 0", len(db.queries))
	}
}

func TestScopedAPITokenStoreResolvesIdentityPersonalTokenThroughActiveRoles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 18, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"sha256:token",
				"personal",
				"tenant_a",
				"workspace_a",
				"sha256:user-subject",
				"",
				"sha256:token-policy",
			}}},
			{rows: [][]any{
				{"role_reader", "sha256:policy-a"},
			}},
			{rows: [][]any{
				{"ask_search", "ask_reasoning"},
				{"ask_search", "documentation_semantic"},
				{"ask_search", "source_content"},
			}},
			{rows: [][]any{
				{"scope://team-a"},
			}},
			{rows: [][]any{
				{"repo://team-a/api", "scope://team-a"},
			}},
		},
	}
	store := NewScopedAPITokenStore(db)

	resolution, ok, err := store.ResolveIdentityAPITokenHash(context.Background(), "sha256:token", now)
	if err != nil {
		t.Fatalf("ResolveIdentityAPITokenHash() error = %v", err)
	}
	if !ok {
		t.Fatal("ResolveIdentityAPITokenHash() ok = false, want true")
	}
	if got, want := resolution.SubjectClass, "user"; got != want {
		t.Fatalf("SubjectClass = %q, want %q", got, want)
	}
	if got, want := resolution.SubjectIDHash, "sha256:user-subject"; got != want {
		t.Fatalf("SubjectIDHash = %q, want %q", got, want)
	}
	if got, want := resolution.RoleIDs, []string{"role_reader"}; !equalStringSlices(got, want) {
		t.Fatalf("RoleIDs = %#v, want %#v", got, want)
	}
	if got, want := resolution.AllowedScopeIDs, []string{"scope://team-a"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedScopeIDs = %#v, want %#v", got, want)
	}
	if got, want := resolution.AllowedPermissionFeatures, []string{"ask_search"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedPermissionFeatures = %#v, want %#v", got, want)
	}
	if got, want := resolution.AllowedPermissionDataClasses, []string{"ask_reasoning", "documentation_semantic", "source_content"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedPermissionDataClasses = %#v, want %#v", got, want)
	}
	if got, want := resolution.AllowedRepositoryIDs, []string{"repo://team-a/api"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedRepositoryIDs = %#v, want %#v", got, want)
	}

	if got, want := len(db.queries), 5; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	for _, want := range []string{
		"FROM identity_token_metadata tok",
		"JOIN identity_users user_subject",
		"tok.revoked_at IS NULL",
		"user_subject.status = 'active'",
	} {
		if !strings.Contains(db.queries[0].query, want) {
			t.Fatalf("subject query missing %q:\n%s", want, db.queries[0].query)
		}
	}
	for _, want := range []string{
		"JOIN identity_tenant_memberships membership",
		"JOIN identity_membership_roles role_assignment",
		"role_assignment.status = 'active'",
	} {
		if !strings.Contains(db.queries[1].query, want) {
			t.Fatalf("role query missing %q:\n%s", want, db.queries[1].query)
		}
	}
	if !strings.Contains(db.queries[2].query, "FROM identity_role_grants grant") {
		t.Fatalf("permission query missing identity_role_grants:\n%s", db.queries[2].query)
	}
	if fakeExecArgsContain(db.queries[0].args, "raw-personal-token") {
		t.Fatalf("subject query args leaked raw token: %#v", db.queries[0].args)
	}
}

func TestScopedAPITokenStoreResolvesIdentityServicePrincipalTokenThroughActiveRoles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 18, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"sha256:token",
				"service_principal",
				"tenant_a",
				"workspace_a",
				"",
				"sp_automation",
				"sha256:token-policy",
			}}},
			{rows: [][]any{
				{"role_automation", "sha256:policy-a"},
			}},
			{rows: [][]any{
				{"ask_search", "ask_reasoning"},
				{"ask_search", "documentation_semantic"},
				{"ask_search", "source_content"},
			}},
			{rows: [][]any{
				{"scope://team-a"},
			}},
			{rows: [][]any{
				{"repo://team-a/infra", "scope://team-a"},
			}},
		},
	}
	store := NewScopedAPITokenStore(db)

	resolution, ok, err := store.ResolveIdentityAPITokenHash(context.Background(), "sha256:token", now)
	if err != nil {
		t.Fatalf("ResolveIdentityAPITokenHash() error = %v", err)
	}
	if !ok {
		t.Fatal("ResolveIdentityAPITokenHash() ok = false, want true")
	}
	if got, want := resolution.SubjectClass, "service_principal"; got != want {
		t.Fatalf("SubjectClass = %q, want %q", got, want)
	}
	if !strings.HasPrefix(resolution.SubjectIDHash, "sha256:") {
		t.Fatalf("SubjectIDHash = %q, want sha256 hash", resolution.SubjectIDHash)
	}
	if strings.Contains(resolution.SubjectIDHash, "sp_automation") {
		t.Fatalf("SubjectIDHash leaked raw service principal id: %q", resolution.SubjectIDHash)
	}
	if got, want := resolution.RoleIDs, []string{"role_automation"}; !equalStringSlices(got, want) {
		t.Fatalf("RoleIDs = %#v, want %#v", got, want)
	}

	for _, want := range []string{
		"JOIN identity_service_principals service_principal",
		"service_principal.status = 'active'",
		"service_principal.disabled_at IS NULL",
	} {
		if !strings.Contains(db.queries[0].query, want) {
			t.Fatalf("subject query missing %q:\n%s", want, db.queries[0].query)
		}
	}
	for _, want := range []string{
		"JOIN identity_service_principal_roles role_assignment",
		"role_assignment.status = 'active'",
	} {
		if !strings.Contains(db.queries[1].query, want) {
			t.Fatalf("role query missing %q:\n%s", want, db.queries[1].query)
		}
	}
}

func TestScopedAPITokenStoreRejectsIdentityTokenWithoutActiveRoles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 18, 45, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"sha256:token",
				"personal",
				"tenant_a",
				"workspace_a",
				"sha256:user-subject",
				"",
				"sha256:token-policy",
			}}},
			{rows: [][]any{}},
		},
	}
	store := NewScopedAPITokenStore(db)

	_, ok, err := store.ResolveIdentityAPITokenHash(context.Background(), "sha256:token", now)
	if err != nil {
		t.Fatalf("ResolveIdentityAPITokenHash() error = %v", err)
	}
	if ok {
		t.Fatal("ResolveIdentityAPITokenHash() ok = true, want false for token without active roles")
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d; queries = %#v", got, want, db.queries)
	}
}

func TestScopedAPITokenStoreResolveIdentityTokenRejectsBlankHash(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewScopedAPITokenStore(db)
	_, ok, err := store.ResolveIdentityAPITokenHash(context.Background(), "", time.Now())
	if err == nil {
		t.Fatal("ResolveIdentityAPITokenHash() error = nil, want validation error")
	}
	if ok {
		t.Fatal("ResolveIdentityAPITokenHash() ok = true, want false")
	}
	if len(db.queries) != 0 {
		t.Fatalf("query count = %d, want 0", len(db.queries))
	}
}

func TestScopedAPITokenStoreResolvePermissionGrantsForRoles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"ask_search", "ask_reasoning"},
				{"ask_search", "documentation_semantic"},
				{"repository_content", "source_content"},
			}},
		},
	}
	store := NewScopedAPITokenStore(db)

	features, dataClasses, err := store.ResolvePermissionGrantsForRoles(
		context.Background(),
		"tenant_a",
		[]string{"role_reader", "role_reader", " "},
		now,
	)
	if err != nil {
		t.Fatalf("ResolvePermissionGrantsForRoles() error = %v", err)
	}
	if got, want := features, []string{"ask_search", "repository_content"}; !equalStringSlices(got, want) {
		t.Fatalf("features = %#v, want %#v", got, want)
	}
	if got, want := dataClasses, []string{"ask_reasoning", "documentation_semantic", "source_content"}; !equalStringSlices(got, want) {
		t.Fatalf("dataClasses = %#v, want %#v", got, want)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "FROM identity_role_grants grant") {
		t.Fatalf("permission query missing identity_role_grants:\n%s", db.queries[0].query)
	}
}

func TestScopedAPITokenStoreResolvePermissionGrantsForRolesEmptyInputs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewScopedAPITokenStore(db)

	features, dataClasses, err := store.ResolvePermissionGrantsForRoles(context.Background(), "tenant_a", nil, now)
	if err != nil {
		t.Fatalf("ResolvePermissionGrantsForRoles() empty roles error = %v", err)
	}
	if len(features) != 0 || len(dataClasses) != 0 {
		t.Fatalf("empty roles grants = %#v / %#v, want empty", features, dataClasses)
	}
	if len(db.queries) != 0 {
		t.Fatalf("empty roles query count = %d, want 0", len(db.queries))
	}

	if _, _, err := store.ResolvePermissionGrantsForRoles(context.Background(), "", []string{"role_reader"}, now); err != nil {
		t.Fatalf("ResolvePermissionGrantsForRoles() blank tenant error = %v", err)
	}
	if len(db.queries) != 0 {
		t.Fatalf("blank tenant query count = %d, want 0", len(db.queries))
	}
}

func TestScopedAPITokenStoreMarksIdentityTokenUsedHashOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 19, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewScopedAPITokenStore(db)

	if err := store.MarkIdentityAPITokenUsed(context.Background(), "sha256:token", now); err != nil {
		t.Fatalf("MarkIdentityAPITokenUsed() error = %v", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE identity_token_metadata",
		"last_used_at = $2",
		"token_hash = $1",
		"revoked_at IS NULL",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("mark-used query missing %q:\n%s", want, query)
		}
	}
	if fakeExecArgsContain(db.execs[0].args, "raw-personal-token") {
		t.Fatalf("mark-used args leaked raw token: %#v", db.execs[0].args)
	}
}
