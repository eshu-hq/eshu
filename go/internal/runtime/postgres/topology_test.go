// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTargetSessionAttrsRoleOverride(t *testing.T) {
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if writer == "" || reader == "" {
		t.Skip("owned PostgreSQL fixture not configured")
	}
	cases := []struct{ name, writer, reader string }{
		{"same DSN", writer + "&target_session_attrs=read-write", ""},
		{"physical standby", writer, reader + "&target_session_attrs=read-write"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfig(func(key string) string {
				switch key {
				case "ESHU_POSTGRES_DSN":
					return tc.writer
				case "ESHU_POSTGRES_READ_DSN":
					return tc.reader
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
			rows, err := access.Reader().QueryContext(ctx, "SELECT 1")
			if err != nil {
				t.Fatal(err)
			}
			_ = rows.Close()
			if _, err := access.reader.ExecContext(ctx, "CREATE TABLE eshu_reader_forbidden(id integer)"); err == nil {
				t.Fatal("reader accepted write")
			}
			access.reader.SetConnMaxLifetime(time.Nanosecond)
			if _, err := access.reader.ExecContext(ctx, "CREATE TABLE eshu_reader_forbidden(id integer)"); err == nil {
				t.Fatal("reconnected reader accepted write")
			}
		})
	}
}

func TestAccessWrongDatabaseFailsBeforeBusinessSQL(t *testing.T) {
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if writer == "" || reader == "" {
		t.Skip("owned PostgreSQL fixture not configured")
	}
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return writer
		case "ESHU_POSTGRES_READ_DSN":
			return strings.Replace(reader, "/eshu?", "/shim_wrong?", 1)
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
	if _, err := access.Reader().QueryContext(ctx, "SELECT 1"); !errors.Is(err, ErrWrongTopology) {
		t.Fatalf("wrong database = %v", err)
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("failed reader borrowed %d", got)
	}
}

func TestAccessParallelBorrowAndObserver(t *testing.T) {
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if reader == "" {
		t.Skip("owned PostgreSQL fixture not configured")
	}
	access := testAccess(t, reader)
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	const workers = 4
	ready := make(chan error, workers)
	release := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := access.Reader().QueryContext(ctx, "SELECT 1")
			ready <- err
			if err != nil {
				return
			}
			<-release
			_ = rows.Close()
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-ready; err != nil {
			close(release)
			wg.Wait()
			t.Fatal(err)
		}
	}
	if got := access.reader.Stats().InUse; got != workers {
		t.Fatalf("parallel borrowed = %d, want %d", got, workers)
	}
	close(release)
	wg.Wait()
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("parallel cursors leaked %d", got)
	}
	observer := access.observer.(*observed)
	for _, stage := range []Stage{StageWriterCheckpoint, StageReaderBorrow, StageReaderIdentity, StageReaderReplay, StageBusinessQuery} {
		if !observer.has(stage, "") {
			t.Errorf("missing diagnostic stage %s", stage)
		}
	}
}

func TestAccessRefusesReadOnlyWriter(t *testing.T) {
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	if writer == "" {
		t.Skip("owned PostgreSQL fixture not configured")
	}
	cfg, err := LoadConfig(func(key string) string {
		if key == "ESHU_POSTGRES_DSN" {
			return writer + "&default_transaction_read_only=on"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	access, err := Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()
	if _, err := access.ContextWithCheckpoint(context.Background()); !errors.Is(err, ErrWrongTopology) {
		t.Fatalf("read-only writer = %v", err)
	}
}

func TestOpenRejectsUnprovedMultiHostBeforeDial(t *testing.T) {
	for _, tc := range []struct{ name, writer, reader string }{
		{"writer", "host=writer-a,writer-b port=5432,5432 user=eshu dbname=eshu", "postgres://eshu@reader/eshu"},
		{"reader", "postgres://eshu@writer/eshu", "host=reader-a,reader-b port=5432,5432 user=eshu dbname=eshu"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				WriterDSN: tc.writer, ReadDSN: tc.reader, WriterMaxOpenConns: 1, ReadMaxOpenConns: 1,
				PingTimeout: time.Second, ReplayTimeout: time.Second,
			}
			_, err := Open(context.Background(), cfg, nil)
			if err == nil || !strings.Contains(err.Error(), "multi-host") {
				t.Fatalf("direct Open = %v", err)
			}
		})
	}
}

func TestAccessWriterDoesNotClaimInfraDerivation(t *testing.T) {
	access := testAccess(t, "")
	var marker string
	if err := access.Writer().QueryRowContext(context.Background(), "SELECT COALESCE(current_setting('eshu.infra_inventory_writer', true), '')").Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != "" {
		t.Fatalf("API/MCP writer falsely claims infra derivation: %q", marker)
	}
	access.writer.SetConnMaxLifetime(time.Nanosecond)
	if err := access.Writer().QueryRowContext(context.Background(), "SELECT COALESCE(current_setting('eshu.infra_inventory_writer', true), '')").Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != "" {
		t.Fatalf("reconnected API/MCP writer falsely claims infra derivation: %q", marker)
	}
}
