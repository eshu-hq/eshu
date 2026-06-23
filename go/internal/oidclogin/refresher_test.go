package oidclogin

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRefresherRevokesSessionWhenGroupsNoLongerMapToGrants(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 11, 0, 0, 0, time.UTC)
	store := &fakeSessionRefreshStore{
		stale: []StaleSession{{
			SessionHash:              "sha256:session1",
			ExternalProviderConfigID: "okta-dev",
			ExternalSubjectIDHash:    "sha256:subject1",
			TenantID:                 "tenant_a",
			WorkspaceID:              "workspace_a",
			PolicyRevisionHash:       "sha256:policy",
			RoleIDs:                  []string{"developer"},
		}},
	}
	resolver := &fakeRefreshResolver{
		// External group removed: subject no longer resolves to any grant.
		ok: false,
	}
	refresher := NewRefresher(store, resolver, RefreshConfig{
		BatchSize:     10,
		Window:        15 * time.Minute,
		SubjectLookup: alwaysActiveSubjectLookup{},
	}, WithRefreshNow(func() time.Time { return now }))

	outcome, err := refresher.RefreshOnce(context.Background())
	if err != nil {
		t.Fatalf("RefreshOnce() error = %v", err)
	}
	if outcome.Revoked != 1 {
		t.Fatalf("revoked = %d, want 1", outcome.Revoked)
	}
	if outcome.Refreshed != 0 {
		t.Fatalf("refreshed = %d, want 0", outcome.Refreshed)
	}
	if len(store.revoked) != 1 || store.revoked[0] != "sha256:session1" {
		t.Fatalf("revoked sessions = %#v, want [sha256:session1]", store.revoked)
	}
	if len(store.updated) != 0 {
		t.Fatalf("updated sessions = %#v, want none", store.updated)
	}
}

func TestRefresherPreservesSessionAndExtendsWindowWhenStillAuthorized(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 11, 10, 0, 0, time.UTC)
	store := &fakeSessionRefreshStore{
		stale: []StaleSession{{
			SessionHash:              "sha256:session1",
			ExternalProviderConfigID: "okta-dev",
			ExternalSubjectIDHash:    "sha256:subject1",
			TenantID:                 "tenant_a",
			WorkspaceID:              "workspace_a",
			PolicyRevisionHash:       "sha256:policy",
			ExternalGroupHashes:      []string{"sha256:group"},
			RoleIDs:                  []string{"developer"},
		}},
	}
	resolver := &fakeRefreshResolver{
		ok: true,
		resolution: GrantResolution{
			RoleIDs:            []string{"developer"},
			PolicyRevisionHash: "sha256:policy",
			AllowedScopeIDs:    []string{"scope_a"},
		},
	}
	refresher := NewRefresher(store, resolver, RefreshConfig{
		BatchSize:     10,
		Window:        15 * time.Minute,
		SubjectLookup: alwaysActiveSubjectLookup{},
	}, WithRefreshNow(func() time.Time { return now }))

	outcome, err := refresher.RefreshOnce(context.Background())
	if err != nil {
		t.Fatalf("RefreshOnce() error = %v", err)
	}
	if outcome.Refreshed != 1 {
		t.Fatalf("refreshed = %d, want 1", outcome.Refreshed)
	}
	if outcome.Revoked != 0 {
		t.Fatalf("revoked = %d, want 0", outcome.Revoked)
	}
	if len(store.updated) != 1 {
		t.Fatalf("updated sessions = %d, want 1", len(store.updated))
	}
	got := store.updated[0]
	if got.SessionHash != "sha256:session1" {
		t.Fatalf("updated session hash = %q, want sha256:session1", got.SessionHash)
	}
	if !got.ExternalAuthValidatedAt.Equal(now) {
		t.Fatalf("validated at = %v, want %v", got.ExternalAuthValidatedAt, now)
	}
	if !got.ExternalAuthStaleAfter.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("stale after = %v, want %v", got.ExternalAuthStaleAfter, now.Add(15*time.Minute))
	}
	if len(store.revoked) != 0 {
		t.Fatalf("revoked = %#v, want none", store.revoked)
	}
	if got := resolver.lastQuery.GroupHashes; len(got) != 1 || got[0] != "sha256:group" {
		t.Fatalf("resolver group hashes = %#v, want stored group hash", got)
	}
	if got := store.updated[0].ExternalGroupHashes; len(got) != 1 || got[0] != "sha256:group" {
		t.Fatalf("updated group hashes = %#v, want stored group hash", got)
	}
}

