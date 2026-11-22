// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/store/audit"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// Seeded RED/GREEN pairs for the classifier and the subset check. Each RED case
// plants a read the declaration does not name and requires a violation; each
// GREEN case is the same read under a declaration that names it.

const reducerKindSQL = `
SELECT fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id
WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'`

func TestUndeclaredReducerReadFailsUnderGraphOnlyDeclaration(t *testing.T) {
	t.Parallel()
	used := audit.Classify(audit.Statement{SQL: reducerKindSQL})
	if got := audit.Undeclared([]string{audit.StoreGraph}, used); len(got) == 0 {
		t.Fatal("RED: a fact_records read of a reducer kind passed under a graph-only declaration")
	}
	if got := audit.Undeclared([]string{audit.StoreFactRecordsReducer}, used); len(got) != 0 {
		t.Fatalf("GREEN: declared reducer read still flagged: %v", got)
	}
}

func TestClassifyResolvesBindKindsAndSplitsReducerFromCollector(t *testing.T) {
	t.Parallel()
	sql := `SELECT fact_id FROM fact_records WHERE fact_kind = ANY($1::text[]) AND scope_id = $2`
	for name, args := range map[string][]any{
		"string slice":    {[]string{"vulnerability.cve", "reducer_ci_cd_run_correlation"}, "s"},
		"array wrapper":   {array.Of([]string{"vulnerability.cve", "reducer_ci_cd_run_correlation"}), "s"},
		"two binds apart": {array.Of([]string{"vulnerability.cve", "reducer_ci_cd_run_correlation"}), "s"},
	} {
		used := audit.Classify(audit.Statement{SQL: sql, Args: args})
		if _, ok := used.Stores[audit.StoreFactRecordsReducer]; !ok {
			t.Errorf("%s: reducer kind bound as an argument was not classified: %v", name, used.Stores)
		}
		if _, ok := used.Stores[audit.StoreFactRecordsCollector]; !ok {
			t.Errorf("%s: collector kind bound as an argument was not classified: %v", name, used.Stores)
		}
	}
	single := audit.Classify(audit.Statement{
		SQL:  `SELECT 1 FROM fact_records AS f WHERE f.fact_kind = $1`,
		Args: []any{"reducer_x"},
	})
	if _, ok := single.Stores[audit.StoreFactRecordsReducer]; !ok || len(single.Stores) != 1 {
		t.Errorf("fact_kind = $1 with a reducer kind classified as %v", single.Stores)
	}
}

func TestClassifyReportsAFactRecordsReadWithNoKindAsUnresolved(t *testing.T) {
	t.Parallel()
	used := audit.Classify(audit.Statement{SQL: `SELECT payload FROM fact_records WHERE scope_id = $1`})
	if len(used.Unresolved) != 1 {
		t.Fatalf("unresolved = %v, want the kindless fact_records read", used.Unresolved)
	}
	got := audit.Undeclared([]string{audit.StoreFactRecordsReducer, audit.StoreFactRecordsCollector}, used)
	if len(got) != 1 || got[0].Store != "unresolved" {
		t.Fatalf("a read the classifier cannot see must fail even under a broad declaration, got %v", got)
	}
}

func TestClassifyIgnoresCTEsStructuralJoinsCommentsAndLiterals(t *testing.T) {
	t.Parallel()
	sql := `
-- reads from fact_records in a comment only
WITH selected AS (
  SELECT scope_id FROM ingestion_scopes
), probe AS MATERIALIZED (
  SELECT 1 AS one FROM selected
)
SELECT w.canonical_key,
       'copied from fact_records' AS note,
       EXTRACT(EPOCH FROM now()) AS epoch
FROM supply_chain_impact_canonical_winners AS w
JOIN scope_generations AS generation ON generation.scope_id = w.winner_scope_id
JOIN probe ON TRUE
WHERE w.winner_scope_id IS DISTINCT FROM 'x'`
	used := audit.Classify(audit.Statement{SQL: sql})
	want := []string{audit.ReadTablePrefix + "supply_chain_impact_canonical_winners"}
	var got []string
	for store := range used.Stores {
		got = append(got, store)
	}
	if !slices.Equal(got, want) || len(used.Unresolved) != 0 {
		t.Fatalf("stores = %v unresolved = %v, want only %v", got, used.Unresolved, want)
	}
}

func TestClassifyMapsViewsContentTablesAndFunctions(t *testing.T) {
	t.Parallel()
	used := audit.Classify(
		audit.Statement{SQL: `SELECT digest FROM container_image_identity_current_supports WHERE image_ref = $1`},
		audit.Statement{SQL: `SELECT 1 FROM content_entities AS e JOIN unnest($1::text[]) AS u(v) ON TRUE`},
		audit.Statement{SQL: `SELECT * FROM container_image_identity_current_support_facts_for($1)`},
	)
	for _, want := range []string{
		audit.ReadTablePrefix + "container_image_identity_supports",
		audit.StoreContentSearch,
	} {
		if _, ok := used.Stores[want]; !ok {
			t.Errorf("missing %s in %v", want, used.Stores)
		}
	}
	if len(used.Stores) != 2 {
		t.Errorf("stores = %v, want exactly the view alias and the content token", used.Stores)
	}
}

func TestRecorderCapturesStatementsAndArgumentsInOrder(t *testing.T) {
	t.Parallel()
	recorder := &audit.Recorder{}
	ctx := context.Background()
	if _, err := recorder.QueryContext(ctx, "SELECT 1 FROM fact_records WHERE fact_kind = $1", "reducer_a"); err != nil {
		t.Fatal(err)
	}
	tx, err := recorder.BeginReadOnlySnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.QueryRowContext(ctx, "SELECT 2 FROM ingestion_scopes")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	statements := recorder.Statements()
	if len(statements) != 2 || !strings.Contains(statements[0].SQL, "fact_records") ||
		statements[0].Args[0] != "reducer_a" || !strings.Contains(statements[1].SQL, "ingestion_scopes") {
		t.Fatalf("recorded = %#v", statements)
	}
	recorder.Reset()
	if len(recorder.Statements()) != 0 {
		t.Fatal("Reset left statements behind")
	}
}
