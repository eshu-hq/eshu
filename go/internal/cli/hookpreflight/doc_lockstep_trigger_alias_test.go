// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"go/ast"
	"go/token"
	"sort"
	"testing"
)

// The belts on the trigger path (see doc_lockstep_test.go for the conventions
// the whole guard follows).
//
// doc_lockstep_trigger_path_test.go names who may write a `.Trigger` field. It
// reads assignments and keyed composite-literal elements, so it sees
// `x.Trigger = ...` and `Input{Trigger: ...}` and nothing else. Two ways of
// putting a value in that field are neither: a write through a pointer taken to
// it (an *ast.StarExpr on the left, with no `.Trigger` selector anywhere), and a
// positional composite literal (no field names at all, so no `Trigger` key to
// match). Both gaps were known and left open on the argument that the
// equivalence property covers any rewrite however it is spelled. Four measured
// evasions say otherwise, each green against the property as it stood:
//
//	tp := &input.Trigger; if len(*tp) > 4 && triggerAllowed((*tp)[:4]) { ... }
//	tp := &input.Trigger; if input.Permission == "elevated" { *tp = "read" }
//	tp := &input.Trigger; if (*tp)[len(*tp)-1] == '@' { *tp = (*tp)[:len(*tp)-1] }
//	normalized = withTrigger(normalized, "read"), where withTrigger returns a
//	positional Input literal, gated on len(trigger) > 6 to clear the one-edit
//	radius
//
// The first reached past the sweep's length bound, the second keyed on a field
// the sweep held fixed, the third on a character outside its alphabet, the
// fourth on a length the neighbourhood does not reach. The sweep and the axis
// set have been widened and now catch the first three -- and that is the
// argument for this file rather than against it. The fourth still passes the
// equivalence, and passes it for a reason README.md states plainly: a remap can
// evade the property by reaching a class the sweep does not generate. Widening
// the sample answers the evasions someone has already found; these rules answer
// the shapes they are written in, which is the part that generalizes.
//
// Three rules, all vacuous over the production files today, which is why the
// fixture drive at the bottom matters more than usual: a rule that has never
// fired is indistinguishable from a rule that cannot.

// triggerAliasFinding is one place a production file takes the address of a
// Trigger field, assigns through a dereferenced pointer, or builds a
// Trigger-carrying struct without naming its fields.
type triggerAliasFinding struct {
	File   string
	Func   string
	Detail string
}

func (f triggerAliasFinding) String() string {
	return f.File + ": " + f.Func + " " + f.Detail
}

// triggerCarryingTypes returns the struct types dir declares that have a
// Trigger field, read out of the source rather than listed, so a new one is
// covered by the positional-literal rule without anybody adding it here.
func triggerCarryingTypes(files map[string]*ast.File) map[string]bool {
	carrying := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, isType := spec.(*ast.TypeSpec)
				if !isType {
					continue
				}
				structType, isStruct := typeSpec.Type.(*ast.StructType)
				if !isStruct || structType.Fields == nil {
					continue
				}
				for _, field := range structType.Fields.List {
					for _, name := range field.Names {
						if name.Name == "Trigger" {
							carrying[typeSpec.Name.Name] = true
						}
					}
				}
			}
		}
	}
	return carrying
}

// compositeLitTypeName reports the bare type name a composite literal
// constructs, seeing through the &T{...} form.
func compositeLitTypeName(lit *ast.CompositeLit) string {
	switch typed := lit.Type.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return exprText(typed.X)
	default:
		return ""
	}
}

// scanTriggerAliases reports all three rules over dir's non-test files, plus how
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
	carrying := triggerCarryingTypes(parsed)
	names := make([]string, 0, len(parsed))
	for name := range parsed {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		for _, decl := range parsed[name].Decls {
			// Package-level declarations get the same rules. Every scanner in
			// this package used to walk function bodies only, which is how a
			// `var readAlias = Input{Trigger: "read"}` at package scope went
			// unlooked-at while a function copied the class out of it.
			if genDecl, isGen := decl.(*ast.GenDecl); isGen {
				findings = append(findings, compositeLitFindings(genDecl, name, packageLevelWriter, carrying)...)
				continue
			}
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
				case *ast.CompositeLit:
					findings = append(findings, compositeLitFindings(typed, name, funcName, carrying)...)
					return false
				}
				return true
			})
		}
	}
	return functions, findings, nil
}

// compositeLitFindings reports every positional composite literal of a
// Trigger-carrying type inside node. It is shared by the function-body walk and
// the package-level one so the two cannot drift.
func compositeLitFindings(node ast.Node, file, owner string, carrying map[string]bool) []triggerAliasFinding {
	var findings []triggerAliasFinding
	ast.Inspect(node, func(inner ast.Node) bool {
		lit, isLit := inner.(*ast.CompositeLit)
		if !isLit {
			return true
		}
		typeName := compositeLitTypeName(lit)
		if !carrying[typeName] || len(lit.Elts) == 0 {
			return true
		}
		for _, element := range lit.Elts {
			if _, keyed := element.(*ast.KeyValueExpr); !keyed {
				findings = append(findings, triggerAliasFinding{
					File: file, Func: owner,
					Detail: "builds a " + typeName + " without naming its fields",
				})
				break
			}
		}
		return true
	})
	return findings
}

// TestDocLockstepNoPointerAliasToATrigger is the positive half: no production
// function takes the address of a Trigger field, writes through a pointer, or
// builds a Trigger-carrying struct positionally.
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
		t.Errorf("%s; a trigger set through a pointer or a positional literal is invisible to the writer scan in doc_lockstep_trigger_path_test.go, which reads `.Trigger` assignments and `Trigger:` literal keys. Name the field, and register the function in docTriggerWriters", finding)
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
		{
			name: "rebuilds_the_input_positionally",
			body: "type Input struct {\n\tHost string\n\tTrigger string\n}\n\n" +
				"func withTrigger(input Input, trigger string) Input {\n\treturn Input{input.Host, trigger}\n}\n",
			wantFindings: 1,
		},
		{
			name: "a_keyed_literal_is_left_to_the_writer_scan",
			body: "type Input struct {\n\tHost string\n\tTrigger string\n}\n\n" +
				"func withTrigger(input Input, trigger string) Input {\n\treturn Input{Host: input.Host, Trigger: trigger}\n}\n",
			wantFindings: 0,
		},
		{
			name: "a_package_level_positional_alias",
			body: "type Input struct {\n\tHost string\n\tTrigger string\n}\n\n" +
				"var readAlias = Input{\"claude\", \"read\"}\n\n" +
				"func normalizeInput(input Input) Input { return input }\n",
			wantFindings: 1,
		},
		{
			name: "a_positional_literal_of_another_type_is_ignored",
			body: "type Scope struct {\n\tKind string\n\tID string\n}\n\n" +
				"func narrow() Scope {\n\treturn Scope{\"repo_path\", \"services/api\"}\n}\n",
			wantFindings: 0,
		},
	}
	if len(cases) < 9 {
		t.Fatalf("alias fixtures = %d, want one per measured evasion plus the controls that must stay quiet", len(cases))
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
