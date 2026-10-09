// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	producerstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/producers"
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
	symbolKeys := []string{"scip-java maven org.acme/lib org.acme/Client#request()."}

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
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("issued %d queries, want the one corpus-wide scan for a key no manifest anchors", got)
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
		"jsonb_typeof(parsed.pfd->'functions') = 'array'",
		"jsonb_typeof(parsed.pfd->'type_aliases') = 'array'",
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
		[]string{"scip-java maven org.acme/lib org.acme/Client#request()."},
	)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("issued %d queries, want the one corpus-wide scan", got)
	}

	query := db.queries[0].query
	for _, field := range []string{"functions", "classes", "structs", "interfaces", "type_aliases"} {
		want := "jsonb_typeof(parsed.pfd->'" + field + "') = 'array'"
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

// codeCallGoModManifestRow stages one go.mod manifest read row: the leaf
// scans (scope_id, content, tag_outcome), content as sql.NullString (#7609
// dirty scopes arrive as NULL) with a clean tag (#7760).
func codeCallGoModManifestRow(scopeID, content string) []any {
	return []any{scopeID, sql.NullString{String: content, Valid: true}, "clean"}
}

func codeCallDefinitionScanRow(factID string) []any {
	return []any{
		factID,
		"scope:producer",
		"generation-producer",
		"file",
		"file:scope:producer:client.go",
		"1.0.0",
		"git",
		int64(0),
		"unknown",
		"git",
		"file:scope:producer:client.go",
		"file:///producer/client.go",
		"client.go",
		time.Date(2026, time.October, 9, 9, 0, 0, 0, time.UTC),
		false,
		[]byte(`{"repo_id":"scope:producer","relative_path":"client.go","parsed_file_data":{"functions":[{"uid":"uid:producer","scip_symbol":"scip-go gomod github.com/acme/lib Client#Request()."}]}}`),
	}
}

func TestFactStoreLoadActiveCodeCallSymbolDefinitionFactsAnchorsGoKeysOnModuleProducers(t *testing.T) {
	t.Parallel()

	goKey := "scip-go gomod github.com/acme/lib Client#Request()."
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				codeCallGoModManifestRow("scope:producer", "module github.com/acme/lib\n\ngo 1.24\n"),
				codeCallGoModManifestRow("scope:noise", "module github.com/acme/noise\n"),
			}},
			{rows: [][]any{codeCallDefinitionScanRow("fact-go-anchored")}},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(context.Background(), []string{goKey})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := factIDs(loaded), []string{"fact-go-anchored"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v", got, want)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("issued %d queries, want the go.mod manifest read then the anchored scan", got)
	}
	if got, want := db.queries[0].query, producerstore.GoModuleManifestsQuery; got != want {
		t.Fatalf("first query is the manifest read:\n%s", got)
	}
	assertCodeCallManifestShape(t, db.queries[0].query, "relative_path = 'go.mod'", "relative_path LIKE '%/go.mod'")
	if got, want := db.queries[1].query, listAnchoredActiveCodeCallSymbolDefinitionFactsQuery; got != want {
		t.Fatalf("second query is the anchored scan:\n%s", got)
	}
	if got, want := db.queries[1].args[4], []string{"scope:producer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored scan producer scopes ($5) = %#v, want %#v", got, want)
	}
}

func TestFactStoreLoadActiveCodeCallSymbolDefinitionFactsDeduplicatesGoAnchoredAgainstCorpusScan(t *testing.T) {
	t.Parallel()

	goKey := "scip-go gomod github.com/acme/lib Client#Request()."
	otherKey := "other-namespace-key"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// Corpus-wide scan for the other key.
			{rows: [][]any{codeCallDefinitionScanRow("fact-shared")}},
			// go.mod read, then the anchored scan for the Go key.
			{rows: [][]any{codeCallGoModManifestRow("scope:producer", "module github.com/acme/lib\n\ngo 1.24\n")}},
			{rows: [][]any{codeCallDefinitionScanRow("fact-shared")}},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(context.Background(), []string{goKey, otherKey})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := factIDs(loaded), []string{"fact-shared"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v", got, want)
	}
	if got, want := len(db.queries), 3; got != want {
		t.Fatalf("issued %d queries, want corpus scan, go.mod read, then anchored scan", got)
	}
}

func TestFactStoreLoadActiveCodeCallSymbolDefinitionFactsIssuesNoScanForGoKeyWithoutModuleProducer(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{codeCallGoModManifestRow("scope:noise", "module github.com/acme/noise\n")}},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(
		context.Background(), []string{"scip-go gomod fmt Printf()."},
	)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded fact ids = %#v, want none: no stored go.mod declares the standard library", factIDs(loaded))
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("issued %d queries, want only the go.mod manifest read and no definition scan", got)
	}
	if got, want := db.queries[0].query, producerstore.GoModuleManifestsQuery; got != want {
		t.Fatalf("the one query is the go.mod manifest read:\n%s", got)
	}
}

// TestLoadActiveCodeCallSymbolDefinitionFactsLogsLoadSummary pins the one log
// line an operator reads at 3 AM to tell which leg made a materialization slow:
// the key counts per kind, the producer scope counts, and which legs ran.
func TestLoadActiveCodeCallSymbolDefinitionFactsLogsLoadSummary(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{}, // corpus-wide scan for the other key
			{rows: [][]any{codeCallGoModManifestRow("scope:producer", "module github.com/acme/lib\n")}},
			{rows: [][]any{codeCallDefinitionScanRow("fact-go")}},
		},
	}
	_, err := NewFactStore(db).LoadActiveCodeCallSymbolDefinitionFacts(context.Background(), []string{
		"scip-java maven org.acme/lib org.acme/Client#request().",
		"scip-go gomod github.com/acme/lib Client#Request().",
		"scip-go gomod fmt Printf().",
	})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	for _, want := range []string{
		"msg=\"code call symbol definition load\"",
		"outcome=ok",
		"other_key_count=1",
		"package_key_count=0",
		"go_key_count=2",
		"package_producer_scope_count=0",
		"go_producer_scope_count=1",
		"other_scan_duration_seconds=",
		"go_leg_duration_seconds=",
	} {
		if !strings.Contains(logged.String(), want) {
			t.Fatalf("load log is missing %q:\n%s", want, logged.String())
		}
	}
}

// TestLoadActiveCodeCallSymbolDefinitionFactsLogsLoadFailure pins that a failed
// load still logs its summary, marked as an error, so an operator does not read
// a zero producer scope count on a failed manifest read as "no producer".
func TestLoadActiveCodeCallSymbolDefinitionFactsLogsLoadFailure(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{err: errors.New("manifest read failed")}},
	}
	_, err := NewFactStore(db).LoadActiveCodeCallSymbolDefinitionFacts(context.Background(), []string{
		"scip-go gomod github.com/acme/lib Client#Request().",
	})
	if err == nil {
		t.Fatal("LoadActiveCodeCallSymbolDefinitionFacts() error = nil, want the manifest read error")
	}
	for _, want := range []string{
		"msg=\"code call symbol definition load\"",
		"outcome=error",
		"go_key_count=1",
		"manifest read failed",
	} {
		if !strings.Contains(logged.String(), want) {
			t.Fatalf("load log is missing %q:\n%s", want, logged.String())
		}
	}
}
