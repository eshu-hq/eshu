// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"os"
	"testing"
)

func TestAccessDiscardsBorrowedWrongTopologyConnection(t *testing.T) {
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if reader == "" {
		t.Skip("owned PostgreSQL reader fixture not configured")
	}
	access := testAccess(t, reader)
	access.reader.SetMaxOpenConns(1)
	access.reader.SetMaxIdleConns(1)
	conn, err := access.reader.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var badPID int
	if err := conn.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&badPID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(t.Context(), "SET default_transaction_read_only=off"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, err := access.ContextWithCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := access.Reader().QueryContext(ctx, "SELECT 1"); !errors.Is(err, ErrWrongTopology) {
		t.Fatalf("first borrow = %v, want wrong topology", err)
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("failed borrow retained %d connections", got)
	}
	rows, err := access.Reader().QueryContext(ctx, "SELECT pg_backend_pid()")
	if err != nil {
		t.Fatalf("next borrow reused wrong-topology connection: %v", err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	var goodPID int
	if err := rows.Scan(&goodPID); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if goodPID == badPID {
		t.Fatalf("wrong-topology backend %d was reused", badPID)
	}
	if err := access.Reader().QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&badPID); err != nil || badPID != goodPID {
		t.Fatalf("healthy backend was not reused: got=%d want=%d err=%v", badPID, goodPID, err)
	}
}
