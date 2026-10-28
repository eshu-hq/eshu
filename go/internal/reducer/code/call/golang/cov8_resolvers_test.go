// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
)

// cov8GoFileEnvelope builds one parsed-file envelope for the call index.
func cov8GoFileEnvelope(repo, relative string, fileData map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":          repo,
			"relative_path":    relative,
			"parsed_file_data": fileData,
		},
	}
}

// cov8GoFunction builds one parsed function row.
func cov8GoFunction(name, uid string) map[string]any {
	return map[string]any{
		"name": name, "uid": uid, "line_number": 1, "end_line": 2,
	}
}

// cov8GoLibFile is a package foo declaring an exported Helper.
func cov8GoLibFile() map[string]any {
	return map[string]any{
		"path": "pkg/foo/foo.go",
		"functions": []any{
			cov8GoFunction("Helper", "entity-lib-helper"),
		},
	}
}

// cov8GoModuleFile declares the example.com/mod module root.
func cov8GoModuleFile() map[string]any {
	return map[string]any{
		"path": "go.mod",
		"lang": "gomod",
		"variables": []any{
			map[string]any{"config_kind": "module_declaration", "value": "example.com/mod"},
		},
	}
}

// TestCov8GoResolversListShape pins the declared phase order and the
// fresh-slice promise.
func TestCov8GoResolversListShape(t *testing.T) {
	t.Parallel()

	resolvers := Resolvers()
	if len(resolvers) != 4 {
		t.Fatalf("len(Resolvers()) = %d, want 4", len(resolvers))
	}
	wantPhases := []shared.Phase{
		shared.PhaseBeforeRepoFallback,
		shared.PhaseBeforeRepoFallback,
		shared.PhaseBeforeRepoFallback,
		shared.PhaseAfterRepoFallback,
	}
	for i, want := range wantPhases {
		if resolvers[i].Phase != want {
			t.Errorf("Resolvers()[%d].Phase = %v, want %v", i, resolvers[i].Phase, want)
		}
		if resolvers[i].Resolve == nil {
			t.Errorf("Resolvers()[%d].Resolve is nil", i)
		}
	}
	resolvers[0].Phase = "mutated"
	if again := Resolvers(); again[0].Phase == "mutated" {
		t.Error("Resolvers() shares slice storage across calls")
	}
}

// TestCov8GoPackageQualifiedCallee pins import-bound resolution: a
// package-qualified call binds through the import to the same-repo
// directory, and every guard fails closed.
func TestCov8GoPackageQualifiedCallee(t *testing.T) {
	t.Parallel()

	index := shared.BuildEntityIndex([]facts.Envelope{
		cov8GoFileEnvelope("repo-go", "pkg/foo/foo.go", cov8GoLibFile()),
	})
	caller := map[string]any{
		"imports": []any{
			map[string]any{"source": "example.com/mod/pkg/foo"},
		},
	}
	ctx := shared.ResolveContext{
		Index:        index,
		RepositoryID: "repo-go",
		FileData:     caller,
		Call:         map[string]any{"name": "Helper", "full_name": "foo.Helper"},
		Language:     "go",
	}
	entityID, file, method := Resolvers()[0].Resolve(ctx)
	if entityID != "entity-lib-helper" || file != "pkg/foo/foo.go" ||
		method != codeprovenance.MethodImportBinding {
		t.Errorf("Resolve() = %q, %q, %q; want entity-lib-helper, pkg/foo/foo.go, import binding",
			entityID, file, method)
	}

	// Unqualified, unknown-dir, and repo-less calls resolve nothing.
	for name, mutate := range map[string]func(*shared.ResolveContext){
		"unqualified": func(c *shared.ResolveContext) {
			c.Call = map[string]any{"name": "Helper", "full_name": "Helper"}
		},
		"unknown import": func(c *shared.ResolveContext) {
			c.FileData = map[string]any{"imports": []any{
				map[string]any{"source": "example.com/other/pkg"},
			}}
		},
		"no repository": func(c *shared.ResolveContext) { c.RepositoryID = "" },
		"chain call": func(c *shared.ResolveContext) {
			c.Call = map[string]any{"name": "Helper", "full_name": "foo.Helper()"}
		},
	} {
		guarded := ctx
		mutate(&guarded)
		if entityID, _, _ := Resolvers()[0].Resolve(guarded); entityID != "" {
			t.Errorf("Resolve(%s) = %q, want empty", name, entityID)
		}
	}
}

