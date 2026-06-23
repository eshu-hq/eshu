package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBrowserSessionStoreListsStaleOIDCSessionsWithBoundedBatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"sha256:session1", "okta-dev", "sha256:subject1", "tenant_a", "workspace_a",
					"sha256:policy", []byte(`["developer"]`), false, []byte(`["scope_a"]`), []byte(`[]`),
					now.Add(-2 * time.Minute), now.Add(-time.Minute)},
				{"sha256:session2", "okta-dev", "sha256:subject2", "tenant_a", "workspace_a",
					"sha256:policy", []byte(`["viewer"]`), false, []byte(`["scope_b"]`), []byte(`[]`),
					now.Add(-3 * time.Minute), now.Add(-2 * time.Minute)},
			}},
		},
	}
	store := NewBrowserSessionStore(db)

	sessions, err := store.ListStaleOIDCSessions(context.Background(), now, 10)
	if err != nil {
		t.Fatalf("ListStaleOIDCSessions() error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions count = %d, want 2", len(sessions))
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(db.queries))
	}
	query := db.queries[0].query
	for _, want := range []string{
		"browser_sessions",
		"external_auth_stale_after",
		"external_provider_config_id",
		"external_subject_id_hash",
		"subject_class = 'external_oidc_user'",
		"revoked_at IS NULL",
		"LIMIT",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("list stale query missing %q:\n%s", want, query)
		}
	}
	// Verify results are unmarshalled correctly.
	if sessions[0].SessionHash != "sha256:session1" || sessions[0].ExternalProviderConfigID != "okta-dev" {
		t.Fatalf("first session = %#v, want sha256:session1 / okta-dev", sessions[0])
	}
	if sessions[1].SessionHash != "sha256:session2" {
		t.Fatalf("second session hash = %q, want sha256:session2", sessions[1].SessionHash)
	}
}

func TestBrowserSessionStoreListStaleOIDCSessionsEmptyResult(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 10, 5, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: nil}},
	}
	store := NewBrowserSessionStore(db)

	sessions, err := store.ListStaleOIDCSessions(context.Background(), now, 10)
	if err != nil {
		t.Fatalf("ListStaleOIDCSessions() empty error = %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions count = %d, want 0", len(sessions))
	}
}

func TestBrowserSessionStoreUpdatesOIDCSessionAuthProof(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 10, 10, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewBrowserSessionStore(db)

	err := store.UpdateOIDCSessionAuthProof(context.Background(), OIDCSessionAuthProofUpdate{
		SessionHash:             "sha256:session1",
		ExternalAuthValidatedAt: now,
		ExternalAuthStaleAfter:  now.Add(15 * time.Minute),
		PolicyRevisionHash:      "sha256:policy-v2",
		RoleIDs:                 []string{"developer"},
		AllowedScopeIDs:         []string{"scope_a"},
		AllowedRepositoryIDs:    []string{"repo_a"},
		UpdatedAt:               now,
	})
	if err != nil {
		t.Fatalf("UpdateOIDCSessionAuthProof() error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE browser_sessions",
		"external_auth_validated_at",
		"external_auth_stale_after",
		"policy_revision_hash",
		"role_ids",
		"WHERE session_hash = $1",
		"subject_class = 'external_oidc_user'",
		"revoked_at IS NULL",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("update auth proof query missing %q:\n%s", want, query)
		}
	}
	// Verify no raw group names, raw tokens, or emails in args.
	for _, forbidden := range []string{"Eshu Developers", "id-token", "access-token", "user@example.test"} {
		if fakeExecArgsContain(db.execs[0].args, forbidden) {
			t.Fatalf("update args leaked raw provider value %q: %#v", forbidden, db.execs[0].args)
		}
	}
}

func TestBrowserSessionStoreUpdateOIDCSessionAuthProofRequiresSessionHash(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 10, 15, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewBrowserSessionStore(db)

	err := store.UpdateOIDCSessionAuthProof(context.Background(), OIDCSessionAuthProofUpdate{
		SessionHash:             "",
		ExternalAuthValidatedAt: now,
		ExternalAuthStaleAfter:  now.Add(15 * time.Minute),
		PolicyRevisionHash:      "sha256:policy",
		UpdatedAt:               now,
	})
	if err == nil {
		t.Fatal("UpdateOIDCSessionAuthProof() with empty session hash must return error")
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec count = %d, want 0 when validation fails", len(db.execs))
	}
}

func TestBrowserSessionStoreSchemaIncludesExternalAuthStaleIndex(t *testing.T) {
	t.Parallel()

	schema := BrowserSessionSchemaSQL()
	for _, want := range []string{
		"browser_sessions_external_auth_stale_idx",
		"external_auth_stale_after",
		"revoked_at IS NULL",
		"external_auth_stale_after IS NOT NULL",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("browser session schema missing stale index component %q", want)
		}
	}
}
