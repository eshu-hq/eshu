package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBrowserSessionStoreCreatesOIDCRefreshProofMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 13, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewBrowserSessionStore(db)

	err := store.CreateSession(context.Background(), BrowserSessionRecord{
		SessionHash:              "sha256:session",
		CSRFTokenHash:            "sha256:csrf",
		TenantID:                 "tenant_a",
		WorkspaceID:              "workspace_a",
		SubjectIDHash:            "sha256:subject",
		SubjectClass:             "external_oidc_user",
		PolicyRevisionHash:       "sha256:policy",
		RoleIDs:                  []string{"developer"},
		AllowedScopeIDs:          []string{"scope_a"},
		ExternalProviderConfigID: "okta-dev",
		ExternalSubjectIDHash:    "sha256:subject",
		ExternalGroupHashes:      []string{"sha256:group"},
		ExternalAuthValidatedAt:  now,
		ExternalAuthStaleAfter:   now.Add(15 * time.Minute),
		IssuedAt:                 now,
		LastSeenAt:               now,
		IdleExpiresAt:            now.Add(30 * time.Minute),
		AbsoluteExpiresAt:        now.Add(12 * time.Hour),
		UpdatedAt:                now,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	for _, want := range []string{
		"external_provider_config_id",
		"external_subject_id_hash",
		"external_auth_validated_at",
		"external_auth_stale_after",
		"external_group_hashes",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("create query missing %q:\n%s", want, query)
		}
	}
	for _, forbidden := range []string{"id-token", "access-token", "refresh-token", "Eshu Developers", "user@example.test"} {
		if fakeExecArgsContain(db.execs[0].args, forbidden) {
			t.Fatalf("create args leaked raw provider value %q: %#v", forbidden, db.execs[0].args)
		}
	}
}

func TestBrowserSessionStoreRevokesStaleOIDCSessionBeforeReturningAuth(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 13, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewBrowserSessionStore(db)

	_, ok, err := store.ResolveSessionHash(
		context.Background(),
		"sha256:session",
		"sha256:csrf",
		false,
		now,
		30*time.Minute,
	)
	if !errors.Is(err, ErrBrowserSessionRefreshRequired) {
		t.Fatalf("ResolveSessionHash() error = %v, want ErrBrowserSessionRefreshRequired", err)
	}
	if ok {
		t.Fatal("ResolveSessionHash() ok = true, want false")
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	if len(db.queries) != 0 {
		t.Fatalf("query count = %d, want 0 after stale revocation", len(db.queries))
	}
	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE browser_sessions",
		"external_auth_stale_after <= $2",
		"subject_class = 'external_oidc_user'",
		"SET revoked_at = $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("resolve query missing %q:\n%s", want, query)
		}
	}
}

func TestBrowserSessionStoreRevokesOIDCSessionMissingProofMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 13, 40, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewBrowserSessionStore(db)

	_, ok, err := store.ResolveSessionHash(
		context.Background(),
		"sha256:session",
		"sha256:csrf",
		false,
		now,
		30*time.Minute,
	)
	if !errors.Is(err, ErrBrowserSessionRefreshRequired) {
		t.Fatalf("ResolveSessionHash() error = %v, want ErrBrowserSessionRefreshRequired", err)
	}
	if ok {
		t.Fatal("ResolveSessionHash() ok = true, want false")
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	if len(db.queries) != 0 {
		t.Fatalf("query count = %d, want 0 after missing-proof revocation", len(db.queries))
	}
	query := db.execs[0].query
	for _, want := range []string{
		"external_auth_validated_at IS NULL",
		"external_auth_stale_after IS NULL",
		"subject_class = 'external_oidc_user'",
		"SET revoked_at = $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("resolve query missing %q:\n%s", want, query)
		}
	}
}

func TestBrowserSessionStoreRevokesStaleOIDCSessionBeforeCSRFFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 13, 45, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewBrowserSessionStore(db)

	_, ok, err := store.ResolveSessionHash(
		context.Background(),
		"sha256:session",
		"",
		true,
		now,
		30*time.Minute,
	)
	if !errors.Is(err, ErrBrowserSessionRefreshRequired) {
		t.Fatalf("ResolveSessionHash() error = %v, want ErrBrowserSessionRefreshRequired", err)
	}
	if ok {
		t.Fatal("ResolveSessionHash() ok = true, want false")
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	if len(db.queries) != 0 {
		t.Fatalf("query count = %d, want 0 after stale revocation", len(db.queries))
	}
}
