// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// TestRegisteredSchemasUseOnlyValidatedKeywords is the validator drift guard:
// a registered tool that adopts a JSON-schema keyword, type, or format the
// router validator does not implement fails here instead of being
// half-validated by call_tool. router.TestUnsupportedSchemaFeaturesFlagsSeededSchema
// is its seeded RED proof.
func TestRegisteredSchemasUseOnlyValidatedKeywords(t *testing.T) {
	t.Parallel()

	for _, tool := range append(ReadOnlyTools(), router.Tools()...) {
		if findings := router.UnsupportedSchemaFeatures(tool.InputSchema); len(findings) != 0 {
			t.Errorf("tool %s uses schema features the router validator does not implement: %v", tool.Name, findings)
		}
	}
}

// TestRegisteredToolExamplesValidate proves the example call find_tool and a
// call_tool validation failure hand back is itself valid for every tool.
func TestRegisteredToolExamplesValidate(t *testing.T) {
	t.Parallel()

	for _, tool := range ReadOnlyTools() {
		example := router.ExampleArguments(tool.InputSchema)
		if errs := router.ValidateArguments(tool.InputSchema, example); len(errs) != 0 {
			t.Errorf("tool %s example %v fails its own schema: %v", tool.Name, example, errs)
		}
	}
}

// TestValidatorAcceptsEveryDispatchedArgumentMap is the differential guard.
// The validator accepts every argument map the dispatch tests send as a
// literal and every map in the catalog sweep corpus, except maps in a recorded
// divergence class (validatorDivergenceClasses). Each class names its handler
// evidence and must match at least one map here or in its pinning test; the
// test fails when a map fits no class, when an exempted map starts
// validating, or when a class stops matching. Maps the dispatch tests build at
// run time are reported as skipped; a one-off runtime capture
// (go/internal/mcp/router/README.md, "Known divergence from a direct call")
// showed every rejected one falls in a recorded class.
func TestValidatorAcceptsEveryDispatchedArgumentMap(t *testing.T) {
	t.Parallel()

	schemas := map[string]any{}
	for _, tool := range ReadOnlyTools() {
		schemas[tool.Name] = tool.InputSchema
	}
	type sample struct {
		tool, origin string
		args         map[string]any
	}
	var samples []sample
	sweep := 0
	for name, cases := range sweepArguments(t) {
		for i, args := range cases {
			samples = append(samples, sample{name, fmt.Sprintf("catalog sweep #%d", i), args})
			sweep++
		}
	}
	extracted, skipped := dispatchTestArgumentMaps(t)
	for _, s := range extracted {
		samples = append(samples, sample(s))
	}
	// The floor keeps the extraction from silently going vacuous if the
	// dispatch tests change call shape.
	if len(extracted) < 300 {
		t.Fatalf("extracted %d literal argument maps from dispatch tests (skipped %d), want >= 300", len(extracted), skipped)
	}

	perClass := map[string]int{}
	checked := 0
	for _, s := range samples {
		schema, registered := schemas[s.tool]
		if !registered {
			continue
		}
		if _, err := resolveRoute(s.tool, s.args); err != nil {
			continue // a negative dispatch case; dispatch rejects it too
		}
		checked++
		errs := router.ValidateArguments(schema, s.args)
		if len(errs) == 0 {
			continue
		}
		class := classifyRejection(s.tool, schema, s.args, errs)
		if class == nil {
			t.Errorf("%s (%s): validator rejects %v that dispatch accepts, in no recorded class: %v", s.tool, s.origin, s.args, errs)
			continue
		}
		perClass[class.id]++
	}
	// Classes A and B are pinned by their own tests; C must keep matching
	// here so a stale class is noticed.
	if perClass["C"] == 0 {
		t.Errorf("divergence class C matched no literal or sweep map; remove or narrow it")
	}
	t.Logf("checked %d dispatch-accepted maps: %d catalog sweep, %d literal from tests; %d test calls skipped (built at run time, including every search_* query-alias map); rejected per class %v",
		checked, sweep, len(extracted), skipped, perClass)
}

