// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"reflect"
	"testing"
)

// TestJavaScriptContainingFunctionSource proves the narrowest containing
// function's source wins and out-of-range lines yield "".
func TestJavaScriptContainingFunctionSource(t *testing.T) {
	t.Parallel()

	fileData := map[string]any{
		"functions": []any{
			map[string]any{"line_number": 1, "end_line": 20, "source": "outer"},
			map[string]any{"line_number": 5, "end_line": 8, "source": "inner"},
			map[string]any{"line_number": 30, "source": "open-ended"},
		},
	}
	if got := JavaScriptContainingFunctionSource(fileData, 6); got != "inner" {
		t.Fatalf("line 6 source = %q, want inner", got)
	}
	if got := JavaScriptContainingFunctionSource(fileData, 15); got != "outer" {
		t.Fatalf("line 15 source = %q, want outer", got)
	}
	if got := JavaScriptContainingFunctionSource(fileData, 30); got != "open-ended" {
		t.Fatalf("open-ended source = %q, want open-ended", got)
	}
	if got := JavaScriptContainingFunctionSource(fileData, 25); got != "" {
		t.Fatalf("gap line source = %q, want empty", got)
	}
	if got := JavaScriptContainingFunctionSource(fileData, 0); got != "" {
		t.Fatalf("line 0 source = %q, want empty", got)
	}
	if got := JavaScriptContainingFunctionSource(map[string]any{}, 3); got != "" {
		t.Fatalf("missing functions source = %q, want empty", got)
	}
}

// TestJavaScriptStaticAliases proves string vars, object entries, and
// destructured names resolve through the alias chain.
func TestJavaScriptStaticAliases(t *testing.T) {
	t.Parallel()

	source := `
const baseUrl = "https://api.example.test";
const handlers = {serve: server.serve, "stop": server.stop};
const {serve} = handlers;
`
	aliases, staticStrings := JavaScriptStaticAliases(source)
	if staticStrings["baseUrl"] != "https://api.example.test" {
		t.Fatalf("static strings = %#v, want baseUrl", staticStrings)
	}
	if aliases["handlers.serve"] != "server.serve" {
		t.Fatalf("object aliases = %#v, want handlers.serve", aliases)
	}
	if aliases["handlers.stop"] != "server.stop" {
		t.Fatalf("quoted-key aliases = %#v, want handlers.stop", aliases)
	}
	if aliases["serve"] != "server.serve" {
		t.Fatalf("destructured aliases = %#v, want serve -> server.serve", aliases)
	}
	emptyAliases, emptyStrings := JavaScriptStaticAliases("serve();")
	if len(emptyAliases) != 0 || len(emptyStrings) != 0 {
		t.Fatalf("plain source aliases = (%#v, %#v), want empty", emptyAliases, emptyStrings)
	}
}

// TestCodeCallJavaScriptSourceFile proves language fields win with the
// extension as fallback.
func TestCodeCallJavaScriptSourceFile(t *testing.T) {
	t.Parallel()

	if !codeCallJavaScriptSourceFile(map[string]any{"language": "typescript"}, "/repo/a.py", "a.py") {
		t.Fatal("typescript language should be a JS source file")
	}
	if !codeCallJavaScriptSourceFile(map[string]any{"lang": "tsx"}, "/repo/a.py", "a.py") {
		t.Fatal("tsx lang should be a JS source file")
	}
	if !codeCallJavaScriptSourceFile(map[string]any{}, "/repo/src/a.mjs", "src/a.mjs") {
		t.Fatal(".mjs extension should be a JS source file")
	}
	if codeCallJavaScriptSourceFile(map[string]any{"lang": "go"}, "/repo/a.go", "a.go") {
		t.Fatal("go file must not be a JS source file")
	}
	if codeCallJavaScriptSourceFile(map[string]any{}, "/repo/a.py", "a.py") {
		t.Fatal("python file must not be a JS source file")
	}
}

// TestCacheJavaScriptStaticAliasSpan proves guards skip indexing while a
// real span lands under both file keys.
func TestCacheJavaScriptStaticAliasSpan(t *testing.T) {
	t.Parallel()

	zero := EntityIndex{}
	cacheJavaScriptStaticAliasSpan(zero, "repo", "", "", 0, 0, "")
	cacheJavaScriptStaticAliasSpan(zero, "repo", "/repo/a.js", "a.js", 0, 5, `const u = "x";`)
	cacheJavaScriptStaticAliasSpan(zero, "repo", "/repo/a.js", "a.js", 1, 5, "   ")
	if len(zero.javaScriptAliasesByFile) != 0 {
		t.Fatalf("guarded cache wrote %#v, want nothing", zero.javaScriptAliasesByFile)
	}
	index := EntityIndex{javaScriptAliasesByFile: map[string]map[string][]javaScriptStaticAliasSpan{}}
	cacheJavaScriptStaticAliasSpan(index, "repo", "/repo/a.js", "a.js", 1, 5, `const u = "x";`)
	for _, key := range []string{"/repo/a.js", "a.js"} {
		spans := index.JavaScriptAliasesByFile("repo", key)
		if len(spans) != 1 || !spans[0].Aliases.Scanned {
			t.Fatalf("spans[%q] = %#v, want one scanned span", key, spans)
		}
	}
}

