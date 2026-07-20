// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBootstrapDefinitionsIncludeGitHubLoginState(t *testing.T) {
	t.Parallel()

	var github Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "identity_github_login" {
			github = def
			break
		}
	}
	if github.Name == "" {
		t.Fatal("identity_github_login definition missing")
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS identity_github_login_states",
		"state_hash TEXT PRIMARY KEY",
		"redirect_uri_hash TEXT NOT NULL",
		"REFERENCES identity_provider_configs(provider_config_id) ON DELETE CASCADE",
	} {
		if !strings.Contains(github.SQL, want) {
			t.Fatalf("github login SQL missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"nonce_hash",
		"raw_group",
		"team_name",
		"org_name",
		"access_token",
		"client_secret",
		"email TEXT",
	} {
		if strings.Contains(strings.ToLower(github.SQL), strings.ToLower(forbidden)) {
			t.Fatalf("github login SQL contains forbidden marker %q", forbidden)
		}
	}
}

func TestGitHubLoginStateStoreCreatesAndConsumesHashesOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 11, 0, 0, 0, time.UTC)
	expires := now.Add(10 * time.Minute)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"sha256:state",
				"github-dev",
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
	store := NewGitHubLoginStore(db)

	if err := store.CreateState(context.Background(), GitHubLoginStateRecord{
		StateHash:        "sha256:state",
		ProviderConfigID: "github-dev",
		ProviderKeyHash:  "sha256:provider-key",
		IssuerHash:       "sha256:base-url",
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
		fakeExecArgsContain(db.execs[0].args, "https://github.com") ||
		fakeExecArgsContain(db.execs[0].args, "client-id") {
		t.Fatalf("create args leaked raw state or provider data: %#v", db.execs[0].args)
	}
	for _, want := range []string{
		"WITH provider AS",
		"INSERT INTO identity_provider_configs",
		"'external_github'",
		"ON CONFLICT (provider_config_id) DO UPDATE",
		"WHERE identity_provider_configs.tombstoned_at IS NULL",
		"INSERT INTO identity_github_login_states",
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
	if record.StateHash != "sha256:state" || record.ProviderConfigID != "github-dev" ||
		record.ReturnToPath != "/console" {
		t.Fatalf("consumed record = %#v, want state metadata", record)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(db.queries))
	}
	for _, want := range []string{
		"UPDATE identity_github_login_states",
		"consumed_at IS NULL",
		"expires_at > $2",
		"RETURNING",
	} {
		if !strings.Contains(db.queries[0].query, want) {
			t.Fatalf("consume query missing %q:\n%s", want, db.queries[0].query)
		}
	}
}

func TestGitHubLoginStateStoreRejectsBlankFieldsBeforeAnyDBWrite(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewGitHubLoginStore(db)
	now := time.Date(2026, 6, 22, 11, 0, 0, 0, time.UTC)

	if err := store.CreateState(context.Background(), GitHubLoginStateRecord{
		StateHash: "sha256:state",
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Minute),
	}); err == nil {
		t.Fatal("CreateState() error = nil, want error for missing provider/tenant/workspace hashes")
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec count = %d, want 0 (must fail before touching the database)", len(db.execs))
	}
}
