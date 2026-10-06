// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

func collapse(statement string) string {
	return strings.Join(strings.Fields(statement), " ")
}

// withoutComments drops SQL line comments so a check on statements does not
// match the prose that explains them.
func withoutComments(statement string) string {
	var kept []string
	for _, line := range strings.Split(statement, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func TestUpsertSQLGuardsOnStrictlyOlderAsOf(t *testing.T) {
	t.Parallel()

	got := collapse(upsertSQL)
	for _, want := range []string{
		"INSERT INTO status_summary_snapshots AS existing",
		"ON CONFLICT (model_key) DO UPDATE",
		"WHERE existing.as_of < EXCLUDED.as_of",
		"clock_timestamp()",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("upsertSQL missing %q in:\n%s", want, got)
		}
	}
	// Strict "<": an equal as_of is a replay and must not rewrite the row.
	if strings.Contains(got, "<=") {
		t.Errorf("upsertSQL must use a strict as_of guard, found <= in:\n%s", got)
	}
}

func TestReadSQLIsAKeyedSingleRowLookup(t *testing.T) {
	t.Parallel()

	got := collapse(readSQL)
	if !strings.HasSuffix(got, "FROM status_summary_snapshots WHERE model_key = $1") {
		t.Errorf("readSQL must end in a keyed lookup, got:\n%s", got)
	}
	for _, banned := range []string{"as_of >", "as_of <", "ORDER BY", "jsonb_array_elements", "LIMIT"} {
		if strings.Contains(got, banned) {
			t.Errorf("readSQL must stay a primary-key lookup, found %q in:\n%s", banned, got)
		}
	}
}

func TestLockSQLIsATransactionScopedTryLock(t *testing.T) {
	t.Parallel()

	if got := collapse(tryLockSQL); got != "SELECT pg_try_advisory_xact_lock($1)" {
		t.Errorf("tryLockSQL = %q, want a transaction-scoped try-lock so a crashed holder frees it", got)
	}
}

func migrationSQL(t *testing.T) string {
	t.Helper()
	for _, definition := range migrations.BootstrapDefinitions() {
		if strings.HasSuffix(definition.Path, "/161_status_summary_snapshots.sql") {
			return definition.SQL
		}
	}
	t.Fatal("migration 161_status_summary_snapshots.sql is not in the embedded bootstrap definitions")
	return ""
}

func TestMigrationIsEmbeddedAndMatchesTheRulingDDL(t *testing.T) {
	t.Parallel()

	got := collapse(withoutComments(migrationSQL(t)))
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS status_summary_snapshots",
		"model_key text PRIMARY KEY",
		"schema_version integer NOT NULL",
		"source_sha256 text NOT NULL",
		"as_of timestamptz NOT NULL",
		"computed_at timestamptz NOT NULL",
		"pass_duration_ms double precision NOT NULL",
		"row_count integer NOT NULL",
		"rows jsonb NOT NULL",
		"fillfactor = 50",
		"autovacuum_vacuum_scale_factor = 0",
		"autovacuum_vacuum_threshold = 50",
		"autovacuum_analyze_scale_factor = 0",
		"autovacuum_analyze_threshold = 50",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("migration 161 missing %q", want)
		}
	}
	// No index beyond the primary key, and no backfill: the table starts empty.
	for _, banned := range []string{"CREATE INDEX", "CREATE UNIQUE INDEX", "INSERT INTO", "UPDATE "} {
		if strings.Contains(got, banned) {
			t.Errorf("migration 161 must not contain %q", banned)
		}
	}
}

func TestStatementsNameOnlyColumnsTheMigrationDefines(t *testing.T) {
	t.Parallel()

	ddl := collapse(withoutComments(migrationSQL(t)))
	// Every column the upsert writes and the read selects must be declared by
	// the migration, so the SQL and the DDL cannot drift apart silently.
	for _, name := range []string{
		"model_key", "schema_version", "source_sha256", "as_of",
		"computed_at", "pass_duration_ms", "row_count", "rows",
	} {
		if !strings.Contains(ddl, name+" ") {
			t.Errorf("migration 161 does not declare column %q", name)
		}
		if !strings.Contains(upsertSQL, name) {
			t.Errorf("upsertSQL does not write column %q", name)
		}
		if !strings.Contains(readSQL, name) {
			t.Errorf("readSQL does not select column %q", name)
		}
	}
}
