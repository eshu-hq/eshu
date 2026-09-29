// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// TestMigration148AddColumnIsBoundedByTheRunner proves migration 148 against
// a held ROW EXCLUSIVE lock, the lock an ingestion commit holds on
// scope_generations while it streams facts. One attempt fails with 55P03
// inside the runner's lock_timeout. The runner's retry then waits the holder
// out; an Ack issued while the ALTER waits defers (ErrWorkAckDeferred) rather
// than failing, and succeeds once the column exists.
func TestMigration148AddColumnIsBoundedByTheRunner(t *testing.T) {
	dsn := proofDSN(t)
	database := provisionFenceProof(t, dsn, "gen-a", []fenceGen{
		genActiveA, {id: "gen-f", commit: "F", status: "pending", minutesAgo: 5},
	}, "gen-f")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := database.ExecContext(ctx, "ALTER TABLE scope_generations DROP COLUMN delta_baseline_commit_sha"); err != nil {
		t.Fatalf("drop column to replay migration 148: %v", err)
	}
	migrationSQL := ""
	for _, def := range BootstrapDefinitions() {
		if strings.HasSuffix(def.Path, "148_scope_generations_delta_baseline_commit_sha.sql") {
			migrationSQL = def.SQL
		}
	}
	if migrationSQL == "" {
		t.Fatal("migration 148 is not in BootstrapDefinitions()")
	}

	holder := searchPathPeer(t, dsn, database)
	holdTx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holdTx.Rollback() }()
	if _, err := holdTx.ExecContext(ctx, "UPDATE scope_generations SET observed_at = observed_at WHERE false"); err != nil {
		t.Fatalf("take ROW EXCLUSIVE: %v", err)
	}

	migrator := SQLDB{DB: searchPathPeer(t, dsn, database)}
	if _, err := migrator.execContextWithLockTimeout(ctx, migrationSQL, 200*time.Millisecond); !coordination.IsLockNotAvailable(err) {
		t.Fatalf("one bounded attempt under the held lock = %v, want 55P03", err)
	}

	migrated := make(chan error, 1)
	go func() {
		allowance := coordination.NewLockRetryAllowance(45 * time.Second)
		policy := coordination.LockRetryPolicy{
			Allowance:      allowance,
			InitialBackoff: 100 * time.Millisecond, MaxBackoff: 200 * time.Millisecond,
		}
		migrated <- coordination.RetryOnLockTimeout(ctx, slog.Default(), "148", policy, coordination.SleepContext, time.Now,
			func() error {
				_, err := migrator.execContextWithLockTimeout(ctx, migrationSQL, defaultSchemaLockTimeout)
				return err
			})
	}()
	waitForPendingAccessExclusive(ctx, t, database)

	queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
	queue.AckScopeLockTimeout = 300 * time.Millisecond
	if err := queue.Ack(ctx, fenceWork("gen-f"), runtime.Result{}); !errors.Is(err, failure.ErrWorkAckDeferred) {
		t.Fatalf("Ack while the ALTER waits = %v, want ErrWorkAckDeferred", err)
	}
	if got := readFenceState(t, database, "gen-f"); got.work != "running" || got.pointer != "gen-a" {
		t.Fatalf("deferred Ack moved state: %+v", got)
	}

	if err := holdTx.Commit(); err != nil {
		t.Fatalf("release holder: %v", err)
	}
	if err := <-migrated; err != nil {
		t.Fatalf("migration 148 through the runner's retry = %v, want nil", err)
	}
	if err := queue.Ack(ctx, fenceWork("gen-f"), runtime.Result{}); err != nil {
		t.Fatalf("Ack after migration = %v, want nil", err)
	}
	if got := readFenceState(t, database, "gen-f"); got.target != "active" || got.pointer != "gen-f" {
		t.Fatalf("state after Ack = %+v, want gen-f active", got)
	}
}

// waitForPendingAccessExclusive blocks until a session waits for ACCESS
// EXCLUSIVE on scope_generations, so the Ack runs while the ALTER is queued.
func waitForPendingAccessExclusive(ctx context.Context, t *testing.T, database *sql.DB) {
	t.Helper()
	for {
		var waiting int
		if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_locks
WHERE NOT granted AND mode = 'AccessExclusiveLock' AND relation = 'scope_generations'::regclass`,
		).Scan(&waiting); err != nil {
			t.Fatalf("read pg_locks: %v", err)
		}
		if waiting > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("migration 148 never waited on the held lock")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
