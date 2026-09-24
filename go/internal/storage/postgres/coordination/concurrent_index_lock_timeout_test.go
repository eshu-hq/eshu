// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordination

import (
	"testing"
	"time"
)

// TestIsSoleConcurrentIndexStatement pins the #7004 statement classifier: it
// must recognize a bare CREATE/DROP INDEX CONCURRENTLY statement (including
// the leading SQL comments every root package migrations/*.sql file that
// uses one carries, per 113/114's header convention) and stay conservative
// about everything else, since a false positive would silently disable
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
			// A `--` that happens to fall inside a string literal is a
			// known, accepted false positive of line-comment stripping: it
			// hides the trailing `;` and the ALTER, so this classifies as
			// a sole statement even though it is not one. It stays inert
			// because Postgres itself refuses to run CONCURRENTLY inside
			// the resulting multi-statement simple-query string ("cannot
			// run inside a transaction block"), so the ALTER never
			// actually executes without lock_timeout (verified live). This
			// pins the classifier's current, accepted answer -- not the
			// ideal one -- so a change to that answer is a deliberate,
			// reviewed decision.
			name: "comment-like text inside a string literal",
			sql:  "CREATE INDEX CONCURRENTLY a ON t (v) WHERE s = '\n--'; ALTER TABLE t ADD COLUMN w INT",
			want: true,
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
			if got := IsSoleConcurrentIndexStatement(tc.sql); got != tc.want {
				t.Fatalf("IsSoleConcurrentIndexStatement(%q) = %v, want %v", tc.sql, got, tc.want)
			}
		})
	}
}

// TestConcurrentIndexBuildPlan pins that only a sole CIC/DIC statement has
// its lock_timeout overridden to disabled (0) and its isConcurrentIndexBuild
// flag set true; everything else keeps the caller's requested bound
// unchanged and a false flag, from the same single classification.
func TestConcurrentIndexBuildPlan(t *testing.T) {
	t.Parallel()
	const requested = 5 * time.Second
	if lockTimeout, isConcurrentIndexBuild := ConcurrentIndexBuildPlan("CREATE INDEX CONCURRENTLY t_v_idx ON t (v)", requested); lockTimeout != 0 || !isConcurrentIndexBuild {
		t.Fatalf("concurrent index build plan = (%s, %v), want (disabled, true)", lockTimeout, isConcurrentIndexBuild)
	}
	if lockTimeout, isConcurrentIndexBuild := ConcurrentIndexBuildPlan("ALTER TABLE t ADD COLUMN w INT", requested); lockTimeout != requested || isConcurrentIndexBuild {
		t.Fatalf("non-concurrent-index statement plan = (%s, %v), want (unchanged %s, false)", lockTimeout, isConcurrentIndexBuild, requested)
	}
}

// TestLockTimeoutSetting pins the literal Postgres GUC value sent for a
// disabled lock_timeout: "0", matching the root package's
// resetSchemaLockTimeout literal, never time.Duration's "0s" string form.
func TestLockTimeoutSetting(t *testing.T) {
	t.Parallel()
	if got := LockTimeoutSetting(0); got != "0" {
		t.Fatalf("LockTimeoutSetting(0) = %q, want %q", got, "0")
	}
	if got := LockTimeoutSetting(5 * time.Second); got != "5s" {
		t.Fatalf("LockTimeoutSetting(5s) = %q, want %q", got, "5s")
	}
}
