// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// ddlParityMigrationBase is a numbered-migration corpus the synthetic cases share. It
// creates a table with a table-level PRIMARY KEY, CONSTRAINT, and UNIQUE
// clause (none of which are columns), adds a column in a later ALTER, and
// creates one named index.
const ddlParityMigrationBase = `
-- a leading comment: CREATE TABLE commented_out (ghost TEXT);
CREATE TABLE IF NOT EXISTS public.widgets (
    widget_id TEXT NOT NULL,
    owner TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (widget_id),
    CONSTRAINT widgets_owner_check CHECK (owner <> 'x,y'),
    UNIQUE (owner, created_at)
);
ALTER TABLE widgets ADD COLUMN IF NOT EXISTS fencing_token BIGINT NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS widgets_owner_idx ON widgets (owner);
CREATE TABLE "Gadgets" (gadget_id TEXT PRIMARY KEY)
`

// TestDDLMigrationParitySyntheticCases is the seeded RED/GREEN proof for the
// guard: each failing case plants one violation of the three rules and must
// produce exactly the expected gap; each passing case must produce none.
func TestDDLMigrationParitySyntheticCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		goSQL   string
		wantGap []string // substrings, one per expected gap; empty means pass
	}{
		{
			name:    "pass: column defined in a migration CREATE TABLE",
			goSQL:   "ALTER TABLE widgets ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT ''",
			wantGap: nil,
		},
		{
			name:    "pass: column added by a later migration ALTER",
			goSQL:   "ALTER TABLE widgets ADD COLUMN IF NOT EXISTS fencing_token BIGINT NOT NULL DEFAULT 0;",
			wantGap: nil,
		},
		{
			name:    "pass: schema-qualified and quoted names",
			goSQL:   `ALTER TABLE public."widgets" ADD COLUMN "owner" TEXT; CREATE INDEX IF NOT EXISTS "widgets_owner_idx" ON public.widgets (owner); ALTER TABLE "Gadgets" ADD COLUMN gadget_id TEXT`,
			wantGap: nil,
		},
		{
			name:    "pass: matching Go CREATE TABLE mirror with table-level clauses",
			goSQL:   "CREATE TABLE IF NOT EXISTS widgets (\n widget_id TEXT NOT NULL,\n owner TEXT,\n PRIMARY KEY (widget_id),\n CONSTRAINT c UNIQUE (owner)\n)",
			wantGap: nil,
		},
		{
			name:    "pass: unique concurrent index present in migrations",
			goSQL:   "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS widgets_owner_idx ON ONLY widgets USING btree (owner)",
			wantGap: nil,
		},
		{
			name:    "pass: DDL only inside a SQL comment is ignored",
			goSQL:   "SELECT 1; -- ALTER TABLE widgets ADD COLUMN ghost TEXT\n/* CREATE INDEX ghost_idx ON widgets (owner) */",
			wantGap: nil,
		},
		{
			name:    "pass: ADD CONSTRAINT is not a column",
			goSQL:   "ALTER TABLE widgets ADD CONSTRAINT widgets_fk FOREIGN KEY (owner) REFERENCES users (id), ADD PRIMARY KEY (widget_id)",
			wantGap: nil,
		},
		{
			name:    "pass: temporary tables are session-local, not schema",
			goSQL:   "CREATE TEMP TABLE scratch_rows (id TEXT)",
			wantGap: nil,
		},
		{
			name:    "fail: Go ADD COLUMN with no migration",
			goSQL:   "ALTER TABLE widgets ADD COLUMN IF NOT EXISTS claim_epoch BIGINT NOT NULL DEFAULT 0",
			wantGap: []string{"column widgets.claim_epoch"},
		},
		{
			name:    "fail: second clause of a multi-clause ALTER, COLUMN keyword omitted",
			goSQL:   "ALTER TABLE widgets\n  ADD COLUMN IF NOT EXISTS owner TEXT,\n  ADD IF NOT EXISTS lease_owner TEXT NULL",
			wantGap: []string{"column widgets.lease_owner"},
		},
		{
			name:    "fail: Go CREATE TABLE mirror carries a column no migration has",
			goSQL:   "CREATE TABLE IF NOT EXISTS widgets (widget_id TEXT, drifted_col TEXT, PRIMARY KEY (widget_id))",
			wantGap: []string{"column widgets.drifted_col"},
		},
		{
			name:    "fail: Go CREATE INDEX with no migration",
			goSQL:   "CREATE INDEX IF NOT EXISTS widgets_claimed_at_idx ON widgets (created_at) WHERE owner = ''",
			wantGap: []string{"index widgets_claimed_at_idx"},
		},
		{
			name:    "fail: Go index name exists in migrations on another table",
			goSQL:   `CREATE INDEX IF NOT EXISTS widgets_owner_idx ON "Gadgets" (gadget_id)`,
			wantGap: []string{"index widgets_owner_idx"},
		},
		{
			name:    "fail: Go-only CREATE TABLE",
			goSQL:   "CREATE UNLOGGED TABLE IF NOT EXISTS go_only_rows (id TEXT PRIMARY KEY); CREATE INDEX go_only_rows_idx ON go_only_rows (id)",
			wantGap: []string{"table go_only_rows"},
		},
		{
			name:    "fail: ALTER on a table no migration creates",
			goSQL:   "ALTER TABLE phantom_rows ADD COLUMN extra TEXT",
			wantGap: []string{"column phantom_rows.extra"},
		},
		{
			name:    "fail: DDL target name the scan cannot resolve",
			goSQL:   "CREATE TABLE IF NOT EXISTS %s (id TEXT)",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: ALTER TABLE whose action list is joined on at runtime",
			goSQL:   "ALTER TABLE widgets ",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: unnamed index",
			goSQL:   "CREATE INDEX ON widgets (owner)",
			wantGap: []string{"unresolved"},
		},
	}

	migInv := extractDDLInventory([]ddlSource{{file: "migrations/001_widgets.sql", line: 1, sql: ddlParityMigrationBase}})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			goInv := extractDDLInventory([]ddlSource{{file: "store/schema.go", line: 10, sql: tc.goSQL}})
			gaps := ddlParityGaps(goInv, migInv, nil, nil)
			if len(gaps) != len(tc.wantGap) {
				t.Fatalf("gaps = %d %q, want %d matching %q", len(gaps), gaps, len(tc.wantGap), tc.wantGap)
			}
			for i, want := range tc.wantGap {
				if !strings.Contains(gaps[i], want) {
					t.Fatalf("gap[%d] = %q, want it to mention %q", i, gaps[i], want)
				}
				if !strings.Contains(gaps[i], "store/schema.go:") {
					t.Fatalf("gap[%d] = %q, want the Go source site", i, gaps[i])
				}
			}
		})
	}
}

