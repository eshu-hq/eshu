// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9975 disposable PostgreSQL proof that migration 278 sets both fact_records autovacuum scale factors in pg_class.reloptions as a catalog-only change (same relation file, no rewrite), and that a ledger-less replay of the statement is a no-op
//eshu:readiness:env ESHU_TEST_FACT_RECORDS_AUTOVACUUM_POSTGRES_DISPOSABLE ESHU_TEST_FACT_RECORDS_AUTOVACUUM_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	factRecordsAutovacuumDSN        = "ESHU_TEST_FACT_RECORDS_AUTOVACUUM_POSTGRES_DSN"
	factRecordsAutovacuumDisposable = "ESHU_TEST_FACT_RECORDS_AUTOVACUUM_POSTGRES_DISPOSABLE"
	factRecordsAutovacuumMigration  = "fact_records_autovacuum_reloptions"
)

// TestFactRecordsAutovacuumReloptionsMigrationLive proves migration 278 on a
// disposable PostgreSQL: every earlier migration leaves fact_records on the
// server defaults, the migration sets exactly the two ruled storage
// parameters without rewriting the relation, and replaying the statement
// without its ledger receipt changes nothing.
func TestFactRecordsAutovacuumReloptionsMigrationLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableDatabase(t,
		os.Getenv(factRecordsAutovacuumDSN), os.Getenv(factRecordsAutovacuumDisposable), 5*time.Minute)
	definitions := BootstrapDefinitions()
	target := -1
	for i, definition := range definitions {
		if definition.Name == factRecordsAutovacuumMigration {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatalf("migration %q missing from BootstrapDefinitions()", factRecordsAutovacuumMigration)
	}
	exec := SQLDB{DB: database}
	apply := func(defs []Definition) {
		t.Helper()
		if err := applyBootstrapDefinitionsWith(ctx, exec, defs, slog.Default(), schemaBootstrapCoordination{}); err != nil {
			t.Fatalf("apply %d definitions: %v", len(defs), err)
		}
	}

	apply(definitions[:target])
	if got := factRecordsReloptions(t, ctx, database); got != "" {
		t.Fatalf("reloptions before migration 278 = %q, want none (server defaults)", got)
	}
	filenodeBefore := factRecordsFilenode(t, ctx, database)

	apply(definitions[target : target+1])
	assertFactRecordsAutovacuumReloptions(t, ctx, database)
	if after := factRecordsFilenode(t, ctx, database); after != filenodeBefore {
		t.Fatalf("relation file changed %d -> %d: the ALTER rewrote fact_records, want catalog-only", filenodeBefore, after)
	}

	// A normal restart skips the receipted migration; both paths must leave the
	// options byte-identical.
	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("restart ApplyBootstrap: %v", err)
	}
	assertFactRecordsAutovacuumReloptions(t, ctx, database)
	optionsAfterRestart := factRecordsReloptions(t, ctx, database)

	// The local supervisor re-applies every definition without a ledger, so the
	// statement itself must be a no-op when it runs a second time.
	if _, err := database.ExecContext(ctx,
		`DELETE FROM eshu_schema_migrations WHERE path = $1`, definitions[target].Path); err != nil {
		t.Fatalf("drop migration 278 receipt: %v", err)
	}
	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("replay ApplyBootstrap without receipt: %v", err)
	}
	if got := factRecordsReloptions(t, ctx, database); got != optionsAfterRestart {
		t.Fatalf("reloptions after replay = %q, want unchanged %q", got, optionsAfterRestart)
	}
	if after := factRecordsFilenode(t, ctx, database); after != filenodeBefore {
		t.Fatalf("relation file changed %d -> %d on replay, want catalog-only", filenodeBefore, after)
	}
	var receipts int
	if err := database.QueryRowContext(ctx,
		`SELECT count(*) FROM eshu_schema_migrations WHERE path = $1`, definitions[target].Path).Scan(&receipts); err != nil {
		t.Fatalf("count migration 278 receipts: %v", err)
	}
	if receipts != 1 {
		t.Fatalf("migration 278 receipts = %d, want exactly 1 after replay", receipts)
	}
}

func assertFactRecordsAutovacuumReloptions(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	got := factRecordsReloptions(t, ctx, database)
	for _, want := range []string{
		"autovacuum_vacuum_scale_factor=0.02",
		"autovacuum_vacuum_insert_scale_factor=0.05",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("fact_records reloptions = %q, want it to contain %q", got, want)
		}
	}
	for _, banned := range []string{"cost_delay", "cost_limit", "toast.", "analyze", "fillfactor"} {
		if strings.Contains(got, banned) {
			t.Errorf("fact_records reloptions = %q, must not carry %q", got, banned)
		}
	}
}

func factRecordsReloptions(t *testing.T, ctx context.Context, database *sql.DB) string {
	t.Helper()
	var options string
	if err := database.QueryRowContext(ctx,
		`SELECT coalesce(array_to_string(reloptions, ','), '') FROM pg_class WHERE oid = 'fact_records'::regclass`).Scan(&options); err != nil {
		t.Fatalf("read fact_records reloptions: %v", err)
	}
	return options
}

func factRecordsFilenode(t *testing.T, ctx context.Context, database *sql.DB) int64 {
	t.Helper()
	var filenode int64
	if err := database.QueryRowContext(ctx,
		`SELECT pg_relation_filenode('fact_records'::regclass)::bigint`).Scan(&filenode); err != nil {
		t.Fatalf("read fact_records relation filenode: %v", err)
	}
	return filenode
}
