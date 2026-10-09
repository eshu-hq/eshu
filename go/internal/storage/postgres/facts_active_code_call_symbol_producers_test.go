// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// codeCallSymbolFactRow builds one fake fact_records row in the column order
// scanFactEnvelope reads.
func codeCallSymbolFactRow(factID, scopeID string, observedAt time.Time) []any {
	return []any{
		factID,
		scopeID,
		"generation-" + scopeID,
		"file",
		"file:" + scopeID + ":" + factID,
		"1.0.0",
		"git",
		int64(0),
		"unknown",
		"git",
		"file:" + scopeID + ":" + factID,
		"file:///" + factID,
		factID,
		observedAt,
		false,
		[]byte(`{"relative_path":"index.js","parsed_file_data":{"functions":[]}}`),
	}
}

func isManifestQuery(query string) bool {
	return strings.Contains(query, "FROM content_files")
}

func isAnchoredDefinitionQuery(query string) bool {
	return strings.Contains(query, "fact.scope_id = ANY($5::text[])")
}

// assertCodeCallManifestShape pins the manifest-read contract both anchors
// share: a MATERIALIZED CTE over content_files joined to the repository
// scopes' active generations, filtered to the manifest's file names.
func assertCodeCallManifestShape(t *testing.T, query string, pathPredicates ...string) {
	t.Helper()
	// The manifest read must stay a MATERIALIZED CTE. Inlined, the planner
	// misestimates the scope join at one row and probes content_files once per
	// repository scope (ops-qa replica, 2026-10-04: 125 ms warm, 758 ms cold,
	// 155k buffers) instead of one trigram-index read (15 to 21 ms, 3,497 buffers).
	if !strings.Contains(query, "WITH manifest AS MATERIALIZED (") {
		t.Fatalf("manifest query must read content_files in a MATERIALIZED CTE:\n%s", query)
	}
	for _, want := range append([]string{
		"scope.source_key = manifest.repo_id",
		"scope.scope_kind = 'repository'",
		"generation.generation_id = scope.active_generation_id",
		"generation.status = 'active'",
	}, pathPredicates...) {
		if !strings.Contains(query, want) {
			t.Fatalf("manifest query missing %q:\n%s", want, query)
		}
	}
}

func TestLoadActiveCodeCallSymbolDefinitionFactsAnchorsPackageKeysOnProducerScopes(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.October, 4, 9, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// Unanchored scan for the key no manifest anchors.
			{rows: [][]any{codeCallSymbolFactRow("fact-go", "scope-go", observedAt)}},
			// Manifest read.
			{rows: [][]any{
				{"scope-logging", `{"name":"@acme/logging","version":"1.0.0"}`},
				{"scope-logging", `{"name":"@acme/logging-internal"}`},
				{"scope-other", `{"name":"@acme/unrelated"}`},
			}},
			// Anchored scan for the package key.
			{rows: [][]any{codeCallSymbolFactRow("fact-logging", "scope-logging", observedAt)}},
		},
	}
	goKey := "scip-java maven org.acme/lib org.acme/Client#request()."
	packageKey := "package:@acme/logging#Logger"

	loaded, err := NewFactStore(db).LoadActiveCodeCallSymbolDefinitionFacts(
		context.Background(),
		[]string{goKey, packageKey},
	)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 3; got != want {
		t.Fatalf("queries = %d, want %d (unanchored, manifest, anchored)", got, want)
	}

	unanchored := db.queries[0]
	if isManifestQuery(unanchored.query) || isAnchoredDefinitionQuery(unanchored.query) {
		t.Fatalf("first query must be the unanchored definition scan:\n%s", unanchored.query)
	}
	if got, want := unanchored.args[0], []string{goKey}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unanchored keys = %#v, want only the non-package keys %#v", got, want)
	}

	manifest := db.queries[1]
	if !isManifestQuery(manifest.query) {
		t.Fatalf("second query must read package.json manifests:\n%s", manifest.query)
	}
	assertCodeCallManifestShape(t, manifest.query, "relative_path = 'package.json'", "relative_path LIKE '%/package.json'")

	anchored := db.queries[2]
	if !isAnchoredDefinitionQuery(anchored.query) {
		t.Fatalf("third query must be the anchored definition scan:\n%s", anchored.query)
	}
	if got, want := anchored.args[0], []string{packageKey}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored keys = %#v, want %#v", got, want)
	}
	if got, want := anchored.args[4], []string{"scope-logging"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored producer scopes = %#v, want %#v", got, want)
	}

	if got, want := factIDs(loaded), []string{"fact-go", "fact-logging"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v", got, want)
	}
}

