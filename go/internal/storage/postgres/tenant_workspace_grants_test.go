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

func TestBootstrapDefinitionsIncludeTenantWorkspaceGrants(t *testing.T) {
	t.Parallel()

	var grants Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "tenant_workspace_grants" {
			grants = def
			break
		}
	}
	if grants.Name == "" {
		t.Fatal("tenant_workspace_grants definition missing")
	}

	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS tenants",
		"CREATE TABLE IF NOT EXISTS workspaces",
		"CREATE TABLE IF NOT EXISTS tenant_scope_grants",
		"CREATE TABLE IF NOT EXISTS tenant_repository_grants",
		"REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE",
		"PRIMARY KEY (tenant_id, workspace_id, scope_id, subject_class)",
		"PRIMARY KEY (tenant_id, workspace_id, repo_id, subject_class)",
		"tenant_scope_grants_active_idx",
		"tenant_repository_grants_active_idx",
	} {
		if !strings.Contains(grants.SQL, want) {
			t.Fatalf("tenant workspace grant SQL missing %q", want)
		}
	}

	for _, forbidden := range []string{
		"display_name",
		"tenant_name",
		"workspace_name",
		"private_url",
		"credential_handle",
		"raw_token",
	} {
		if strings.Contains(strings.ToLower(grants.SQL), forbidden) {
			t.Fatalf("tenant workspace grant SQL contains forbidden column marker %q", forbidden)
		}
	}
}

func TestTenantWorkspaceGrantStoreUpsertsScopeGrantIdempotently(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewTenantWorkspaceGrantStore(db)

	if err := store.UpsertTenant(ctxForTenantGrantTest(), TenantRecord{
		TenantID:           "tenant_a",
		Status:             "active",
		DisplayHandleHash:  "sha256:tenant",
		PolicyRevisionHash: "sha256:policy",
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("UpsertTenant() error = %v", err)
	}
	if err := store.UpsertWorkspace(ctxForTenantGrantTest(), WorkspaceRecord{
		TenantID:           "tenant_a",
		WorkspaceID:        "workspace_a",
		Status:             "active",
		DisplayHandleHash:  "sha256:workspace",
		PolicyRevisionHash: "sha256:policy",
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("UpsertWorkspace() error = %v", err)
	}
	grant := TenantScopeGrant{
		TenantID:           "tenant_a",
		WorkspaceID:        "workspace_a",
		ScopeID:            "scope_a",
		SubjectClass:       "runtime",
		GrantSource:        "operator",
		PolicyRevisionHash: "sha256:policy",
		EffectiveAt:        now,
		UpdatedAt:          now,
	}
	if err := store.UpsertScopeGrant(ctxForTenantGrantTest(), grant); err != nil {
		t.Fatalf("UpsertScopeGrant() first error = %v", err)
	}
	if err := store.UpsertScopeGrant(ctxForTenantGrantTest(), grant); err != nil {
		t.Fatalf("UpsertScopeGrant() duplicate error = %v", err)
	}

	if got := len(db.execs); got != 4 {
		t.Fatalf("exec count = %d, want 4", got)
	}
	scopeQuery := db.execs[2].query
	if !strings.Contains(scopeQuery, "ON CONFLICT (tenant_id, workspace_id, scope_id, subject_class) DO UPDATE") {
		t.Fatalf("scope grant upsert is not conflict-idempotent:\n%s", scopeQuery)
	}
	for _, call := range db.execs {
		if fakeExecArgsContain(call.args, "tenant name") || fakeExecArgsContain(call.args, "workspace name") {
			t.Fatalf("upsert persisted display text in args: %#v", call.args)
		}
	}
}

func TestTenantWorkspaceGrantStoreListsOnlyActiveScopeGrants(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 9, 13, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"tenant_a",
				"workspace_a",
				"scope_a",
				"runtime",
				"operator",
				"sha256:policy",
				now.Add(-time.Hour),
				sql.NullTime{},
			}},
		}},
	}
	store := NewTenantWorkspaceGrantStore(db)

	grants, err := store.ListScopeGrants(ctxForTenantGrantTest(), TenantWorkspaceGrantQuery{
		TenantID:     "tenant_a",
		WorkspaceID:  "workspace_a",
		SubjectClass: "runtime",
		AsOf:         now,
		Limit:        25,
	})
	if err != nil {
		t.Fatalf("ListScopeGrants() error = %v", err)
	}
	if len(grants) != 1 || grants[0].ScopeID != "scope_a" {
		t.Fatalf("ListScopeGrants() = %#v, want scope_a", grants)
	}

	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(db.queries))
	}
	query := db.queries[0].query
	for _, want := range []string{
		"JOIN tenants t",
		"JOIN workspaces w",
		"t.status = 'active'",
		"w.status = 'active'",
		"g.tombstoned_at IS NULL",
		"g.effective_at <= $4",
		"(g.expires_at IS NULL OR g.expires_at > $4)",
		"g.scope_id = ANY($5::text[])",
		"ORDER BY g.scope_id ASC",
		"LIMIT $6",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("scope grant query missing %q:\n%s", want, query)
		}
	}
	wantArgs := []any{"tenant_a", "workspace_a", "runtime", now, []string{}, 25}
	if !sameArgs(db.queries[0].args, wantArgs) {
		t.Fatalf("scope grant query args = %#v, want %#v", db.queries[0].args, wantArgs)
	}
}

