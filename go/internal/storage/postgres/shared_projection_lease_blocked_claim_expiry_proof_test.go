// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestClaimPartitionLeaseBlockedClaimKeepsFullTTLAgainstPostgres is the live
// proof for #6760. The Ifá killworker cell parks a reducer's partition lease
// claims behind a test holder's advisory lock and kills the reducer while
// they are still blocked; the orphaned claim transactions commit only once
// the holder releases. When lease_expires_at is bound client-side at call
// time, the seconds spent blocked burn TTL before the row exists, so the
// committed row lands already stale and the cell's capture step (which
// requires more than 4s of remaining TTL) rejects it. Set
// ESHU_SHARED_PROJECTION_CLAIM_EXPIRY_PROOF_DSN to run it; it is skipped
// otherwise, matching the sibling live proofs in this package.
//
// The proof holds the production advisory key for 3s while a claim with an
// 8s TTL blocks behind it, then releases and asserts the committed row keeps
// a full TTL:
//
//   - Unpatched (client-side expiry): the row expires ~5s after commit, so
//     the remaining-TTL assertion fails.
//   - Patched (server-side expiry at commit): the row expires ~8s after
//     commit and the assertion passes.
func TestClaimPartitionLeaseBlockedClaimKeepsFullTTLAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ESHU_SHARED_PROJECTION_CLAIM_EXPIRY_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_SHARED_PROJECTION_CLAIM_EXPIRY_PROOF_DSN to run the blocked claim expiry proof")
	}

	ctx := context.Background()
	bootstrapDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open bootstrap connection: %v", err)
	}
	schemaName := fmt.Sprintf("shared_projection_claim_expiry_proof_%d", time.Now().UnixNano())
	if _, err := bootstrapDB.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		_ = bootstrapDB.Close()
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := bootstrapDB.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE"); err != nil {
			t.Errorf("drop proof schema: %v", err)
		}
		if err := bootstrapDB.Close(); err != nil {
			t.Errorf("close bootstrap connection: %v", err)
		}
	})

	scopedDSN := dsn + "?search_path=" + schemaName
	if strings.Contains(dsn, "?") {
		scopedDSN = dsn + "&search_path=" + schemaName
	}
	database, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open scoped connection pool: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	database.SetMaxOpenConns(6)

	// Use the real production DDL, not a hand-copied subset, so the proof
	// exercises the actual claim SQL text.
	if _, err := database.ExecContext(ctx, SharedIntentSchemaSQL()); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}

	const domain = "claim-expiry-proof-domain"
	const leaseTTL = 8 * time.Second
	const blockedHold = 3 * time.Second

	// A dedicated holder connection takes the exact production advisory key
	// the claim CTE waits on, mirroring the Ifá runner-lease-hold client.
	holderDB, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open holder connection: %v", err)
	}
	t.Cleanup(func() { _ = holderDB.Close() })
	holderDB.SetMaxOpenConns(1)
	holderTx, err := holderDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder transaction: %v", err)
	}
	if _, err := holderTx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext('shared_projection_partition_leases'), hashtext($1))", domain); err != nil {
		_ = holderTx.Rollback()
		t.Fatalf("acquire holder advisory lock: %v", err)
	}

	store := NewSharedIntentStore(SQLDB{DB: database})
	claimErr := make(chan error, 1)
	claimDone := make(chan bool, 1)
	go func() {
		claimed, err := store.ClaimPartitionLease(ctx, domain, 0, 4, "expiry-proof-owner", leaseTTL)
		if err != nil {
			claimErr <- err
			return
		}
		claimDone <- claimed
	}()

	// Let the claim park behind the holder (it must still be blocked here;
	// the claim returns only after the release below).
	select {
	case <-claimDone:
		_ = holderTx.Rollback()
		t.Fatal("claim returned while the holder still held the advisory key")
	case <-time.After(blockedHold):
	}

	beforeRelease := time.Now().UTC()
	if err := holderTx.Rollback(); err != nil {
		t.Fatalf("release holder advisory lock: %v", err)
	}

	var claimed bool
	select {
	case err := <-claimErr:
		t.Fatalf("blocked claim error = %v", err)
	case claimed = <-claimDone:
	case <-time.After(30 * time.Second):
		t.Fatal("blocked claim did not return within 30s of the holder release")
	}
	if !claimed {
		t.Fatal("blocked claim claimed = false, want true")
	}

	var expiresAt time.Time
	if err := database.QueryRowContext(ctx, `SELECT lease_expires_at FROM shared_projection_partition_leases WHERE projection_domain = $1`, domain).Scan(&expiresAt); err != nil {
		t.Fatalf("read committed lease expiry: %v", err)
	}
	remaining := expiresAt.Sub(beforeRelease)
	// The row must keep a full TTL measured from commit: it was blocked for
	// ~3s, so a client-side expiry bound at call time leaves only ~5s while
	// a server-side expiry bound at commit leaves ~8s.
	if remaining < 7*time.Second || remaining > 9*time.Second {
		t.Fatalf("committed lease keeps %v of %v TTL after a %v blocked claim, want within [7s, 9s]", remaining, leaseTTL, blockedHold)
	}
}

