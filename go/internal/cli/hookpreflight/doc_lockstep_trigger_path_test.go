// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Plumbing half of the trigger-class pin. doc_lockstep_switch_test.go holds
// triggerAllowed to a readable shape; that is a claim about one function, and
// "which trigger classes get an advisory" is a claim about the whole path from
// Input.Trigger to the switch case that consults it.
//
// Two rewrites made triggerAllowed byte-identical and still widened the
// accepted set, and both passed every earlier version of this guard:
//
//	normalizeInput mapping "list" onto "read" before Evaluate ever asks
//	Evaluate calling a widened twin, leaving triggerAllowed as a decoy
//
// The first also rewrote Output.Trigger on the wire, so the emitted advisory
// claimed a class the caller never sent. So this file pins the two ends the
// switch scanner cannot see: who is allowed to write a Trigger field and with
// what, and that Evaluate's switch still consults triggerAllowed itself.

// triggerWriteShape names the RHS forms a Trigger field may be written from.
type triggerWriteShape string

const (
	// shapeNormalize is `<x>.Trigger` under any nesting of the pure
	// normalizers -- case folding and whitespace trimming, which cannot turn
	// one class into another.
	shapeNormalize triggerWriteShape = "normalized <x>.Trigger"
	// shapeClaudeTool is `triggerFromClaudeTool(<x>.ToolName)`, the one
	// deliberate translation in the package. What that translation may
	// produce is pinned behaviorally by
	// TestDocLockstepClaudeToolTriggerClasses.
	shapeClaudeTool triggerWriteShape = "triggerFromClaudeTool(<x>.ToolName)"
)

// docTriggerWriters is every function permitted to put a value into a Trigger
// field, and the RHS shape it is held to. Anything else writing one is a
// finding, and a name here that writes none is a finding too -- a pin that
// stopped matching anything would otherwise pass while guarding nothing.
func docTriggerWriters() map[string]triggerWriteShape {
	return map[string]triggerWriteShape{
		"normalizeInput":             shapeNormalize,
		"baseOutput":                 shapeNormalize,
		"MergeClaudePreToolUseInput": shapeClaudeTool,
	}
}

// triggerWrite is one place a Trigger field receives a value.
type triggerWrite struct {
	File      string
	Func      string
	RHS       string
	Want      triggerWriteShape
	Permitted bool
	OK        bool
}

func (w triggerWrite) String() string {
	if !w.Permitted {
		return fmt.Sprintf("%s: %s writes Trigger from %s, and docTriggerWriters does not list it", w.File, w.Func, w.RHS)
	}
	return fmt.Sprintf("%s: %s writes Trigger from %s, want %s", w.File, w.Func, w.RHS, w.Want)
}

// selectorAfterPureNormalizers reports the `<receiver>.<field>` selector expr
// resolves to once any pure normalizer wrappers are peeled off.
func selectorAfterPureNormalizers(expr ast.Expr) (receiver, field string, ok bool) {
	root, pure := unwrapPureNormalizers(expr)
	if !pure {
		return "", "", false
	}
	selector, isSelector := root.(*ast.SelectorExpr)
	if !isSelector {
		return "", "", false
	}
	ident, isIdent := selector.X.(*ast.Ident)
	if !isIdent {
		return "", "", false
	}
	return ident.Name, selector.Sel.Name, true
}

// matchesTriggerWriteShape reports whether rhs is the form want describes.
func matchesTriggerWriteShape(rhs ast.Expr, want triggerWriteShape) bool {
	switch want {
	case shapeNormalize:
		_, field, ok := selectorAfterPureNormalizers(rhs)
		return ok && field == "Trigger"
	case shapeClaudeTool:
		call, ok := rhs.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return false
		}
		fn, ok := call.Fun.(*ast.Ident)
		if !ok || fn.Name != "triggerFromClaudeTool" {
			return false
		}
		selector, ok := call.Args[0].(*ast.SelectorExpr)
		return ok && selector.Sel.Name == "ToolName"
	default:
		return false
	}
}

