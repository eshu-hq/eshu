// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Test7237IndexMigrationsAllowConcurrentWritersLive applies the embedded
// migrations through the tracked bootstrap runner while writes are active.
func Test7237IndexMigrationsAllowConcurrentWritersLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_7237_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_7237_TEST_DSN to an empty disposable PostgreSQL database")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, statement := range []string{
		"CREATE TABLE fact_records (fact_id TEXT, fact_kind TEXT, is_tombstone BOOLEAN, observed_at TIMESTAMPTZ)",
		"CREATE TABLE content_entities (entity_id TEXT, repo_id TEXT, relative_path TEXT, start_line INTEGER)",
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("create fixture table: %v", err)
		}
	}

	matched := 0
	for _, definition := range BootstrapDefinitions() {
		var table, insert string
		switch {
		case strings.Contains(definition.Path, "131_fact_records_semantic_code_hint_order_idx"):
			table = "fact_records"
			insert = "INSERT INTO fact_records (fact_id, fact_kind, is_tombstone, observed_at) VALUES ('a', 'semantic.code_hint', FALSE, now())"
		case strings.Contains(definition.Path, "132_content_entities_repo_path_start_idx"):
			table = "content_entities"
			insert = "INSERT INTO content_entities (entity_id, repo_id, relative_path, start_line) VALUES ('a', 'r', 'p', 1)"
		default:
			continue
		}
		matched++
		t.Run(table, func(t *testing.T) {
			writer, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin held writer: %v", err)
			}
			defer func() { _ = writer.Rollback() }()
			if _, err := writer.ExecContext(ctx, insert); err != nil {
				t.Fatalf("hold uncommitted write: %v", err)
			}
			applied := make(chan error, 1)
			go func() {
				applied <- applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, []Definition{definition}, slog.Default(), schemaBootstrapCoordination{})
			}()
			progress := false
			for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
				if err := database.QueryRowContext(ctx,
					"SELECT EXISTS (SELECT 1 FROM pg_stat_progress_create_index WHERE relid = $1::regclass)", table,
				).Scan(&progress); err != nil {
					t.Fatalf("index build progress: %v", err)
				}
				if progress {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !progress {
				t.Fatal("concurrent index build did not enter progress while writer was open")
			}
			if _, err := database.ExecContext(ctx, insert); err != nil {
				t.Fatalf("second writer during index build: %v", err)
			}
			select {
			case err := <-applied:
				t.Fatalf("index build finished before held writer committed: %v", err)
			default:
			}
			if err := writer.Commit(); err != nil {
				t.Fatalf("commit held writer: %v", err)
			}
			if err := <-applied; err != nil {
				t.Fatalf("tracked migration: %v", err)
			}
			var valid, ready bool
			indexName := "fact_records_semantic_code_hint_order_idx"
			if table == "content_entities" {
				indexName = "content_entities_repo_path_start_idx"
			}
			if err := database.QueryRowContext(ctx, `SELECT i.indisvalid, i.indisready
			FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
			WHERE c.relname = $1`, indexName).Scan(&valid, &ready); err != nil || !valid || !ready {
				t.Fatalf("index status valid=%t ready=%t err=%v", valid, ready, err)
			}
			var checksum string
			if err := database.QueryRowContext(ctx,
				"SELECT checksum_sha256 FROM eshu_schema_migrations WHERE path = $1 AND variant = 'full'", definition.Path,
			).Scan(&checksum); err != nil || checksum != migrationChecksum(definition.SQL) {
				t.Fatalf("tracked checksum=%q err=%v", checksum, err)
			}
		})
	}
	if matched != 2 {
		t.Fatalf("matched %d migrations, want 2", matched)
	}
}
