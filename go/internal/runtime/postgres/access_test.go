// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type observed struct {
	mu   sync.Mutex
	seen map[Stage]map[Outcome]int
}

func (o *observed) Observe(_ string, stage Stage, outcome Outcome, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.seen[stage] == nil {
		o.seen[stage] = map[Outcome]int{}
	}
	o.seen[stage][outcome]++
}

func (o *observed) has(stage Stage, outcome Outcome) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if outcome == "" {
		return len(o.seen[stage]) > 0
	}
	return o.seen[stage][outcome] > 0
}

func testAccess(t *testing.T, reader string) *Access {
	t.Helper()
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	if writer == "" {
		t.Skip("owned disposable PostgreSQL fixture not configured")
	}
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return writer
		case "ESHU_POSTGRES_READ_DSN":
			return reader
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.ReplayTimeout = 500 * time.Millisecond
	access, err := Open(context.Background(), cfg, &observed{seen: map[Stage]map[Outcome]int{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := access.Close(); err != nil {
			t.Error(err)
		}
	})
	return access
}

func TestAccessSamePrimaryAndPrivateCheckpoint(t *testing.T) {
	access := testAccess(t, "")
	if _, err := access.Reader().QueryContext(context.Background(), "SELECT 1"); !errors.Is(err, ErrMissingCheckpoint) {
		t.Fatalf("missing checkpoint error = %v", err)
	}
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := access.Reader().QueryContext(ctx, "SELECT 42")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	var value int
	if err := rows.Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != 42 {
		t.Fatalf("value = %d", value)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("cursor leaked %d connections", got)
	}
	other := &Access{}
	if _, err := (fencedQueryer{access: other}).QueryContext(ctx, "SELECT 1"); !errors.Is(err, ErrMissingCheckpoint) {
		t.Fatalf("foreign checkpoint = %v", err)
	}
	if _, err := access.reader.ExecContext(ctx, "CREATE TABLE eshu_reader_forbidden(id integer)"); err == nil {
		t.Fatal("same-primary reader accepted write")
	}
	access.reader.SetConnMaxLifetime(time.Nanosecond)
	if _, err := access.reader.ExecContext(ctx, "CREATE TABLE eshu_reader_forbidden(id integer)"); err == nil {
		t.Fatal("reconnected reader accepted write")
	}
}

func TestAccessStreamingReaderFenceAndCursors(t *testing.T) {
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if reader == "" {
		t.Skip("owned streaming reader fixture not configured")
	}
	access := testAccess(t, reader)
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := access.Reader().QueryContext(ctx, "SELECT 1 UNION ALL SELECT 2")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var value int
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("exhausted cursor leaked %d connections", got)
	}
	rows, err = access.Reader().QueryContext(ctx, "SELECT 'bad'")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	var number int
	if err := rows.Scan(&number); err == nil {
		t.Fatal("expected scan error")
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("scan-error cursor leaked %d connections", got)
	}
	for i := 0; i < 100; i++ {
		req, cancel := context.WithCancel(ctx)
		rows, err := access.Reader().QueryContext(req, "SELECT 1")
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
		_ = rows.Close()
	}
	if got := access.reader.Stats().InUse; got != 0 {
		t.Fatalf("cancelled cursor leaked %d connections", got)
	}
	if _, err := access.reader.ExecContext(ctx, "CREATE TABLE eshu_reader_forbidden(id integer)"); err == nil {
		t.Fatal("standby reader accepted write")
	}
}

func TestAccessStreamingRoleAndReplayFailClosed(t *testing.T) {
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if reader == "" {
		t.Skip("owned streaming reader fixture not configured")
	}
	access := testAccess(t, reader)
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := access.Writer().ExecContext(ctx, "INSERT INTO reader_theory.cross_scope_completion_upgrade_markers(marker_name, applied_at) VALUES($1,now())", fmt.Sprintf("core-%d", time.Now().UnixNano())); err != nil {
		t.Fatal(err)
	}
	ctx, err = access.ContextWithCheckpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := access.Reader().QueryContext(ctx, "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return writer
		case "ESHU_POSTGRES_READ_DSN":
			return writer + "&application_name=wrong-role"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := Open(context.Background(), cfg, nil)
	if err == nil {
		_ = wrong.Close()
		t.Fatal("writer mislabeled reader was accepted")
	}
	if !strings.Contains(err.Error(), "hot standby") {
		t.Fatalf("wrong role error = %v", err)
	}
}

func TestAccessReplayDeadlineBeforeBusinessSQL(t *testing.T) {
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if reader == "" {
		t.Skip("owned streaming reader fixture not configured")
	}
	access := testAccess(t, reader)
	if _, err := access.reader.Exec("SELECT pg_wal_replay_pause()"); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = access.reader.Exec("SELECT pg_wal_replay_resume()") }()
	marker := fmt.Sprintf("core-lag-%d", time.Now().UnixNano())
	if _, err := access.Writer().Exec("INSERT INTO reader_theory.cross_scope_completion_upgrade_markers(marker_name, applied_at) VALUES($1,now())", marker); err != nil {
		t.Fatal(err)
	}
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = access.Reader().QueryContext(ctx, "SELECT marker_name FROM reader_theory.cross_scope_completion_upgrade_markers WHERE marker_name=$1", marker)
	if !errors.Is(err, ErrReaderStale) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("paused replay error = %v", err)
	}
	observer := access.observer.(*observed)
	if !observer.has(StageReaderReplay, OutcomeDeadline) || observer.has(StageBusinessQuery, "") {
		t.Fatal("replay deadline diagnostics missing or business SQL executed")
	}
	if _, err := access.reader.Exec("SELECT pg_wal_replay_resume()"); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		rows, err := access.Reader().QueryContext(deadline, "SELECT marker_name FROM reader_theory.cross_scope_completion_upgrade_markers WHERE marker_name=$1", marker)
		if err == nil {
			if !rows.Next() {
				t.Fatal(rows.Err())
			}
			var got string
			if err := rows.Scan(&got); err != nil {
				t.Fatal(err)
			}
			_ = rows.Close()
			if got != marker {
				t.Fatal(got)
			}
			break
		}
		if deadline.Err() != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLoadConfigRedactsMalformedEndpoint(t *testing.T) {
	for _, bad := range []string{"postgres://secret:password@%", "host=oops password=secret badarg"} {
		_, err := LoadConfig(func(key string) string {
			if key == "ESHU_POSTGRES_DSN" {
				return bad
			}
			return ""
		})
		if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "password") {
			t.Fatalf("parse error = %v", err)
		}
	}
}

func TestOpenRejectsInconsistentSamePrimaryBeforeDial(t *testing.T) {
	cfg := Config{
		WriterDSN: "postgres://user:secret@writer/db", ReadDSN: "postgres://user:secret@reader/db", SamePrimary: true,
		WriterMaxOpenConns: 15, ReadMaxOpenConns: 15, WriterMaxIdleConns: 5, ReadMaxIdleConns: 5, PingTimeout: time.Second, ReplayTimeout: time.Second,
	}
	_, err := Open(context.Background(), cfg, nil)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("Open error = %v", err)
	}
}