type extractedArgs struct {
	tool, origin string
	args         map[string]any
}

// dispatchTestArgumentMaps parses this package's _test.go files and returns
// every resolveRoute, dispatchTool, and dispatchToolWithOptions call whose
// tool name and argument map are literals. It reports how many calls it
// skipped because a value was computed at run time.
func dispatchTestArgumentMaps(t *testing.T) ([]extractedArgs, int) {
	t.Helper()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("glob test files: %v", err)
	}
	fset := token.NewFileSet()
	var out []extractedArgs
	skipped := 0
	for _, file := range files {
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			nameIdx := map[string]int{"resolveRoute": 0, "dispatchTool": 2, "dispatchToolWithOptions": 2}
			idx, ok := nameIdx[fn.Name]
			if !ok || len(call.Args) <= idx+1 {
				return true
			}
			lit, ok := call.Args[idx].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				skipped++
				return true
			}
			tool, _ := strconv.Unquote(lit.Value)
			value, ok := literalValue(call.Args[idx+1])
			args, isMap := value.(map[string]any)
			if !ok || !isMap {
				skipped++
				return true
			}
			out = append(out, extractedArgs{tool: tool, origin: fset.Position(call.Pos()).String(), args: args})
			return true
		})
	}
	return out, skipped
}

// literalValue evaluates a Go literal expression into the value the test
// would pass at run time. It reports false for anything computed.
func literalValue(expr ast.Expr) (any, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		switch e.Kind {
		case token.STRING:
			s, err := strconv.Unquote(e.Value)
			return s, err == nil
		case token.INT:
			i, err := strconv.Atoi(e.Value)
			return i, err == nil
		case token.FLOAT:
			f, err := strconv.ParseFloat(e.Value, 64)
			return f, err == nil
		}
	case *ast.Ident:
		switch e.Name {
		case "true":
			return true, true
		case "false":
			return false, true
		case "nil":
			return nil, true
		}
	case *ast.UnaryExpr:
		if e.Op == token.SUB {
			switch v, ok := literalValue(e.X); n := v.(type) {
			case int:
				return -n, ok
			case float64:
				return -n, ok
			}
		}
	case *ast.CallExpr:
		if fn, ok := e.Fun.(*ast.Ident); ok && len(e.Args) == 1 {
			v, ok := literalValue(e.Args[0])
			n, isInt := v.(int)
			switch fn.Name {
			case "float64":
				if isInt {
					return float64(n), ok
				}
				return v, ok
			case "int", "int64":
				if fn.Name == "int64" && isInt {
					return int64(n), ok
				}
				return v, ok && isInt
			}
		}
	case *ast.CompositeLit:
		return compositeValue(e)
	}
	return nil, false
}

func compositeValue(e *ast.CompositeLit) (any, bool) {
	switch typ := e.Type.(type) {
	case *ast.MapType:
		if key, ok := typ.Key.(*ast.Ident); !ok || key.Name != "string" {
			return nil, false
		}
		out := map[string]any{}
		for _, elt := range e.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				return nil, false
			}
			key, ok := literalValue(kv.Key)
			keyString, isString := key.(string)
			if !ok || !isString {
				return nil, false
			}
			value, ok := elementValue(kv.Value, typ.Value)
			if !ok {
				return nil, false
			}
			out[keyString] = value
		}
		return out, true
	case *ast.ArrayType:
		out := make([]any, 0, len(e.Elts))
		for _, elt := range e.Elts {
			value, ok := elementValue(elt, typ.Elt)
			if !ok {
				return nil, false
			}
			out = append(out, value)
		}
		return out, true
	}
	return nil, false
}

// elementValue evaluates a composite element, filling an elided inner
// composite type ([]map[string]any{{...}}) from its container.
func elementValue(expr ast.Expr, elemType ast.Expr) (any, bool) {
	if inner, ok := expr.(*ast.CompositeLit); ok && inner.Type == nil {
		inner = &ast.CompositeLit{Type: elemType, Elts: inner.Elts}
		return compositeValue(inner)
	}
	return literalValue(expr)
}