func TestRefresherRevokesWhenStoredGroupHashesMissing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 11, 15, 0, 0, time.UTC)
	store := &fakeSessionRefreshStore{
		stale: []StaleSession{{
			SessionHash:              "sha256:session1",
			ExternalProviderConfigID: "okta-dev",
			ExternalSubjectIDHash:    "sha256:subject1",
			TenantID:                 "tenant_a",
			WorkspaceID:              "workspace_a",
			PolicyRevisionHash:       "sha256:policy",
			RoleIDs:                  []string{"developer"},
		}},
	}
	resolver := &fakeRefreshResolver{
		ok:         true,
		resolution: GrantResolution{RoleIDs: []string{"developer"}, PolicyRevisionHash: "sha256:policy"},
	}
	refresher := NewRefresher(store, resolver, RefreshConfig{
		BatchSize:     10,
		Window:        15 * time.Minute,
		SubjectLookup: alwaysActiveSubjectLookup{},
	}, WithRefreshNow(func() time.Time { return now }))

	outcome, err := refresher.RefreshOnce(context.Background())
	if err != nil {
		t.Fatalf("RefreshOnce() error = %v", err)
	}
	if outcome.Revoked != 1 || outcome.Refreshed != 0 {
		t.Fatalf("outcome = %#v, want revoke without refresh", outcome)
	}
	if len(store.revoked) != 1 || store.revoked[0] != "sha256:session1" {
		t.Fatalf("revoked sessions = %#v, want [sha256:session1]", store.revoked)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver calls = %d, want 0 because stale role ids alone are insufficient", resolver.calls)
	}
}

func TestRefresherRevokesWhenExternalSubjectDisabled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 11, 20, 0, 0, time.UTC)
	store := &fakeSessionRefreshStore{
		stale: []StaleSession{{
			SessionHash:              "sha256:session1",
			ExternalProviderConfigID: "okta-dev",
			ExternalSubjectIDHash:    "sha256:subject1",
			TenantID:                 "tenant_a",
			WorkspaceID:              "workspace_a",
			PolicyRevisionHash:       "sha256:policy",
			RoleIDs:                  []string{"developer"},
		}},
	}
	// Resolver would still grant, but the subject is disabled at the IdP linkage.
	resolver := &fakeRefreshResolver{
		ok:         true,
		resolution: GrantResolution{RoleIDs: []string{"developer"}, PolicyRevisionHash: "sha256:policy"},
	}
	refresher := NewRefresher(store, resolver, RefreshConfig{
		BatchSize:     10,
		Window:        15 * time.Minute,
		SubjectLookup: fixedSubjectLookup{active: false},
	}, WithRefreshNow(func() time.Time { return now }))

	outcome, err := refresher.RefreshOnce(context.Background())
	if err != nil {
		t.Fatalf("RefreshOnce() error = %v", err)
	}
	if outcome.Revoked != 1 {
		t.Fatalf("revoked = %d, want 1", outcome.Revoked)
	}
	if len(store.revoked) != 1 {
		t.Fatalf("revoked = %#v, want one disabled-subject revocation", store.revoked)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver calls = %d, want 0 when subject disabled fails closed first", resolver.calls)
	}
}

func TestRefresherDoesNotRevokeOnProviderUnavailable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 11, 30, 0, 0, time.UTC)
	store := &fakeSessionRefreshStore{
		stale: []StaleSession{{
			SessionHash:              "sha256:session1",
			ExternalProviderConfigID: "okta-dev",
			ExternalSubjectIDHash:    "sha256:subject1",
			TenantID:                 "tenant_a",
			WorkspaceID:              "workspace_a",
			PolicyRevisionHash:       "sha256:policy",
			RoleIDs:                  []string{"developer"},
		}},
	}
	resolver := &fakeRefreshResolver{err: errors.New("provider unavailable")}
	refresher := NewRefresher(store, resolver, RefreshConfig{
		BatchSize:     10,
		Window:        15 * time.Minute,
		SubjectLookup: alwaysActiveSubjectLookup{},
	}, WithRefreshNow(func() time.Time { return now }))

	outcome, err := refresher.RefreshOnce(context.Background())
	if err != nil {
		t.Fatalf("RefreshOnce() error = %v", err)
	}
	if outcome.ProviderUnavailable != 1 {
		t.Fatalf("provider unavailable = %d, want 1", outcome.ProviderUnavailable)
	}
	// Provider failure must not eagerly revoke; the request-time fail-closed
	// stale check still protects access until the provider recovers.
	if len(store.revoked) != 0 {
		t.Fatalf("revoked = %#v, want none on provider failure", store.revoked)
	}
	if len(store.updated) != 0 {
		t.Fatalf("updated = %#v, want none on provider failure", store.updated)
	}
}

