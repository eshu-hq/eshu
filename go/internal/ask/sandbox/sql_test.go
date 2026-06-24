// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sandbox

import (
	"strings"
	"testing"
)

func TestValidateSQL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		query       string
		wantAllowed bool
		// reasonContains is checked only when wantAllowed == false; it must appear
		// in the Reason string (case-insensitive). Also verifies that the full
		// query body is NOT echoed verbatim in the reason (bounded reason contract).
		reasonContains string
	}{
		// ── ALLOW cases ──────────────────────────────────────────────────────────
		{
			name:        "simple SELECT with LIMIT",
			query:       "SELECT id FROM repositories LIMIT 10",
			wantAllowed: true,
		},
		{
			name:        "CTE resolving to SELECT",
			query:       "WITH x AS (SELECT 1) SELECT * FROM x",
			wantAllowed: true,
		},
		{
			// Key string-masking test: DELETE appears inside a string literal.
			// normalize masks string contents, so DELETE is never seen as a token.
			name:        "keyword inside string literal is masked away — allowed",
			query:       "SELECT 'DELETE me' AS note",
			wantAllowed: true,
		},
		{
			// Key false-positive guard: created_at and update_time are whole tokens;
			// they must NOT match the denylist keywords CREATE or UPDATE.
			name:        "column names created_at and update_time must not false-positive",
			query:       "SELECT created_at, update_time FROM t",
			wantAllowed: true,
		},
		{
			name:        "SELECT with WHERE and JOIN",
			query:       "SELECT a.id, b.name FROM a JOIN b ON a.id = b.a_id WHERE a.active = true",
			wantAllowed: true,
		},
		{
			name:        "complex CTE chain",
			query:       "WITH a AS (SELECT 1 AS x), b AS (SELECT x+1 AS y FROM a) SELECT y FROM b",
			wantAllowed: true,
		},

		// ── DENY: write / DDL statements ─────────────────────────────────────────
		// These queries do not start with SELECT or WITH, so the leading-token
		// check (step 3) fires before the denylist scan (step 4). The reason
		// contains "SELECT or WITH", which is the bounded message for that check.
		// The queries are still correctly denied; the exact denylist keyword is
		// reported only for SELECT/WITH-led queries that contain a forbidden token.
		{
			name:           "UPDATE statement",
			query:          "UPDATE t SET x=1",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "DELETE statement",
			query:          "DELETE FROM t",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "INSERT statement",
			query:          "INSERT INTO t VALUES (1)",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "TRUNCATE statement",
			query:          "TRUNCATE t",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "DROP TABLE",
			query:          "DROP TABLE t",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "CREATE TABLE",
			query:          "CREATE TABLE t (id INT)",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "ALTER TABLE",
			query:          "ALTER TABLE t ADD COLUMN x INT",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "GRANT privilege",
			query:          "GRANT SELECT ON t TO role_x",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "REVOKE privilege",
			query:          "REVOKE SELECT ON t FROM role_x",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "COPY to program",
			query:          "COPY t TO PROGRAM 'sh -c rm'",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "CALL procedure",
			query:          "CALL my_proc()",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "DO block — dollar-quote masks body; DO is leading token",
			query:          "DO $$ BEGIN RAISE NOTICE 'x'; END $$",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "SET search_path",
			query:          "SET search_path = x",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "VACUUM",
			query:          "VACUUM t",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "REINDEX",
			query:          "REINDEX TABLE t",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "LOCK TABLE",
			query:          "LOCK TABLE t",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "BEGIN transaction",
			query:          "BEGIN",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "COMMIT transaction",
			query:          "COMMIT",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "ROLLBACK transaction",
			query:          "ROLLBACK",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			name:           "MERGE statement",
			query:          "MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET x = s.x",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},

		// ── DENY: dangerous function calls ────────────────────────────────────────
		{
			name:           "pg_sleep DoS vector",
			query:          "SELECT pg_sleep(10)",
			wantAllowed:    false,
			reasonContains: "PG_SLEEP",
		},
		{
			name:           "pg_terminate_backend",
			query:          "SELECT pg_terminate_backend(12345)",
			wantAllowed:    false,
			reasonContains: "PG_TERMINATE_BACKEND",
		},
		{
			name:           "dblink",
			query:          "SELECT * FROM dblink('host=evil', 'SELECT 1') AS t(x int)",
			wantAllowed:    false,
			reasonContains: "DBLINK",
		},
		{
			name:           "pg_read_file",
			query:          "SELECT pg_read_file('/etc/passwd')",
			wantAllowed:    false,
			reasonContains: "PG_READ_FILE",
		},
		{
			name:           "lo_import",
			query:          "SELECT lo_import('/etc/passwd')",
			wantAllowed:    false,
			reasonContains: "LO_IMPORT",
		},
		{
			name:           "lo_export",
			query:          "SELECT lo_export(1234, '/tmp/x')",
			wantAllowed:    false,
			reasonContains: "LO_EXPORT",
		},

		// ── DENY: SELECT INTO, sequence mutation, large-object, advisory locks ────
		{
			// SELECT * INTO newtable FROM t creates a table — write side-effect
			// hidden inside SELECT syntax (critical bypass).
			name:           "SELECT INTO creates a table",
			query:          "SELECT * INTO newtable FROM t",
			wantAllowed:    false,
			reasonContains: "INTO",
		},
		{
			// nextval('s') advances a sequence — write side-effect callable from SELECT.
			name:           "nextval advances a sequence",
			query:          "SELECT nextval('my_seq')",
			wantAllowed:    false,
			reasonContains: "NEXTVAL",
		},
		{
			// setval('s', n) mutates a sequence — write side-effect callable from SELECT.
			name:           "setval mutates a sequence",
			query:          "SELECT setval('my_seq', 100)",
			wantAllowed:    false,
			reasonContains: "SETVAL",
		},
		{
			name:           "lo_unlink deletes a large object",
			query:          "SELECT lo_unlink(12345)",
			wantAllowed:    false,
			reasonContains: "LO_UNLINK",
		},
		{
			name:           "lo_create creates a large object",
			query:          "SELECT lo_create(0)",
			wantAllowed:    false,
			reasonContains: "LO_CREATE",
		},
		{
			name:           "pg_advisory_lock — session lock DoS vector",
			query:          "SELECT pg_advisory_lock(1)",
			wantAllowed:    false,
			reasonContains: "PG_ADVISORY_LOCK",
		},
		{
			name:           "pg_try_advisory_xact_lock — transaction lock DoS vector",
			query:          "SELECT pg_try_advisory_xact_lock(1)",
			wantAllowed:    false,
			reasonContains: "PG_TRY_ADVISORY_XACT_LOCK",
		},

		// ── ALLOW: false-positive guards for the new INTO and sequence entries ────
		{
			// currval is read-only (reads current sequence value without advancing it)
			// and must NOT be denied.
			name:        "currval is read-only and must be allowed",
			query:       "SELECT currval('my_seq')",
			wantAllowed: true,
		},
		{
			// into_account and info_table are column/table identifiers whose tokens
			// (INTO_ACCOUNT, INFO_TABLE) do not equal the bare INTO token. Whole-word
			// matching must not produce false positives for these identifiers.
			name:        "identifiers containing 'into' as a substring must not be denied",
			query:       "SELECT into_account, info_table FROM t",
			wantAllowed: true,
		},

		// ── DENY: structural / stacking ───────────────────────────────────────────
		{
			name:           "stacked statements",
			query:          "SELECT 1; DROP TABLE t",
			wantAllowed:    false,
			reasonContains: "multiple statements",
		},
		{
			// CTE-wrapped write: WITH is the leading token (passes step 2) but DELETE
			// appears in the masked body as a token → step 3 denies.
			name:           "CTE-wrapped DELETE",
			query:          "WITH x AS (DELETE FROM t RETURNING *) SELECT * FROM x",
			wantAllowed:    false,
			reasonContains: "DELETE",
		},
		{
			// EXPLAIN ANALYZE: does not start with SELECT/WITH → denied at step 2.
			// ANALYZE is also in the denylist, but the leading-token check fires first.
			name:           "EXPLAIN ANALYZE",
			query:          "EXPLAIN ANALYZE SELECT 1",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},
		{
			// SET as leading token: not SELECT/WITH → denied at the leading-token
			// check (step 3). SET also appears in the denylist, but step 3 fires
			// first and produces the "read-only queries must start with SELECT or WITH"
			// reason. The query is still correctly denied.
			name:           "SET as leading keyword",
			query:          "SET time zone 'UTC'",
			wantAllowed:    false,
			reasonContains: "SELECT or WITH",
		},

		// ── DENY: normalize errors ────────────────────────────────────────────────
		{
			name:           "unterminated string literal",
			query:          "SELECT 'oops",
			wantAllowed:    false,
			reasonContains: "unterminated",
		},
		{
			name:           "control character in query",
			query:          "SELECT\x00 1",
			wantAllowed:    false,
			reasonContains: "control character",
		},

		// ── DENY: empty query ─────────────────────────────────────────────────────
		{
			name:           "empty query",
			query:          "",
			wantAllowed:    false,
			reasonContains: "empty query",
		},
		{
			name:           "whitespace only",
			query:          "   \t\n  ",
			wantAllowed:    false,
			reasonContains: "empty query",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := validateSQL(tc.query)
			if got.Allowed != tc.wantAllowed {
				t.Errorf("validateSQL(%q): Allowed = %v, want %v (Reason: %q)",
					tc.query, got.Allowed, tc.wantAllowed, got.Reason)
				return
			}
			if !tc.wantAllowed {
				// Reason must be non-empty.
				if got.Reason == "" {
					t.Errorf("validateSQL(%q): denied but Reason is empty", tc.query)
				}
				// Reason must contain the expected substring (case-insensitive).
				if tc.reasonContains != "" &&
					!strings.Contains(strings.ToUpper(got.Reason), strings.ToUpper(tc.reasonContains)) {
					t.Errorf("validateSQL(%q): Reason = %q, want it to contain %q",
						tc.query, got.Reason, tc.reasonContains)
				}
				// Bounded reason contract: reason must not echo the full query body.
				// A reason longer than 120 chars is a smell; reasons must be fixed-prefix
				// plus at most one keyword.
				if len(got.Reason) > 120 {
					t.Errorf("validateSQL(%q): Reason is too long (%d bytes), possible query echo: %q",
						tc.query, len(got.Reason), got.Reason)
				}
				// Explicit check: the query body must not appear verbatim in the reason.
				if len(tc.query) > 4 && strings.Contains(got.Reason, tc.query) {
					t.Errorf("validateSQL(%q): Reason echoes the query body: %q", tc.query, got.Reason)
				}
			} else {
				// When allowed, Reason must be empty.
				if got.Reason != "" {
					t.Errorf("validateSQL(%q): Allowed=true but Reason is non-empty: %q",
						tc.query, got.Reason)
				}
			}
		})
	}
}
