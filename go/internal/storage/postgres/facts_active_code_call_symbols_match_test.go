// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// legacyCodeCallSymbolMatchClause is the definition test the loader used
// before #7601's single-extraction change: it reads fact.payload directly in
// each of its ten places. It is kept only as the oracle for the differential
// proof below, so that reading parsed_file_data once cannot change which facts
// match. Only this match predicate is frozen here. The SELECT head and the
// paging tail are derived from the shipped query (see legacyDefinitionQuery)
// so the oracle cannot drift from them.
const legacyCodeCallSymbolMatchClause = `  AND EXISTS (
    SELECT 1
    FROM (
      SELECT definition.item FROM jsonb_array_elements(CASE WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'functions') = 'array' THEN fact.payload->'parsed_file_data'->'functions' ELSE '[]'::jsonb END) AS definition(item)
      UNION ALL
      SELECT definition.item FROM jsonb_array_elements(CASE WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'classes') = 'array' THEN fact.payload->'parsed_file_data'->'classes' ELSE '[]'::jsonb END) AS definition(item)
      UNION ALL
      SELECT definition.item FROM jsonb_array_elements(CASE WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'structs') = 'array' THEN fact.payload->'parsed_file_data'->'structs' ELSE '[]'::jsonb END) AS definition(item)
      UNION ALL
      SELECT definition.item FROM jsonb_array_elements(CASE WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'interfaces') = 'array' THEN fact.payload->'parsed_file_data'->'interfaces' ELSE '[]'::jsonb END) AS definition(item)
      UNION ALL
      SELECT definition.item FROM jsonb_array_elements(CASE WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'type_aliases') = 'array' THEN fact.payload->'parsed_file_data'->'type_aliases' ELSE '[]'::jsonb END) AS definition(item)
    ) AS code_definition(item)
    WHERE code_definition.item->>'scip_symbol' = ANY($1::text[])
       OR code_definition.item->>'scip_symbol_key' = ANY($1::text[])
       OR code_definition.item->>'scip_moniker' = ANY($1::text[])
       OR code_definition.item->>'symbol' = ANY($1::text[])
       OR code_definition.item->>'package_export_symbol' = ANY($1::text[])
       OR code_definition.item->>'export_symbol' = ANY($1::text[])
       OR code_definition.item->>'stable_symbol_key' = ANY($1::text[])
       OR (
         COALESCE(NULLIF(code_definition.item->>'package_id', ''), '') <> ''
         AND COALESCE(NULLIF(COALESCE(code_definition.item->>'export_name', code_definition.item->>'exported_name'), ''), '') <> ''
         AND (
           'package:' || (code_definition.item->>'package_id') || '#' ||
           COALESCE(code_definition.item->>'export_name', code_definition.item->>'exported_name')
         ) = ANY($1::text[])
       )
  )
`

// codeCallDefinitionPagingMarker starts the keyset-paging tail every
// definition scan shares.
const codeCallDefinitionPagingMarker = "  AND (\n    $2::timestamptz IS NULL"

// legacyDefinitionQuery is the anchored definition scan with the legacy match
// predicate: the shipped head, the producer-scope filter, the frozen legacy
// match, then the shipped paging tail.
func legacyDefinitionQuery() string {
	return codeCallSymbolDefinitionFactsSelect +
		"  AND fact.scope_id = ANY($5::text[])\n" +
		legacyCodeCallSymbolMatchClause +
		pagingTail(listAnchoredActiveCodeCallSymbolDefinitionFactsQuery)
}

// pagingTail returns the keyset-paging tail of a definition scan, starting at
// the shared marker, or the empty string when the marker is missing.
func pagingTail(query string) string {
	_, tail, found := strings.Cut(query, codeCallDefinitionPagingMarker)
	if !found {
		return ""
	}
	return codeCallDefinitionPagingMarker + tail
}

// TestCodeCallDefinitionOracleSharesProductionPagingAndHead guards the
// differential: the oracle must page and select exactly like production, so a
// difference in result sets can only come from the match predicate.
func TestCodeCallDefinitionOracleSharesProductionPagingAndHead(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"corpus-wide": listActiveCodeCallSymbolDefinitionFactsQuery,
		"anchored":    listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
	} {
		if !strings.HasPrefix(query, codeCallSymbolDefinitionFactsSelect) {
			t.Fatalf("%s query does not start with the shared SELECT head", name)
		}
		if !strings.Contains(query, codeCallDefinitionPagingMarker) {
			t.Fatalf("%s query lost the keyset paging tail marker %q", name, codeCallDefinitionPagingMarker)
		}
	}
	if got, want := pagingTail(legacyDefinitionQuery()), pagingTail(listAnchoredActiveCodeCallSymbolDefinitionFactsQuery); got == "" || got != want {
		t.Fatal("oracle and production paging tails differ or are missing")
	}
}

