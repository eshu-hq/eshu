// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// ddlParityKindsMigration is a migration corpus that creates one object of
// each kind the guard compares by name only: a sequence, a view, a
// materialized view, a procedure, a type, and a domain.
const ddlParityKindsMigration = `
CREATE TABLE IF NOT EXISTS widgets (widget_id TEXT PRIMARY KEY);
CREATE SEQUENCE IF NOT EXISTS widgets_epoch_seq;
CREATE OR REPLACE VIEW widgets_current AS SELECT widget_id FROM widgets;
CREATE MATERIALIZED VIEW IF NOT EXISTS widgets_rollup AS SELECT count(*) FROM widgets;
CREATE OR REPLACE PROCEDURE public.prune_widgets(keep INT) LANGUAGE sql AS $$ SELECT 1 $$;
CREATE TYPE widget_state AS ENUM ('new', 'done');
CREATE DOMAIN widget_label AS TEXT CHECK (VALUE <> '');
`

// TestDDLMigrationParityObjectKinds is the seeded RED/GREEN proof for the
// statement kinds beyond tables, columns, indexes, constraints, triggers, and
// functions. A sequence, view, materialized view, procedure, type, or domain
// in Go DDL must exist in a migration under the same name and kind. ALTER
// INDEX, ALTER SEQUENCE, ALTER TYPE, ALTER DOMAIN, and ALTER [MATERIALIZED]
// VIEW are never compared, so they are unresolved and fail unless listed in
// ddlParityDynamicDDL. Temporary views and sequences are session-local.
func TestDDLMigrationParityObjectKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		goSQL   string
		wantGap []string // sorted substrings, one per expected gap
	}{
		{name: "pass: sequence present in migrations", goSQL: "CREATE SEQUENCE IF NOT EXISTS public.widgets_epoch_seq START 1"},
		{name: "pass: view present in migrations", goSQL: "CREATE OR REPLACE VIEW widgets_current AS SELECT widget_id FROM widgets"},
		{name: "pass: recursive view keyword", goSQL: "CREATE RECURSIVE VIEW widgets_current (widget_id) AS SELECT 'a'"},
		{name: "pass: materialized view present in migrations", goSQL: "CREATE MATERIALIZED VIEW widgets_rollup AS SELECT 1"},
		{name: "pass: procedure present in migrations", goSQL: "CREATE OR REPLACE PROCEDURE prune_widgets(keep INT) LANGUAGE sql AS $$ SELECT 1 $$"},
		{name: "pass: type present in migrations", goSQL: "CREATE TYPE widget_state AS ENUM ('new', 'done')"},
		{name: "pass: domain present in migrations", goSQL: `CREATE DOMAIN "widget_label" AS TEXT`},
		{name: "pass: temporary view and sequence are session-local", goSQL: "CREATE TEMP VIEW scratch_view AS SELECT 1; CREATE TEMPORARY SEQUENCE scratch_seq"},
		{
			name:    "fail: Go-only sequence",
			goSQL:   "CREATE SEQUENCE IF NOT EXISTS widgets_fencing_seq",
			wantGap: []string{"sequence widgets_fencing_seq"},
		},
		{
			name:    "fail: Go-only unlogged sequence",
			goSQL:   "CREATE UNLOGGED SEQUENCE widgets_fencing_seq",
			wantGap: []string{"sequence widgets_fencing_seq"},
		},
		{
			name:    "fail: Go-only view",
			goSQL:   "CREATE OR REPLACE VIEW widgets_stale AS SELECT 1",
			wantGap: []string{"view widgets_stale"},
		},
		{
			name:    "fail: Go-only materialized view",
			goSQL:   "CREATE MATERIALIZED VIEW IF NOT EXISTS widgets_hourly AS SELECT 1",
			wantGap: []string{"materialized view widgets_hourly"},
		},
		{
			name:    "fail: a plain view does not match a materialized view of the same name",
			goSQL:   "CREATE VIEW widgets_rollup AS SELECT 1",
			wantGap: []string{"view widgets_rollup"},
		},
		{
			name:    "fail: Go-only procedure",
			goSQL:   "CREATE PROCEDURE archive_widgets() LANGUAGE sql AS $$ SELECT 1 $$",
			wantGap: []string{"procedure archive_widgets"},
		},
		{
			name:    "fail: Go-only type",
			goSQL:   "CREATE TYPE widget_kind AS ENUM ('a')",
			wantGap: []string{"type widget_kind"},
		},
		{
			name:    "fail: Go-only domain",
			goSQL:   "CREATE DOMAIN widget_code AS TEXT",
			wantGap: []string{"domain widget_code"},
		},
		{
			name:    "fail: sequence name joined on at runtime",
			goSQL:   "CREATE SEQUENCE IF NOT EXISTS %s",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: view with no readable name",
			goSQL:   "CREATE OR REPLACE VIEW ",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: ALTER INDEX is never compared",
			goSQL:   "ALTER INDEX widgets_pkey RENAME TO widgets_id_pkey",
			wantGap: []string{`unresolved DDL in "ALTER INDEX widgets_pkey RENAME TO widgets_id_pkey"`},
		},
		{
			name:    "fail: ALTER SEQUENCE is never compared",
			goSQL:   "ALTER SEQUENCE widgets_epoch_seq RESTART",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: ALTER TYPE is never compared",
			goSQL:   "ALTER TYPE widget_state ADD VALUE IF NOT EXISTS 'archived'",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: ALTER DOMAIN is never compared",
			goSQL:   "ALTER DOMAIN widget_label SET NOT NULL",
			wantGap: []string{"unresolved"},
		},
		{
			name:    "fail: ALTER VIEW and ALTER MATERIALIZED VIEW are never compared",
			goSQL:   "ALTER VIEW widgets_current RENAME TO widgets_now; ALTER MATERIALIZED VIEW widgets_rollup SET SCHEMA archive",
			wantGap: []string{"unresolved", "unresolved"},
		},
	}

	migInv := extractDDLInventory([]ddlSource{{file: "migrations/003_kinds.sql", line: 1, sql: ddlParityKindsMigration}})
	for _, obj := range []ddlObject{
		{kind: "sequence", name: "widgets_epoch_seq"},
		{kind: "view", name: "widgets_current"},
		{kind: "materialized view", name: "widgets_rollup"},
		{kind: "procedure", name: "prune_widgets"},
		{kind: "type", name: "widget_state"},
		{kind: "domain", name: "widget_label"},
	} {
		if _, ok := migInv.objects[obj]; !ok {
			t.Errorf("migration object %v not extracted; objects = %v", obj, migInv.objects)
		}
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