// scanTriggerWrites reports every assignment to a `.Trigger` field and every
// `Trigger:` element of a composite literal in dir's non-test files, judged
// against allowed.
func scanTriggerWrites(dir string, allowed map[string]triggerWriteShape) (writes []triggerWrite, err error) {
	_, parsed, _, err := parseNonTestGoFiles(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(parsed))
	for name := range parsed {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		for _, decl := range parsed[name].Decls {
			funcDecl, ok := decl.(*ast.FuncDecl)
			if !ok || funcDecl.Body == nil {
				continue
			}
			record := func(rhs ast.Expr) {
				want, permitted := allowed[funcDecl.Name.Name]
				writes = append(writes, triggerWrite{
					File:      name,
					Func:      funcDecl.Name.Name,
					RHS:       exprText(rhs),
					Want:      want,
					Permitted: permitted,
					OK:        permitted && matchesTriggerWriteShape(rhs, want),
				})
			}
			ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.AssignStmt:
					for i, lhs := range typed.Lhs {
						selector, ok := lhs.(*ast.SelectorExpr)
						if !ok || selector.Sel.Name != "Trigger" || i >= len(typed.Rhs) {
							continue
						}
						record(typed.Rhs[i])
					}
				case *ast.KeyValueExpr:
					key, ok := typed.Key.(*ast.Ident)
					if ok && key.Name == "Trigger" {
						record(typed.Value)
					}
				}
				return true
			})
		}
	}
	return writes, nil
}

