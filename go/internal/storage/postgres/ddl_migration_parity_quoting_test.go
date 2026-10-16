// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestDDLMigrationParityStripCommentsQuoting pins how stripSQLComments treats
// quoted text (#7792). Text inside a single-quoted literal, a dollar-quoted
// body ($$...$$ or $tag$...$tag$), or a double-quoted identifier is not a
// comment: a "--", "/*", or apostrophe there stays body text and does not
// change the quoting state for the SQL after it.
func TestDDLMigrationParityStripCommentsQuoting(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, sql, want string
	}{
		{
			name: "apostrophe in a dollar body does not hide the comment after it",
			sql:  "SELECT $$it's$$; -- gone\nX",
			want: "SELECT $$it's$$;        \nX",
		},
		{
			name: "line comment marker inside a dollar body is body text",
			sql:  "AS $$ BEGIN -- keep\n RETURN 1; END $$;",
			want: "AS $$ BEGIN -- keep\n RETURN 1; END $$;",
		},
		{
			name: "block comment marker inside a dollar body is body text",
			sql:  "AS $$ /* keep */ $$;",
			want: "AS $$ /* keep */ $$;",
		},
		{
			name: "tagged body holds an inner $$, an apostrophe, and a comment marker",
			sql:  "AS $body$ a $$ b' -- keep $$ $body$; -- gone",
			want: "AS $body$ a $$ b' -- keep $$ $body$;        ",
		},
		{
			name: "a different tag does not close the body",
			sql:  "AS $a$ x $b$ -- keep\n $a$ -- gone",
			want: "AS $a$ x $b$ -- keep\n $a$        ",
		},
		{
			name: "real comments after the closing tag are stripped",
			sql:  "$x$ don't $x$ -- gone\n/* gone */ y",
			want: "$x$ don't $x$        \n           y",
		},
		{
			name: "positional parameters are not dollar quotes",
			sql:  "WHERE a = $1 AND b = $2 -- gone",
			want: "WHERE a = $1 AND b = $2        ",
		},
		{
			name: "dollar signs inside an identifier are not dollar quotes",
			sql:  "SELECT a$$b, \"c$$\" -- gone",
			want: "SELECT a$$b, \"c$$\"        ",
		},
		{
			name: "an apostrophe inside a double-quoted identifier is not a literal",
			sql:  "SELECT \"it's\" -- gone",
			want: "SELECT \"it's\"        ",
		},
		{
			name: "comment marker inside a single-quoted literal is text",
			sql:  "SELECT 'a -- b', 'it''s' -- gone",
			want: "SELECT 'a -- b', 'it''s'        ",
		},
		{
			name: "an unterminated dollar body runs to the end",
			sql:  "AS $fn$ -- keep",
			want: "AS $fn$ -- keep",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := stripSQLComments(tc.sql); got != tc.want {
				t.Fatalf("stripSQLComments(%q)\n got %q\nwant %q", tc.sql, got, tc.want)
			}
		})
	}
}

// TestDDLMigrationParityDollarBodyKeepsCommentsComments proves the extractor
// end to end: an apostrophe inside a migration function body must not turn a
// later commented-out CREATE TABLE into a table the migrations declare.
func TestDDLMigrationParityDollarBodyKeepsCommentsComments(t *testing.T) {
	t.Parallel()

	sql := "CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $body$ BEGIN RAISE NOTICE $q$it's$q$; -- note\nEND $body$;\n" +
		"-- CREATE TABLE ghost_rows (id TEXT);\n" +
		"CREATE TABLE real_rows (id TEXT);"
	inv := extractDDLInventory([]ddlSource{{file: "m.sql", line: 1, sql: sql}})
	if _, ok := inv.tables["ghost_rows"]; ok {
		t.Error("table from a SQL comment after a dollar-quoted body was extracted")
	}
	if _, ok := inv.tables["real_rows"]; !ok {
		t.Errorf("table real_rows not extracted; tables = %v", inv.tables)
	}
}

// ddlParityInlineMigration is a migration corpus whose columns carry inline
// named constraints of every kind the column grammar allows.
const ddlParityInlineMigration = `
CREATE TABLE gizmos (
    gizmo_id TEXT CONSTRAINT gizmos_pk PRIMARY KEY,
    owner TEXT CONSTRAINT gizmos_owner_nn NOT NULL CONSTRAINT gizmos_owner_chk CHECK (owner <> 'no constraint bogus'),
    parent TEXT CONSTRAINT gizmos_parent_fk REFERENCES gizmos (gizmo_id),
    token TEXT CONSTRAINT gizmos_token_key UNIQUE,
    weight INT CONSTRAINT gizmos_weight_default DEFAULT 0
);
ALTER TABLE gizmos ADD COLUMN IF NOT EXISTS lease TEXT CONSTRAINT gizmos_lease_chk CHECK (lease <> '');
`