func TestTenantWorkspaceGrantStoreListsOnlyActiveRepositoryGrants(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 9, 14, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"tenant_a",
				"workspace_a",
				"repo_a",
				"scope_a",
				"runtime",
				"operator",
				"sha256:policy",
				now.Add(-time.Hour),
				sql.NullTime{Time: now.Add(time.Hour), Valid: true},
			}},
		}},
	}
	store := NewTenantWorkspaceGrantStore(db)

	grants, err := store.ListRepositoryGrants(ctxForTenantGrantTest(), TenantWorkspaceGrantQuery{
		TenantID:     "tenant_a",
		WorkspaceID:  "workspace_a",
		SubjectClass: "runtime",
		AsOf:         now,
		Limit:        50,
	})
	if err != nil {
		t.Fatalf("ListRepositoryGrants() error = %v", err)
	}
	if len(grants) != 1 || grants[0].RepoID != "repo_a" || grants[0].ScopeID != "scope_a" {
		t.Fatalf("ListRepositoryGrants() = %#v, want repo_a/scope_a", grants)
	}

	query := db.queries[0].query
	for _, want := range []string{
		"JOIN tenants t",
		"JOIN workspaces w",
		"JOIN tenant_scope_grants sg",
		"g.tombstoned_at IS NULL",
		"sg.tombstoned_at IS NULL",
		"(g.expires_at IS NULL OR g.expires_at > $4)",
		"g.scope_id = ANY($5::text[])",
		"ORDER BY g.repo_id ASC",
		"LIMIT $6",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("repository grant query missing %q:\n%s", want, query)
		}
	}
}

func TestTenantWorkspaceGrantStoreRejectsUnboundedQueries(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewTenantWorkspaceGrantStore(db)

	_, err := store.ListScopeGrants(ctxForTenantGrantTest(), TenantWorkspaceGrantQuery{
		TenantID:    "tenant_a",
		WorkspaceID: "",
		AsOf:        time.Date(2026, 6, 9, 15, 0, 0, 0, time.UTC),
		Limit:       10,
	})
	if err == nil {
		t.Fatal("ListScopeGrants() error = nil, want validation error")
	}
	if len(db.queries) != 0 {
		t.Fatalf("query count after validation failure = %d, want 0", len(db.queries))
	}
}

func ctxForTenantGrantTest() context.Context {
	return context.Background()
}

func sameArgs(got []any, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		switch wantValue := want[i].(type) {
		case time.Time:
			gotValue, ok := got[i].(time.Time)
			if !ok || !gotValue.Equal(wantValue) {
				return false
			}
		case []string:
			gotValue, ok := got[i].([]string)
			if !ok || len(gotValue) != len(wantValue) {
				return false
			}
			for index := range gotValue {
				if gotValue[index] != wantValue[index] {
					return false
				}
			}
		default:
			if got[i] != wantValue {
				return false
			}
		}
	}
	return true
}
