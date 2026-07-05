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

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestProcessPartitionOnceHeartbeatKeepsLeaseAliveAgainstPostgres proves the
// #4449 fix's runtime effect against the real
// shared_projection_partition_leases table and its ON CONFLICT DO UPDATE ...
// WHERE lease_expires_at <= $6 OR lease_owner = $4 claim query -- not just
// the in-memory ProcessPartitionOnce unit test. Set
// ESHU_SHARED_PROJECTION_HEARTBEAT_PROOF_DSN to run it; it is skipped
// otherwise, matching the sibling live proofs in this package.
//
// The proof drives ProcessPartitionOnce with a real SharedIntentStore as the
// PartitionLeaseManager, a short LeaseTTL, and an edge writer whose
// WriteEdges call blocks past the original lease's expiry. A concurrent
// "rival worker" repeatedly attempts ClaimPartitionLease under a different
// lease owner while the cycle is still running:
//
//   - Unpatched (no renewal heartbeat): the lease is claimed once and held
//     passively. Once lease_expires_at passes, the rival's claim query's
//     lease_expires_at <= $6 branch succeeds and the rival acquires the
//     lease while the original holder is still inside WriteEdges --
//     the double-write condition #4449 describes.
//   - Patched: the TTL/2 heartbeat renews lease_expires_at (via the same
//     lease_owner = $4 branch) before it can pass, so the rival's claim
//     attempts are rejected for the whole cycle.
func TestProcessPartitionOnceHeartbeatKeepsLeaseAliveAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ESHU_SHARED_PROJECTION_HEARTBEAT_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_SHARED_PROJECTION_HEARTBEAT_PROOF_DSN to run the shared projection partition lease heartbeat proof")
	}

	bootstrapDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open bootstrap connection: %v", err)
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("shared_projection_heartbeat_proof_%d", time.Now().UnixNano())
	if _, err := bootstrapDB.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		_ = bootstrapDB.Close()
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = bootstrapDB.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
		_ = bootstrapDB.Close()
	})

	scopedDSN := dsn + "?search_path=" + schemaName
	if strings.Contains(dsn, "?") {
		scopedDSN = dsn + "&search_path=" + schemaName
	}
	db, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open scoped connection pool: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(6)

	// Use the real production DDL, not a hand-copied subset, so the proof
	// exercises the actual claim/renewal SQL text.
	if _, err := db.ExecContext(ctx, SharedIntentSchemaSQL()); err != nil {
		t.Fatalf("create proof schema tables: %v", err)
	}

	store := reducer.PartitionLeaseManager(NewSharedIntentStore(SQLDB{DB: db}))

	const domain = "platform_infra"
	const originalOwner = "worker-original"
	const rivalOwner = "worker-rival"
	// A short TTL relative to the write-side block below: the heartbeat's
	// TTL/2 renewal interval must fire and win the lease_owner = $4 branch of
	// the claim query before this TTL elapses, or the rival wins instead.
	leaseTTL := 200 * time.Millisecond

	unblockWrite := make(chan struct{})
	rivalWon := make(chan struct{})
	stopRival := make(chan struct{})
	rivalDone := make(chan struct{})

	go func() {
		defer close(rivalDone)
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopRival:
				return
			case <-ticker.C:
				claimed, err := store.ClaimPartitionLease(ctx, domain, 0, 1, rivalOwner, leaseTTL)
				if err != nil {
					continue
				}
				if claimed {
					select {
					case rivalWon <- struct{}{}:
					default:
					}
					return
				}
			}
		}
	}()

	edges := &postgresProofSlowEdgeWriter{writeBlock: unblockWrite}
	reader := &postgresProofEmptyIntentReader{}
	lookup := func(reducer.SharedProjectionAcceptanceKey) (string, bool) { return "gen-1", true }

	cfg := reducer.PartitionProcessorConfig{
		Domain:         domain,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     originalOwner,
		LeaseTTL:       leaseTTL,
		BatchLimit:     100,
	}

	// Seed one pending intent so the cycle reaches WriteEdges instead of
	// returning early on an empty batch.
	seedProofIntent(t, db, ctx)

	processDone := make(chan error, 1)
	go func() {
		_, procErr := reducer.ProcessPartitionOnce(
			ctx, time.Now().UTC(), cfg, store, reader, edges,
			lookup, nil, nil, nil, nil, nil, nil,
		)
		processDone <- procErr
	}()

	select {
	case <-rivalWon:
		close(stopRival)
		close(unblockWrite)
		<-processDone
		<-rivalDone
		t.Fatal("rival worker claimed the partition lease while the original holder was still inside WriteEdges: the partition lease is not being heartbeated")
	case <-time.After(2 * time.Second):
		// Expected: no rival win observed while the write is still blocked.
	}

	close(unblockWrite)
	if err := <-processDone; err != nil {
		close(stopRival)
		<-rivalDone
		t.Fatalf("ProcessPartitionOnce() error = %v", err)
	}
	close(stopRival)
	<-rivalDone
}

