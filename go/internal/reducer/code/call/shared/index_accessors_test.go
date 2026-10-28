// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// accessorIndex builds an EntityIndex with one populated entry per lookup
// table so every read-only accessor has a hit and a miss case.
func accessorIndex() EntityIndex {
	return EntityIndex{
		uniqueNameByPath:        map[string]map[string]string{"/repo/a.js": {"serve": "entity-serve"}},
		uniqueNameByRepo:        map[string]map[string]string{"repo": {"serve": "entity-serve"}},
		uniqueNameByRepoDir:     map[string]map[string]map[string]string{"repo": {"src": {"serve": "entity-serve"}}},
		uniqueNameByRepoPath:    map[string]map[string]map[string]string{"repo": {"/repo/a.js": {"serve": "entity-serve"}}},
		goMethodReturnTypes:     map[string]map[string]string{"repo": {"Worker.Run": "Result"}},
		goExportByImportPath:    map[string]map[string]goCrossRepoExportEntry{"example.com/mod": {"Serve": {EntityID: "entity-serve", RepositoryID: "repo", Count: 1}}},
		javaScriptAliasesByFile: map[string]map[string][]javaScriptStaticAliasSpan{"repo": {"/repo/a.js": {{StartLine: 1, EndLine: 5}}}},
		pythonClassBasesByRepo:  map[string]map[string][]string{"repo": {"Handler": {"Base"}}},
		rustTraitMethodsByRepo:  map[string]map[string]string{"repo": {"Serve::run": "entity-run"}},
		spansByFile:             map[string]map[string][]FunctionSpan{"repo": {"/repo/a.js": {{StartLine: 1, EndLine: 5, EntityID: "entity-serve"}}}},
		typeScriptInterfaceMethodsByRepo: map[string]map[string]map[string]string{
			"repo": {"Server": {"serve": "entity-serve"}},
		},
		repositoryImportPathsByRepo: map[string][]string{"repo": {"/repo/a.js"}},
		entityFileByID:              map[string]string{"entity-serve": "/repo/a.js"},
		entityTypeByID:              map[string]string{"entity-serve": "Function"},
		nodePackageNamesByRepo:      map[string]map[string]struct{}{"repo": {"pkg": {}}},
		entityByStableSymbolKey: map[string]codeCallSymbolResolution{
			"scip-test": {entityID: "entity-serve", method: codeprovenance.MethodImportBinding},
		},
	}
}

// TestEntityIndexAccessors proves every read-only accessor returns its hit
// and reports ""/nil/false on a miss.
func TestEntityIndexAccessors(t *testing.T) {
	t.Parallel()

	index := accessorIndex()
	if got := index.EntityFileByID("entity-serve"); got != "/repo/a.js" {
		t.Fatalf("EntityFileByID hit = %q, want /repo/a.js", got)
	}
	if got := index.EntityFileByID("missing"); got != "" {
		t.Fatalf("EntityFileByID miss = %q, want empty", got)
	}
	if got := index.UniqueNameByRepoDir("repo", "src", "serve"); got != "entity-serve" {
		t.Fatalf("UniqueNameByRepoDir hit = %q, want entity-serve", got)
	}
	if got := index.UniqueNameByRepoDir("repo", "other", "serve"); got != "" {
		t.Fatalf("UniqueNameByRepoDir miss = %q, want empty", got)
	}
	if got := index.GoMethodReturnTypes("repo", "Worker.Run"); got != "Result" {
		t.Fatalf("GoMethodReturnTypes hit = %q, want Result", got)
	}
	if got := index.GoMethodReturnTypes("repo", "Worker.Missing"); got != "" {
		t.Fatalf("GoMethodReturnTypes miss = %q, want empty", got)
	}
	entry, ok := index.GoExportByImportPath("example.com/mod", "Serve")
	if !ok || entry.EntityID != "entity-serve" || entry.Count != 1 {
		t.Fatalf("GoExportByImportPath hit = (%#v, %v), want entity-serve", entry, ok)
	}
	if _, ok := index.GoExportByImportPath("example.com/mod", "Missing"); ok {
		t.Fatal("GoExportByImportPath miss reported ok, want false")
	}
	if !index.HasGoExports() {
		t.Fatal("HasGoExports = false on a populated index, want true")
	}
	if (EntityIndex{}).HasGoExports() {
		t.Fatal("HasGoExports = true on an empty index, want false")
	}
	if got := index.JavaScriptAliasesByFile("repo", "/repo/a.js"); len(got) != 1 {
		t.Fatalf("JavaScriptAliasesByFile hit = %#v, want 1 span", got)
	}
	if got := index.JavaScriptAliasesByFile("repo", "/repo/other.js"); len(got) != 0 {
		t.Fatalf("JavaScriptAliasesByFile miss = %#v, want empty", got)
	}
	if got := index.PythonClassBasesByRepo("repo", "Handler"); !reflect.DeepEqual(got, []string{"Base"}) {
		t.Fatalf("PythonClassBasesByRepo hit = %#v, want [Base]", got)
	}
	if got := index.PythonClassBasesByRepo("repo", "Missing"); got != nil {
		t.Fatalf("PythonClassBasesByRepo miss = %#v, want nil", got)
	}
	if got := index.RustTraitMethodsByRepo("repo", "Serve::run"); got != "entity-run" {
		t.Fatalf("RustTraitMethodsByRepo hit = %q, want entity-run", got)
	}
	if got := index.RustTraitMethodsByRepo("repo", "Serve::missing"); got != "" {
		t.Fatalf("RustTraitMethodsByRepo miss = %q, want empty", got)
	}
	if got := index.SpansByFile("repo", "/repo/a.js"); len(got) != 1 || got[0].EntityID != "entity-serve" {
		t.Fatalf("SpansByFile hit = %#v, want the serve span", got)
	}
	if got := index.TypeScriptInterfaceMethodsByRepo("repo", "Server", "serve"); got != "entity-serve" {
		t.Fatalf("TypeScriptInterfaceMethodsByRepo hit = %q, want entity-serve", got)
	}
	if got := index.TypeScriptInterfaceMethodsByRepo("repo", "Server", "missing"); got != "" {
		t.Fatalf("TypeScriptInterfaceMethodsByRepo miss = %q, want empty", got)
	}
	if got := index.RepositoryImportPathsByRepo("repo"); !reflect.DeepEqual(got, []string{"/repo/a.js"}) {
		t.Fatalf("RepositoryImportPathsByRepo hit = %#v, want [/repo/a.js]", got)
	}
	if got := index.UniqueNameByPath("/repo/a.js", "serve"); got != "entity-serve" {
		t.Fatalf("UniqueNameByPath hit = %q, want entity-serve", got)
	}
	if got := index.UniqueNameByPath("/repo/a.js", "missing"); got != "" {
		t.Fatalf("UniqueNameByPath miss = %q, want empty", got)
	}
	if got := index.UniqueNameByRepo("repo", "serve"); got != "entity-serve" {
		t.Fatalf("UniqueNameByRepo hit = %q, want entity-serve", got)
	}
	if got := index.UniqueNameByRepo("repo", "missing"); got != "" {
		t.Fatalf("UniqueNameByRepo miss = %q, want empty", got)
	}
	if !index.RepoPublishesNodePackage("repo", "pkg") {
		t.Fatal("RepoPublishesNodePackage = false for a published package, want true")
	}
	if index.RepoPublishesNodePackage("repo", "other") {
		t.Fatal("RepoPublishesNodePackage = true for an unpublished package, want false")
	}
}

