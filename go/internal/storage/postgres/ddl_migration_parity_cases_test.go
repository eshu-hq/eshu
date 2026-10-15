// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// ddlParityMigrationBase is a numbered-migration corpus the synthetic cases share. It
// creates a table with a table-level PRIMARY KEY, CONSTRAINT, and UNIQUE
// clause (none of which are columns), adds a column and a named constraint
// in later ALTERs, and creates one named index, one function, and one
// trigger.
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
ALTER TABLE widgets ADD CONSTRAINT widgets_fencing_check CHECK (fencing_token >= 0);
CREATE OR REPLACE FUNCTION touch_widgets() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RETURN NEW; END $$;
CREATE TRIGGER widgets_touch BEFORE UPDATE ON widgets FOR EACH ROW EXECUTE FUNCTION touch_widgets();
CREATE TABLE "Gadgets" (gadget_id TEXT PRIMARY KEY)
`

// TestDDLMigrationParitySyntheticCases is the seeded RED/GREEN proof for the
// guard: each failing case plants one violation of the five rules listed on
// TestDDLMigrationParity and must produce exactly the expected gaps; each
// passing case must produce none.
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
			goSQL:   "CREATE TABLE IF NOT EXISTS widgets (\n widget_id TEXT NOT NULL,\n owner TEXT,\n PRIMARY KEY (widget_id),\n CONSTRAINT widgets_owner_check CHECK (owner <> '')\n)",
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
			name:    "pass: ADD CONSTRAINT is a constraint, not a column",
			goSQL:   "ALTER TABLE widgets ADD CONSTRAINT widgets_fencing_check CHECK (fencing_token >= 0), ADD PRIMARY KEY (widget_id)",
			wantGap: nil,
		},
		{
			name:    "pass: temporary tables are session-local, not schema",
			goSQL:   "CREATE TEMP TABLE scratch_rows (id TEXT)",
			wantGap: nil,
		},
		{
			name: "pass: function, trigger, and constraint present in migrations",
			goSQL: "CREATE OR REPLACE FUNCTION public.touch_widgets() RETURNS trigger AS $$ BEGIN RETURN NEW; END $$ LANGUAGE plpgsql;\n" +
				"CREATE TRIGGER widgets_touch BEFORE UPDATE OF owner, payload ON widgets FOR EACH ROW EXECUTE FUNCTION touch_widgets()",
			wantGap: nil,
		},
		{
			name:    "fail: Go ADD CONSTRAINT with no migration",
			goSQL:   "ALTER TABLE widgets ADD CONSTRAINT widgets_lease_check CHECK (owner <> '')",
			wantGap: []string{"constraint widgets_lease_check on widgets"},
		},
		{
			name:    "fail: Go CREATE TABLE names a constraint no migration has",
			goSQL:   "CREATE TABLE IF NOT EXISTS widgets (widget_id TEXT, CONSTRAINT widgets_extra_check CHECK (true))",
			wantGap: []string{"constraint widgets_extra_check on widgets"},
		},
		{
			name:    "fail: Go CREATE TRIGGER with no migration",
			goSQL:   "CREATE OR REPLACE TRIGGER widgets_audit AFTER INSERT ON widgets FOR EACH ROW EXECUTE FUNCTION touch_widgets()",
			wantGap: []string{"trigger widgets_audit on widgets"},
		},
		{
			name:    "fail: Go trigger name exists in migrations on another table",
			goSQL:   `CREATE TRIGGER widgets_touch BEFORE UPDATE ON "Gadgets" FOR EACH ROW EXECUTE FUNCTION touch_widgets()`,
			wantGap: []string{"trigger widgets_touch on Gadgets"},
		},
		{
			name:    "fail: Go CREATE FUNCTION with no migration",
			goSQL:   "CREATE FUNCTION audit_widgets() RETURNS trigger AS $$ BEGIN RETURN NEW; END $$ LANGUAGE plpgsql",
			wantGap: []string{"function audit_widgets"},
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
		{
			name:    "fail: literal table name with its column list joined on at runtime",
			goSQL:   "CREATE TABLE IF NOT EXISTS go_only_tbl",
			wantGap: []string{"table go_only_tbl", "unresolved"},
		},
		{
			name:    "fail: Go-only PARTITION OF table is a table, not unresolved",
			goSQL:   "CREATE TABLE IF NOT EXISTS widgets_p0 PARTITION OF widgets FOR VALUES WITH (MODULUS 2, REMAINDER 0)",
			wantGap: []string{"table widgets_p0"},
		},
		{
			name:    "fail: column list cut off by a runtime join",
			goSQL:   "CREATE TABLE widgets (widget_id TEXT, ",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: ALTER TABLE IF EXISTS with the table joined on at runtime",
			goSQL:   "ALTER TABLE IF EXISTS ",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: trailing action joined on at runtime",
			goSQL:   "ALTER TABLE widgets ADD COLUMN owner TEXT, ",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: constraint name joined on at runtime",
			goSQL:   "ALTER TABLE widgets ADD CONSTRAINT ",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: bare ADD with the column definition joined on at runtime",
			goSQL:   "ALTER TABLE widgets ADD ",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: second ADD joined on at runtime",
			goSQL:   "ALTER TABLE widgets ADD COLUMN owner TEXT, ADD ",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: ADD COLUMN with the name joined on at runtime",
			goSQL:   "ALTER TABLE widgets ADD COLUMN ",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: DROP COLUMN with the name joined on at runtime",
			goSQL:   "ALTER TABLE widgets DROP COLUMN ",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: ALTER COLUMN with the name joined on at runtime",
			goSQL:   "ALTER TABLE widgets ALTER COLUMN ",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: RENAME TO with the new name joined on at runtime",
			goSQL:   "ALTER TABLE widgets RENAME TO ",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: unrecognized ALTER TABLE action",
			goSQL:   "ALTER TABLE widgets FROBNICATE owner",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: CREATE TABLE column with no type",
			goSQL:   "CREATE TABLE widgets (widget_id, owner TEXT)",
			wantGap: []string{"unresolved"},
		},
		{
			// Only names are compared, so a readable constraint name is
			// enough; its definition may be joined on at runtime.
			name:    "pass: ADD CONSTRAINT with a readable name and a runtime definition",
			goSQL:   "ALTER TABLE widgets ADD CONSTRAINT widgets_fencing_check ",
			wantGap: nil,
		},
		{
			name:    "fail: bare ADD of a column whose name starts with CHECK",
			goSQL:   "ALTER TABLE widgets ADD checksum TEXT",
			wantGap: []string{"column widgets.checksum"},
		},
		{
			name:    "fail: bare ADD of a column whose name starts with UNIQUE",
			goSQL:   "ALTER TABLE widgets ADD unique_token TEXT",
			wantGap: []string{"column widgets.unique_token"},
		},
		{
			name:    "fail: bare ADD of a column whose name starts with EXCLUDE",
			goSQL:   "ALTER TABLE widgets ADD excluded BOOLEAN",
			wantGap: []string{"column widgets.excluded"},
		},
		{
			name:    "fail: CREATE TABLE column named exclude with no migration",
			goSQL:   "CREATE TABLE widgets (widget_id TEXT, exclude BOOLEAN)",
			wantGap: []string{"column widgets.exclude"},
		},
		{
			name:    "fail: bare ADD of a column named exclude with no migration",
			goSQL:   "ALTER TABLE widgets ADD exclude BOOLEAN",
			wantGap: []string{"column widgets.exclude"},
		},
		{
			name:    "pass: CREATE TABLE constraints written without a space before the paren",
			goSQL:   "CREATE TABLE widgets (widget_id TEXT, PRIMARY KEY(widget_id), UNIQUE(owner), CHECK(owner <> ''))",
			wantGap: nil,
		},
		{
			name:    "fail: column name joined on at runtime",
			goSQL:   "ALTER TABLE widgets ADD COLUMN IF NOT EXISTS ",
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
	wantObjects := []ddlObject{
		{"constraint", "widgets", "widgets_owner_check"},
		{"constraint", "widgets", "widgets_fencing_check"},
		{"function", "", "touch_widgets"},
		{"trigger", "widgets", "widgets_touch"},
	}
	for _, obj := range wantObjects {
		if _, ok := inv.objects[obj]; !ok {
			t.Errorf("object %v not extracted", obj)
		}
	}
	if len(inv.objects) != len(wantObjects) {
		t.Errorf("objects = %d %v, want %d", len(inv.objects), inv.objects, len(wantObjects))
	}
}

// TestDDLMigrationParityAllowlists proves the two explicit allowlists exempt
// only the entries they name, that a dynamic-DDL key is scoped to its Go
// declaration, and that an entry matching nothing fails.
func TestDDLMigrationParityAllowlists(t *testing.T) {
	t.Parallel()

	migInv := extractDDLInventory([]ddlSource{{file: "m.sql", line: 1, sql: ddlParityMigrationBase}})
	goInv := extractDDLInventory([]ddlSource{
		{file: "store/ledger.go", scope: "ledgerSQL", line: 5, sql: "CREATE TABLE IF NOT EXISTS go_ledger (id TEXT); CREATE TABLE IF NOT EXISTS %s (id TEXT)"},
		{file: "store/ledger.go", scope: "otherSQL", line: 9, sql: "CREATE TABLE IF NOT EXISTS %s (id TEXT)"},
		{file: "store/ledger.go", scope: "onlySQL", line: 12, sql: "CREATE TABLE other_only (id TEXT)"},
	})
	gaps := ddlParityGaps(goInv, migInv,
		map[string]string{"go_ledger": "reason", "stale_table": "reason"},
		map[string]string{
			"store/ledger.go|ledgerSQL|CREATE TABLE IF NOT EXISTS %s": "reason",
			"store/gone.go|goneSQL|CREATE TABLE IF NOT EXISTS %s":     "reason",
		},
	)
	want := []string{
		`ddlParityDynamicDDL entry "store/gone.go|goneSQL|CREATE TABLE IF NOT EXISTS %s" matched no unresolved Go DDL`,
		`ddlParityGoOnlyTables entry "stale_table" matched no Go-only table`,
		"store/ledger.go:12: table other_only",
		`store/ledger.go:9: unresolved DDL in "CREATE TABLE IF NOT EXISTS %s"`,
	}
	if len(gaps) != len(want) {
		t.Fatalf("gaps = %d %q, want %d", len(gaps), gaps, len(want))
	}
	for i := range want {
		if !strings.Contains(gaps[i], want[i]) {
			t.Errorf("gap[%d] = %q, want it to contain %q", i, gaps[i], want[i])
		}
	}
}

// TestDDLMigrationParityTempTableScope proves a temporary table exempts
// only DDL in the file that creates it: a session-local table in one file
// must not hide an ALTER on a real table of the same name elsewhere.
func TestDDLMigrationParityTempTableScope(t *testing.T) {
	t.Parallel()

	migInv := extractDDLInventory([]ddlSource{{file: "m.sql", line: 1, sql: ddlParityMigrationBase}})
	goInv := extractDDLInventory([]ddlSource{
		{file: "store/a.go", line: 3, sql: "CREATE TEMP TABLE phantom (id TEXT); ALTER TABLE phantom ADD COLUMN scratch TEXT"},
		{file: "store/b.go", line: 7, sql: "ALTER TABLE phantom ADD COLUMN extra TEXT"},
	})
	gaps := ddlParityGaps(goInv, migInv, nil, nil)
	if len(gaps) != 1 || !strings.Contains(gaps[0], "store/b.go:7: column phantom.extra") {
		t.Fatalf("gaps = %q, want only store/b.go's phantom.extra", gaps)
	}
}
