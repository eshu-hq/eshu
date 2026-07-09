// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestConcurrentUpdateAndEnableSerializeAgainstRealPostgresRowLock is the
// real-Postgres counterpart to TestProviderConfigConcurrentUpdateDuringEnableRejectsStaleRevision
// (identity_provider_config_enable_test.go), which proves the same invariant
// against a mutex-serialized in-memory fake. Concurrency proofs against a
// fake can only show this package's Go-level logic is correct; they cannot
// prove Postgres's actual FOR UPDATE row-lock semantics hold. This test runs
// the identical Update-vs-Enable race with two independent live Postgres
// connections (one per goroutine, each capped at a single connection so they
// represent two genuinely separate backend sessions that can block on each
// other's row lock) against a real, freshly bootstrapped schema.
//
// Run with: ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu
//
//	go test ./internal/storage/postgres -run 'ProviderConfig' -race -count=1
func TestProviderConfigConcurrentUpdateAndEnableAgainstRealPostgresRowLock(t *testing.T) {
	dsn := providerConfigLiveProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres provider-config concurrency proof")
	}

	ctx := context.Background()
	schemaDB, schemaName := openProviderConfigLiveSchema(t, ctx, dsn)
	seedProviderConfigLiveTenant(t, ctx, schemaDB, "tenant_live")

	keyring := testKeyring(t)
	primaryStore := NewIdentitySubjectStore(ExecQueryer(SQLDB{DB: schemaDB}))
	primaryStore.SetProviderSecretKeyring(keyring)

	if _, err := primaryStore.CreateProviderConfig(ctx, ProviderConfigCreate{
		ProviderConfigID: "pc_live_1", TenantID: "tenant_live", ProviderKind: "external_oidc",
		ProviderKeyHash: "hash_live_1", RevisionID: "rev_1", ConfigurationHash: "h1",
		PlaintextSecret: `{"client_secret":"s"}`, Now: time.Now(),
	}); err != nil {
		t.Fatalf("seed CreateProviderConfig: %v", err)
	}

	// Two independent single-connection handles bound to the same schema —
	// two real, separate Postgres backend sessions.
	updateConn := openProviderConfigLiveSchemaConn(t, ctx, dsn, schemaName)
	enableConn := openProviderConfigLiveSchemaConn(t, ctx, dsn, schemaName)
	updateStore := NewIdentitySubjectStore(ExecQueryer(SQLDB{DB: updateConn}))
	updateStore.SetProviderSecretKeyring(keyring)
	enableStore := NewIdentitySubjectStore(ExecQueryer(SQLDB{DB: enableConn}))
	enableStore.SetProviderSecretKeyring(keyring)

	var wg sync.WaitGroup
	var enableErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := updateStore.UpdateProviderConfig(ctx, ProviderConfigUpdate{
			ProviderConfigID: "pc_live_1", TenantID: "tenant_live", RevisionID: "rev_2",
			ConfigurationHash: "h2", PlaintextSecret: `{"client_secret":"s2"}`, Now: time.Now(),
		})
		if err != nil {
			t.Errorf("concurrent Update (real postgres) error = %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		_, enableErr = enableStore.EnableProviderConfig(ctx, ProviderConfigEnable{
			ProviderConfigID: "pc_live_1", TenantID: "tenant_live", ExpectedActiveRevisionID: "rev_1", Now: time.Now(),
		})
	}()
	wg.Wait()

	if enableErr != nil && enableErr != ErrProviderConfigRevisionChanged { //nolint:errorlint // exact sentinel, not wrapped, across a real DB round trip
		t.Fatalf("EnableProviderConfig() (real postgres) unexpected error = %v", enableErr)
	}

	// Read back the final state with a fresh, independently-verifying
	// connection and assert the same invariant the fake-backed test proves:
	// never active with the untested rev_2, and exactly one active revision.
	verifyConn := openProviderConfigLiveSchemaConn(t, ctx, dsn, schemaName)
	verifyStore := NewIdentitySubjectStore(ExecQueryer(SQLDB{DB: verifyConn}))
	detail, found, err := verifyStore.GetProviderConfigDetail(ctx, "pc_live_1", "tenant_live")
	if err != nil || !found {
		t.Fatalf("GetProviderConfigDetail() (real postgres) = %+v, %v, %v", detail, found, err)
	}
	if detail.Status == "active" && detail.ActiveRevisionID != "rev_1" {
		t.Fatalf("(real postgres) provider config is active with revision %q, which was never tested — TOCTOU not closed", detail.ActiveRevisionID)
	}
	if detail.ActiveRevisionID != "rev_2" {
		t.Fatalf("(real postgres) active_revision_id = %q after both operations completed, want rev_2 (Update always applies)", detail.ActiveRevisionID)
	}

	revisions, err := verifyStore.ListProviderConfigRevisions(ctx, "pc_live_1", "tenant_live")
	if err != nil {
		t.Fatalf("ListProviderConfigRevisions() (real postgres) error = %v", err)
	}
	activeCount := 0
	for _, rev := range revisions {
		if rev.Status == "active" {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Fatalf("(real postgres) active revision count = %d, want exactly 1", activeCount)
	}
}

