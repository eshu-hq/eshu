// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func TestReadStoreRowAndSnapshot(t *testing.T) {
	access := testAccess(t, "")
	store := access.Reader()
	if err := store.QueryRowContext(context.Background(), "SELECT 1").Scan(new(int)); !errors.Is(err, ErrMissingCheckpoint) {
		t.Fatalf("missing row checkpoint: %v", err)
	}
	if _, err := store.BeginReadOnlySnapshot(context.Background()); !errors.Is(err, ErrMissingCheckpoint) {
		t.Fatalf("missing snapshot checkpoint: %v", err)
	}
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got int
	if err := store.QueryRowContext(ctx, "SELECT 42").Scan(&got); err != nil || got != 42 {
		t.Fatalf("row=%d error=%v", got, err)
	}
	if err := store.QueryRowContext(ctx, "SELECT 1 WHERE false").Scan(&got); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("empty row: %v", err)
	}
	var raw sql.RawBytes
	if err := store.QueryRowContext(ctx, "SELECT 1").Scan(&raw); err == nil {
		t.Fatal("RawBytes accepted")
	}
	tx, err := store.BeginReadOnlySnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := access.reader.Stats().InUse; got != 1 {
		t.Fatalf("snapshot in use=%d", got)
	}
	rows, err := tx.QueryContext(ctx, "SELECT 7")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	if err := rows.Scan(&got); err != nil || got != 7 {
		t.Fatalf("snapshot cursor=%d error=%v", got, err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if got := access.reader.Stats().InUse; got != 1 {
		t.Fatalf("snapshot released by cursor: %d", got)
	}
	if err := tx.QueryRowContext(ctx, "SELECT 9").Scan(&got); err != nil || got != 9 {
		t.Fatalf("snapshot row=%d error=%v", got, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("terminal rollback: %v", err)
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("snapshot leaked=%d", got)
	}
}

func TestReadStoreStreamingSnapshotAndCancellation(t *testing.T) {
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if reader == "" {
		t.Skip("owned physical standby not configured")
	}
	access := testAccess(t, reader)
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := access.Reader().BeginReadOnlySnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var before, after int
	query := "SELECT count(*) FROM reader_theory.cross_scope_completion_upgrade_markers"
	if err := tx.QueryRowContext(ctx, query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Writer().ExecContext(ctx, "INSERT INTO reader_theory.cross_scope_completion_upgrade_markers(marker_name, applied_at) VALUES($1,now())", fmt.Sprintf("snapshot-%d", time.Now().UnixNano())); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, query).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("repeatable snapshot changed: %d to %d", before, after)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, query).Scan(&after); !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("query after commit: %v", err)
	}
	if _, ok := any(tx).(db.Executor); ok {
		t.Fatal("read transaction exposes writes")
	}
	if _, ok := any(access.Reader()).(db.Executor); ok {
		t.Fatal("read store exposes writes")
	}
	ctx2, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	next, err := access.Reader().BeginReadOnlySnapshot(ctx2)
	if err != nil {
		t.Fatal(err)
	}
	if err := next.QueryRowContext(ctx2, query).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Fatalf("new snapshot count=%d before=%d", after, before)
	}
	if err := next.Rollback(); err != nil {
		t.Fatal(err)
	}
	cancelBase, cancel := context.WithCancel(ctx2)
	canceled, err := access.Reader().BeginReadOnlySnapshot(cancelBase)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for access.reader.Stats().InUse != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("canceled snapshot leaked=%d", got)
	}
	_ = canceled.Rollback()
}

