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
		"comma after a JOIN item": `SELECT 1 FROM ingestion_scopes s JOIN scope_generations g ON TRUE, fact_records f
			WHERE f.fact_kind = 'reducer_x'`,
		"comma after CROSS JOIN LATERAL": `SELECT 1 FROM ingestion_scopes s CROSS JOIN LATERAL (SELECT 1) x, fact_records f
			WHERE f.fact_kind = 'reducer_x'`,
		"parenthesized join tree": `SELECT 1 FROM (fact_records f JOIN ingestion_scopes s ON TRUE)
			WHERE f.fact_kind = 'reducer_x'`,
		"nested join tree with a comma list": `SELECT 1 FROM ((ingestion_scopes s JOIN scope_generations g ON TRUE), fact_records f)
			WHERE f.fact_kind = 'reducer_x'`,
		"subquery whose WHERE is rescued by OR TRUE": `SELECT 1 FROM ingestion_scopes WHERE EXISTS (
			SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x' AND FALSE OR TRUE)`,
		"subquery WHERE with AND TRUE": `SELECT 1 FROM ingestion_scopes WHERE EXISTS (
			SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x' AND TRUE)`,
		"NOT TRUE on the left leaves the right arm live": `SELECT 1 FROM ingestion_scopes WHERE
			((NOT TRUE) OR (EXISTS (SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x')))`,
		"trim over a subquery": `SELECT trim(both ' ' from (SELECT f.k FROM fact_records f
			WHERE f.fact_kind = 'reducer_x' LIMIT 1)) FROM ingestion_scopes w`,
		"substring over a subquery": `SELECT substring((SELECT f.k FROM fact_records f
			WHERE f.fact_kind = 'reducer_x' LIMIT 1) from 2) FROM ingestion_scopes w`,
		"overlay over a subquery": `SELECT overlay((SELECT f.k FROM fact_records f
			WHERE f.fact_kind = 'reducer_x' LIMIT 1) placing 'a' from 2) FROM ingestion_scopes w`,
		"a live UNION arm after an empty one": `SELECT 1 FROM ingestion_scopes WHERE EXISTS (
			SELECT 1 FROM ingestion_scopes WHERE FALSE UNION ALL
			SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x')`,
		"a live first EXCEPT arm": `SELECT 1 FROM ingestion_scopes WHERE EXISTS (
			SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x'
			EXCEPT SELECT 1 FROM ingestion_scopes WHERE FALSE)`,
		"INTERSECT with no empty arm": `SELECT 1 FROM ingestion_scopes WHERE EXISTS (
			SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x'
			INTERSECT SELECT 1 FROM ingestion_scopes WHERE scope_id = 'a')`,
		"mixed set operators stay live": `SELECT 1 FROM ingestion_scopes WHERE EXISTS (
			SELECT 1 FROM ingestion_scopes WHERE FALSE UNION
			SELECT 1 FROM ingestion_scopes EXCEPT
			SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x' AND FALSE)`,
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
		"subquery whose WHERE ends in AND FALSE": `SELECT 1 FROM ingestion_scopes WHERE EXISTS (
			SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x' AND f.scope_id = 'a' AND FALSE)`,
		"NOT EXISTS over an always-false subquery": `SELECT 1 FROM ingestion_scopes WHERE NOT EXISTS (
			SELECT 1 FROM fact_records f JOIN scope_generations g ON TRUE WHERE f.fact_kind = 'reducer_x' AND FALSE
			ORDER BY f.fact_id)`,
		"a nested always-false group": `SELECT 1 FROM ingestion_scopes WHERE
			(EXISTS (SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x') AND (a OR FALSE) AND (FALSE))`,
		"UNION whose every arm is empty": `SELECT 1 FROM ingestion_scopes WHERE EXISTS (
			SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x' AND FALSE UNION ALL
			SELECT 1 FROM fact_records g WHERE g.fact_kind = 'reducer_y' AND FALSE)`,
		"EXCEPT with an empty first arm": `SELECT 1 FROM ingestion_scopes WHERE EXISTS (
			SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x' AND FALSE
			EXCEPT SELECT 1 FROM ingestion_scopes)`,
		"INTERSECT with an empty arm": `SELECT 1 FROM ingestion_scopes WHERE EXISTS (
			SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x'
			INTERSECT SELECT 1 FROM ingestion_scopes WHERE FALSE)`,
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
		"fact_kind LIKE": {SQL: `SELECT 1 FROM fact_records f WHERE f.fact_kind LIKE 'reducer_%'
			AND EXISTS (SELECT 1 FROM ingestion_scopes o WHERE o.scope_id = 'x' AND f.fact_kind = 'vulnerability.cve')`},
		"fact_kind <> literal arm":     {SQL: `SELECT 1 FROM fact_records f WHERE f.fact_kind = 'vulnerability.cve' OR f.fact_kind <> 'x'`},
		"fact_kind regular expression": {SQL: `SELECT 1 FROM fact_records f WHERE f.fact_kind ~ '^reducer_'`},
		"lower over fact_kind with a sibling literal": {
			SQL: `SELECT 1 FROM fact_records f WHERE lower(f.fact_kind) = 'reducer_x' OR f.fact_kind = 'vulnerability.cve'`,
		},
		"starts_with over fact_kind with a sibling literal": {
			SQL: `SELECT 1 FROM fact_records f WHERE starts_with(f.fact_kind, 'reducer_') OR f.fact_kind = 'vulnerability.cve'`,
		},
		"function over an unqualified fact_kind": {
			SQL: `SELECT 1 FROM fact_records WHERE left(fact_kind, 8) = 'reducer_' OR fact_kind = 'vulnerability.cve'`,
		},
		"fact_kind in a later function argument": {
			SQL: `SELECT 1 FROM fact_records f WHERE coalesce('x', f.fact_kind) = 'reducer_x' OR f.fact_kind = 'vulnerability.cve'`,
		},
		"NOT IN next to a sibling literal": {
			SQL: `SELECT 1 FROM fact_records f WHERE f.fact_kind NOT IN ('vulnerability.cve') AND EXISTS (
				SELECT 1 FROM ingestion_scopes o WHERE o.scope_id = 'x') OR f.fact_kind = 'vulnerability.warning'`,
		},
		"a quoted identifier named false": {
			SQL: `SELECT 1 FROM ingestion_scopes WHERE "false" AND EXISTS (
				SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x')`,
		},
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
		"NOT IN over kinds the statement reads by name": {
			SQL: `SELECT 1 FROM fact_records f WHERE f.fact_kind IN ('reducer_x', 'reducer_y')
				AND (f.fact_kind NOT IN ('reducer_y') OR f.scope_id = 'a')`,
		},
		"a comparison inside an aggregate call": {
			SQL: `SELECT bool_or(f.fact_kind = 'reducer_x') FILTER (WHERE f.fact_kind = 'reducer_x') FROM fact_records f
				WHERE f.fact_kind = 'reducer_x'`,
		},
		"fact_kind in a window clause": {
			SQL: `SELECT row_number() OVER (PARTITION BY f.fact_kind ORDER BY f.fact_id) FROM fact_records f
				WHERE f.fact_kind = 'reducer_x'`,
		},
		"trim over a column":      {SQL: `SELECT trim(both ' ' from f.k) FROM fact_records f WHERE f.fact_kind = 'reducer_x'`},
		"substring over a column": {SQL: `SELECT substring(f.k from 2) FROM fact_records f WHERE f.fact_kind = 'reducer_x'`},
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