// TestConcurrentUpdateAndRevertSerializeAgainstRealPostgresRowLock is the
// real-Postgres counterpart to TestProviderConfigConcurrentUpdateAndRevertSerializeToOneActiveRevision
// (identity_provider_config_writes_test.go).
func TestProviderConfigConcurrentUpdateAndRevertAgainstRealPostgresRowLock(t *testing.T) {
	dsn := providerConfigLiveProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres provider-config concurrency proof")
	}

	ctx := context.Background()
	schemaDB, schemaName := openProviderConfigLiveSchema(t, ctx, dsn)
	seedProviderConfigLiveTenant(t, ctx, schemaDB, "tenant_live2")

	keyring := testKeyring(t)
	primaryStore := NewIdentitySubjectStore(ExecQueryer(SQLDB{DB: schemaDB}))
	primaryStore.SetProviderSecretKeyring(keyring)
	if _, err := primaryStore.CreateProviderConfig(ctx, ProviderConfigCreate{
		ProviderConfigID: "pc_live_2", TenantID: "tenant_live2", ProviderKind: "external_oidc",
		ProviderKeyHash: "hash_live_2", RevisionID: "rev_0", ConfigurationHash: "h0",
		PlaintextSecret: `{"client_secret":"seed"}`, Now: time.Now(),
	}); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	conn1 := openProviderConfigLiveSchemaConn(t, ctx, dsn, schemaName)
	conn2 := openProviderConfigLiveSchemaConn(t, ctx, dsn, schemaName)
	conn3 := openProviderConfigLiveSchemaConn(t, ctx, dsn, schemaName)
	store1 := NewIdentitySubjectStore(ExecQueryer(SQLDB{DB: conn1}))
	store1.SetProviderSecretKeyring(keyring)
	store2 := NewIdentitySubjectStore(ExecQueryer(SQLDB{DB: conn2}))
	store2.SetProviderSecretKeyring(keyring)
	store3 := NewIdentitySubjectStore(ExecQueryer(SQLDB{DB: conn3}))
	store3.SetProviderSecretKeyring(keyring)

	var wg sync.WaitGroup
	errs := make(chan error, 3)
	wg.Add(3)
	go func() {
		defer wg.Done()
		_, err := store1.UpdateProviderConfig(ctx, ProviderConfigUpdate{
			ProviderConfigID: "pc_live_2", TenantID: "tenant_live2", RevisionID: "rev_n",
			ConfigurationHash: "hn", PlaintextSecret: `{"client_secret":"n"}`, Now: time.Now(),
		})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := store2.UpdateProviderConfig(ctx, ProviderConfigUpdate{
			ProviderConfigID: "pc_live_2", TenantID: "tenant_live2", RevisionID: "rev_n_plus_1",
			ConfigurationHash: "hn1", PlaintextSecret: `{"client_secret":"n+1"}`, Now: time.Now(),
		})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := store3.RevertProviderConfig(ctx, ProviderConfigRevert{
			ProviderConfigID: "pc_live_2", TenantID: "tenant_live2", TargetRevisionID: "rev_0", Now: time.Now(),
		})
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("(real postgres) concurrent write error = %v", err)
		}
	}

	verifyConn := openProviderConfigLiveSchemaConn(t, ctx, dsn, schemaName)
	verifyStore := NewIdentitySubjectStore(ExecQueryer(SQLDB{DB: verifyConn}))
	revisions, err := verifyStore.ListProviderConfigRevisions(ctx, "pc_live_2", "tenant_live2")
	if err != nil {
		t.Fatalf("ListProviderConfigRevisions() (real postgres) error = %v", err)
	}
	activeCount := 0
	for _, rev := range revisions {
		if rev.Status == "active" {
			activeCount++
		}
		if !rev.HasSecret {
			t.Fatalf("(real postgres) revision %q lost its sealed_secret", rev.RevisionID)
		}
	}
	if activeCount != 1 {
		t.Fatalf("(real postgres) active revision count after concurrent writes = %d, want exactly 1", activeCount)
	}

	detail, found, err := verifyStore.GetProviderConfigDetail(ctx, "pc_live_2", "tenant_live2")
	if err != nil || !found || detail.ActiveRevisionID == "" {
		t.Fatalf("(real postgres) GetProviderConfigDetail() = %+v, %v, %v, want a resolved active_revision_id", detail, found, err)
	}
}

