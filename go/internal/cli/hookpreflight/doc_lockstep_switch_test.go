// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Structural half of the trigger-class pin (see doc_lockstep_test.go for the
// conventions the whole guard follows).
//
// The first version of this scanner collected the string literals of any case
// clause whose body held a bare `return true`. That reads the accepted set out
// of the code, which is the thing a hand-written probe list cannot do -- but it
// still recognizes exactly one way to write the function. Four rewrites of
// triggerAllowed that accept a new class all passed the entire suite: an early
// `if trigger == "list" { return true }` before the switch, an `if` guard
// inside a case clause, a case clause that returns a local variable, and a case
// clause that returns a comparison. Every one of them made
// triggerAllowed("list") report true.
//
// So the scanner asserts the shape of the function rather than pattern-matching
// one spelling of it. triggerAllowed's body must be exactly one tagged switch
// whose clauses are string-literal cases returning a bare `true`, plus one
// default returning a bare `false`. A statement before or after the switch, a
// conditional or an assignment inside a clause, a returned variable or
// expression, a tagless switch, a missing default, a default that returns true,
// or a case that returns anything but `true` are all findings. That turns each
// evasion above into a structural violation instead of an invisible one.

// switchFinding is one departure from the closed-switch shape, named so a
// failure says which rule the function broke rather than reporting a bare
// mismatch.
type switchFinding struct {
	Func   string
	Detail string
}

func (f switchFinding) String() string { return f.Func + ": " + f.Detail }

// findFuncDecl returns the package-level function named name declared in one of
// dir's non-test files, and errors when there is none -- an empty result would
// otherwise read to a caller as "this function accepts nothing".
func findFuncDecl(dir, name string) (*ast.FuncDecl, error) {
	_, parsed, err := parseNonTestGoFiles(dir)
	if err != nil {
		return nil, err
	}
	fileNames := make([]string, 0, len(parsed))
	for fileName := range parsed {
		fileNames = append(fileNames, fileName)
	}
	sort.Strings(fileNames)
	for _, fileName := range fileNames {
		for _, decl := range parsed[fileName].Decls {
			funcDecl, ok := decl.(*ast.FuncDecl)
			if ok && funcDecl.Recv == nil && funcDecl.Name.Name == name {
				return funcDecl, nil
			}
		}
	}
	return nil, fmt.Errorf("func %s not found in %s", name, dir)
}

// bareReturnIdent reports the identifier a clause body returns, when that body
// is exactly one `return <ident>` and nothing else. Anything longer or anything
// returning an expression fails here, which is what stops a conditional, an
// assignment, or a comparison from standing in for the literal answer.
func bareReturnIdent(body []ast.Stmt) (string, bool) {
	if len(body) != 1 {
		return "", false
	}
	ret, ok := body[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return "", false
	}
	ident, ok := ret.Results[0].(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// scanClosedStringSwitch requires funcName in dir to be a closed string switch
// and returns the literals its true-returning clauses carry, how many such
// clauses it saw, and every departure from that shape. It reports findings
// rather than failing so the negative tests below can drive the same code over
// fixture directories.
func scanClosedStringSwitch(dir, funcName string) (clauses int, values []string, findings []switchFinding, err error) {
	target, err := findFuncDecl(dir, funcName)
	if err != nil {
		return 0, nil, nil, err
	}
	report := func(format string, args ...any) {
		findings = append(findings, switchFinding{Func: funcName, Detail: fmt.Sprintf(format, args...)})
	}

	statements := 0
	if target.Body != nil {
		statements = len(target.Body.List)
	}
	if statements != 1 {
		report("body holds %d statements, want exactly one switch; a statement before or after the switch can accept a class the switch never names", statements)
		return 0, nil, findings, nil
	}
	switchStmt, ok := target.Body.List[0].(*ast.SwitchStmt)
	if !ok {
		report("the body statement is %T, want a switch", target.Body.List[0])
		return 0, nil, findings, nil
	}
	if switchStmt.Init != nil {
		report("the switch carries an init statement, which can settle the answer before any case is compared")
	}
	if switchStmt.Tag == nil {
		report("the switch has no tag, so its cases are arbitrary boolean expressions rather than string literals")
	}
	if switchStmt.Body == nil {
		report("the switch has no body")
		return 0, nil, findings, nil
	}

	defaults := 0
	for _, stmt := range switchStmt.Body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			report("the switch holds a %T, want only case clauses", stmt)
			continue
		}
		if clause.List == nil {
			defaults++
			if name, ok := bareReturnIdent(clause.Body); !ok || name != "false" {
				report("the default clause is not a bare `return false`")
			}
			continue
		}
		literals := make([]string, 0, len(clause.List))
		for _, expr := range clause.List {
			lit, ok := expr.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				report("a case compares a %T rather than a string literal", expr)
				continue
			}
			value, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr != nil {
				return clauses, values, findings, unquoteErr
			}
			literals = append(literals, value)
		}
		if name, ok := bareReturnIdent(clause.Body); !ok || name != "true" {
			report("case %v is not a bare `return true`", literals)
			continue
		}
		clauses++
		values = append(values, literals...)
	}
	if defaults != 1 {
		report("the switch has %d default clauses, want exactly one returning false", defaults)
	}
	sort.Strings(values)
	return clauses, values, findings, nil
}