// TestCov8GoMethodReturnChainCallee pins chain inference:
// ctx.Actions().Get binds through the recorded return type, and calls
// without a provable chain stay unresolved.
func TestCov8GoMethodReturnChainCallee(t *testing.T) {
	t.Parallel()

	fileData := map[string]any{
		"path": "pkg/store/store.go",
		"functions": []any{
			map[string]any{
				"name": "Actions", "uid": "entity-actions",
				"class_context": "Client", "return_type": "Store",
				"line_number": 1, "end_line": 2,
			},
			map[string]any{
				"name": "Get", "uid": "entity-store-get",
				"class_context": "Store", "line_number": 5, "end_line": 6,
			},
		},
	}
	index := shared.BuildEntityIndex([]facts.Envelope{
		cov8GoFileEnvelope("repo-go", "pkg/store/store.go", fileData),
	})
	ctx := shared.ResolveContext{
		Index:        index,
		RepositoryID: "repo-go",
		Call: map[string]any{
			"name": "Get", "full_name": "client.Actions().Get",
			"chain_receiver_obj_type": "Client", "chain_receiver_method": "Actions",
		},
		Language: "go",
	}
	entityID, _, method := Resolvers()[1].Resolve(ctx)
	if entityID != "entity-store-get" || method != codeprovenance.MethodTypeInferred {
		t.Errorf("Resolve() = %q, %q; want entity-store-get, type inferred", entityID, method)
	}

	for name, call := range map[string]map[string]any{
		"no chain":         {"name": "Get", "full_name": "Store.Get"},
		"missing receiver": {"name": "Get", "full_name": "x.Y().Get"},
		"unknown method": {
			"name": "Get", "full_name": "client.Missing().Get",
			"chain_receiver_obj_type": "Client", "chain_receiver_method": "Missing",
		},
	} {
		guarded := ctx
		guarded.Call = call
		if entityID, _, _ := Resolvers()[1].Resolve(guarded); entityID != "" {
			t.Errorf("Resolve(%s) = %q, want empty", name, entityID)
		}
	}
	emptyRepo := ctx
	emptyRepo.RepositoryID = ""
	if entityID, _, _ := Resolvers()[1].Resolve(emptyRepo); entityID != "" {
		t.Errorf("Resolve(no repo) = %q, want empty", entityID)
	}
}

// TestCov8GoSameDirectoryCallee pins directory-scoped resolution: an
// unqualified call binds to the unique same-directory declaration, and a
// qualified call never uses this fallback.
func TestCov8GoSameDirectoryCallee(t *testing.T) {
	t.Parallel()

	index := shared.BuildEntityIndex([]facts.Envelope{
		cov8GoFileEnvelope("repo-go", "pkg/foo/foo.go", cov8GoLibFile()),
	})
	ctx := shared.ResolveContext{
		Index:        index,
		RepositoryID: "repo-go",
		RelativePath: "pkg/foo/main.go",
		Call:         map[string]any{"name": "Helper", "full_name": "Helper"},
		Language:     "go",
	}
	entityID, _, method := Resolvers()[2].Resolve(ctx)
	if entityID != "entity-lib-helper" || method != codeprovenance.MethodScopeUniqueName {
		t.Errorf("Resolve() = %q, %q; want entity-lib-helper, scope unique name", entityID, method)
	}

	qualified := ctx
	qualified.Call = map[string]any{"name": "Helper", "full_name": "foo.Helper"}
	if entityID, _, _ := Resolvers()[2].Resolve(qualified); entityID != "" {
		t.Errorf("Resolve(qualified) = %q, want empty (never a fallback)", entityID)
	}
	elsewhere := ctx
	elsewhere.RelativePath = "pkg/other/main.go"
	if entityID, _, _ := Resolvers()[2].Resolve(elsewhere); entityID != "" {
		t.Errorf("Resolve(other dir) = %q, want empty", entityID)
	}
}

