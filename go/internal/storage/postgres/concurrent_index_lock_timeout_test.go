// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"
)

// TestIsSoleConcurrentIndexStatement pins the #7004 statement classifier: it
// must recognize a bare CREATE/DROP INDEX CONCURRENTLY statement (including
// the leading SQL comments every migrations/*.sql file that uses one
// carries, per 113/114's header convention) and stay conservative about
// everything else, since a false positive would silently disable
// lock_timeout on an ordinary DDL statement.
func TestIsSoleConcurrentIndexStatement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		sql  string
		want bool
	}{
		{
			name: "bare create index concurrently",
			sql:  "CREATE INDEX CONCURRENTLY t_v_idx ON t (v)",
			want: true,
		},
		{
			name: "unique if not exists",
			sql:  "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS t_v_idx ON t (v)",
			want: true,
		},
		{
			name: "drop index concurrently if exists",
			sql:  "DROP INDEX CONCURRENTLY IF EXISTS t_v_idx",
			want: true,
		},
		{
			name: "with leading migration header comments",
			sql: "-- #6887: some rationale\n" +
				"-- more rationale across lines\n" +
				"CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_cloud_retract_admission_uid_idx\n" +
				"    ON fact_records ((payload->>'cloud_resource_uid'))\n" +
				"    WHERE fact_kind = 'reducer_cloud_resource_identity'\n" +
				"      AND is_tombstone = FALSE;\n",
			want: true,
		},
		{
			name: "trailing semicolon and whitespace",
			sql:  "  CREATE INDEX CONCURRENTLY t_v_idx ON t (v);  \n",
			want: true,
		},
		{
			name: "combined with another statement stays conservative",
			sql:  "CREATE INDEX CONCURRENTLY t_v_idx ON t (v); ALTER TABLE t ADD COLUMN w INT;",
			want: false,
		},
		{
			name: "plain create index without concurrently",
			sql:  "CREATE INDEX t_v_idx ON t (v)",
			want: false,
		},
		{
			name: "create table",
			sql:  "CREATE TABLE t (id INTEGER NOT NULL)",
			want: false,
		},
		{
			name: "alter table",
			sql:  "ALTER TABLE t ADD COLUMN IF NOT EXISTS w INT",
			want: false,
		},
		{
			name: "empty",
			sql:  "",
			want: false,
		},
		{
			name: "whitespace and comments only",
			sql:  "-- nothing here\n   \n",
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isSoleConcurrentIndexStatement(tc.sql); got != tc.want {
				t.Fatalf("isSoleConcurrentIndexStatement(%q) = %v, want %v", tc.sql, got, tc.want)
			}
		})
	}
}

// TestConcurrentIndexBuildLockTimeout pins that only a sole CIC/DIC statement
// has its lock_timeout overridden to disabled (0); everything else keeps the
// caller's requested bound unchanged.
func TestConcurrentIndexBuildLockTimeout(t *testing.T) {
	t.Parallel()
	const requested = 5 * time.Second
	if got := concurrentIndexBuildLockTimeout("CREATE INDEX CONCURRENTLY t_v_idx ON t (v)", requested); got != 0 {
		t.Fatalf("concurrent index build lock timeout = %s, want disabled (0)", got)
	}
	if got := concurrentIndexBuildLockTimeout("ALTER TABLE t ADD COLUMN w INT", requested); got != requested {
		t.Fatalf("non-concurrent-index statement lock timeout = %s, want unchanged %s", got, requested)
	}
}

// TestLockTimeoutSetting pins the literal Postgres GUC value sent for a
// disabled lock_timeout: "0", matching resetSchemaLockTimeout's literal,
// never time.Duration's "0s" string form.
func TestLockTimeoutSetting(t *testing.T) {
	t.Parallel()
	if got := lockTimeoutSetting(0); got != "0" {
		t.Fatalf("lockTimeoutSetting(0) = %q, want %q", got, "0")
	}
	if got := lockTimeoutSetting(5 * time.Second); got != "5s" {
		t.Fatalf("lockTimeoutSetting(5s) = %q, want %q", got, "5s")
	}
}
