// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/store/audit"
)

// One seeded RED/GREEN pair per SQL shape the classifier must read, or fail
// closed on. Each RED case plants a reducer-kind read the declaration omits and
// requires a violation; each GREEN case is the same shape under a declaration
// that names it (or a shape that really does read nothing).

const reducerOnly = audit.StoreFactRecordsReducer

func assertRedGreen(t *testing.T, name string, statement audit.Statement) {
	t.Helper()
	used := audit.Classify(statement)
	if got := audit.Undeclared([]string{audit.StoreGraph}, used); len(got) == 0 {
		t.Errorf("%s RED: the read passed under a graph-only declaration (used %v, unresolved %v)", name, used.Stores, used.Unresolved)
	}
	if got := audit.Undeclared([]string{reducerOnly}, used); len(got) != 0 {
		t.Errorf("%s GREEN: the declared read is still flagged: %v", name, got)
	}
}

func TestShapesThatReadARealStoreAreSeen(t *testing.T) {
	t.Parallel()
	for name, sql := range map[string]string{
		"comma join, store second": `SELECT f.payload FROM ingestion_scopes s, fact_records f WHERE f.fact_kind = 'reducer_x'`,
		"comma join with alias and subquery first": `SELECT 1 FROM (SELECT 1) AS q, fact_records AS f
			WHERE f.fact_kind = 'reducer_x'`,
		"comma list of three": `SELECT 1 FROM ingestion_scopes a, scope_generations b, fact_records c WHERE c.fact_kind = 'reducer_x'`,
		"quoted identifier":   `SELECT payload FROM "fact_records" WHERE fact_kind = 'reducer_x'`,
		"double dash inside a literal": `SELECT 'a--b' AS note, payload
			FROM fact_records WHERE fact_kind = 'reducer_x'`,
		"OR TRUE AND FALSE keeps the first arm": `SELECT 1 FROM ingestion_scopes WHERE
			(EXISTS (SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x') OR TRUE AND FALSE)`,
		"a live arm after a non-leading NOT FALSE": `SELECT 1 FROM ingestion_scopes WHERE
			(scope_id = 'a' AND (NOT FALSE) OR (EXISTS (SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x')))`,
		"AND TRUE does not close a branch": `SELECT 1 FROM ingestion_scopes WHERE
			(EXISTS (SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x') AND TRUE)`,
		"NOT TRUE on the left leaves the right arm live": `SELECT 1 FROM ingestion_scopes WHERE
			((NOT TRUE) OR (EXISTS (SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x')))`,
	} {
		assertRedGreen(t, name, audit.Statement{SQL: sql})
	}
}

func TestShapesThatReadNothingAreDropped(t *testing.T) {
	t.Parallel()
	for name, sql := range map[string]string{
		"AND FALSE closes the group": `SELECT 1 FROM ingestion_scopes WHERE
			(EXISTS (SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x') AND FALSE)`,
		"NOT FALSE opens the group, so the right arm is dead": `SELECT 1 FROM ingestion_scopes WHERE
			((NOT FALSE) OR (EXISTS (SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x')))`,
		"TRUE opens the group, so the right arm is dead": `SELECT 1 FROM ingestion_scopes WHERE
			(TRUE OR EXISTS (SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x'))`,
		"NOT TRUE conjunct": `SELECT 1 FROM ingestion_scopes WHERE
			(EXISTS (SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x') AND NOT TRUE)`,
		"a nested always-false group": `SELECT 1 FROM ingestion_scopes WHERE
			(EXISTS (SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x') AND (a OR FALSE) AND (FALSE))`,
	} {
		used := audit.Classify(audit.Statement{SQL: sql})
		if len(used.Stores) != 0 || len(used.Unresolved) != 0 {
			t.Errorf("%s: a branch a constant closes still counted: stores %v unresolved %v", name, used.Stores, used.Unresolved)
		}
	}
}

