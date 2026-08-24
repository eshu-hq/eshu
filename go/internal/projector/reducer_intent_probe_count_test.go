// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestReducerIntentProbeCountMatchesDocumentedCount is a documentation-drift
// guard. reducer_intent_fact_index.go and README.md both cite a specific
// reducer-intent builder count in prose, and that prose has gone stale
// silently more than once: it said "41" while the source already called 43
// distinct probes, and the #5759 change that added a 44th probe bumped the
// prose to "42" instead of 44. Neither drift tripped any gate because nothing
// checked the prose against the real call count.
//
// This test parses scope_generation_intents.go with go/ast -- not a regex, so
// reordering, comments, or reformatting the file cannot fool it -- finds
// appendScopeGenerationReducerIntents's body, and counts its distinct root and
// package-qualified reducer-intent builder calls. That count must equal
// documentedReducerIntentProbeCount (go/internal/projector/reducer_intent_fact_index.go),
// the single constant the doc prose cites. Whoever adds or removes a probe
// must update that constant -- and the "N probes" prose in
// reducer_intent_fact_index.go and README.md that cites it -- in the same
// change, or this test fails immediately instead of the number drifting
// unnoticed for another release.
func TestReducerIntentProbeCountMatchesDocumentedCount(t *testing.T) {
	t.Parallel()

	got := countReducerIntentProbeCalls(t)
	if got != documentedReducerIntentProbeCount {
		t.Fatalf(
			"appendScopeGenerationReducerIntents calls %d distinct reducer-intent builder probes, "+
				"but documentedReducerIntentProbeCount = %d; update that constant AND the \"N probes\" "+
				"prose in reducer_intent_fact_index.go and README.md that cites it",
			got, documentedReducerIntentProbeCount,
		)
	}
}

// countReducerIntentProbeCalls parses scope_generation_intents.go (a sibling
// of this test file) and returns the number of distinct reducer-intent builders
// called from within appendScopeGenerationReducerIntents's body. Root builders
// are identifiers named build*ReducerIntent; extracted family builders are
// package-qualified selectors named Build*ReducerIntent.
func countReducerIntentProbeCalls(t *testing.T) int {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to resolve this test file's own path")
	}
	sourcePath := filepath.Join(filepath.Dir(thisFile), "scope_generation_intents.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, sourcePath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", sourcePath, err)
	}

	var target *ast.FuncDecl
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if ok && funcDecl.Name.Name == "appendScopeGenerationReducerIntents" {
			target = funcDecl
			break
		}
	}
	if target == nil || target.Body == nil {
		t.Fatalf("%s: appendScopeGenerationReducerIntents function body not found", sourcePath)
	}

	seen := map[string]struct{}{}
	ast.Inspect(target.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		key, ok := reducerIntentProbeKey(call.Fun)
		if !ok {
			return true
		}
		seen[key] = struct{}{}
		return true
	})
	return len(seen)
}

func reducerIntentProbeKey(expression ast.Expr) (string, bool) {
	switch callable := expression.(type) {
	case *ast.Ident:
		if strings.HasPrefix(callable.Name, "build") && strings.HasSuffix(callable.Name, "ReducerIntent") {
			return callable.Name, true
		}
	case *ast.SelectorExpr:
		qualifier, ok := callable.X.(*ast.Ident)
		if ok && strings.HasPrefix(callable.Sel.Name, "Build") && strings.HasSuffix(callable.Sel.Name, "ReducerIntent") {
			return qualifier.Name + "." + callable.Sel.Name, true
		}
	}
	return "", false
}

func TestReducerIntentProbeKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression ast.Expr
		wantKey    string
		wantOK     bool
	}{
		{
			name:       "root builder",
			expression: ast.NewIdent("buildGCPResourceMaterializationReducerIntent"),
			wantKey:    "buildGCPResourceMaterializationReducerIntent",
			wantOK:     true,
		},
		{
			name: "extracted builder",
			expression: &ast.SelectorExpr{
				X:   ast.NewIdent("projectorazure"),
				Sel: ast.NewIdent("BuildResourceMaterializationReducerIntent"),
			},
			wantKey: "projectorazure.BuildResourceMaterializationReducerIntent",
			wantOK:  true,
		},
		{
			name:       "unrelated call",
			expression: ast.NewIdent("append"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			gotKey, gotOK := reducerIntentProbeKey(test.expression)
			if gotKey != test.wantKey || gotOK != test.wantOK {
				t.Fatalf("reducerIntentProbeKey() = %q, %v; want %q, %v", gotKey, gotOK, test.wantKey, test.wantOK)
			}
		})
	}
}
