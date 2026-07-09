// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestEnableProviderConfigRequiresExpectedRevision proves Enable rejects a
// call that omits ExpectedActiveRevisionID rather than silently activating
// whatever the current revision happens to be.
func TestEnableProviderConfigRequiresExpectedRevision(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	store := NewIdentitySubjectStore(db)
	store.SetProviderSecretKeyring(testKeyring(t))
	ctx := context.Background()

	if _, err := store.CreateProviderConfig(ctx, ProviderConfigCreate{
		ProviderConfigID: "pc_enable_1", TenantID: "tenant_a", ProviderKind: "external_oidc",
		ProviderKeyHash: "hash_e1", RevisionID: "rev_1", ConfigurationHash: "h1",
		PlaintextSecret: `{"client_secret":"s"}`, Now: time.Now(),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := store.EnableProviderConfig(ctx, ProviderConfigEnable{
		ProviderConfigID: "pc_enable_1", TenantID: "tenant_a", Now: time.Now(),
	})
	if err == nil {
		t.Fatal("EnableProviderConfig() error = nil, want a required-field error for missing ExpectedActiveRevisionID")
	}
	if db.configs["pc_enable_1"].status != "draft" {
		t.Fatalf("status = %q after rejected enable, want draft (unchanged)", db.configs["pc_enable_1"].status)
	}
}

// TestEnableProviderConfigSucceedsWhenRevisionMatches proves the happy path:
// Enable activates the provider config when ExpectedActiveRevisionID matches
// the current active_revision_id.
func TestEnableProviderConfigSucceedsWhenRevisionMatches(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	store := NewIdentitySubjectStore(db)
	store.SetProviderSecretKeyring(testKeyring(t))
	ctx := context.Background()

	if _, err := store.CreateProviderConfig(ctx, ProviderConfigCreate{
		ProviderConfigID: "pc_enable_2", TenantID: "tenant_a", ProviderKind: "external_oidc",
		ProviderKeyHash: "hash_e2", RevisionID: "rev_1", ConfigurationHash: "h1",
		PlaintextSecret: `{"client_secret":"s"}`, Now: time.Now(),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := store.EnableProviderConfig(ctx, ProviderConfigEnable{
		ProviderConfigID: "pc_enable_2", TenantID: "tenant_a", ExpectedActiveRevisionID: "rev_1", Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("EnableProviderConfig() error = %v", err)
	}
	if !result.Found || !result.Changed || result.Status != "active" {
		t.Fatalf("EnableProviderConfig() result = %+v, want Found=true Changed=true Status=active", result)
	}
	if db.configs["pc_enable_2"].status != "active" {
		t.Fatalf("status = %q, want active", db.configs["pc_enable_2"].status)
	}
}

// TestEnableProviderConfigFailsWhenRevisionChanged proves the TOCTOU fix
// directly: if the active revision changed (via Update) after the caller's
// test-connection call but before Enable runs, Enable rejects with
// ErrProviderConfigRevisionChanged instead of activating the new,
// never-tested revision.
func TestEnableProviderConfigFailsWhenRevisionChanged(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	store := NewIdentitySubjectStore(db)
	store.SetProviderSecretKeyring(testKeyring(t))
	ctx := context.Background()

	if _, err := store.CreateProviderConfig(ctx, ProviderConfigCreate{
		ProviderConfigID: "pc_enable_3", TenantID: "tenant_a", ProviderKind: "external_oidc",
		ProviderKeyHash: "hash_e3", RevisionID: "rev_1", ConfigurationHash: "h1",
		PlaintextSecret: `{"client_secret":"s"}`, Now: time.Now(),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Simulate a test-connection call having tested rev_1, then a concurrent
	// Update landing rev_2 as the new active revision before Enable runs.
	if _, err := store.UpdateProviderConfig(ctx, ProviderConfigUpdate{
		ProviderConfigID: "pc_enable_3", TenantID: "tenant_a", RevisionID: "rev_2",
		ConfigurationHash: "h2", PlaintextSecret: `{"client_secret":"s2"}`, Now: time.Now(),
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	_, err := store.EnableProviderConfig(ctx, ProviderConfigEnable{
		ProviderConfigID: "pc_enable_3", TenantID: "tenant_a", ExpectedActiveRevisionID: "rev_1", Now: time.Now(),
	})
	if !errors.Is(err, ErrProviderConfigRevisionChanged) {
		t.Fatalf("EnableProviderConfig() error = %v, want ErrProviderConfigRevisionChanged", err)
	}
	if db.configs["pc_enable_3"].status != "draft" {
		t.Fatalf("status = %q after rejected enable, want draft (unchanged, never activated the untested rev_2)", db.configs["pc_enable_3"].status)
	}
}

// TestConcurrentUpdateDuringEnableRejectsStaleRevision runs Update and Enable
// concurrently against the same provider config under -race, proving the
// row-locked compare-and-swap in EnableProviderConfig, combined with
// activateProviderConfigActiveRevisionQuery's unconditional status='draft'
// reset (see that query's doc comment), makes the invariant hold under BOTH
// possible interleavings:
//
//   - Enable wins the race first: it legitimately activates rev_1 (the
//     revision it was told to expect, matched under the row lock). If Update
//     then lands rev_2 afterward, activateProviderConfigActiveRevisionQuery
//     demotes status back to 'draft' as part of that same write — so the
//     provider does not stay "active" pointed at the new, untested rev_2.
//   - Update wins the race first: it lands rev_2 and demotes status to
//     'draft'. Enable then runs against a row whose active_revision_id is
//     rev_2, not the rev_1 it expected, so it fails closed with
//     ErrProviderConfigRevisionChanged and writes nothing.
//
// The one invariant that must hold no matter which goroutine wins: the
// provider config is NEVER observed active with a revision other than rev_1
// (rev_2 is never tested in this test), and exactly one revision is ever
// marked active in the revisions table.
func TestConcurrentUpdateDuringEnableRejectsStaleRevision(t *testing.T) {
	db := newProviderConfigFakeDB()
	store := NewIdentitySubjectStore(db)
	store.SetProviderSecretKeyring(testKeyring(t))
	ctx := context.Background()

	if _, err := store.CreateProviderConfig(ctx, ProviderConfigCreate{
		ProviderConfigID: "pc_enable_4", TenantID: "tenant_a", ProviderKind: "external_oidc",
		ProviderKeyHash: "hash_e4", RevisionID: "rev_1", ConfigurationHash: "h1",
		PlaintextSecret: `{"client_secret":"s"}`, Now: time.Now(),
	}); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	var wg sync.WaitGroup
	var enableErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := store.UpdateProviderConfig(ctx, ProviderConfigUpdate{
			ProviderConfigID: "pc_enable_4", TenantID: "tenant_a", RevisionID: "rev_2",
			ConfigurationHash: "h2", PlaintextSecret: `{"client_secret":"s2"}`, Now: time.Now(),
		})
		if err != nil {
			t.Errorf("concurrent update error = %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		_, enableErr = store.EnableProviderConfig(ctx, ProviderConfigEnable{
			ProviderConfigID: "pc_enable_4", TenantID: "tenant_a", ExpectedActiveRevisionID: "rev_1", Now: time.Now(),
		})
	}()
	wg.Wait()

	if enableErr != nil && !errors.Is(enableErr, ErrProviderConfigRevisionChanged) {
		t.Fatalf("EnableProviderConfig() unexpected error = %v", enableErr)
	}

	status := db.configs["pc_enable_4"].status
	activeRevision := db.configs["pc_enable_4"].activeRevisionID
	// The core invariant: never active with the untested rev_2, under either
	// interleaving.
	if status == "active" && activeRevision != "rev_1" {
		t.Fatalf("provider config is active with revision %q, which was never tested — TOCTOU not closed", activeRevision)
	}
	// Update must always have landed rev_2 as the pointer, regardless of
	// ordering (it runs unconditionally in this test, unlike Enable which can
	// be rejected).
	if activeRevision != "rev_2" {
		t.Fatalf("active_revision_id = %q after both operations completed, want rev_2 (Update always applies)", activeRevision)
	}
	// Exactly one revision is ever marked active in the ledger.
	activeCount := 0
	for id, rev := range db.revisions["pc_enable_4"] {
		if rev.status == "active" {
			activeCount++
		}
		if rev.sealedSecret == "" {
			t.Fatalf("revision %q lost its sealed_secret", id)
		}
	}
	if activeCount != 1 {
		t.Fatalf("active revision count = %d, want exactly 1", activeCount)
	}
}