// TestClaimPartitionLeaseBlockedClaimTakesExpiredRivalAgainstPostgres is the
// live proof for #6765: rival-row liveness must be judged at grant time, not
// call time. A rival lease expiring 1s after the claim starts must be taken
// once the claim is granted 2s later:
//
//   - Unpatched (call-time $6 comparisons): the rival still looks active and
//     the claim reports false.
//   - Patched (clock_timestamp() comparisons): the claim takes the lease.
//
// Same DSN gate as the sibling proof above; skipped when unset.
func TestClaimPartitionLeaseBlockedClaimTakesExpiredRivalAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ESHU_SHARED_PROJECTION_CLAIM_EXPIRY_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_SHARED_PROJECTION_CLAIM_EXPIRY_PROOF_DSN to run the rival-takeover proof")
	}

	ctx := context.Background()
	bootstrapDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open bootstrap connection: %v", err)
	}
	schemaName := fmt.Sprintf("shared_projection_claim_rival_proof_%d", time.Now().UnixNano())
	if _, err := bootstrapDB.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		_ = bootstrapDB.Close()
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := bootstrapDB.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE"); err != nil {
			t.Errorf("drop proof schema: %v", err)
		}
		if err := bootstrapDB.Close(); err != nil {
			t.Errorf("close bootstrap connection: %v", err)
		}
	})

	scopedDSN := dsn + "?search_path=" + schemaName
	if strings.Contains(dsn, "?") {
		scopedDSN = dsn + "&search_path=" + schemaName
	}
	database, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open scoped connection pool: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	database.SetMaxOpenConns(6)

	// Use the real production DDL, not a hand-copied subset.
	if _, err := database.ExecContext(ctx, SharedIntentSchemaSQL()); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}

	const domain = "claim-rival-proof-domain"
	const leaseTTL = 8 * time.Second
	const blockedHold = 2 * time.Second

	// A rival lease that expires 1s from now: still active at call time,
	// expired by grant time.
	if _, err := database.ExecContext(ctx, `INSERT INTO shared_projection_partition_leases (
		projection_domain, partition_id, partition_count,
		lease_owner, lease_expires_at, updated_at
	) VALUES ($1, 0, 4, 'rival-owner', clock_timestamp() + INTERVAL '1 second', clock_timestamp())`, domain); err != nil {
		t.Fatalf("seed rival lease: %v", err)
	}

	holderDB, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open holder connection: %v", err)
	}
	t.Cleanup(func() { _ = holderDB.Close() })
	holderDB.SetMaxOpenConns(1)
	holderTx, err := holderDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder transaction: %v", err)
	}
	if _, err := holderTx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext('shared_projection_partition_leases'), hashtext($1))", domain); err != nil {
		_ = holderTx.Rollback()
		t.Fatalf("acquire holder advisory lock: %v", err)
	}

	store := NewSharedIntentStore(SQLDB{DB: database})
	claimErr := make(chan error, 1)
	claimDone := make(chan bool, 1)
	go func() {
		claimed, err := store.ClaimPartitionLease(ctx, domain, 0, 4, "rival-proof-owner", leaseTTL)
		if err != nil {
			claimErr <- err
			return
		}
		claimDone <- claimed
	}()

	select {
	case <-claimDone:
		_ = holderTx.Rollback()
		t.Fatal("claim returned while the holder still held the advisory key")
	case <-time.After(blockedHold):
	}

	if err := holderTx.Rollback(); err != nil {
		t.Fatalf("release holder advisory lock: %v", err)
	}

	var claimed bool
	select {
	case err := <-claimErr:
		t.Fatalf("blocked claim error = %v", err)
	case claimed = <-claimDone:
	case <-time.After(30 * time.Second):
		t.Fatal("blocked claim did not return within 30s of the holder release")
	}
	if !claimed {
		t.Fatal("blocked claim claimed = false against a rival expired mid-block, want true")
	}
	var owner string
	if err := database.QueryRowContext(ctx, `SELECT lease_owner FROM shared_projection_partition_leases WHERE projection_domain = $1`, domain).Scan(&owner); err != nil {
		t.Fatalf("read lease owner: %v", err)
	}
	if owner != "rival-proof-owner" {
		t.Fatalf("lease owner = %q, want rival-proof-owner", owner)
	}
}