// TestCodeCallDefinitionQueriesReadParsedFileDataOnce pins the cost fix for
// #7601. Every definition test reads the file's parsed_file_data, a large
// out-of-line value, and each read through fact.payload detoasts it again. The
// shared head extracts it once in a LATERAL subquery and the match reads it from
// there. OFFSET 0 is load-bearing: without it the planner folds the subquery
// back into its callers and the ten reads return.
func TestCodeCallDefinitionQueriesReadParsedFileDataOnce(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"corpus-wide": listActiveCodeCallSymbolDefinitionFactsQuery,
		"anchored":    listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
	} {
		if got := strings.Count(query, "'parsed_file_data'"); got != 1 {
			t.Fatalf("%s query reads 'parsed_file_data' %d times, want exactly once (the LATERAL extraction):\n%s", name, got, query)
		}
		if !strings.Contains(query, "CROSS JOIN LATERAL (\n  SELECT fact.payload->'parsed_file_data' AS pfd\n  OFFSET 0\n) AS parsed") {
			t.Fatalf("%s query lost the OFFSET 0 fence around the parsed_file_data extraction:\n%s", name, query)
		}
		if got := strings.Count(query, "parsed.pfd->"); got != 10 {
			t.Fatalf("%s query reads parsed.pfd %d times, want 10 (typeof and value for five arrays)", name, got)
		}
	}
}