// writeSwitchFixture writes body as the sole non-test file of a fresh
// directory, next to a _test.go file the scanner must ignore.
func writeSwitchFixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte("package fixture\n\n"+body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ignored := "package fixture\n\nfunc allowed(v string) bool {\n\treturn v == \"from_a_test_file\"\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture_test.go"), []byte(ignored), 0o600); err != nil {
		t.Fatalf("write ignored fixture: %v", err)
	}
	return dir
}

// TestDocLockstepSwitchScannerReadsTheClosedShape proves the scanner reads the
// literals out of a well-formed closed switch, skips _test.go files, and errors
// on a missing function rather than returning an empty set a caller could read
// as an empty allow-list.
func TestDocLockstepSwitchScannerReadsTheClosedShape(t *testing.T) {
	t.Parallel()

	dir := writeSwitchFixture(t, "func allowed(v string) bool {\n\tswitch v {\n"+
		"\tcase \"read\", \"list\":\n\t\treturn true\n"+
		"\tcase \"symbol\":\n\t\treturn true\n"+
		"\tdefault:\n\t\treturn false\n\t}\n}\n")

	clauses, values, findings, err := scanClosedStringSwitch(dir, "allowed")
	if err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %v, want none for a well-formed closed switch", findings)
	}
	if clauses != 2 {
		t.Fatalf("true-returning clauses = %d, want 2", clauses)
	}
	if strings.Join(values, ",") != "list,read,symbol" {
		t.Fatalf("values = %v, want [list read symbol]", values)
	}
	if _, _, _, err := scanClosedStringSwitch(dir, "notThere"); err == nil {
		t.Fatal("scanning a missing function returned no error; a caller could read the empty set as an empty allow-list")
	}
}

// TestDocLockstepSwitchScannerRejectsEvasions is the negative half. The first
// four bodies are the rewrites of triggerAllowed that accept "list" while the
// old bare-`return true` scanner saw nothing; the rest are the neighbouring
// shapes that express the same change differently. Each must produce a finding.
//
// Two of the bodies deliberately do not compile (a function that can fall off
// the end). The scanner only parses, and a fixture that has to compile could
// not isolate a missing default from the extra statement Go would require.
func TestDocLockstepSwitchScannerRejectsEvasions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{
			name: "guard_before_switch",
			body: "func allowed(v string) bool {\n\tif v == \"list\" {\n\t\treturn true\n\t}\n" +
				"\tswitch v {\n\tcase \"read\":\n\t\treturn true\n\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "conditional_inside_case",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tcase \"list\":\n\t\tif v != \"\" {\n\t\t\treturn true\n\t\t}\n\t\treturn false\n" +
				"\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "variable_returned_from_case",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tcase \"list\":\n\t\tok := true\n\t\treturn ok\n\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "expression_returned_from_case",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tcase \"list\":\n\t\treturn v != \"\"\n\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "tagless_switch",
			body: "func allowed(v string) bool {\n\tswitch {\n\tcase v == \"read\" || v == \"list\":\n\t\treturn true\n" +
				"\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "switch_init_statement",
			body: "func allowed(v string) bool {\n\tswitch t := v; t {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "case_returns_false",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tcase \"edit\":\n\t\treturn false\n\tdefault:\n\t\treturn false\n\t}\n}\n",
		},
		{
			name: "default_returns_true",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tdefault:\n\t\treturn true\n\t}\n}\n",
		},
		{
			name: "no_default_clause",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\", \"list\":\n\t\treturn true\n\t}\n}\n",
		},
		{
			name: "statement_after_switch",
			body: "func allowed(v string) bool {\n\tswitch v {\n\tcase \"read\":\n\t\treturn true\n" +
				"\tdefault:\n\t\tbreak\n\t}\n\treturn v == \"list\"\n}\n",
		},
	}
	if len(cases) < 10 {
		t.Fatalf("evasion cases = %d, want one per shape that expresses a widened allow-list", len(cases))
	}

	checked := 0
	for _, tc := range cases {
		dir := writeSwitchFixture(t, tc.body)
		_, values, findings, err := scanClosedStringSwitch(dir, "allowed")
		if err != nil {
			t.Fatalf("%s: scan fixture: %v", tc.name, err)
		}
		if len(findings) == 0 {
			t.Errorf("%s: scanner reported no finding and read the accepted set as %v; this shape widens the allow-list invisibly", tc.name, values)
		}
		checked++
	}
	if checked != len(cases) {
		t.Fatalf("scanned %d of %d evasion fixtures", checked, len(cases))
	}
}