// TestCTEReadingItsOwnNameIsARealRead pins the shadowing case for a read
// table: a non-recursive CTE named like a table that selects from that table
// reads the real table, and a CTE that only shares a name with a later
// reference does not.
func TestCTEReadingItsOwnNameIsARealRead(t *testing.T) {
	t.Parallel()
	const table = audit.ReadTablePrefix + "workflow_runs"
	used := audit.Classify(audit.Statement{SQL: `WITH workflow_runs AS (SELECT * FROM workflow_runs)
		SELECT 1 FROM ingestion_scopes s, workflow_runs r`})
	if got := audit.Undeclared([]string{audit.StoreGraph}, used); len(got) == 0 {
		t.Fatal("RED: a CTE that selects from its own name hid the real table read")
	}
	if got := audit.Undeclared([]string{table}, used); len(got) != 0 {
		t.Fatalf("GREEN: the declared table read is still flagged: %v", got)
	}
	benign := audit.Classify(audit.Statement{SQL: `WITH workflow_runs AS (SELECT 1 AS one)
		SELECT 1 FROM ingestion_scopes s, workflow_runs r`})
	if len(benign.Stores) != 0 || len(benign.Unresolved) != 0 {
		t.Fatalf("a CTE that reads nothing was counted: %v %v", benign.Stores, benign.Unresolved)
	}
}