// TestEndpointEntityType proves the entity-kind resolution: indexed type,
// repository-prefixed file fallback, and blank/unknown rejection.
func TestEndpointEntityType(t *testing.T) {
	t.Parallel()

	index := accessorIndex()
	if got := EndpointEntityType(index, "repo", "entity-serve"); got != "Function" {
		t.Fatalf("EndpointEntityType indexed = %q, want Function", got)
	}
	if got := EndpointEntityType(index, "repo", "repo:/repo/a.js"); got != "File" {
		t.Fatalf("EndpointEntityType repo-prefixed = %q, want File", got)
	}
	if got := EndpointEntityType(index, "", "repo:/repo/a.js"); got != "" {
		t.Fatalf("EndpointEntityType without repo = %q, want empty", got)
	}
	if got := EndpointEntityType(index, "repo", "unknown"); got != "" {
		t.Fatalf("EndpointEntityType unknown = %q, want empty", got)
	}
	if got := EndpointEntityType(index, "repo", "   "); got != "" {
		t.Fatalf("EndpointEntityType blank = %q, want empty", got)
	}
	for bucket, want := range map[string]string{
		"classes": "Class", "structs": "Struct", "interfaces": "Interface", "type_aliases": "TypeAlias",
	} {
		if got := codeCallEntityTypeForBucket(bucket); got != want {
			t.Fatalf("codeCallEntityTypeForBucket(%q) = %q, want %q", bucket, got, want)
		}
	}
	if got := codeCallEntityTypeForBucket("functions"); got != "" {
		t.Fatalf("codeCallEntityTypeForBucket(functions) = %q, want empty", got)
	}
}

// TestResolveContextCallName proves CallName trims and tolerates absence.
func TestResolveContextCallName(t *testing.T) {
	t.Parallel()

	ctx := ResolveContext{Call: map[string]any{"name": "  serve  "}}
	if got := ctx.CallName(); got != "serve" {
		t.Fatalf("CallName = %q, want serve", got)
	}
	if got := (ResolveContext{}).CallName(); got != "" {
		t.Fatalf("CallName without call = %q, want empty", got)
	}
}

// TestSpanMatchesAnyName proves span matching over candidate names.
func TestSpanMatchesAnyName(t *testing.T) {
	t.Parallel()

	probe := FunctionSpan{names: []string{"serve"}}
	if !SpanMatchesAnyName(probe, []string{"other", "serve"}) {
		t.Fatal("SpanMatchesAnyName = false for a contained name, want true")
	}
	if SpanMatchesAnyName(probe, []string{"other"}) {
		t.Fatal("SpanMatchesAnyName = true without overlap, want false")
	}
	if SpanMatchesAnyName(FunctionSpan{}, []string{"serve"}) {
		t.Fatal("SpanMatchesAnyName = true for a nameless span, want false")
	}
}

