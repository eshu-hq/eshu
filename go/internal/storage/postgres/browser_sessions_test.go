package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBootstrapDefinitionsIncludeBrowserSessions(t *testing.T) {
	t.Parallel()

	var sessions Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "browser_sessions" {
			sessions = def
			break
		}
	}
	if sessions.Name == "" {
		t.Fatal("browser_sessions definition missing")
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS browser_sessions",
		"session_hash TEXT PRIMARY KEY",
		"csrf_token_hash TEXT NOT NULL",
		"idle_expires_at TIMESTAMPTZ NOT NULL",
		"absolute_expires_at TIMESTAMPTZ NOT NULL",
		"external_provider_config_id TEXT NULL",
		"external_subject_id_hash TEXT NULL",
		"external_auth_validated_at TIMESTAMPTZ NULL",
		"external_auth_stale_after TIMESTAMPTZ NULL",
		"ALTER TABLE browser_sessions",
		"ADD COLUMN IF NOT EXISTS role_ids JSONB NOT NULL DEFAULT '[]'::jsonb",
		"ADD COLUMN IF NOT EXISTS external_auth_stale_after TIMESTAMPTZ NULL",
		"FOREIGN KEY (tenant_id, workspace_id)",
		"REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE",
		"browser_sessions_active_idx",
	} {
		if !strings.Contains(sessions.SQL, want) {
			t.Fatalf("browser session SQL missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"session_id",
		"raw_session",
		"cookie_value",
		"csrf_token TEXT",
		"token_value",
		"tenant_name",
		"workspace_name",
	} {
		if strings.Contains(strings.ToLower(sessions.SQL), strings.ToLower(forbidden)) {
			t.Fatalf("browser session SQL contains forbidden marker %q", forbidden)
		}
	}
}

func TestBrowserSessionStoreCreatesHashOnlyRecord(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 14, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewBrowserSessionStore(db)

	err := store.CreateSession(context.Background(), BrowserSessionRecord{
		SessionHash:          "sha256:session",
		CSRFTokenHash:        "sha256:csrf",
		TenantID:             "tenant_a",
		WorkspaceID:          "workspace_a",
		SubjectIDHash:        "sha256:subject",
		SubjectClass:         "human",
		PolicyRevisionHash:   "sha256:policy",
		AllScopes:            false,
		AllowedScopeIDs:      []string{"scope_a", "scope_b"},
		AllowedRepositoryIDs: []string{"repo_a"},
		IssuedAt:             now,
		LastSeenAt:           now,
		IdleExpiresAt:        now.Add(30 * time.Minute),
		AbsoluteExpiresAt:    now.Add(12 * time.Hour),
		UpdatedAt:            now,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO browser_sessions") {
		t.Fatalf("create query missing insert:\n%s", db.execs[0].query)
	}
	if !strings.Contains(db.execs[0].query, "ON CONFLICT (session_hash) DO UPDATE") {
		t.Fatalf("create query missing conflict clause:\n%s", db.execs[0].query)
	}
	if fakeExecArgsContain(db.execs[0].args, "session-secret") ||
		fakeExecArgsContain(db.execs[0].args, "csrf-secret") {
		t.Fatalf("create args leaked raw secret: %#v", db.execs[0].args)
	}
}

func TestBrowserSessionStoreCreatesSessionWithWorkspacePolicyFallback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 14, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewBrowserSessionStore(db)

	err := store.CreateSession(context.Background(), BrowserSessionRecord{
		SessionHash:       "sha256:session",
		CSRFTokenHash:     "sha256:csrf",
		TenantID:          "tenant_a",
		WorkspaceID:       "workspace_a",
		AllScopes:         true,
		IssuedAt:          now,
		LastSeenAt:        now,
		IdleExpiresAt:     now.Add(30 * time.Minute),
		AbsoluteExpiresAt: now.Add(12 * time.Hour),
		UpdatedAt:         now,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	for _, want := range []string{
		"FROM workspaces ws",
		"JOIN tenants ten ON ten.tenant_id = ws.tenant_id",
		"COALESCE(NULLIF($7, ''), ws.policy_revision_hash)",
		"ws.tenant_id = $3",
		"ws.workspace_id = $4",
		"ws.status = 'active'",
		"ten.status = 'active'",
	} {
		if !strings.Contains(db.execs[0].query, want) {
			t.Fatalf("create query missing %q:\n%s", want, db.execs[0].query)
		}
	}
}

func TestBrowserSessionStoreRejectsInactiveWorkspaceOnCreate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 14, 45, 0, 0, time.UTC)
	db := &fakeExecQueryer{execResults: []sql.Result{rowsAffectedResult{rowsAffected: 0}}}
	store := NewBrowserSessionStore(db)

	err := store.CreateSession(context.Background(), BrowserSessionRecord{
		SessionHash:       "sha256:session",
		CSRFTokenHash:     "sha256:csrf",
		TenantID:          "tenant_a",
		WorkspaceID:       "workspace_a",
		IssuedAt:          now,
		LastSeenAt:        now,
		IdleExpiresAt:     now.Add(30 * time.Minute),
		AbsoluteExpiresAt: now.Add(12 * time.Hour),
		UpdatedAt:         now,
	})
	if err == nil {
		t.Fatal("CreateSession() error = nil, want inactive workspace error")
	}
	if !strings.Contains(err.Error(), "active tenant/workspace") {
		t.Fatalf("CreateSession() error = %v, want active tenant/workspace", err)
	}
}

func TestBrowserSessionStoreResolvesOnlyActiveUnrevokedSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 15, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 0}},
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"sha256:session",
				"sha256:csrf",
				"tenant_a",
				"workspace_a",
				"sha256:subject",
				"human",
				"sha256:policy",
				[]byte(`[]`),
				false,
				[]byte(`["scope_a","scope_b"]`),
				[]byte(`["repo_a"]`),
				now.Add(-time.Hour),
				now.Add(-time.Minute),
				now.Add(29 * time.Minute),
				now.Add(11 * time.Hour),
				sql.NullTime{},
				true,
			}},
		}},
	}
	store := NewBrowserSessionStore(db)

	session, ok, err := store.ResolveSessionHash(
		context.Background(),
		"sha256:session",
		"sha256:csrf",
		true,
		now,
		30*time.Minute,
	)
	if err != nil {
		t.Fatalf("ResolveSessionHash() error = %v", err)
	}
	if !ok {
		t.Fatal("ResolveSessionHash() ok = false, want true")
	}
	if session.TenantID != "tenant_a" || session.WorkspaceID != "workspace_a" ||
		session.SubjectClass != "human" || session.SubjectIDHash != "sha256:subject" {
		t.Fatalf("resolved session = %#v, want tenant/workspace subject", session)
	}
	if got, want := session.AllowedScopeIDs, []string{"scope_a", "scope_b"}; !sameStrings(got, want) {
		t.Fatalf("allowed scopes = %#v, want %#v", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"JOIN tenants ten",
		"JOIN workspaces ws",
		"sess.session_hash = $1",
		"sess.revoked_at IS NULL",
		"sess.idle_expires_at > $4",
		"sess.absolute_expires_at > $4",
		"SET last_seen_at = $4",
		"idle_expires_at = LEAST(sess.absolute_expires_at, $5)",
		"ten.status = 'active'",
		"ws.status = 'active'",
		"sess.policy_revision_hash = ws.policy_revision_hash",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("resolve query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[1], "sha256:csrf"; got != want {
		t.Fatalf("csrf query arg = %v, want %v", got, want)
	}
	if got, want := db.queries[0].args[2], true; got != want {
		t.Fatalf("require csrf arg = %v, want %v", got, want)
	}
	if got, want := db.queries[0].args[4], now.Add(30*time.Minute); got != want {
		t.Fatalf("next idle expiry arg = %v, want %v", got, want)
	}
}

func TestBrowserSessionStoreRejectsCSRFMismatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 15, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 0}},
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"sha256:session",
				"sha256:expected-csrf",
				"tenant_a",
				"workspace_a",
				"sha256:subject",
				"human",
				"sha256:policy",
				[]byte(`[]`),
				false,
				[]byte(`[]`),
				[]byte(`[]`),
				now.Add(-time.Hour),
				now.Add(-time.Minute),
				now.Add(29 * time.Minute),
				now.Add(11 * time.Hour),
				sql.NullTime{},
				false,
			}},
		}},
	}
	store := NewBrowserSessionStore(db)

	_, ok, err := store.ResolveSessionHash(
		context.Background(),
		"sha256:session",
		"sha256:wrong-csrf",
		true,
		now,
		30*time.Minute,
	)
	if !errors.Is(err, ErrBrowserSessionCSRFInvalid) {
		t.Fatalf("ResolveSessionHash() error = %v, want ErrBrowserSessionCSRFInvalid", err)
	}
	if ok {
		t.Fatal("ResolveSessionHash() ok = true, want false")
	}
}

