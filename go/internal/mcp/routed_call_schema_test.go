// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
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

// TestValidatorAcceptsEveryDispatchedArgumentMap is the differential guard:
// every argument map the existing dispatch tests and the catalog sweep corpus
// send, and that dispatch accepts, must pass the router validator. A false
// rejection would make call_tool refuse a call a direct tools/call serves.
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
	for name, cases := range sweepArguments(t) {
		for i, args := range cases {
			samples = append(samples, sample{name, fmt.Sprintf("catalog sweep #%d", i), args})
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

	seenExemptions := map[string]bool{}
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
		key := exemptionKey(t, s.tool, s.args)
		if _, exempt := validatorDifferentialExemptions[key]; exempt {
			seenExemptions[key] = true
			if errs := router.ValidateArguments(schema, s.args); len(errs) == 0 {
				t.Errorf("%s (%s): exempted map %v now validates; remove its exemption", s.tool, s.origin, s.args)
			}
			continue
		}
		if errs := router.ValidateArguments(schema, s.args); len(errs) != 0 {
			t.Errorf("%s (%s): validator rejects %v that dispatch accepts: %v", s.tool, s.origin, s.args, errs)
		}
	}
	for key := range validatorDifferentialExemptions {
		if !seenExemptions[key] {
			t.Errorf("exemption %s matched no dispatch test map; remove it", key)
		}
	}
	t.Logf("validated %d dispatch-accepted argument maps (%d extracted from tests, %d test maps skipped as non-literal)", checked, len(extracted), skipped)
}

// validatorDifferentialExemptions lists dispatch-test maps that resolveRoute
// builds a route for but that break the advertised schema, so the validator
// rejects them on purpose. Each was checked against its handler; the test
// fails when an entry stops matching or starts validating.
var validatorDifferentialExemptions = map[string]string{
	// Handler rejects: route-shape tests whose handler answers HTTP 400.
	`list_container_image_tag_history {"repository_id":"oci-registry://ghcr.io/eshu-hq/demo"}`: "test asserts dispatch errors without the tag selector",
	`find_cross_repo_dead_code {}`:              "codequery/deadcode/cross_repo.go: repo_id is required",
	`resolve_entity {}`:                         "query/entity/handler.go: name is required",
	`get_entity_content {}`:                     "query/contentread/content_handler.go: entity_id is required",
	`compare_environments {"repo_id":"repo-1"}`: "query/compare/handler.go: workload_id, left, and right are required",
	// Undocumented alias: resolve_entity's legacy name/type keys are not in
	// its input schema, which requires query. A schema-following caller sends
	// query; call_tool answers name-only calls with a field error that says so.
	`resolve_entity {"limit":5,"name":"exactName","repo_id":"repo-1","type":"function"}`: "legacy name alias outside the advertised schema",
}

func exemptionKey(t *testing.T, tool string, args map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal %s args: %v", tool, err)
	}
	return tool + " " + string(raw)
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