// TestCopyOptionalField proves present non-nil values copy while absent
// and nil values leave the destination alone.
func TestCopyOptionalField(t *testing.T) {
	t.Parallel()

	dst := map[string]any{"keep": 1}
	CopyOptionalField(dst, map[string]any{"name": "serve", "nil": nil}, "name")
	CopyOptionalField(dst, map[string]any{"nil": nil}, "nil")
	CopyOptionalField(dst, map[string]any{}, "missing")
	want := map[string]any{"keep": 1, "name": "serve"}
	if !reflect.DeepEqual(dst, want) {
		t.Fatalf("CopyOptionalField dst = %#v, want %#v", dst, want)
	}
}

// TestResolveReceiverMethodCallee proves receiver-typed calls resolve to
// the unique declaration with type-inferred provenance, and stay
// unresolved when the receiver, method, or declaration is absent.
func TestResolveReceiverMethodCallee(t *testing.T) {
	t.Parallel()

	index := EntityIndex{
		receiverMethodsByRepo: map[string]map[string]map[string]string{
			"repo": {"Worker": {"run": "entity-run"}},
		},
		entityFileByID: map[string]string{"entity-run": "/repo/worker.swift"},
	}
	ctx := ResolveContext{
		Index:        index,
		RepositoryID: "repo",
		Call:         map[string]any{"name": "run", "inferred_obj_type": "Worker"},
	}
	entityID, file, method := ResolveReceiverMethodCallee(ctx)
	if entityID != "entity-run" || file != "/repo/worker.swift" || method != codeprovenance.MethodTypeInferred {
		t.Fatalf("ResolveReceiverMethodCallee = (%q, %q, %q), want type-inferred entity-run", entityID, file, method)
	}
	for name, mutate := range map[string]func(*ResolveContext){
		"missing_repo":     func(c *ResolveContext) { c.RepositoryID = "" },
		"missing_receiver": func(c *ResolveContext) { c.Call = map[string]any{"name": "run"} },
		"missing_method":   func(c *ResolveContext) { c.Call = map[string]any{"inferred_obj_type": "Worker"} },
		"unknown_method":   func(c *ResolveContext) { c.Call = map[string]any{"name": "stop", "inferred_obj_type": "Worker"} },
	} {
		probe := ctx
		mutate(&probe)
		if id, _, _ := ResolveReceiverMethodCallee(probe); id != "" {
			t.Fatalf("ResolveReceiverMethodCallee(%s) = %q, want empty", name, id)
		}
	}
}

// TestResolveSymbolCallee proves stable-symbol edges resolve with SCIP
// provenance winning over the stored import-binding method.
func TestResolveSymbolCallee(t *testing.T) {
	t.Parallel()

	index := accessorIndex()
	entityID, file, method := ResolveSymbolCallee(index, map[string]any{"callee_symbol": "scip-test"})
	if entityID != "entity-serve" || file != "/repo/a.js" {
		t.Fatalf("ResolveSymbolCallee = (%q, %q), want entity-serve", entityID, file)
	}
	if method != codeprovenance.MethodSCIP {
		t.Fatalf("ResolveSymbolCallee method = %q, want SCIP for a scip- key", method)
	}
	if id, _, _ := ResolveSymbolCallee(index, map[string]any{"callee_symbol": "unknown"}); id != "" {
		t.Fatalf("ResolveSymbolCallee unknown = %q, want empty", id)
	}
	if id, _, _ := ResolveSymbolCallee(EntityIndex{}, map[string]any{}); id != "" {
		t.Fatalf("ResolveSymbolCallee empty = %q, want empty", id)
	}
}

// TestReferencedSymbolKeys proves SCIP and import-binding edges across
// file facts collapse to the deduplicated key set, skipping other kinds.
func TestReferencedSymbolKeys(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: factload.FactKindFile,
			Payload: map[string]any{
				"parsed_file_data": map[string]any{
					"function_calls_scip": []any{
						map[string]any{"callee_symbol": "scip-a"},
					},
					"function_calls": []any{
						map[string]any{"callee_symbol": "scip-a"},
						map[string]any{"package_export_symbol": "pkg#Serve"},
					},
				},
			},
		},
		{FactKind: "repository", Payload: map[string]any{}},
	}
	got := ReferencedSymbolKeys(envelopes)
	seen := map[string]bool{}
	for _, key := range got {
		seen[key] = true
	}
	if !seen["scip-a"] || !seen["pkg#Serve"] {
		t.Fatalf("ReferencedSymbolKeys = %#v, want scip-a and pkg#Serve", got)
	}
	if len(got) != len(seen) {
		t.Fatalf("ReferencedSymbolKeys = %#v, want deduplicated keys", got)
	}
	if got := ReferencedSymbolKeys(nil); len(got) != 0 {
		t.Fatalf("ReferencedSymbolKeys(nil) = %#v, want empty", got)
	}
}
