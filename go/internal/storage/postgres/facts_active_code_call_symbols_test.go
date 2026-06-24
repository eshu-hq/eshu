// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFactStoreLoadActiveCodeCallSymbolDefinitionFactsUsesActiveGenerations(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"fact-file-1",
					"repository:repo-lib",
					"generation-lib",
					"file",
					"file:repo-lib:client.go",
					"1.0.0",
					"git",
					int64(0),
					"unknown",
					"git",
					"file:repo-lib:client.go",
					"file:///repo-lib/client.go",
					"client.go",
					time.Date(2026, time.June, 17, 9, 0, 0, 0, time.UTC),
					false,
					[]byte(`{"repo_id":"repo-lib","relative_path":"client.go","parsed_file_data":{"functions":[{"uid":"uid:lib:request","scip_symbol":"scip-go gomod github.com/acme/lib Client#Request()."}]}}`),
				}},
			},
		},
	}
	store := NewFactStore(db)
	symbolKeys := []string{"scip-go gomod github.com/acme/lib Client#Request()."}

	loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(context.Background(), symbolKeys)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() len = %d, want %d", got, want)
	}
	if got, want := loaded[0].FactKind, "file"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(db.queries[0].args[0], symbolKeys) {
		t.Fatalf("symbol arg = %#v, want %#v", db.queries[0].args[0], symbolKeys)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.fact_kind = 'file'",
		"fact.is_tombstone = FALSE",
		"jsonb_typeof(fact.payload->'parsed_file_data'->'functions') = 'array'",
		"jsonb_typeof(fact.payload->'parsed_file_data'->'type_aliases') = 'array'",
		"code_definition.item->>'scip_symbol' = ANY($1::text[])",
		"code_definition.item->>'package_export_symbol' = ANY($1::text[])",
		"'package:' || (code_definition.item->>'package_id') || '#'",
		"ORDER BY fact.observed_at ASC, fact.fact_id ASC",
		"LIMIT $4",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}

func TestFactStoreLoadActiveCodeCallSymbolDefinitionFactsGuardsNonArrayDefinitionPayloads(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{}},
	}
	store := NewFactStore(db)

	_, err := store.LoadActiveCodeCallSymbolDefinitionFacts(
		context.Background(),
		[]string{"scip-go gomod github.com/acme/lib Client#Request()."},
	)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}

	query := db.queries[0].query
	for _, field := range []string{"functions", "classes", "structs", "interfaces", "type_aliases"} {
		want := "jsonb_typeof(fact.payload->'parsed_file_data'->'" + field + "') = 'array'"
		if !strings.Contains(query, want) {
			t.Fatalf("query must guard %s before jsonb_array_elements; missing %q:\n%s", field, want, query)
		}
	}
}

func TestFactStoreLoadActiveCodeCallSymbolDefinitionFactsSkipsEmptySymbols(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewFactStore(db)

	loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(
		context.Background(),
		[]string{"", "   "},
	)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded len = %d, want 0", len(loaded))
	}
	if len(db.queries) != 0 {
		t.Fatalf("queries = %d, want 0", len(db.queries))
	}
}