// TestReducerContentionGateActiveCodeCallSymbolMatchEqualsLegacy proves on
// real Postgres that reading parsed_file_data once returns exactly the facts
// the direct reads returned, for every key field, every definition array and
// both package-pair spellings, and that it never fails on payloads the legacy
// test guarded against.
func TestReducerContentionGateActiveCodeCallSymbolMatchEqualsLegacy(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:prod", "repository:r_prod", "generation-prod", now)

	type fixture struct {
		id      string
		payload string
		matches bool
	}
	field := func(array, key, value string) string {
		return fmt.Sprintf(`{"parsed_file_data":{%q:[{%q:%q}]}}`, array, key, value)
	}
	var fixtures []fixture
	for _, array := range []string{"functions", "classes", "structs", "interfaces", "type_aliases"} {
		for _, key := range []string{"scip_symbol", "scip_symbol_key", "scip_moniker", "symbol", "package_export_symbol", "export_symbol", "stable_symbol_key"} {
			fixtures = append(fixtures, fixture{"hit-" + array + "-" + key, field(array, key, "wanted-key"), true})
			fixtures = append(fixtures, fixture{"miss-" + array + "-" + key, field(array, key, "other-key"), false})
		}
		fixtures = append(fixtures,
			fixture{"pair-export-" + array, fmt.Sprintf(`{"parsed_file_data":{%q:[{"package_id":"@acme/lib","export_name":"Thing"}]}}`, array), true},
			fixture{"pair-exported-" + array, fmt.Sprintf(`{"parsed_file_data":{%q:[{"package_id":"@acme/lib","exported_name":"Thing"}]}}`, array), true},
			fixture{"pair-wrong-id-" + array, fmt.Sprintf(`{"parsed_file_data":{%q:[{"package_id":"@acme/other","export_name":"Thing"}]}}`, array), false},
			fixture{"pair-wrong-name-" + array, fmt.Sprintf(`{"parsed_file_data":{%q:[{"package_id":"@acme/lib","export_name":"Nope"}]}}`, array), false},
			fixture{"pair-empty-id-" + array, fmt.Sprintf(`{"parsed_file_data":{%q:[{"package_id":"","export_name":"Thing"}]}}`, array), false},
		)
	}
	fixtures = append(fixtures,
		fixture{"match-in-second-element", `{"parsed_file_data":{"functions":[{"scip_symbol":"nope"},{"scip_symbol":"wanted-key"}]}}`, true},
		fixture{"match-in-later-array", `{"parsed_file_data":{"functions":[{"scip_symbol":"nope"}],"interfaces":[{"symbol":"wanted-key"}]}}`, true},
		fixture{"call-site-not-a-definition", `{"parsed_file_data":{"function_calls":[{"scip_symbol":"wanted-key"}]}}`, false},
		fixture{"functions-is-a-string", `{"parsed_file_data":{"functions":"wanted-key"}}`, false},
		fixture{"functions-is-null", `{"parsed_file_data":{"functions":null}}`, false},
		fixture{"functions-is-a-number", `{"parsed_file_data":{"functions":7}}`, false},
		fixture{"element-is-a-scalar", `{"parsed_file_data":{"functions":["wanted-key", 5, null]}}`, false},
		fixture{"no-parsed-file-data", `{"repo_id":"r"}`, false},
		fixture{"parsed-file-data-is-an-array", `{"parsed_file_data":[{"scip_symbol":"wanted-key"}]}`, false},
		fixture{"empty-arrays", `{"parsed_file_data":{"functions":[],"classes":[]}}`, false},
		fixture{"key-field-is-a-number", `{"parsed_file_data":{"functions":[{"scip_symbol":7}]}}`, false},
		fixture{"key-field-number-matches-its-text", `{"parsed_file_data":{"functions":[{"scip_symbol":42}]}}`, true},
		fixture{"package-id-number-matches-its-text", `{"parsed_file_data":{"functions":[{"package_id":5,"export_name":"Num"}]}}`, true},
		fixture{"key-field-is-null", `{"parsed_file_data":{"functions":[{"scip_symbol":null}]}}`, false},
	)
	for index, f := range fixtures {
		if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES ($1, 'scope:prod', 'generation-prod', 'file', 'file:' || $1, 'git', $1, $2, $2, $3::jsonb)`,
			f.id, now.Add(time.Duration(index)*time.Millisecond), f.payload); err != nil {
			t.Fatalf("insert fixture %q: %v", f.id, err)
		}
	}

	keys := []string{"wanted-key", "42", "package:@acme/lib#Thing", "package:5#Num"}
	scopes := []string{"scope:prod"}
	legacy := queryFactIDs(t, ctx, database, legacyDefinitionQuery(), keys, scopes)
	current := queryFactIDs(t, ctx, database, listAnchoredActiveCodeCallSymbolDefinitionFactsQuery, keys, scopes)

	var wantHits []string
	for _, f := range fixtures {
		if f.matches {
			wantHits = append(wantHits, f.id)
		}
	}
	sort.Strings(wantHits)
	if !reflect.DeepEqual(legacy, wantHits) {
		t.Fatalf("fixture drifted from the legacy oracle:\nlegacy = %v\nwant   = %v", legacy, wantHits)
	}
	if missing := difference(legacy, current); len(missing) != 0 {
		t.Fatalf("single-extraction test dropped %d fact(s) the legacy test returned: %v", len(missing), missing)
	}
	if extra := difference(current, legacy); len(extra) != 0 {
		t.Fatalf("single-extraction test returned %d fact(s) the legacy test did not: %v", len(extra), extra)
	}
}

// TestReducerContentionGateActiveCodeCallSymbolPlanReadsParsedFileDataOnce
// proves the OFFSET 0 fence on the real planner. The differential above passes
// with or without the fence because the predicate is unchanged, so only the
// plan can show that the five jsonb_array_elements calls read the extracted
// value and not fact.payload. When the planner folds the subquery back in, each
// call detoasts the payload again: 800 ms instead of 120 ms on the heaviest
// real consumer (#7601).
func TestReducerContentionGateActiveCodeCallSymbolPlanReadsParsedFileDataOnce(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)

	for name, query := range map[string]string{
		"corpus-wide": listActiveCodeCallSymbolDefinitionFactsQuery,
		"anchored":    listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
	} {
		args := []any{[]string{"wanted-key"}, nil, "", 500}
		if name == "anchored" {
			args = append(args, []string{"scope:prod"})
		}
		rows, err := database.QueryContext(ctx, "EXPLAIN (VERBOSE, COSTS OFF) "+query, args...)
		if err != nil {
			t.Fatalf("%s: explain: %v", name, err)
		}
		var calls []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				_ = rows.Close()
				t.Fatalf("%s: scan plan line: %v", name, err)
			}
			if strings.Contains(line, "jsonb_array_elements(") {
				calls = append(calls, line)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("%s: plan rows: %v", name, err)
		}
		_ = rows.Close()
		if len(calls) != 5 {
			t.Fatalf("%s: plan has %d jsonb_array_elements calls, want 5 (one per definition array):\n%s", name, len(calls), strings.Join(calls, "\n"))
		}
		for _, line := range calls {
			if strings.Contains(line, "fact.payload") || !strings.Contains(line, "parsed.pfd") {
				t.Fatalf("%s: plan reads the payload directly instead of the extracted value: %s", name, line)
			}
		}
	}
}

// queryFactIDs runs a definition scan with the given first argument and
// producer scopes and returns the sorted fact ids it selects. The first
// argument is the text[] of requested keys.
func queryFactIDs(t *testing.T, ctx context.Context, database *sql.DB, query string, first any, scopes []string) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, "SELECT q.fact_id FROM ("+query+") AS q", first, nil, "", 500, scopes)
	if err != nil {
		t.Fatalf("definition scan: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan fact id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("definition scan rows: %v", err)
	}
	sort.Strings(ids)
	return ids
}

// difference returns the members of a that are not in b.
func difference(a, b []string) []string {
	seen := make(map[string]struct{}, len(b))
	for _, id := range b {
		seen[id] = struct{}{}
	}
	var out []string
	for _, id := range a {
		if _, ok := seen[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}