func providerConfigLiveProofDSN() string {
	return os.Getenv("ESHU_POSTGRES_DSN")
}

// openProviderConfigLiveSchema creates an isolated throwaway schema and
// applies exactly the migrations identity_provider_configs and
// identity_provider_config_revisions depend on: ingestion_scopes (an FK
// target of tenant_workspace_grants), tenant_workspace_grants (tenants and
// workspaces), the identity subject tables, and this PR's sealed_secret
// /configuration columns — not the full bootstrap set, to keep the proof
// fast and focused.
func openProviderConfigLiveSchema(t *testing.T, ctx context.Context, dsn string) (*sql.DB, string) {
	t.Helper()
	schemaName := fmt.Sprintf("provider_config_live_%d", time.Now().UnixNano())
	db := openProviderConfigLiveSchemaConn(t, ctx, dsn, schemaName)
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create provider config live schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	for _, defName := range []string{"ingestion_scopes", "tenant_workspace_grants", "identity_subjects", "provider_config_sealed_secret"} {
		if _, err := db.ExecContext(ctx, MigrationSQL(defName)); err != nil {
			t.Fatalf("apply migration %q: %v", defName, err)
		}
	}
	return db, schemaName
}

// openProviderConfigLiveSchemaConn opens a pgx handle capped at one
// connection, pinned to schemaName's search_path, mirroring
// openReducerFairnessSchemaConn (reducer_queue_domain_fairness_test.go) —
// the package's established live-Postgres concurrency-proof pattern. One
// connection per handle is what lets two handles represent two genuinely
// separate backend sessions whose FOR UPDATE locks can actually contend.
func openProviderConfigLiveSchemaConn(t *testing.T, ctx context.Context, dsn, schemaName string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	return db
}

func seedProviderConfigLiveTenant(t *testing.T, ctx context.Context, db *sql.DB, tenantID string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
INSERT INTO tenants (tenant_id, status, policy_revision_hash, created_at, updated_at)
VALUES ($1, 'active', 'seed_policy_rev', $2, $2)
ON CONFLICT (tenant_id) DO NOTHING`, tenantID, now); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
}
