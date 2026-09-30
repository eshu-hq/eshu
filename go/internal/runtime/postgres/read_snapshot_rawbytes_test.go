// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"
)

func TestReadSnapshotRawBytesRejectsAndCancellationReleases(t *testing.T) {
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if reader == "" {
		t.Skip("owned physical reader fixture not configured")
	}
	access := testAccess(t, reader)
	base, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	req, cancel := context.WithCancel(base)
	defer cancel()
	tx, err := access.Reader().BeginReadOnlySnapshot(req)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(req, "SELECT 'raw'::text")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	raw := sql.RawBytes("untouched")
	err = rows.Scan(&raw)
	if err == nil || !strings.Contains(err.Error(), "RawBytes") || !strings.Contains(err.Error(), "*[]byte") {
		t.Fatalf("snapshot RawBytes error=%v", err)
	}
	if string(raw) != "untouched" {
		t.Fatalf("RawBytes destination changed=%q", raw)
	}
	if got := access.reader.Stats().InUse; got != 1 {
		t.Fatalf("cursor rejection ended transaction: in_use=%d", got)
	}
	cancel()
	deadline := time.Now().Add(700 * time.Millisecond)
	for access.reader.Stats().InUse != 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("canceled snapshot held connection until caller closed cursor: in_use=%d", got)
	}
}

func TestReadSnapshotRawBytesMixedDestinationsRemainUntouched(t *testing.T) {
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if reader == "" {
		t.Skip("owned physical reader fixture not configured")
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
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT 'first'::text, 'second'::text")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	first := "unchanged"
	raw := sql.RawBytes("still-unchanged")
	err = rows.Scan(&first, &raw)
	if err == nil || !strings.Contains(err.Error(), "RawBytes") {
		t.Fatalf("mixed scan error=%v", err)
	}
	if first != "unchanged" || string(raw) != "still-unchanged" {
		t.Fatalf("partial scan writes: first=%q raw=%q", first, raw)
	}
	if got := access.reader.Stats().InUse; got != 1 {
		t.Fatalf("cursor rejection ended transaction: in_use=%d", got)
	}
}

func TestReadSnapshotBytesStillScan(t *testing.T) {
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if reader == "" {
		t.Skip("owned physical reader fixture not configured")
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
	rows, err := tx.QueryContext(ctx, "SELECT 'hello'::text, decode('00ff','hex')")
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if !rows.Next() {
		_ = rows.Close()
		_ = tx.Rollback()
		t.Fatal(rows.Err())
	}
	var value string
	var bytes []byte
	if err := rows.Scan(&value, &bytes); err != nil {
		_ = rows.Close()
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if value != "hello" || len(bytes) != 2 || bytes[0] != 0 || bytes[1] != 255 {
		_ = rows.Close()
		_ = tx.Rollback()
		t.Fatalf("value=%q bytes=%x", value, bytes)
	}
	if err := rows.Close(); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if got := access.reader.Stats().InUse; got != 1 {
		_ = tx.Rollback()
		t.Fatalf("cursor close ended transaction: in_use=%d", got)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("commit leaked connection: in_use=%d", got)
	}
	ordinary, err := access.Reader().QueryContext(ctx, "SELECT 'ordinary'::text")
	if err != nil {
		t.Fatal(err)
	}
	if !ordinary.Next() {
		_ = ordinary.Close()
		t.Fatal(ordinary.Err())
	}
	var raw sql.RawBytes
	if err := ordinary.Scan(&raw); err != nil || string(raw) != "ordinary" {
		_ = ordinary.Close()
		t.Fatalf("ordinary cursor RawBytes=%q err=%v", raw, err)
	}
	if err := ordinary.Close(); err != nil {
		t.Fatal(err)
	}
}
