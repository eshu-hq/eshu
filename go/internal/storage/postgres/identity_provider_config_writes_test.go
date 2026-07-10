// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// See identity_provider_config_fake_db_test.go for providerConfigFakeDB,
// providerConfigFakeTx, and testKeyring — the shared in-memory model of
// identity_provider_configs/identity_provider_config_revisions used by every
// test in this file, mutex-serialized per the single-row FOR UPDATE conflict
// domain so concurrent-writer tests can run under `go test -race`.

// TestCreateProviderConfigSealsSecret proves the plaintext secret never
// reaches the fake DB unsealed: the stored sealed_secret is a well-formed
// ESK1 envelope, not the plaintext, and GetProviderConfigDetail derives
// HasSecret/SecretFingerprint/SecretKeyID from it without ever returning it.
func TestCreateProviderConfigSealsSecret(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	store := NewIdentitySubjectStore(db)
	store.SetProviderSecretKeyring(testKeyring(t))

	const plaintext = "correct-horse-redaction-canary"
	result, err := store.CreateProviderConfig(context.Background(), ProviderConfigCreate{
		ProviderConfigID:  "pc_1",
		TenantID:          "tenant_a",
		ProviderKind:      "external_oidc",
		ProviderKeyHash:   "hash_1",
		RevisionID:        "rev_1",
		Configuration:     `{"issuer":"https://idp.example.test"}`,
		ConfigurationHash: "cfg_hash_1",
		PlaintextSecret:   `{"client_secret":"` + plaintext + `"}`,
		Now:               time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateProviderConfig() error = %v", err)
	}
	if !result.Found || !result.Changed || result.Status != "draft" {
		t.Fatalf("CreateProviderConfig() result = %+v, want Found=true Changed=true Status=draft", result)
	}

	stored := db.revisions["pc_1"]["rev_1"]
	if stored == nil {
		t.Fatal("revision row not written")
	}
	if strings.Contains(stored.sealedSecret, plaintext) {
		t.Fatalf("sealed_secret contains plaintext canary: %q", stored.sealedSecret)
	}
	if !strings.HasPrefix(stored.sealedSecret, "ESK1.") {
		t.Fatalf("sealed_secret = %q, want ESK1 envelope", stored.sealedSecret)
	}

	detail, found, err := store.GetProviderConfigDetail(context.Background(), "pc_1", "tenant_a")
	if err != nil || !found {
		t.Fatalf("GetProviderConfigDetail() = %+v, %v, %v", detail, found, err)
	}
	if !detail.HasSecret {
		t.Fatal("HasSecret = false, want true")
	}
	if detail.SecretKeyID != "k1" {
		t.Fatalf("SecretKeyID = %q, want k1", detail.SecretKeyID)
	}
	if detail.SecretFingerprint == "" {
		t.Fatal("SecretFingerprint is empty")
	}
}

// TestGetProviderConfigDetailNeverReturnsPlaintext proves the detail struct
// carries no field containing the plaintext secret or the raw envelope text —
// only derived, non-reversible metadata.
func TestGetProviderConfigDetailNeverReturnsPlaintext(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	store := NewIdentitySubjectStore(db)
	store.SetProviderSecretKeyring(testKeyring(t))

	const plaintext = "correct-horse-redaction-canary"
	if _, err := store.CreateProviderConfig(context.Background(), ProviderConfigCreate{
		ProviderConfigID:  "pc_2",
		TenantID:          "tenant_a",
		ProviderKind:      "external_oidc",
		ProviderKeyHash:   "hash_2",
		RevisionID:        "rev_1",
		ConfigurationHash: "cfg_hash",
		PlaintextSecret:   `{"client_secret":"` + plaintext + `"}`,
		Now:               time.Now(),
	}); err != nil {
		t.Fatalf("CreateProviderConfig() error = %v", err)
	}

	detail, found, err := store.GetProviderConfigDetail(context.Background(), "pc_2", "tenant_a")
	if err != nil || !found {
		t.Fatalf("GetProviderConfigDetail() = %+v, %v, %v", detail, found, err)
	}
	fields := []string{detail.ProviderConfigID, detail.TenantID, detail.ProviderKind, detail.Status, detail.ActiveRevisionID, detail.Configuration, detail.SecretFingerprint, detail.SecretKeyID}
	for _, field := range fields {
		if strings.Contains(field, plaintext) {
			t.Fatalf("ProviderConfigDetail field contains plaintext canary: %q", field)
		}
	}
}

// TestCreateProviderConfigWithoutKeyringFailsClosed proves a write carrying a
// secret is rejected — not silently persisted unsealed — when no keyring is
// configured.
func TestCreateProviderConfigWithoutKeyringFailsClosed(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	store := NewIdentitySubjectStore(db)

	_, err := store.CreateProviderConfig(context.Background(), ProviderConfigCreate{
		ProviderConfigID: "pc_3",
		TenantID:         "tenant_a",
		ProviderKind:     "external_oidc",
		ProviderKeyHash:  "hash_3",
		RevisionID:       "rev_1",
		PlaintextSecret:  `{"client_secret":"whatever"}`,
		Now:              time.Now(),
	})
	if err == nil {
		t.Fatal("CreateProviderConfig() error = nil, want ErrProviderSecretKeyringUnavailable")
	}
	if !strings.Contains(err.Error(), "provider secret keyring is not configured") {
		t.Fatalf("CreateProviderConfig() error = %v, want keyring-unavailable error", err)
	}
	if _, ok := db.configs["pc_3"]; ok {
		t.Fatal("provider config row written despite keyring failure")
	}
}

// TestUpdateProviderConfigRejectsProviderKindMismatch proves an update whose
// ProviderKind disagrees with the existing provider config's immutable
// provider_kind is rejected without writing a new revision — otherwise a SAML
// configuration/secret JSON shape could land under an OIDC provider_kind (or
// vice versa), which provider_kind-driven consumers (oidclogin.TestConnection,
// samlauth.TestConnection) would then fail to parse.
func TestUpdateProviderConfigRejectsProviderKindMismatch(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	store := NewIdentitySubjectStore(db)
	store.SetProviderSecretKeyring(testKeyring(t))
	ctx := context.Background()

	if _, err := store.CreateProviderConfig(ctx, ProviderConfigCreate{
		ProviderConfigID: "pc_kind", TenantID: "tenant_a", ProviderKind: "external_oidc",
		ProviderKeyHash: "hash_kind", RevisionID: "rev_1", ConfigurationHash: "h1",
		PlaintextSecret: `{"client_secret":"first"}`, Now: time.Now(),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := store.UpdateProviderConfig(ctx, ProviderConfigUpdate{
		ProviderConfigID: "pc_kind", TenantID: "tenant_a", ProviderKind: "external_saml",
		RevisionID: "rev_2", ConfigurationHash: "h2",
		PlaintextSecret: `{"sp_private_key":"x","sp_certificate":"y"}`, Now: time.Now(),
	})
	if !errors.Is(err, ErrProviderConfigKindMismatch) {
		t.Fatalf("UpdateProviderConfig() error = %v, want ErrProviderConfigKindMismatch", err)
	}
	if _, ok := db.revisions["pc_kind"]["rev_2"]; ok {
		t.Fatal("a new revision was written despite the provider_kind mismatch")
	}
	if db.configs["pc_kind"].activeRevisionID != "rev_1" {
		t.Fatalf("active revision changed to %q despite rejected update, want rev_1", db.configs["pc_kind"].activeRevisionID)
	}
}

// TestUpdateProviderConfigReturnsPostUpdateStatusNotStaleActiveStatus proves
// UpdateProviderConfig's returned Status reflects the row's status AFTER the
// transaction commits, not the value read before it started. Update always
// resets an existing active_revision_id pointer via
// activateProviderConfigActiveRevisionQuery, which unconditionally sets
// status='draft' in the same statement (see that query's doc comment: an
// update invalidates the prior test-connection, so the provider must be
// re-tested via Enable before it is trusted again). A caller that was
// 'active' before this Update must see Status="draft" in the result, not the
// stale pre-transaction "active" value — callers (#4967 admin UI) trust this
// field directly.
func TestUpdateProviderConfigReturnsPostUpdateStatusNotStaleActiveStatus(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	store := NewIdentitySubjectStore(db)
	store.SetProviderSecretKeyring(testKeyring(t))
	ctx := context.Background()

	if _, err := store.CreateProviderConfig(ctx, ProviderConfigCreate{
		ProviderConfigID: "pc_status", TenantID: "tenant_a", ProviderKind: "external_oidc",
		ProviderKeyHash: "hash_status", RevisionID: "rev_1", ConfigurationHash: "h1",
		PlaintextSecret: `{"client_secret":"first"}`, Now: time.Now(),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	enableResult, err := store.EnableProviderConfig(ctx, ProviderConfigEnable{
		ProviderConfigID: "pc_status", TenantID: "tenant_a", ExpectedActiveRevisionID: "rev_1", Now: time.Now(),
	})
	if err != nil || enableResult.Status != "active" {
		t.Fatalf("EnableProviderConfig() = %+v, err = %v, want Status=active", enableResult, err)
	}

	result, err := store.UpdateProviderConfig(ctx, ProviderConfigUpdate{
		ProviderConfigID: "pc_status", TenantID: "tenant_a", RevisionID: "rev_2",
		ConfigurationHash: "h2", PlaintextSecret: `{"client_secret":"second"}`, Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("UpdateProviderConfig() error = %v", err)
	}
	if result.Status != "draft" {
		t.Fatalf("UpdateProviderConfig() result.Status = %q, want %q (the post-transaction persisted status, not the pre-update active status)", result.Status, "draft")
	}
	if db.configs["pc_status"].status != "draft" {
		t.Fatalf("persisted status = %q, want draft", db.configs["pc_status"].status)
	}
}

// TestRevertProviderConfigRestoresPriorRevisionSecret proves reverting to a
// prior revision restores exactly that revision's secret (by fingerprint —
// the read path never opens either revision to compare plaintext), without
// re-sealing or opening anything.
func TestRevertProviderConfigRestoresPriorRevisionSecret(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	store := NewIdentitySubjectStore(db)
	store.SetProviderSecretKeyring(testKeyring(t))
	ctx := context.Background()

	if _, err := store.CreateProviderConfig(ctx, ProviderConfigCreate{
		ProviderConfigID: "pc_4", TenantID: "tenant_a", ProviderKind: "external_oidc",
		ProviderKeyHash: "hash_4", RevisionID: "rev_1", ConfigurationHash: "h1",
		PlaintextSecret: `{"client_secret":"first-secret"}`, Now: time.Now(),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	firstFingerprint := db.revisions["pc_4"]["rev_1"].sealedSecret

	if _, err := store.UpdateProviderConfig(ctx, ProviderConfigUpdate{
		ProviderConfigID: "pc_4", TenantID: "tenant_a", RevisionID: "rev_2",
		ConfigurationHash: "h2", PlaintextSecret: `{"client_secret":"second-secret"}`, Now: time.Now(),
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if db.configs["pc_4"].activeRevisionID != "rev_2" {
		t.Fatalf("active revision after update = %q, want rev_2", db.configs["pc_4"].activeRevisionID)
	}
	if db.revisions["pc_4"]["rev_1"].status != "superseded" {
		t.Fatalf("rev_1 status after update = %q, want superseded", db.revisions["pc_4"]["rev_1"].status)
	}
	// rev_1's sealed_secret must be untouched by the update.
	if db.revisions["pc_4"]["rev_1"].sealedSecret != firstFingerprint {
		t.Fatal("rev_1 sealed_secret mutated by an unrelated update")
	}

	result, err := store.RevertProviderConfig(ctx, ProviderConfigRevert{
		ProviderConfigID: "pc_4", TenantID: "tenant_a", TargetRevisionID: "rev_1", Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if !result.Changed || result.RevisionID != "rev_1" {
		t.Fatalf("RevertProviderConfig() result = %+v, want Changed=true RevisionID=rev_1", result)
	}
	if db.configs["pc_4"].activeRevisionID != "rev_1" {
		t.Fatalf("active revision after revert = %q, want rev_1", db.configs["pc_4"].activeRevisionID)
	}
	if db.revisions["pc_4"]["rev_1"].status != "active" {
		t.Fatal("rev_1 not reactivated")
	}
	if db.revisions["pc_4"]["rev_2"].status != "superseded" {
		t.Fatal("rev_2 not superseded after revert")
	}
	// The reactivated revision's ciphertext is byte-identical to what was
	// originally sealed for it — revert never re-seals or opens.
	if db.revisions["pc_4"]["rev_1"].sealedSecret != firstFingerprint {
		t.Fatal("revert mutated rev_1's sealed_secret")
	}

	// Exactly one active revision at all times.
	activeCount := 0
	for _, rev := range db.revisions["pc_4"] {
		if rev.status == "active" {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Fatalf("active revision count = %d, want 1", activeCount)
	}
}

// TestProviderConfigConcurrentUpdateAndRevertSerializeToOneActiveRevision runs Update and
// Revert concurrently against the same provider config under -race, proving
// this package's transaction logic (not just the SQL shape) leaves exactly
// one active revision and every revision's sealed_secret intact. The fake
// DB's Begin/Commit mutex stands in for Postgres's FOR UPDATE row lock on
// this single conflict domain (one identity_provider_configs row).
func TestProviderConfigConcurrentUpdateAndRevertSerializeToOneActiveRevision(t *testing.T) {
	db := newProviderConfigFakeDB()
	store := NewIdentitySubjectStore(db)
	store.SetProviderSecretKeyring(testKeyring(t))
	ctx := context.Background()

	if _, err := store.CreateProviderConfig(ctx, ProviderConfigCreate{
		ProviderConfigID: "pc_5", TenantID: "tenant_a", ProviderKind: "external_oidc",
		ProviderKeyHash: "hash_5", RevisionID: "rev_0", ConfigurationHash: "h0",
		PlaintextSecret: `{"client_secret":"seed"}`, Now: time.Now(),
	}); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 3)
	wg.Add(3)
	go func() {
		defer wg.Done()
		_, err := store.UpdateProviderConfig(ctx, ProviderConfigUpdate{
			ProviderConfigID: "pc_5", TenantID: "tenant_a", RevisionID: "rev_n",
			ConfigurationHash: "hn", PlaintextSecret: `{"client_secret":"n"}`, Now: time.Now(),
		})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := store.UpdateProviderConfig(ctx, ProviderConfigUpdate{
			ProviderConfigID: "pc_5", TenantID: "tenant_a", RevisionID: "rev_n_plus_1",
			ConfigurationHash: "hn1", PlaintextSecret: `{"client_secret":"n+1"}`, Now: time.Now(),
		})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		// Revert races the two updates; whichever runs last under the fake's
		// serializing lock determines the final active revision, but the
		// invariant below (exactly one active revision, all ciphertext
		// intact) must hold regardless of interleaving.
		_, err := store.RevertProviderConfig(ctx, ProviderConfigRevert{
			ProviderConfigID: "pc_5", TenantID: "tenant_a", TargetRevisionID: "rev_0", Now: time.Now(),
		})
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write error = %v", err)
		}
	}

	activeCount := 0
	for id, rev := range db.revisions["pc_5"] {
		if rev.status == "active" {
			activeCount++
		}
		if rev.sealedSecret == "" {
			t.Fatalf("revision %q lost its sealed_secret", id)
		}
		if !strings.HasPrefix(rev.sealedSecret, "ESK1.") {
			t.Fatalf("revision %q sealed_secret is not a well-formed envelope: %q", id, rev.sealedSecret)
		}
	}
	if activeCount != 1 {
		t.Fatalf("active revision count after concurrent writes = %d, want exactly 1", activeCount)
	}
	if db.configs["pc_5"].activeRevisionID == "" {
		t.Fatal("provider config has no active_revision_id after concurrent writes")
	}
}

// See identity_provider_config_enable_test.go for the EnableProviderConfig
// compare-and-swap tests (TestEnableProviderConfigRequiresExpectedRevision,
// TestEnableProviderConfigSucceedsWhenRevisionMatches,
// TestEnableProviderConfigFailsWhenRevisionChanged,
// TestProviderConfigConcurrentUpdateDuringEnableRejectsStaleRevision).
