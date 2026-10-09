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
			{}, // No stored go.mod declares the key's module: the load falls back.
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
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("issued %d queries, want the go.mod manifest read then the corpus fallback scan", got)
	}
	if got, want := db.queries[0].query, listActiveCodeCallGoModuleManifestsQuery; got != want {
		t.Fatalf("first query is the go.mod manifest read:\n%s", got)
	}
	if !reflect.DeepEqual(db.queries[1].args[0], symbolKeys) {
		t.Fatalf("symbol arg = %#v, want %#v", db.queries[1].args[0], symbolKeys)
	}
	query := db.queries[1].query
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
		queryResponses: []queueFakeRows{{}, {}},
	}
	store := NewFactStore(db)

	_, err := store.LoadActiveCodeCallSymbolDefinitionFacts(
		context.Background(),
		[]string{"scip-go gomod github.com/acme/lib Client#Request()."},
	)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("issued %d queries, want the go.mod manifest read then the corpus fallback scan", got)
	}

	query := db.queries[1].query
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

func TestSplitCodeCallGoSymbolKeys(t *testing.T) {
	t.Parallel()

	goFunc := "scip-go gomod github.com/acme/lib Client#Request()."
	goMethod := "scip-go gomod github.com/acme/lib/pkg Svc#Serve()."
	packageKey := "package:@acme/logging#Logger"
	scipPython := "scip-python python . . django/`conf/`settings#DEBUG."
	goKeys, otherKeys := splitCodeCallGoSymbolKeys([]string{
		goFunc, packageKey, scipPython, "scip-go gomod", "scip-go gomod  ", goMethod,
	})
	if got, want := goKeys, []string{goFunc, goMethod}; !reflect.DeepEqual(got, want) {
		t.Fatalf("goKeys = %#v, want %#v", got, want)
	}
	if got, want := otherKeys, []string{packageKey, scipPython, "scip-go gomod", "scip-go gomod  "}; !reflect.DeepEqual(got, want) {
		t.Fatalf("otherKeys = %#v, want %#v", got, want)
	}
}

func TestCodeCallGoSymbolImportPath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		key  string
		want string
	}{
		{"scip-go gomod github.com/acme/lib Client#Request().", "github.com/acme/lib"},
		{"scip-go gomod github.com/acme/lib/pkg Svc#Serve().", "github.com/acme/lib/pkg"},
		{"scip-go gomod fmt Printf().", "fmt"},
		{"scip-go gomod", ""},
		{"scip-go gomod  ", ""},
		{"scip-go gomod  github.com/acme/lib  Client#Request().", "github.com/acme/lib"},
		{"package:@acme/logging#Logger", ""},
		{"", ""},
	} {
		if got := codeCallGoSymbolImportPath(tc.key); got != tc.want {
			t.Errorf("codeCallGoSymbolImportPath(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestCodeCallGoModuleManifestPath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{"bare", "module github.com/acme/lib\n\ngo 1.24\n", "github.com/acme/lib"},
		{"comments first", "// fork of upstream\n\nmodule github.com/acme/fork\n", "github.com/acme/fork"},
		{"indented", "\tmodule  github.com/acme/spaced  \n", "github.com/acme/spaced"},
		{"trailing comment", "module github.com/acme/trailing // keep serving\n", "github.com/acme/trailing"},
		{"no module line", "go 1.24\n\nrequire github.com/acme/lib v1.0.0\n", ""},
		{"empty", "", ""},
		{"module with no path", "module\n", ""},
		{"module prefix only", "modular github.com/acme/nope\n", ""},
		{"bare CR endings", "\r// fork of upstream\rmodule github.com/acme/cr\r", "github.com/acme/cr"},
		{"CRLF endings", "\r\n// fork of upstream\r\nmodule github.com/acme/crlf\r\n", "github.com/acme/crlf"},
	} {
		if got := codeCallGoModuleManifestPath(tc.content); got != tc.want {
			t.Errorf("%s: codeCallGoModuleManifestPath() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCodeCallGoImportPathInModule(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		importPath string
		module     string
		want       bool
	}{
		{"github.com/acme/lib", "github.com/acme/lib", true},
		{"github.com/acme/lib/pkg", "github.com/acme/lib", true},
		{"github.com/acme/lib/pkg/deep", "github.com/acme/lib", true},
		{"github.com/acme/lib/pkg", "github.com/acme/lib/pkg", true},
		{"github.com/acme/libext", "github.com/acme/lib", false},
		{"github.com/acme/lib", "github.com/acme/lib/pkg", false},
		{"github.com/other/mod", "github.com/acme/lib", false},
		{"", "github.com/acme/lib", false},
		{"github.com/acme/lib", "", false},
	} {
		if got := codeCallGoImportPathInModule(tc.importPath, tc.module); got != tc.want {
			t.Errorf("codeCallGoImportPathInModule(%q, %q) = %v, want %v", tc.importPath, tc.module, got, tc.want)
		}
	}
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
				{"scope:producer", "module github.com/acme/lib\n\ngo 1.24\n"},
				{"scope:noise", "module github.com/acme/noise\n"},
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
	if got, want := db.queries[0].query, listActiveCodeCallGoModuleManifestsQuery; got != want {
		t.Fatalf("first query is the manifest read:\n%s", got)
	}
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
			{rows: [][]any{{"scope:producer", "module github.com/acme/lib\n\ngo 1.24\n"}}},
			{rows: [][]any{codeCallDefinitionScanRow("fact-shared")}},
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
		t.Fatalf("issued %d queries, want manifest read, anchored scan, then corpus scan", got)
	}
}

func TestFactStoreLoadActiveCodeCallSymbolDefinitionFactsFallsBackToCorpusScanWithoutModuleProducer(t *testing.T) {
	t.Parallel()

	stdlibKey := "scip-go gomod fmt Printf()."
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"scope:noise", "module github.com/acme/noise\n"}}},
			{rows: [][]any{codeCallDefinitionScanRow("fact-stdlib")}},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(context.Background(), []string{stdlibKey})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := factIDs(loaded), []string{"fact-stdlib"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v", got, want)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("issued %d queries, want the go.mod manifest read then the corpus fallback scan", got)
	}
	if got, want := db.queries[1].query, listActiveCodeCallSymbolDefinitionFactsQuery; got != want {
		t.Fatalf("second query is the corpus-wide scan:\n%s", got)
	}
	if got, want := db.queries[1].args[0], []string{stdlibKey}; !reflect.DeepEqual(got, want) {
		t.Fatalf("corpus fallback keys = %#v, want %#v", got, want)
	}
}
