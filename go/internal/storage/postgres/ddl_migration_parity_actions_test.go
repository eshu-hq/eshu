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
		"FROBNICATE owner",
	}
	for _, action := range rejected {
		if got, ok := classifyAlterAction(action); ok {
			t.Errorf("classifyAlterAction(%q) = %+v, accepted; want rejected", action, got)
		}
	}
}