// TestCov8GoCrossRepoExportCallee pins export resolution across the
// repository boundary: exactly one foreign export resolves, while the
// same-repo, unexported, and export-less shapes stay empty.
func TestCov8GoCrossRepoExportCallee(t *testing.T) {
	t.Parallel()

	plain := shared.BuildEntityIndex([]facts.Envelope{
		cov8GoFileEnvelope("repo-app", "main.go", map[string]any{"path": "main.go"}),
	})
	call := map[string]any{"name": "Helper", "full_name": "foo.Helper"}
	imports := map[string]any{"imports": []any{
		map[string]any{"source": "example.com/mod/pkg/foo"},
	}}
	if entityID, _, _ := Resolvers()[3].Resolve(shared.ResolveContext{
		Index: plain, RepositoryID: "repo-app", FileData: imports, Call: call, Language: "go",
	}); entityID != "" {
		t.Errorf("Resolve(no exports) = %q, want empty", entityID)
	}

	withExports := shared.BuildEntityIndex([]facts.Envelope{
		cov8GoFileEnvelope("repo-lib", "go.mod", cov8GoModuleFile()),
		cov8GoFileEnvelope("repo-lib", "pkg/foo/foo.go", cov8GoLibFile()),
		cov8GoFileEnvelope("repo-app", "main.go", map[string]any{"path": "main.go"}),
	})
	foreign := shared.ResolveContext{
		Index: withExports, RepositoryID: "repo-app",
		FileData: imports, Call: call, Language: "go",
	}
	entityID, file, method := Resolvers()[3].Resolve(foreign)
	if entityID != "entity-lib-helper" || file != "pkg/foo/foo.go" ||
		method != codeprovenance.MethodCrossRepoExportPackage {
		t.Errorf("Resolve(foreign) = %q, %q, %q; want entity-lib-helper via cross-repo export",
			entityID, file, method)
	}

	sameRepo := foreign
	sameRepo.RepositoryID = "repo-lib"
	if entityID, _, _ := Resolvers()[3].Resolve(sameRepo); entityID != "" {
		t.Errorf("Resolve(same repo) = %q, want empty", entityID)
	}
	unexported := foreign
	unexported.Call = map[string]any{"name": "helper", "full_name": "foo.helper"}
	if entityID, _, _ := Resolvers()[3].Resolve(unexported); entityID != "" {
		t.Errorf("Resolve(unexported) = %q, want empty", entityID)
	}
}

// TestCov8GoImportHelpers pins the import-row parsing: qualifier splits,
// source fallbacks, alias and basename local names, and directory
// candidate expansion.
func TestCov8GoImportHelpers(t *testing.T) {
	t.Parallel()

	if qualifier, ok := goPackageQualifier("foo.Helper", "Helper"); !ok || qualifier != "foo" {
		t.Errorf("goPackageQualifier() = %q, %v; want foo, true", qualifier, ok)
	}
	for name, args := range map[string][2]string{
		"empty":            {"", "Helper"},
		"chain":            {"foo.Helper()", "Helper"},
		"suffix miss":      {"other.Helper", "HelperX"},
		"dotted qualifier": {"a.b.Helper", "Helper"},
	} {
		if _, ok := goPackageQualifier(args[0], args[1]); ok {
			t.Errorf("goPackageQualifier(%s) = true, want false", name)
		}
	}

	entry := map[string]any{"source": "example.com/mod/pkg/foo", "name": "fallback"}
	if got := goImportSource(entry); got != "example.com/mod/pkg/foo" {
		t.Errorf("goImportSource() = %q, want the source field", got)
	}
	if got := goImportSource(map[string]any{"name": "fallback"}); got != "fallback" {
		t.Errorf("goImportSource() = %q, want the name fallback", got)
	}
	if got := goImportLocalName(map[string]any{"alias": "f", "source": "example.com/mod/pkg/foo"}, "example.com/mod/pkg/foo"); got != "f" {
		t.Errorf("goImportLocalName() = %q, want the alias", got)
	}
	if got := goImportLocalName(map[string]any{}, "example.com/mod/pkg/foo"); got != "foo" {
		t.Errorf("goImportLocalName() = %q, want the base", got)
	}

	candidates := goImportDirectoryCandidates("example.com/mod/pkg/foo")
	want := []string{"example.com/mod/pkg/foo", "mod/pkg/foo", "pkg/foo", "foo"}
	if len(candidates) != len(want) {
		t.Fatalf("candidates = %v, want %v", candidates, want)
	}
	for i := range want {
		if candidates[i] != want[i] {
			t.Fatalf("candidates = %v, want %v", candidates, want)
		}
	}
	if got := goImportDirectoryCandidates(""); len(got) != 0 {
		t.Errorf("candidates(empty) = %v, want nil", got)
	}
}