// TestTypeScriptInterfaceMethods proves interface declarations join their
// unique implementer methods while ambiguous or undeclared ones drop out.
func TestTypeScriptInterfaceMethods(t *testing.T) {
	t.Parallel()

	candidates := newTypeScriptIndexCandidates()
	method := map[string]any{"lang": "typescript", "class_context": "Server", "name": "serve"}
	candidates.addFunction("repo", method, "entity-serve")
	candidates.addFunction("repo", map[string]any{"lang": "go", "class_context": "Server", "name": "skip"}, "entity-skip")
	candidates.addFunction("repo", map[string]any{"lang": "typescript", "name": "noname"}, "entity-noname")
	candidates.addType("classes", "repo", map[string]any{
		"lang": "typescript", "name": "Server", "implemented_interfaces": []string{"IServer"},
	})
	candidates.addType("classes", "repo", map[string]any{"lang": "typescript", "name": "Bare"})
	candidates.addType("interfaces", "repo", map[string]any{"lang": "typescript", "name": "IServer"})
	candidates.addType("interfaces", "repo", map[string]any{"lang": "go", "name": "ISkip"})
	candidates.addType("structs", "repo", map[string]any{"lang": "typescript", "name": "IStruct"})
	unique := candidates.uniqueMethods()
	if got := unique["repo"]["IServer"]["serve"]; got != "entity-serve" {
		t.Fatalf("interface method = %q, want entity-serve", got)
	}
	if !codeCallTypeScriptEntity(map[string]any{"lang": "tsx"}) {
		t.Fatal("tsx should be a TypeScript entity")
	}
	if codeCallTypeScriptEntity(map[string]any{"lang": "javascript"}) {
		t.Fatal("javascript must not be a TypeScript entity")
	}
}

// TestPythonClassBases proves single-declaration bases survive while
// conflicting or repeated declarations drop out.
func TestPythonClassBases(t *testing.T) {
	t.Parallel()

	candidates := map[string]map[string]map[string]pythonClassBaseCandidate{}
	addPythonClassBaseCandidate(candidates, "repo", map[string]any{"name": "Handler", "bases": []string{"Base"}})
	addPythonClassBaseCandidate(candidates, "repo", map[string]any{"name": "Ignored"})
	addPythonClassBaseCandidate(candidates, "", map[string]any{"name": "Handler", "bases": []string{"Base"}})
	addPythonClassBaseCandidate(candidates, "repo", map[string]any{"name": "Multi", "bases": []string{"A"}})
	addPythonClassBaseCandidate(candidates, "repo", map[string]any{"name": "Multi", "bases": []string{"B"}})
	unique := uniquePythonClassBasesByRepo(candidates)
	if got := unique["repo"]["Handler"]; !reflect.DeepEqual(got, []string{"Base"}) {
		t.Fatalf("Handler bases = %#v, want [Base]", got)
	}
	if _, ok := unique["repo"]["Multi"]; ok {
		t.Fatalf("conflicted Multi bases = %#v, want absent", unique["repo"]["Multi"])
	}
}

// TestRustTraitMethods proves unique trait methods resolve while
// ambiguous or context-carrying rows drop out.
func TestRustTraitMethods(t *testing.T) {
	t.Parallel()

	candidates := map[string]map[string]map[string]struct{}{}
	addRustTraitMethodCandidate(candidates, "repo",
		map[string]any{"trait_context": "Serve", "name": "run"}, "entity-run")
	addRustTraitMethodCandidate(candidates, "repo",
		map[string]any{"trait_context": "Serve", "name": "run"}, "entity-run-dup")
	addRustTraitMethodCandidate(candidates, "repo",
		map[string]any{"trait_context": "Solo", "name": "run"}, "entity-solo")
	addRustTraitMethodCandidate(candidates, "repo",
		map[string]any{"trait_context": "Skip", "name": "run", "impl_context": "x"}, "entity-skip")
	addRustTraitMethodCandidate(candidates, "",
		map[string]any{"trait_context": "Skip", "name": "run"}, "entity-noname")
	unique := uniqueRustTraitMethodCandidates(candidates)
	if got := unique["repo"]["Solo::run"]; got != "entity-solo" {
		t.Fatalf("Solo::run = %q, want entity-solo", got)
	}
	if _, ok := unique["repo"]["Serve::run"]; ok {
		t.Fatalf("ambiguous Serve::run = %q, want absent", unique["repo"]["Serve::run"])
	}
	if _, ok := unique["repo"]["Skip::run"]; ok {
		t.Fatal("impl-context row entered the candidates, want absent")
	}
}

// TestReceiverMethodCandidates proves unique receiver methods collapse
// while ambiguous ones and foreign languages drop out.
func TestReceiverMethodCandidates(t *testing.T) {
	t.Parallel()

	candidates := newReceiverMethodCandidates()
	candidates.add("repo", map[string]any{"lang": "swift", "class_context": "Worker", "name": "run"}, "entity-run")
	candidates.add("repo", map[string]any{"lang": "swift", "class_context": "Worker", "name": "stop"}, "entity-stop-a")
	candidates.add("repo", map[string]any{"lang": "swift", "class_context": "Worker", "name": "stop"}, "entity-stop-b")
	candidates.add("repo", map[string]any{"lang": "go", "class_context": "Worker", "name": "skip"}, "entity-skip")
	candidates.add("", map[string]any{"lang": "swift", "class_context": "Worker", "name": "skip"}, "entity-noname")
	unique := candidates.unique()
	if got := unique["repo"]["Worker"]["run"]; got != "entity-run" {
		t.Fatalf("Worker.run = %q, want entity-run", got)
	}
	if _, ok := unique["repo"]["Worker"]["stop"]; ok {
		t.Fatalf("ambiguous stop = %q, want absent", unique["repo"]["Worker"]["stop"])
	}
	for _, lang := range []string{"swift", "javascript", "jsx"} {
		if !receiverMethodLanguage(lang) {
			t.Fatalf("receiverMethodLanguage(%q) = false, want true", lang)
		}
	}
	if receiverMethodLanguage("go") {
		t.Fatal("receiverMethodLanguage(go) = true, want false")
	}
}
