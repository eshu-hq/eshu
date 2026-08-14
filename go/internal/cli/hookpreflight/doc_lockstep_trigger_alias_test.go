// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"go/ast"
	"go/token"
	"sort"
	"testing"
)

// The belt on the trigger path (see doc_lockstep_test.go for the conventions
// the whole guard follows).
//
// doc_lockstep_trigger_path_test.go names who may write a `.Trigger` field.
// It reads assignments, so it sees `x.Trigger = ...` and nothing else: a write
// through a pointer taken to the field is an *ast.StarExpr on the left, with no
// `.Trigger` selector there at all. That gap was known and left open on the
// argument that the equivalence property covers any rewrite however it is
// spelled. Three measured evasions say otherwise, all of them pointer writes,
// all green against the property at the time:
//
//	tp := &input.Trigger; if len(*tp) > 4 && triggerAllowed((*tp)[:4]) { ... }
//	tp := &input.Trigger; if input.Permission == "elevated" { *tp = "read" }
//	tp := &input.Trigger; if (*tp)[len(*tp)-1] == '@' { *tp = (*tp)[:len(*tp)-1] }
//
// The first reached past the sweep's length bound, the second keyed on a field
// the sweep held fixed, the third on a character outside its alphabet. The
// sweep and the axis set have since been widened and now catch all three -- and
// that is the argument for this file rather than against it. Each of those
// three was found by someone looking; the widening answers the three that were
// found, and this rule answers the shape they share. It costs one AST walk and
// reports nothing today.
//
// Two rules, both currently vacuous over the production files, which is why the
// fixture drive at the bottom matters more than usual: a rule that has never
// fired is indistinguishable from a rule that cannot.

// triggerAliasFinding is one place a production file takes the address of a
// Trigger field, or assigns through a dereferenced pointer.
type triggerAliasFinding struct {
	File   string
	Func   string
	Detail string
}

func (f triggerAliasFinding) String() string {
	return f.File + ": " + f.Func + " " + f.Detail
}

// scanTriggerAliases reports both rules over dir's non-test files, plus how
// many function bodies it walked so a caller can refuse a vacuous pass.
//
// The second rule is deliberately broader than the trigger: any write through a
// dereferenced pointer is reported, not only one whose pointer is provably a
// Trigger. Proving that needs type information the other scanners here do not
// use, and the broad form costs nothing while no production function writes
// through a pointer at all. If one legitimately needs to, the finding is the
// prompt to say so here.
func scanTriggerAliases(dir string) (functions int, findings []triggerAliasFinding, err error) {
	_, parsed, _, err := parseNonTestGoFiles(dir)
	if err != nil {
		return 0, nil, err
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
			functions++
			funcName := funcDisplayName(funcDecl)
			ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.UnaryExpr:
					if typed.Op != token.AND {
						return true
					}
					selector, isSelector := typed.X.(*ast.SelectorExpr)
					if isSelector && selector.Sel.Name == "Trigger" {
						findings = append(findings, triggerAliasFinding{
							File: name, Func: funcName,
							Detail: "takes the address of " + exprText(typed.X),
						})
					}
				case *ast.AssignStmt:
					for _, lhs := range typed.Lhs {
						if star, isStar := lhs.(*ast.StarExpr); isStar {
							findings = append(findings, triggerAliasFinding{
								File: name, Func: funcName,
								Detail: "assigns through the pointer " + exprText(star.X),
							})
						}
					}
				}
				return true
			})
		}
	}
	return functions, findings, nil
}

// TestDocLockstepNoPointerAliasToATrigger is the positive half: no production
// function takes the address of a Trigger field or writes through a pointer.
func TestDocLockstepNoPointerAliasToATrigger(t *testing.T) {
	t.Parallel()

	functions, findings, err := scanTriggerAliases(".")
	if err != nil {
		t.Fatalf("scan trigger aliases: %v", err)
	}
	if functions == 0 {
		t.Fatal("walked 0 production function bodies; the assertion would be vacuous")
	}
	for _, finding := range findings {
		t.Errorf("%s; a trigger written through a pointer is invisible to the writer scan in doc_lockstep_trigger_path_test.go, which reads `.Trigger` assignments. Write the field directly, and register the function in docTriggerWriters", finding)
	}
}

// TestDocLockstepTriggerAliasScannerReportsPointerWrites is the negative half,
// and the reason the rules above are worth having: each fixture is one of the
// three measured evasions, run over a throwaway package on disk.
func TestDocLockstepTriggerAliasScannerReportsPointerWrites(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		body         string
		wantFindings int
	}{
		{
			name:         "clean_package_reports_nothing",
			body:         "func normalizeInput(input Input) Input {\n\tinput.Trigger = strings.ToLower(input.Trigger)\n\treturn input\n}\n",
			wantFindings: 0,
		},
		{
			name: "truncates_a_longer_class_onto_an_allowed_one",
			body: "func normalizeInput(input Input) Input {\n\ttp := &input.Trigger\n" +
				"\tif len(*tp) > 4 && triggerAllowed((*tp)[:4]) && !triggerAllowed(*tp) {\n\t\t*tp = (*tp)[:4]\n\t}\n\treturn input\n}\n",
			wantFindings: 2,
		},
		{
			name: "gates_the_remap_on_another_field",
			body: "func normalizeInput(input Input) Input {\n\tif input.Permission == \"elevated\" {\n" +
				"\t\ttp := &input.Trigger\n\t\t*tp = \"read\"\n\t}\n\treturn input\n}\n",
			wantFindings: 2,
		},
		{
			name: "strips_a_suffix_outside_the_sweep_alphabet",
			body: "func normalizeInput(input Input) Input {\n\ttp := &input.Trigger\n" +
				"\tif n := len(*tp); n > 1 && (*tp)[n-1] == '@' {\n\t\t*tp = (*tp)[:n-1]\n\t}\n\treturn input\n}\n",
			wantFindings: 2,
		},
		{
			name:         "writes_through_a_pointer_from_elsewhere",
			body:         "func normalizeInput(input Input) Input {\n\tp := triggerSlot(&input)\n\t*p = \"read\"\n\treturn input\n}\n",
			wantFindings: 1,
		},
	}
	if len(cases) < 5 {
		t.Fatalf("alias fixtures = %d, want one per measured evasion plus the clean control and a pointer from elsewhere", len(cases))
	}

	for _, tc := range cases {
		functions, findings, err := scanTriggerAliases(writeTriggerFixture(t, tc.body))
		if err != nil {
			t.Fatalf("%s: scan fixture: %v", tc.name, err)
		}
		if functions == 0 {
			t.Fatalf("%s: walked 0 functions", tc.name)
		}
		if len(findings) != tc.wantFindings {
			t.Errorf("%s: %d findings, want %d: %v", tc.name, len(findings), tc.wantFindings, findings)
		}
	}
}
