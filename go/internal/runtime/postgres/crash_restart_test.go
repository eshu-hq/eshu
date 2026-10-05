// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestAccessCrashRestartUnderWriteLoadRecovers kills the owned primary with
// SIGKILL while asynchronous commits are in flight, so crash recovery ends
// below the insert LSN the last checkpoint observed. The same Access must
// still recover: its restart watermark is the flushed LSN, which crash
// recovery cannot lose. A watermark built from the insert LSN wedges here.
func TestAccessCrashRestartUnderWriteLoadRecovers(t *testing.T) {
	container := restartContainer(t)
	writerDSN := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	access := testAccess(t, "")
	ctx := context.Background()
	if _, err := access.Writer().ExecContext(ctx, "CREATE TABLE IF NOT EXISTS eshu_lineage_crash_probe(id bigserial PRIMARY KEY, payload text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	// A long WAL writer delay keeps asynchronous commits in WAL buffers long
	// enough for the kill to discard them. The fixture is owned and disposable.
	if _, err := access.Writer().ExecContext(ctx, "ALTER SYSTEM SET wal_writer_delay = '10s'"); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Writer().ExecContext(ctx, "SELECT pg_reload_conf()"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = access.Writer().ExecContext(context.Background(), "ALTER SYSTEM RESET wal_writer_delay")
		_, _ = access.Writer().ExecContext(context.Background(), "SELECT pg_reload_conf()")
	})
	for attempt := 1; attempt <= 3; attempt++ {
		lastInsert := crashUnderLoad(t, access, container)
		lost := crashLostInsertLSN(t, writerDSN, lastInsert)
		requireRecovered(t, access)
		if lost {
			t.Logf("attempt %d: recovery ended below checkpoint insert LSN %s and the same Access recovered", attempt, lastInsert)
			return
		}
		t.Logf("attempt %d: crash kept all WAL up to %s; retrying", attempt, lastInsert)
	}
	t.Fatal("crash never discarded WAL past the last checkpoint insert LSN; the flush-versus-insert hazard was not exercised")
}

// crashUnderLoad runs asynchronous-commit inserts, takes a checkpoint, and
// immediately SIGKILLs the primary. It returns that checkpoint's insert LSN.
func crashUnderLoad(t *testing.T, access *Access, container string) string {
	t.Helper()
	loadCtx, stopLoad := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for loadCtx.Err() == nil {
				tx, err := access.Writer().BeginTx(loadCtx, nil)
				if err != nil {
					continue
				}
				_, _ = tx.ExecContext(loadCtx, "SET LOCAL synchronous_commit = off")
				_, _ = tx.ExecContext(loadCtx, "INSERT INTO eshu_lineage_crash_probe(payload) SELECT repeat('x', 200) FROM generate_series(1, 500)")
				_ = tx.Commit()
			}
		}()
	}
	time.Sleep(500 * time.Millisecond)
	// Quiesce first: later WAL would push the checkpoint's unflushed tail out
	// of WAL buffers before the kill lands.
	stopLoad()
	wg.Wait()
	checked, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	docker(t, "kill", "-s", "KILL", container)
	docker(t, "start", container)
	return checked.Value(checkpointKey{}).(checkpoint).lsn
}

// crashLostInsertLSN reports, through an independent connection, whether the
// recovered primary's insert position is below the pre-crash checkpoint.
func crashLostInsertLSN(t *testing.T, dsn, lastInsert string) bool {
	t.Helper()
	var lost bool
	err := eventually(30*time.Second, func() error {
		conn, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		return conn.QueryRow(context.Background(), "SELECT pg_current_wal_insert_lsn() < $1::pg_lsn", lastInsert).Scan(&lost)
	})
	if err != nil {
		t.Fatalf("recovered primary never accepted a probe connection: %v", err)
	}
	return lost
}
