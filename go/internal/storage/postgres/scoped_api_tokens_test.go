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