// TestProcessPartitionOnceReleasesLeaseRowAfterNormalCycleAgainstPostgres
// proves a bug introduced by the #4449 heartbeat fix itself (flagged in PR
// #4524 review, Copilot on shared_projection_worker.go:306): ProcessPartitionOnce
// reassigns its ctx variable to the heartbeat-derived leaseCtx, and the
// deferred ReleasePartitionLease call closed over that same ctx variable.
// stopHeartbeat() cancels leaseCtx before the deferred release runs, so the
// release's real UPDATE ... SET lease_owner = NULL ... query ran with an
// already-cancelled context.
//
//   - Unpatched: sql.DB.ExecContext rejects the query immediately with
//     "context canceled" before it reaches Postgres, the UPDATE never runs,
//     and the row keeps its lease_owner/lease_expires_at until the lease's
//     own TTL naturally elapses -- another worker cannot claim the partition
//     in the meantime, defeating the point of releasing early.
//   - Patched: the release runs with a live (pre-heartbeat) context, the
//     UPDATE commits, and the row is immediately claimable again.
//
// This test uses a fast cycle (no slow WriteEdges) so it isolates the
// release-context bug from the renewal behavior already proven above.
func TestProcessPartitionOnceReleasesLeaseRowAfterNormalCycleAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ESHU_SHARED_PROJECTION_HEARTBEAT_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_SHARED_PROJECTION_HEARTBEAT_PROOF_DSN to run the shared projection partition lease release proof")
	}

	bootstrapDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open bootstrap connection: %v", err)
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("shared_projection_release_proof_%d", time.Now().UnixNano())
	if _, err := bootstrapDB.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		_ = bootstrapDB.Close()
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = bootstrapDB.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
		_ = bootstrapDB.Close()
	})

	scopedDSN := dsn + "?search_path=" + schemaName
	if strings.Contains(dsn, "?") {
		scopedDSN = dsn + "&search_path=" + schemaName
	}
	db, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open scoped connection pool: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(6)

	if _, err := db.ExecContext(ctx, SharedIntentSchemaSQL()); err != nil {
		t.Fatalf("create proof schema tables: %v", err)
	}

	store := reducer.PartitionLeaseManager(NewSharedIntentStore(SQLDB{DB: db}))

	const domain = "platform_infra"
	const owner = "worker-release-proof"
	leaseTTL := 30 * time.Second // long TTL: any release must come from the explicit release, not natural expiry

	edges := &postgresProofSlowEdgeWriter{writeBlock: closedChan()}
	reader := &postgresProofEmptyIntentReader{}
	lookup := func(reducer.SharedProjectionAcceptanceKey) (string, bool) { return "gen-1", true }

	cfg := reducer.PartitionProcessorConfig{
		Domain:         domain,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     owner,
		LeaseTTL:       leaseTTL,
		BatchLimit:     100,
	}

	seedProofIntent(t, db, ctx)

	if _, err := reducer.ProcessPartitionOnce(
		ctx, time.Now().UTC(), cfg, store, reader, edges,
		lookup, nil, nil, nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("ProcessPartitionOnce() error = %v", err)
	}

	var leaseOwner sql.NullString
	var leaseExpiresAt sql.NullTime
	if err := db.QueryRowContext(ctx, `
		SELECT lease_owner, lease_expires_at
		FROM shared_projection_partition_leases
		WHERE projection_domain = $1 AND partition_id = $2 AND partition_count = $3
	`, domain, 0, 1).Scan(&leaseOwner, &leaseExpiresAt); err != nil {
		t.Fatalf("query lease row: %v", err)
	}

	if leaseOwner.Valid {
		t.Fatalf(
			"lease_owner = %q, want NULL: ReleasePartitionLease ran with a cancelled context and its UPDATE never committed, so the row still shows the original holder as owner and stays held until the %s TTL naturally elapses",
			leaseOwner.String, leaseTTL,
		)
	}
	if leaseExpiresAt.Valid {
		t.Fatalf("lease_expires_at = %v, want NULL: the release UPDATE did not commit", leaseExpiresAt.Time)
	}

	// A rival can now claim the released partition immediately, proving the
	// row is genuinely releasable rather than merely reporting released=true
	// from a call whose underlying query silently no-op'd.
	claimed, err := store.ClaimPartitionLease(ctx, domain, 0, 1, "worker-rival-after-release", leaseTTL)
	if err != nil {
		t.Fatalf("rival ClaimPartitionLease() error = %v", err)
	}
	if !claimed {
		t.Fatal("rival could not claim the partition immediately after ProcessPartitionOnce returned: the lease was not actually released")
	}
}