func TestLoadActiveCodeCallSymbolDefinitionFactsKeepsDuplicateNameProducers(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"scope-b", `{"name":"@acme/shared"}`},
				{"scope-a", `{"name":"@acme/shared"}`},
				// A nested workspace manifest maps its name to its repository.
				{"scope-mono", `{"name":"@acme/widgets"}`},
			}},
			{},
		},
	}

	_, err := NewFactStore(db).LoadActiveCodeCallSymbolDefinitionFacts(
		context.Background(),
		[]string{"package:@acme/shared#Thing", "package:@acme/widgets#Button"},
	)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("queries = %d, want %d (manifest, anchored)", got, want)
	}
	if got, want := db.queries[1].args[4], []string{"scope-a", "scope-b", "scope-mono"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored producer scopes = %#v, want every scope publishing a requested name %#v", got, want)
	}
}

func TestLoadActiveCodeCallSymbolDefinitionFactsSkipsUnusableManifests(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{"scope-invalid", `{"name": "@acme/logging",`},
				{"scope-unnamed", `{"version":"1.0.0"}`},
				{"scope-blank", `{"name":"   "}`},
				{"scope-not-object", `["@acme/logging"]`},
				{"scope-wrong-type", `{"name":42}`},
				{"scope-nul", `{"name":"@acme/logging","description":"a\u0000b"}`},
			}},
			{},
		},
	}

	_, err := NewFactStore(db).LoadActiveCodeCallSymbolDefinitionFacts(
		context.Background(),
		[]string{"package:@acme/logging#Logger"},
	)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil; bad manifests must be skipped", err)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("queries = %d, want %d (manifest, anchored)", got, want)
	}
	if got, want := db.queries[1].args[4], []string{"scope-nul"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored producer scopes = %#v, want %#v", got, want)
	}
}

func TestLoadActiveCodeCallSymbolDefinitionFactsIssuesNoScanWithoutProducer(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"scope-other", `{"name":"@acme/unrelated"}`}}},
		},
	}

	loaded, err := NewFactStore(db).LoadActiveCodeCallSymbolDefinitionFacts(
		context.Background(),
		[]string{
			"package:@acme/missing#Logger",
			// Malformed package keys name no package and resolve no producer.
			"package:#Logger",
			"package:@acme/no-export",
		},
	)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded len = %d, want 0", len(loaded))
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("queries = %d, want %d: a package key with no producer must not scan definitions", got, want)
	}
	if !isManifestQuery(db.queries[0].query) {
		t.Fatalf("only query must be the manifest read:\n%s", db.queries[0].query)
	}
	if got, want := db.queries[0].args, []any(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest args = %#v, want none", got)
	}
}

func TestLoadActiveCodeCallSymbolDefinitionFactsKeepsNonPackageKeysUnanchored(t *testing.T) {
	t.Parallel()

	// A scip-java symbol and a one-field scip-go key name no producer in any
	// manifest, so both keep the corpus-wide scan.
	javaKey := "scip-java maven org.acme/lib org.acme/Client#request()."
	otherKey := "typescript:@acme/lib#run"
	bareGoKey := "scip-go gomod github.com/acme/lib"
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}

	if _, err := NewFactStore(db).LoadActiveCodeCallSymbolDefinitionFacts(context.Background(), []string{javaKey, otherKey, bareGoKey}); err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("queries = %d, want %d (the corpus-wide scan only)", got, want)
	}
	query := db.queries[0].query
	if isManifestQuery(query) || isAnchoredDefinitionQuery(query) {
		t.Fatalf("non-anchored keys must use the corpus-wide scan:\n%s", query)
	}
	if got, want := db.queries[0].args[0], []string{javaKey, otherKey, bareGoKey}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unanchored keys = %#v, want %#v", got, want)
	}
	if got, want := len(db.queries[0].args), 4; got != want {
		t.Fatalf("unanchored args = %d, want %d", got, want)
	}
}

