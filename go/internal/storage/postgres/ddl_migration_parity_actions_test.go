// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "testing"

// TestDDLMigrationParityRecognizedAlterForms proves every form in
// ddlAlterForms accepts a complete action, so the default-deny classifier
// does not fail real DDL, and that each truncated form is rejected.
func TestDDLMigrationParityRecognizedAlterForms(t *testing.T) {
	t.Parallel()

	accepted := []struct {
		action, form, column, constraint string
	}{
		{"ADD CONSTRAINT widgets_fk FOREIGN KEY (owner) REFERENCES users (id)", "add constraint", "", "widgets_fk"},
		{"ADD PRIMARY KEY (widget_id)", "add unnamed constraint", "", ""},
		{"ADD UNIQUE (owner)", "add unnamed constraint", "", ""},
		{"ADD CHECK (owner <> '')", "add unnamed constraint", "", ""},
		{"ADD FOREIGN KEY (owner) REFERENCES users (id)", "add unnamed constraint", "", ""},
		{"ADD EXCLUDE USING gist (owner WITH =)", "add unnamed constraint", "", ""},
		{"ADD CHECK(owner <> '')", "add unnamed constraint", "", ""},
		{"ADD UNIQUE NULLS NOT DISTINCT (owner)", "add unnamed constraint", "", ""},
		{"ADD UNIQUE USING INDEX widgets_owner_idx", "add unnamed constraint", "", ""},
		{"ADD PRIMARY KEY USING INDEX widgets_pkey_idx", "add unnamed constraint", "", ""},
		{"ADD NOT NULL owner", "add unnamed constraint", "", ""},
		{"ADD exclude BOOLEAN", "add column", "exclude", ""},
		// EXCLUDE is not reserved, so a bare ADD EXCLUDE with no USING or
		// paren after it is a column named exclude, like ADD owner.
		{"ADD EXCLUDE", "add column", "exclude", ""},
		{"ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT ''", "add column", "owner", ""},
		{"ADD owner TEXT", "add column", "owner", ""},
		{"ALTER CONSTRAINT widgets_fk DEFERRABLE", "alter constraint", "", ""},
		{"ALTER COLUMN owner SET NOT NULL", "alter column", "", ""},
		{"ALTER owner TYPE TEXT", "alter column", "", ""},
		{"ALTER COLUMN owner SET STATISTICS 500", "alter column", "", ""},
		{"DROP CONSTRAINT IF EXISTS widgets_fk", "drop constraint", "", ""},
		{"DROP COLUMN IF EXISTS owner CASCADE", "drop column", "", ""},
		{"DROP owner", "drop column", "", ""},
		{"RENAME TO widgets_old", "rename to", "", ""},
		{"RENAME COLUMN owner TO owner_id", "rename column or constraint", "", ""},
		{"RENAME CONSTRAINT widgets_fk TO widgets_owner_fk", "rename column or constraint", "", ""},
		{"VALIDATE CONSTRAINT widgets_fk", "validate constraint", "", ""},
		{"SET (fillfactor = 70, autovacuum_vacuum_scale_factor = 0.05)", "set or reset storage parameters", "", ""},
		{"RESET (fillfactor)", "set or reset storage parameters", "", ""},
		{"SET UNLOGGED", "set table option", "", ""},
		{"SET WITHOUT CLUSTER", "set table option", "", ""},
		{"SET SCHEMA archive", "set schema, tablespace, or access method", "", ""},
		{"SET ACCESS METHOD heap", "set schema, tablespace, or access method", "", ""},
		{"DISABLE TRIGGER widgets_touch", "enable or disable trigger or rule", "", ""},
		{"ENABLE ALWAYS TRIGGER widgets_touch", "enable or disable trigger or rule", "", ""},
		{"ENABLE ROW LEVEL SECURITY", "row level security", "", ""},
		{"NO FORCE ROW LEVEL SECURITY", "row level security", "", ""},
		{"OWNER TO eshu", "owner to", "", ""},
		{"ATTACH PARTITION widgets_p0 FOR VALUES WITH (MODULUS 2, REMAINDER 0)", "attach or detach partition", "", ""},
		{"DETACH PARTITION widgets_p0", "attach or detach partition", "", ""},
		{"CLUSTER ON widgets_owner_idx", "cluster on", "", ""},
		{"REPLICA IDENTITY FULL", "replica identity", "", ""},
		{"REPLICA IDENTITY USING INDEX widgets_owner_idx", "replica identity", "", ""},
		{"INHERIT base_widgets", "inherit", "", ""},
		{"NO INHERIT base_widgets", "inherit", "", ""},
		{"NOT OF", "of type", "", ""},
	}
	for _, tc := range accepted {
		got, ok := classifyAlterAction(tc.action)
		if !ok || got.form != tc.form || got.column != tc.column || got.constraint != tc.constraint {
			t.Errorf("classifyAlterAction(%q) = %+v, %v; want form %q column %q constraint %q",
				tc.action, got, ok, tc.form, tc.column, tc.constraint)
		}
	}

	rejected := []string{
		"", "ADD", "ADD CONSTRAINT", "ADD COLUMN", "ADD COLUMN IF NOT EXISTS", "ADD UNIQUE",
		"ALTER", "ALTER COLUMN", "ALTER COLUMN owner", "ALTER CONSTRAINT widgets_fk",
		"DROP", "DROP COLUMN", "DROP CONSTRAINT", "RENAME TO", "RENAME COLUMN owner TO",
		"VALIDATE CONSTRAINT", "SET (fillfactor = 70", "SET", "OWNER TO", "ADD COLUMN %s TEXT",
		"FROBNICATE owner", "ADD PRIMARY KEY", "ADD FOREIGN KEY", "ADD NOT NULL", "ADD NOT NULL %s",
	}
	for _, action := range rejected {
		if got, ok := classifyAlterAction(action); ok {
			t.Errorf("classifyAlterAction(%q) = %+v, accepted; want rejected", action, got)
		}
	}
}