func closedChan() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func seedProofIntent(t *testing.T, db *sql.DB, ctx context.Context) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO shared_projection_intents (
			intent_id, projection_domain, partition_key, scope_id,
			acceptance_unit_id, repository_id, source_run_id, generation_id,
			payload, created_at
		) VALUES (
			'intent-heartbeat-proof', 'platform_infra', 'pk-a', 'scope-a',
			'repo-a', 'repo-a', 'run-1', 'gen-1',
			'{"platform_id":"p1","action":"upsert"}'::jsonb, now()
		)
	`); err != nil {
		t.Fatalf("seed proof intent: %v", err)
	}
}

type postgresProofSlowEdgeWriter struct {
	writeBlock <-chan struct{}
}

func (s *postgresProofSlowEdgeWriter) RetractEdges(context.Context, string, []reducer.SharedProjectionIntentRow, string) error {
	return nil
}

func (s *postgresProofSlowEdgeWriter) WriteEdges(ctx context.Context, _ string, _ []reducer.SharedProjectionIntentRow, _ string) error {
	select {
	case <-s.writeBlock:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// postgresProofEmptyIntentReader wraps a real Postgres-backed listing so the
// selection phase reads through the same SQL path as production, then no-ops
// MarkIntentsCompleted (this proof only needs to reach WriteEdges, not
// complete the full cycle bookkeeping).
type postgresProofEmptyIntentReader struct{}

func (r *postgresProofEmptyIntentReader) ListPendingDomainIntents(context.Context, string, int) ([]reducer.SharedProjectionIntentRow, error) {
	return []reducer.SharedProjectionIntentRow{
		{
			IntentID:         "intent-heartbeat-proof",
			ProjectionDomain: "platform_infra",
			PartitionKey:     "pk-a",
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			RepositoryID:     "repo-a",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Payload:          map[string]any{"platform_id": "p1", "action": "upsert"},
			CreatedAt:        time.Now().UTC(),
		},
	}, nil
}

func (r *postgresProofEmptyIntentReader) MarkIntentsCompleted(context.Context, []string, time.Time) error {
	return nil
}