func TestLoadActiveCodeCallSymbolDefinitionFactsPagesAnchoredScan(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.October, 4, 9, 0, 0, 0, time.UTC)
	fullPage := make([][]any, 0, listFactsByKindPageSize)
	for i := range listFactsByKindPageSize {
		fullPage = append(fullPage, codeCallSymbolFactRow(fmt.Sprintf("fact-%04d", i), "scope-logging", start.Add(time.Duration(i)*time.Second)))
	}
	lastObservedAt := start.Add(time.Duration(listFactsByKindPageSize-1) * time.Second)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"scope-logging", `{"name":"@acme/logging"}`}}},
			{rows: fullPage},
			{rows: [][]any{codeCallSymbolFactRow("fact-last", "scope-logging", lastObservedAt.Add(time.Second))}},
		},
	}

	loaded, err := NewFactStore(db).LoadActiveCodeCallSymbolDefinitionFacts(
		context.Background(),
		[]string{"package:@acme/logging#Logger"},
	)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), listFactsByKindPageSize+1; got != want {
		t.Fatalf("loaded len = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 3; got != want {
		t.Fatalf("queries = %d, want %d (manifest, two anchored pages)", got, want)
	}
	first, second := db.queries[1], db.queries[2]
	if first.args[1] != nil || first.args[2] != "" {
		t.Fatalf("first page cursor = (%v, %q), want (nil, \"\")", first.args[1], first.args[2])
	}
	if got, want := second.args[1], lastObservedAt; got != want {
		t.Fatalf("second page cursor observed_at = %v, want %v", got, want)
	}
	if got, want := second.args[2], "fact-0499"; got != want {
		t.Fatalf("second page cursor fact_id = %v, want %v", got, want)
	}
	if got, want := second.args[3], listFactsByKindPageSize; got != want {
		t.Fatalf("second page limit = %v, want %v", got, want)
	}
	if got, want := second.args[4], []string{"scope-logging"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second page producer scopes = %#v, want %#v", got, want)
	}
}

func TestLoadActiveCodeCallSymbolDefinitionFactsDeduplicatesAcrossScans(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.October, 4, 9, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{codeCallSymbolFactRow("fact-shared", "scope-logging", observedAt)}},
			{rows: [][]any{{"scope-logging", `{"name":"@acme/logging"}`}}},
			{rows: [][]any{codeCallSymbolFactRow("fact-shared", "scope-logging", observedAt)}},
		},
	}

	loaded, err := NewFactStore(db).LoadActiveCodeCallSymbolDefinitionFacts(
		context.Background(),
		[]string{"scip-java maven org.acme/lib org.acme/Client#request().", "package:@acme/logging#Logger"},
	)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := factIDs(loaded), []string{"fact-shared"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v", got, want)
	}
}

func TestLoadActiveCodeCallSymbolDefinitionFactsTrimsPackageKeyName(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.October, 4, 9, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// Manifest names are trimmed when parsed, so a key whose package part
			// carries stray whitespace must still find its producer.
			{rows: [][]any{{"scope-logging", `{"name":"@acme/logging"}`}}},
			{rows: [][]any{codeCallSymbolFactRow("fact-logging", "scope-logging", observedAt)}},
		},
	}

	loaded, err := NewFactStore(db).LoadActiveCodeCallSymbolDefinitionFacts(
		context.Background(),
		[]string{"package: @acme/logging #Logger"},
	)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("queries = %d, want %d (manifest, anchored): a padded package name must still resolve its producer", got, want)
	}
	if got, want := db.queries[1].args[4], []string{"scope-logging"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored producer scopes = %#v, want %#v", got, want)
	}
	if got, want := factIDs(loaded), []string{"fact-logging"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v", got, want)
	}
}