func TestBrowserSessionStoreRevokesByHash(t *testing.T) {
	t.Parallel()

	revokedAt := time.Date(2026, 6, 21, 16, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewBrowserSessionStore(db)

	if err := store.RevokeSession(context.Background(), "sha256:session", revokedAt); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	for _, want := range []string{
		"UPDATE browser_sessions",
		"SET revoked_at = $2",
		"WHERE session_hash = $1",
		"AND revoked_at IS NULL",
	} {
		if !strings.Contains(db.execs[0].query, want) {
			t.Fatalf("revoke query missing %q:\n%s", want, db.execs[0].query)
		}
	}
}

func TestBrowserSessionStoreSwitchesActiveWorkspaceByHash(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 17, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"sha256:session",
				"sha256:csrf",
				"tenant_b",
				"workspace_b",
				"sha256:subject",
				"human",
				"sha256:policy_b",
				[]byte(`[]`),
				true,
				[]byte(`[]`),
				[]byte(`[]`),
				now.Add(-time.Hour),
				now,
				now.Add(30 * time.Minute),
				now.Add(11 * time.Hour),
				sql.NullTime{},
				true,
			}},
		}},
	}
	store := NewBrowserSessionStore(db)

	session, ok, err := store.SwitchSessionWorkspace(
		context.Background(),
		"sha256:session",
		"tenant_b",
		"workspace_b",
		now,
	)
	if err != nil {
		t.Fatalf("SwitchSessionWorkspace() error = %v", err)
	}
	if !ok {
		t.Fatal("SwitchSessionWorkspace() ok = false, want true")
	}
	if session.TenantID != "tenant_b" || session.WorkspaceID != "workspace_b" {
		t.Fatalf("switched session = %#v, want tenant_b/workspace_b", session)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"UPDATE browser_sessions sess",
		"FROM workspaces ws",
		"JOIN tenants ten",
		"sess.session_hash = $1",
		"ws.tenant_id = $2",
		"ws.workspace_id = $3",
		"ws.status = 'active'",
		"ten.status = 'active'",
		"sess.revoked_at IS NULL",
		"sess.all_scopes = true",
		"sess.idle_expires_at > $4",
		"sess.absolute_expires_at > $4",
		"RETURNING",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("switch query missing %q:\n%s", want, query)
		}
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