// TestDDLMigrationParityInlineConstraints proves Rule 4 compares inline
// named column constraints (CREATE TABLE elements and ALTER TABLE ... ADD
// COLUMN), not only table-level CONSTRAINT clauses.
func TestDDLMigrationParityInlineConstraints(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		goSQL   string
		wantGap []string // sorted substrings, one per expected gap
	}{
		{
			name:  "pass: matching inline constraints of every kind",
			goSQL: "CREATE TABLE IF NOT EXISTS gizmos (gizmo_id TEXT CONSTRAINT gizmos_pk PRIMARY KEY, owner TEXT CONSTRAINT gizmos_owner_nn NOT NULL CONSTRAINT gizmos_owner_chk CHECK (owner <> ''), parent TEXT CONSTRAINT gizmos_parent_fk REFERENCES gizmos (gizmo_id), token TEXT CONSTRAINT gizmos_token_key UNIQUE, weight INT CONSTRAINT gizmos_weight_default DEFAULT 0)",
		},
		{
			name:  "pass: lowercase keyword and quoted constraint name",
			goSQL: `CREATE TABLE gizmos (owner TEXT constraint "gizmos_owner_chk" check (owner <> ''))`,
		},
		{
			name:  "pass: the word constraint inside a literal or CHECK body is not a name",
			goSQL: "CREATE TABLE gizmos (owner TEXT DEFAULT 'a constraint bogus' CHECK (owner <> 'no constraint bogus'))",
		},
		{
			name:  "pass: matching inline constraint on ALTER TABLE ADD COLUMN",
			goSQL: "ALTER TABLE gizmos ADD COLUMN IF NOT EXISTS lease TEXT CONSTRAINT gizmos_lease_chk CHECK (lease <> '')",
		},
		{
			name:    "fail: Go-only inline CHECK",
			goSQL:   "CREATE TABLE gizmos (owner TEXT CONSTRAINT gizmos_go_chk CHECK (owner <> ''))",
			wantGap: []string{"constraint gizmos_go_chk on gizmos"},
		},
		{
			name:    "fail: Go-only inline REFERENCES",
			goSQL:   "CREATE TABLE gizmos (parent TEXT CONSTRAINT gizmos_go_fk REFERENCES gizmos (gizmo_id))",
			wantGap: []string{"constraint gizmos_go_fk on gizmos"},
		},
		{
			name:    "fail: Go-only inline UNIQUE",
			goSQL:   "CREATE TABLE gizmos (token TEXT CONSTRAINT gizmos_go_key UNIQUE)",
			wantGap: []string{"constraint gizmos_go_key on gizmos"},
		},
		{
			name:    "fail: Go-only inline PRIMARY KEY",
			goSQL:   "CREATE TABLE gizmos (gizmo_id TEXT CONSTRAINT gizmos_go_pkey PRIMARY KEY)",
			wantGap: []string{"constraint gizmos_go_pkey on gizmos"},
		},
		{
			name:    "fail: Go-only inline NOT NULL and DEFAULT, second of two on a column",
			goSQL:   "CREATE TABLE gizmos (owner TEXT CONSTRAINT gizmos_owner_chk CHECK (owner <> '') CONSTRAINT gizmos_go_nn NOT NULL, weight INT CONSTRAINT gizmos_go_default DEFAULT 0)",
			wantGap: []string{"constraint gizmos_go_default on gizmos", "constraint gizmos_go_nn on gizmos"},
		},
		{
			name:    "fail: Go-only inline constraint on ALTER TABLE ADD COLUMN",
			goSQL:   "ALTER TABLE gizmos ADD COLUMN lease TEXT CONSTRAINT gizmos_go_lease_chk CHECK (lease <> '')",
			wantGap: []string{"constraint gizmos_go_lease_chk on gizmos"},
		},
		{
			name:    "fail: inline constraint name joined on at runtime",
			goSQL:   "CREATE TABLE gizmos (owner TEXT CONSTRAINT %s CHECK (owner <> ''))",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: inline CONSTRAINT keyword with the rest joined on at runtime",
			goSQL:   "ALTER TABLE gizmos ADD COLUMN lease TEXT CONSTRAINT ",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: quoted column named constraint is a column, not a constraint",
			goSQL:   `CREATE TABLE gizmos ("constraint" TEXT)`,
			wantGap: []string{"column gizmos.constraint"},
		},
	}

	migInv := extractDDLInventory([]ddlSource{{file: "migrations/002_gizmos.sql", line: 1, sql: ddlParityInlineMigration}})
	for _, obj := range []string{"gizmos_pk", "gizmos_owner_nn", "gizmos_owner_chk", "gizmos_parent_fk", "gizmos_token_key", "gizmos_weight_default", "gizmos_lease_chk"} {
		if _, ok := migInv.objects[ddlObject{kind: "constraint", table: "gizmos", name: obj}]; !ok {
			t.Errorf("migration inline constraint %s not extracted; objects = %v", obj, migInv.objects)
		}
	}
	if _, ok := migInv.objects[ddlObject{kind: "constraint", table: "gizmos", name: "bogus"}]; ok {
		t.Error("the word constraint inside a CHECK literal was extracted as a constraint name")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			goInv := extractDDLInventory([]ddlSource{{file: "store/schema.go", line: 10, sql: tc.goSQL}})
			gaps := ddlParityGaps(goInv, migInv, nil, nil)
			if len(gaps) != len(tc.wantGap) {
				t.Fatalf("gaps = %d %q, want %d matching %q", len(gaps), gaps, len(tc.wantGap), tc.wantGap)
			}
			for i, want := range tc.wantGap {
				if !strings.Contains(gaps[i], want) || !strings.Contains(gaps[i], "store/schema.go:") {
					t.Fatalf("gap[%d] = %q, want it to mention %q at the Go source site", i, gaps[i], want)
				}
			}
		})
	}
}