func TestReadStoreCandidatePoolAggregateCap(t *testing.T) {
	writerDSN := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	readerCandidates := os.Getenv("ESHU_READER_TEST_READ_CANDIDATES_DSN")
	if writerDSN == "" || readerCandidates == "" {
		t.Skip("owned writer and reader candidate DSNs not configured")
	}
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return writerDSN
		case "ESHU_POSTGRES_READ_DSN":
			return readerCandidates
		case "ESHU_POSTGRES_READ_MAX_OPEN_CONNS":
			return "6"
		case "ESHU_POSTGRES_READ_MAX_IDLE_CONNS":
			return "0"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	access, err := Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for attempt := 0; attempt < 8 && len(seen) < 2; attempt++ {
		held := make([]db.Rows, 0, 6)
		for i := 0; i < 6; i++ {
			rows, err := access.Reader().QueryContext(ctx, "SELECT inet_server_addr()::text")
			if err != nil {
				t.Fatal(err)
			}
			held = append(held, rows)
			if !rows.Next() {
				t.Fatal(rows.Err())
			}
			var addr string
			if err := rows.Scan(&addr); err != nil {
				t.Fatal(err)
			}
			seen[addr] = true
		}
		if got := access.reader.Stats(); got.InUse != 6 || got.OpenConnections > 6 {
			t.Fatalf("candidate pool exceeded shared cap: %+v", got)
		}
		for _, rows := range held {
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(seen) != 2 {
		t.Fatalf("reader candidates not both selected: %v", seen)
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("candidate cursors leaked=%d", got)
	}
}

type failingCloseRows struct {
	next                         bool
	scanErr, cursorErr, closeErr error
}

func (r *failingCloseRows) Next() bool {
	if r.next {
		r.next = false
		return true
	}
	return false
}
func (r *failingCloseRows) Scan(...any) error { return r.scanErr }
func (r *failingCloseRows) Err() error        { return r.cursorErr }
func (r *failingCloseRows) Close() error      { return r.closeErr }

func TestReadStoreRowCloseAndErrorPrecedence(t *testing.T) {
	closeErr := errors.New("close failed")
	scanErr := errors.New("scan failed")
	if err := (&fencedRow{rows: &failingCloseRows{next: true, closeErr: closeErr}}).Scan(new(int)); !errors.Is(err, closeErr) {
		t.Fatalf("successful scan close error=%v", err)
	}
	if err := (&fencedRow{rows: &failingCloseRows{next: true, scanErr: scanErr, closeErr: closeErr}}).Scan(new(int)); !errors.Is(err, scanErr) {
		t.Fatalf("scan error precedence=%v", err)
	}
	if err := (&fencedRow{rows: &failingCloseRows{cursorErr: scanErr, closeErr: closeErr}}).Scan(new(int)); !errors.Is(err, scanErr) {
		t.Fatalf("cursor error precedence=%v", err)
	}
	if err := (&fencedRow{rows: &failingCloseRows{closeErr: closeErr}}).Scan(new(int)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("empty row=%v", err)
	}
}

func TestReadStoreImmediateCancelRace(t *testing.T) {
	access := testAccess(t, "")
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		req, cancel := context.WithCancel(ctx)
		tx, err := access.Reader().BeginReadOnlySnapshot(req)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
		_ = tx.Rollback()
	}
	deadline := time.Now().Add(2 * time.Second)
	for access.reader.Stats().InUse != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("immediate cancel leaked=%d", got)
	}
}

type rowCloseProbe struct {
	closeErr error
	closes   int
	nexts    int
	scans    int
}

func (r *rowCloseProbe) Next() bool        { r.nexts++; return true }
func (r *rowCloseProbe) Scan(...any) error { r.scans++; return nil }
func (r *rowCloseProbe) Err() error        { return nil }
func (r *rowCloseProbe) Close() error      { r.closes++; return r.closeErr }

func TestFencedRowRawBytesPreservesCloseError(t *testing.T) {
	closeErr := errors.New("injected cursor close failure")
	rows := &rowCloseProbe{closeErr: closeErr}
	first := "untouched"
	raw := sql.RawBytes("still-untouched")
	err := (&fencedRow{rows: rows}).Scan(&first, &raw)
	if err == nil || !strings.Contains(err.Error(), "RawBytes") || !errors.Is(err, closeErr) {
		t.Fatalf("RawBytes and Close error=%v", err)
	}
	if rows.closes != 1 || rows.nexts != 0 || rows.scans != 0 {
		t.Fatalf("row cursor calls closes=%d nexts=%d scans=%d", rows.closes, rows.nexts, rows.scans)
	}
	if first != "untouched" || string(raw) != "still-untouched" {
		t.Fatalf("partial scan writes: first=%q raw=%q", first, raw)
	}
}