// TestDDLMigrationParityKeywordPrefixedColumns proves no form swallows a bare
// ADD of a column whose name starts with a keyword the forms match, such as
// ADD checksum TEXT read as ADD CHECK. Each must classify as "add column"
// and record the column through the extractor.
func TestDDLMigrationParityKeywordPrefixedColumns(t *testing.T) {
	t.Parallel()

	keywords := []string{
		"add", "primary", "key", "unique", "check", "foreign", "exclude", "constraint",
		"column", "if", "exists", "alter", "drop", "rename", "to", "validate", "set",
		"reset", "logged", "unlogged", "without", "schema", "tablespace", "access",
		"enable", "disable", "always", "replica", "trigger", "rule", "row", "force",
		"owner", "attach", "detach", "partition", "cluster", "identity", "inherit",
		"of", "not", "no", "using", "nulls",
	}
	// Names that run a keyword into the token its form needs next.
	extra := []string{"uniquenulls", "excludeusing", "primarykey", "notnull", "exclude"}
	for _, keyword := range append(keywords, extra...) {
		for _, column := range []string{keyword + "sum", keyword + "_at", keyword + "ed", keyword} {
			if ddlKeywordNames[column] {
				continue // a reserved word cannot be an unquoted column name
			}
			action := "ADD " + column + " TEXT"
			got, ok := classifyAlterAction(action)
			if !ok || got.form != "add column" || got.column != column {
				t.Errorf("classifyAlterAction(%q) = %+v, %v; want add column %q", action, got, ok, column)
				continue
			}
			inv := extractDDLInventory([]ddlSource{{file: "store/schema.go", line: 1, sql: "ALTER TABLE widgets " + action}})
			if _, recorded := inv.columns[ddlColumn{table: "widgets", column: column}]; !recorded || len(inv.unresolved) != 0 {
				t.Errorf("ALTER TABLE widgets %s: column recorded = %v, unresolved = %v", action, recorded, inv.unresolved)
			}
		}
	}
}

// TestDDLMigrationParityTableElements proves a CREATE TABLE element is read
// as a constraint only when its keyword is followed by the token the
// grammar requires, so a column named exclude, or one whose name starts
// with a constraint keyword, is recorded as a column.
func TestDDLMigrationParityTableElements(t *testing.T) {
	t.Parallel()

	constraintElements := []string{
		"PRIMARY KEY (widget_id)", "PRIMARY KEY(widget_id)", "UNIQUE (owner)", "UNIQUE(owner)",
		"UNIQUE NULLS NOT DISTINCT (owner)", "CHECK (owner <> '')", "CHECK(owner <> '')",
		"FOREIGN KEY (owner) REFERENCES users (id)", "EXCLUDE USING gist (owner WITH =)",
		"EXCLUDE (owner WITH =)", "LIKE base_widgets INCLUDING ALL", "NOT NULL owner",
		"CONSTRAINT widgets_owner_check CHECK (owner <> '')",
	}
	for _, element := range constraintElements {
		inv := extractDDLInventory([]ddlSource{{file: "store/schema.go", line: 1, sql: "CREATE TABLE widgets (widget_id TEXT, " + element + ")"}})
		if len(inv.columns) != 1 || len(inv.unresolved) != 0 {
			t.Errorf("element %q: columns = %v, unresolved = %v; want only widget_id", element, inv.columns, inv.unresolved)
		}
	}

	var columns []string
	for _, word := range []string{"constraint", "primary", "unique", "check", "foreign", "exclude", "like", "not"} {
		columns = append(columns, word+"_count", word+"sum", word+"ed")
	}
	columns = append(columns, "exclude", "uniquenulls", "excludeusing")
	for _, column := range columns {
		inv := extractDDLInventory([]ddlSource{{file: "store/schema.go", line: 1, sql: "CREATE TABLE widgets (widget_id TEXT, " + column + " BOOLEAN)"}})
		if _, ok := inv.columns[ddlColumn{table: "widgets", column: column}]; !ok || len(inv.unresolved) != 0 {
			t.Errorf("column %q: columns = %v, unresolved = %v; want it recorded", column, inv.columns, inv.unresolved)
		}
	}

	// A reserved word cannot be an unquoted column, and a keyword with
	// nothing after it is a clause joined on at runtime: both fail safe.
	for _, element := range []string{"check BOOLEAN", "exclude", "UNIQUE", "LIKE"} {
		inv := extractDDLInventory([]ddlSource{{file: "store/schema.go", line: 1, sql: "CREATE TABLE widgets (widget_id TEXT, " + element + ")"}})
		if len(inv.unresolved) != 1 {
			t.Errorf("element %q: unresolved = %v, want one entry", element, inv.unresolved)
		}
	}
}