func TestRefresherIsBoundedByBatchSize(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 11, 40, 0, 0, time.UTC)
	store := &fakeSessionRefreshStore{}
	resolver := &fakeRefreshResolver{ok: false}
	refresher := NewRefresher(store, resolver, RefreshConfig{
		BatchSize:     5,
		Window:        15 * time.Minute,
		SubjectLookup: alwaysActiveSubjectLookup{},
	}, WithRefreshNow(func() time.Time { return now }))

	if _, err := refresher.RefreshOnce(context.Background()); err != nil {
		t.Fatalf("RefreshOnce() error = %v", err)
	}
	if store.listLimit != 5 {
		t.Fatalf("list limit = %d, want bounded batch size 5", store.listLimit)
	}
}

func TestRefresherIsIdempotentUnderDuplicateAndConcurrentDelivery(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 11, 50, 0, 0, time.UTC)
	makeStore := func() *fakeSessionRefreshStore {
		return &fakeSessionRefreshStore{
			stale: []StaleSession{{
				SessionHash:              "sha256:session1",
				ExternalProviderConfigID: "okta-dev",
				ExternalSubjectIDHash:    "sha256:subject1",
				TenantID:                 "tenant_a",
				WorkspaceID:              "workspace_a",
				PolicyRevisionHash:       "sha256:policy",
				RoleIDs:                  []string{"developer"},
			}},
		}
	}
	resolver := &fakeRefreshResolver{ok: false}
	store := makeStore()
	refresher := NewRefresher(store, resolver, RefreshConfig{
		BatchSize:     10,
		Window:        15 * time.Minute,
		SubjectLookup: alwaysActiveSubjectLookup{},
	}, WithRefreshNow(func() time.Time { return now }))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := refresher.RefreshOnce(context.Background()); err != nil {
				t.Errorf("concurrent RefreshOnce() error = %v", err)
			}
		}()
	}
	wg.Wait()

	// Revoking the same session repeatedly is idempotent at the storage layer
	// (revoke is a no-op WHERE revoked_at IS NULL). The refresher must not panic
	// or corrupt shared state under concurrent passes.
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, hash := range store.revoked {
		if hash != "sha256:session1" {
			t.Fatalf("revoked unexpected session %q", hash)
		}
	}
}

func TestRefresherEmptyQueueIsNoOp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	store := &fakeSessionRefreshStore{}
	resolver := &fakeRefreshResolver{}
	refresher := NewRefresher(store, resolver, RefreshConfig{
		BatchSize:     10,
		Window:        15 * time.Minute,
		SubjectLookup: alwaysActiveSubjectLookup{},
	}, WithRefreshNow(func() time.Time { return now }))

	outcome, err := refresher.RefreshOnce(context.Background())
	if err != nil {
		t.Fatalf("RefreshOnce() error = %v", err)
	}
	if outcome.Scanned != 0 || outcome.Revoked != 0 || outcome.Refreshed != 0 {
		t.Fatalf("outcome = %#v, want zero work on empty queue", outcome)
	}
}

// --- fakes ---

type fakeSessionRefreshStore struct {
	mu        sync.Mutex
	stale     []StaleSession
	listLimit int
	revoked   []string
	updated   []SessionAuthProofUpdate
}

func (s *fakeSessionRefreshStore) ListStaleSessions(
	_ context.Context,
	_ time.Time,
	limit int,
) ([]StaleSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listLimit = limit
	return append([]StaleSession(nil), s.stale...), nil
}

func (s *fakeSessionRefreshStore) RevokeSession(_ context.Context, sessionHash string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revoked = append(s.revoked, sessionHash)
	return nil
}

func (s *fakeSessionRefreshStore) UpdateSessionAuthProof(_ context.Context, update SessionAuthProofUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updated = append(s.updated, update)
	return nil
}

type fakeRefreshResolver struct {
	mu         sync.Mutex
	calls      int
	lastQuery  GrantQuery
	ok         bool
	resolution GrantResolution
	err        error
}

func (r *fakeRefreshResolver) ResolveGroupGrants(
	_ context.Context,
	query GrantQuery,
) (GrantResolution, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.lastQuery = query
	if r.err != nil {
		return GrantResolution{}, false, r.err
	}
	return r.resolution, r.ok, nil
}

type alwaysActiveSubjectLookup struct{}

func (alwaysActiveSubjectLookup) ExternalSubjectActive(
	_ context.Context,
	_ string,
	_ string,
) (bool, error) {
	return true, nil
}

type fixedSubjectLookup struct {
	active bool
	err    error
}

func (l fixedSubjectLookup) ExternalSubjectActive(
	_ context.Context,
	_ string,
	_ string,
) (bool, error) {
	return l.active, l.err
}