// TestDDLMigrationParityMigrationExtraction pins what the extractor reads from
// a migration corpus, so a regex regression that drops columns or indexes
// fails here and not as a silent pass of the real-tree guard.
func TestDDLMigrationParityMigrationExtraction(t *testing.T) {
	t.Parallel()

	inv := extractDDLInventory([]ddlSource{{file: "m.sql", line: 1, sql: ddlParityMigrationBase}})
	for _, table := range []string{"widgets", "Gadgets"} {
		if _, ok := inv.tables[table]; !ok {
			t.Errorf("table %q not extracted; tables = %v", table, inv.tables)
		}
	}
	if _, ok := inv.tables["commented_out"]; ok {
		t.Error("table from a SQL comment was extracted")
	}
	wantCols := []ddlColumn{
		{"widgets", "widget_id"},
		{"widgets", "owner"},
		{"widgets", "payload"},
		{"widgets", "created_at"},
		{"widgets", "fencing_token"},
		{"Gadgets", "gadget_id"},
	}
	for _, col := range wantCols {
		if _, ok := inv.columns[col]; !ok {
			t.Errorf("column %v not extracted", col)
		}
	}
	if len(inv.columns) != len(wantCols) {
		t.Errorf("columns = %d %v, want %d (table-level clauses must not count)", len(inv.columns), inv.columns, len(wantCols))
	}
	if idx, ok := inv.indexes["widgets_owner_idx"]; !ok || idx.table != "widgets" {
		t.Errorf("index widgets_owner_idx = %+v, %v; want on widgets", idx, ok)
	}
}

// TestDDLMigrationParityAllowlists proves the two explicit allowlists exempt
// only the entries they name.
func TestDDLMigrationParityAllowlists(t *testing.T) {
	t.Parallel()

	migInv := extractDDLInventory([]ddlSource{{file: "m.sql", line: 1, sql: ddlParityMigrationBase}})
	goInv := extractDDLInventory([]ddlSource{{file: "store/ledger.go", line: 5, sql: "CREATE TABLE IF NOT EXISTS go_ledger (id TEXT); CREATE TABLE IF NOT EXISTS %s (id TEXT); CREATE TABLE other_only (id TEXT)"}})
	gaps := ddlParityGaps(goInv, migInv,
		map[string]string{"go_ledger": "reason"},
		map[string]string{"store/ledger.go|CREATE TABLE IF NOT EXISTS %s": "reason"},
	)
	if len(gaps) != 1 || !strings.Contains(gaps[0], "table other_only") {
		t.Fatalf("gaps = %q, want only the unlisted table other_only", gaps)
	}
}