// exprText renders expr compactly enough to name it in a failure message.
func exprText(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.BasicLit:
		return typed.Value
	case *ast.SelectorExpr:
		return exprText(typed.X) + "." + typed.Sel.Name
	case *ast.CallExpr:
		args := make([]string, 0, len(typed.Args))
		for _, arg := range typed.Args {
			args = append(args, exprText(arg))
		}
		return exprText(typed.Fun) + "(" + strings.Join(args, ", ") + ")"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

// TestDocLockstepTriggerReachesTheGateUnrewritten pins who may write a Trigger
// field and from what. normalizeInput lowercasing and trimming is the whole of
// what the doc describes; a line there mapping one class onto another decides
// eligibility before triggerAllowed is consulted and changes Output.Trigger on
// the wire, with triggerAllowed untouched for any reader to check.
func TestDocLockstepTriggerReachesTheGateUnrewritten(t *testing.T) {
	t.Parallel()

	allowed := docTriggerWriters()
	writes, err := scanTriggerWrites(".", allowed)
	if err != nil {
		t.Fatalf("scan trigger writes: %v", err)
	}
	if len(writes) == 0 {
		t.Fatal("found no Trigger writes; the assertion would be vacuous")
	}

	seen := map[string]bool{}
	for _, write := range writes {
		seen[write.Func] = true
		if !write.OK {
			t.Errorf("%s; rewriting the trigger outside triggerAllowed decides eligibility where no reader of that function can see it", write)
		}
	}
	for name := range allowed {
		if !seen[name] {
			t.Errorf("docTriggerWriters names %s but it writes no Trigger field; drop it from the pin or restore the write", name)
		}
	}
}

// evaluateGateCall is the case expression Evaluate must carry to consult
// triggerAllowed on the value it normalized.
const evaluateGateCall = "!triggerAllowed(<x>.Trigger)"

// scanEvaluateTriggerGate reports how many of funcName's switch case clauses
// are `!triggerAllowed(<x>.Trigger)`, how many clauses that switch has, and how
// many of them do something other than return a skip.
func scanEvaluateTriggerGate(dir, funcName string) (gates, clauses, nonSkip int, err error) {
	target, err := findFuncDecl(dir, funcName)
	if err != nil {
		return 0, 0, 0, err
	}
	ast.Inspect(target.Body, func(node ast.Node) bool {
		clause, ok := node.(*ast.CaseClause)
		if !ok {
			return true
		}
		clauses++
		if !returnsSkipCall(clause.Body) {
			nonSkip++
		}
		for _, expr := range clause.List {
			unary, ok := expr.(*ast.UnaryExpr)
			if !ok || unary.Op.String() != "!" {
				continue
			}
			call, ok := unary.X.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				continue
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok || fn.Name != "triggerAllowed" {
				continue
			}
			if selector, ok := call.Args[0].(*ast.SelectorExpr); ok && selector.Sel.Name == "Trigger" {
				gates++
			}
		}
		return true
	})
	return gates, clauses, nonSkip, nil
}

// returnsSkipCall reports whether body is exactly one `return skip(...)`.
func returnsSkipCall(body []ast.Stmt) bool {
	if len(body) != 1 {
		return false
	}
	ret, ok := body[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	call, ok := ret.Results[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := call.Fun.(*ast.Ident)
	return ok && fn.Name == "skip"
}

// TestDocLockstepEvaluateConsultsTriggerAllowed pins the other end of the same
// path: the eligibility switch must ask triggerAllowed itself, on a bare
// Trigger field. Pointing that case at a twin carrying a wider list leaves
// triggerAllowed pristine for the source scan to read while production advises
// on a class the docs do not name.
//
// The clause counts come with it. Each of Evaluate's five ineligible cases
// returns a skip, so a sixth clause -- or an existing one that stops returning
// a skip -- is a change to the fail-open contract AGENTS.md describes, not an
// implementation detail.
func TestDocLockstepEvaluateConsultsTriggerAllowed(t *testing.T) {
	t.Parallel()

	gates, clauses, nonSkip, err := scanEvaluateTriggerGate(".", "Evaluate")
	if err != nil {
		t.Fatalf("scan Evaluate: %v", err)
	}
	if gates != 1 {
		t.Errorf("Evaluate's switch holds %d %s cases, want exactly 1; the eligibility gate must consult triggerAllowed, not a twin of it", gates, evaluateGateCall)
	}
	if clauses != 5 {
		t.Errorf("Evaluate's switch has %d case clauses, want 5 (timeout, host, enabled, trigger, permission); a new clause changes which reason a caller sees", clauses)
	}
	if nonSkip != 0 {
		t.Errorf("%d of Evaluate's switch clauses do not return skip(...); every ineligible case fails open, and a clause that advises there would bypass the trigger gate entirely", nonSkip)
	}
}

// writeTriggerFixture writes body as the sole non-test file of a fresh
// directory.
func writeTriggerFixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte("package fixture\n\n"+body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dir
}

// TestDocLockstepTriggerPathScannersReportRewrites is the negative half. Each
// fixture is one of the two rewrites that widened the accepted set with
// triggerAllowed untouched, plus the neighbouring shapes.
func TestDocLockstepTriggerPathScannersReportRewrites(t *testing.T) {
	t.Parallel()

	const claudeMerge = "func MergeClaudePreToolUseInput(input *Input, payload ClaudePreToolUseInput) {\n" +
		"\tinput.Trigger = triggerFromClaudeTool(payload.ToolName)\n}\n"
	const baseOut = "func baseOutput(input Input) Output {\n\treturn Output{Trigger: input.Trigger}\n}\n"

	writeCases := []struct {
		name    string
		body    string
		wantBad int
	}{
		{
			name: "clean_package_reports_nothing",
			body: "func normalizeInput(input Input) Input {\n" +
				"\tinput.Trigger = strings.ToLower(strings.TrimSpace(input.Trigger))\n\treturn input\n}\n\n" +
				baseOut + "\n" + claudeMerge,
			wantBad: 0,
		},
		{
			name: "normalize_remaps_a_class",
			body: "func normalizeInput(input Input) Input {\n" +
				"\tinput.Trigger = strings.ToLower(strings.TrimSpace(input.Trigger))\n" +
				"\tinput.Trigger = canonicalTrigger(input.Trigger)\n\treturn input\n}\n\n" +
				baseOut + "\n" + claudeMerge,
			wantBad: 1,
		},
		{
			name: "normalize_writes_a_literal",
			body: "func normalizeInput(input Input) Input {\n\tinput.Trigger = \"read\"\n\treturn input\n}\n\n" +
				baseOut + "\n" + claudeMerge,
			wantBad: 1,
		},
		{
			name: "an_unlisted_function_writes_the_trigger",
			body: "func normalizeInput(input Input) Input {\n" +
				"\tinput.Trigger = strings.TrimSpace(input.Trigger)\n\treturn input\n}\n\n" +
				baseOut + "\n" + claudeMerge + "\n" +
				"func widen(input *Input) {\n\tinput.Trigger = \"read\"\n}\n",
			wantBad: 1,
		},
		{
			name: "base_output_rewrites_the_wire_trigger",
			body: "func normalizeInput(input Input) Input {\n" +
				"\tinput.Trigger = strings.TrimSpace(input.Trigger)\n\treturn input\n}\n\n" +
				"func baseOutput(input Input) Output {\n\treturn Output{Trigger: canonicalTrigger(input.Trigger)}\n}\n\n" +
				claudeMerge,
			wantBad: 1,
		},
		{
			name: "merge_bypasses_the_tool_mapping",
			body: "func normalizeInput(input Input) Input {\n" +
				"\tinput.Trigger = strings.TrimSpace(input.Trigger)\n\treturn input\n}\n\n" + baseOut + "\n" +
				"func MergeClaudePreToolUseInput(input *Input, payload ClaudePreToolUseInput) {\n" +
				"\tinput.Trigger = strings.ToLower(payload.ToolName)\n}\n",
			wantBad: 1,
		},
	}
	if len(writeCases) < 6 {
		t.Fatalf("trigger-write cases = %d, want one per shape that rewrites a class outside triggerAllowed", len(writeCases))
	}

	for _, tc := range writeCases {
		writes, err := scanTriggerWrites(writeTriggerFixture(t, tc.body), docTriggerWriters())
		if err != nil {
			t.Fatalf("%s: scan fixture: %v", tc.name, err)
		}
		bad := 0
		for _, write := range writes {
			if !write.OK {
				bad++
			}
		}
		if bad != tc.wantBad {
			t.Errorf("%s: %d bad writes out of %d, want %d", tc.name, bad, len(writes), tc.wantBad)
		}
	}

	gateCases := []struct {
		name      string
		body      string
		wantGates int
		wantSkips int
	}{
		{
			name: "gate_consults_trigger_allowed",
			body: "func Evaluate(input Input) Output {\n\tnormalized := normalizeInput(input)\n\tout := baseOutput(normalized)\n" +
				"\tswitch {\n\tcase !triggerAllowed(normalized.Trigger):\n\t\treturn skip(out, \"a\", \"b\")\n\t}\n\treturn out\n}\n",
			wantGates: 1,
		},
		{
			name: "gate_points_at_a_twin",
			body: "func Evaluate(input Input) Output {\n\tnormalized := normalizeInput(input)\n\tout := baseOutput(normalized)\n" +
				"\tswitch {\n\tcase !triggerEligible(normalized.Trigger):\n\t\treturn skip(out, \"a\", \"b\")\n\t}\n\treturn out\n}\n",
			wantGates: 0,
		},
		{
			name: "gate_argument_is_rewritten_in_place",
			body: "func Evaluate(input Input) Output {\n\tnormalized := normalizeInput(input)\n\tout := baseOutput(normalized)\n" +
				"\tswitch {\n\tcase !triggerAllowed(canonicalTrigger(normalized.Trigger)):\n\t\treturn skip(out, \"a\", \"b\")\n\t}\n\treturn out\n}\n",
			wantGates: 0,
		},
		{
			name: "a_clause_advises_instead_of_skipping",
			body: "func Evaluate(input Input) Output {\n\tnormalized := normalizeInput(input)\n\tout := baseOutput(normalized)\n" +
				"\tswitch {\n\tcase normalized.Trigger == \"list\":\n\t\treturn advise(out)\n" +
				"\tcase !triggerAllowed(normalized.Trigger):\n\t\treturn skip(out, \"a\", \"b\")\n\t}\n\treturn out\n}\n",
			wantGates: 1,
			wantSkips: 1,
		},
	}
	if len(gateCases) < 4 {
		t.Fatalf("gate cases = %d, want one per way the eligibility case stops asking triggerAllowed", len(gateCases))
	}

	for _, tc := range gateCases {
		gates, _, nonSkip, err := scanEvaluateTriggerGate(writeTriggerFixture(t, tc.body), "Evaluate")
		if err != nil {
			t.Fatalf("%s: scan fixture: %v", tc.name, err)
		}
		if gates != tc.wantGates {
			t.Errorf("%s: gates = %d, want %d", tc.name, gates, tc.wantGates)
		}
		if nonSkip != tc.wantSkips {
			t.Errorf("%s: non-skip clauses = %d, want %d", tc.name, nonSkip, tc.wantSkips)
		}
	}
}