func TestShapesTheClassifierCannotReadFailClosed(t *testing.T) {
	t.Parallel()
	for name, statement := range map[string]audit.Statement{
		"CTE shadowing the table": {SQL: `WITH fact_records AS (SELECT 1 AS payload)
			SELECT payload FROM fact_records WHERE fact_kind = 'reducer_x'`},
		"mixed literal kind and a non-string bind": {
			SQL:  `SELECT 1 FROM fact_records WHERE fact_kind = 'vulnerability.cve' OR fact_kind = $1`,
			Args: []any{42},
		},
		"bind with no argument": {SQL: `SELECT 1 FROM fact_records WHERE fact_kind = ANY($2::text[])`, Args: []any{"x"}},
		"IN list with a bad bind": {
			SQL:  `SELECT 1 FROM fact_records WHERE fact_kind IN ('vulnerability.cve', $1)`,
			Args: []any{3.5},
		},
		"unbalanced parentheses":      {SQL: `SELECT 1 FROM fact_records WHERE (fact_kind = 'reducer_x'`},
		"unterminated string literal": {SQL: `SELECT 1 FROM fact_records WHERE fact_kind = 'reducer_x`},
		"unterminated block comment":  {SQL: `SELECT 1 /* never closed FROM fact_records WHERE fact_kind = 'reducer_x'`},
		"unparseable FROM item":       {SQL: `SELECT 1 FROM $1`},
	} {
		used := audit.Classify(statement)
		if len(used.Unresolved) == 0 {
			t.Errorf("%s RED: a shape the classifier cannot read passed (stores %v)", name, used.Stores)
			continue
		}
		if got := audit.Undeclared([]string{reducerOnly, audit.StoreFactRecordsCollector}, used); len(got) == 0 {
			t.Errorf("%s: unresolved reads must fail even under a broad declaration", name)
		}
	}
	// GREEN: the same shapes written so the classifier can read them are clean.
	for name, statement := range map[string]audit.Statement{
		"bind with a string slice": {SQL: `SELECT 1 FROM fact_records WHERE fact_kind = ANY($1::text[])`, Args: []any{[]string{"reducer_x"}}},
		"IN list with a string bind": {
			SQL:  `SELECT 1 FROM fact_records WHERE fact_kind IN ('reducer_y', $1)`,
			Args: []any{"reducer_x"},
		},
		"double-quote inside a literal": {SQL: `SELECT 1 FROM fact_records WHERE fact_kind = 'reducer_x' AND note <> 'say "hi"'`},
	} {
		used := audit.Classify(statement)
		if len(used.Unresolved) != 0 {
			t.Errorf("%s GREEN: a readable shape was reported unresolved: %v", name, used.Unresolved)
		}
		if got := audit.Undeclared([]string{reducerOnly}, used); len(got) != 0 {
			t.Errorf("%s GREEN: flagged under the declared store: %v", name, got)
		}
	}
}

// TestWatermarkIsTheWinnersStore pins that the winners freshness watermark is
// the winners read model, not a separate or structural table: a read of it
// needs the winners declaration (arbiter ruling for #10880 PR-B0).
func TestWatermarkIsTheWinnersStore(t *testing.T) {
	t.Parallel()
	used := audit.Classify(audit.Statement{SQL: `SELECT materialized_at FROM supply_chain_impact_winners_materialization LIMIT 1`})
	const winners = audit.ReadTablePrefix + "supply_chain_impact_canonical_winners"
	if got := audit.Undeclared([]string{audit.StoreFactRecordsReducer}, used); len(got) == 0 {
		t.Fatal("RED: a watermark-only read passed under a declaration without the winners store")
	}
	if got := audit.Undeclared([]string{winners}, used); len(got) != 0 {
		t.Fatalf("GREEN: the winners declaration still flags the watermark read: %v", got)
	}
}

// TestSystemCatalogsAreNotAStore pins the schema-introspection rule: pg_*
// catalogs and information_schema read nothing a capability answers from, while
// an ordinary table beside them still does.
func TestSystemCatalogsAreNotAStore(t *testing.T) {
	t.Parallel()
	catalog := audit.Classify(
		audit.Statement{SQL: `SELECT attname FROM pg_attribute WHERE attrelid = to_regclass($1)`},
		audit.Statement{SQL: `SELECT column_name FROM information_schema.columns WHERE table_name = $1`},
		audit.Statement{SQL: `SELECT datname FROM pg_database`},
	)
	if len(catalog.Stores) != 0 || len(catalog.Unresolved) != 0 {
		t.Fatalf("GREEN: system catalogs counted as a store: %v %v", catalog.Stores, catalog.Unresolved)
	}
	mixed := audit.Classify(audit.Statement{SQL: `SELECT 1 FROM pg_attribute a, vulnerability_source_states s`})
	if got := audit.Undeclared([]string{audit.StoreGraph}, mixed); len(got) == 0 {
		t.Fatal("RED: a real table beside a system catalog was dropped")
	}
}
