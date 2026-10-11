// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// constructorScan is what the AST scan of the extractor sources found. scoped
// holds the evidence kinds passed to appendConfigArtifactRow, the only
// constructor that strips the repository name from the path. unscoped holds the
// kinds the Ansible extractor passes to its addRow closure and the kinds of
// the row map literals the kustomize extractor builds directly. problems lists
// every construction the scan cannot attribute to one literal kind: a
// non-literal kind argument, a constructor used as a value, or a row literal
// whose evidence_kind is not a string literal. A problem is a guard failure: a
// kind the scan cannot read could carry either name scope.
type constructorScan struct {
	scoped, unscoped map[string]bool
	problems         []string
}

const (
	scopedConstructor   = "appendConfigArtifactRow"
	unscopedConstructor = "addRow"
	evidenceKindKey     = "evidence_kind"
)

// scanRowConstructors reads every call of the two row constructors and every
// evidence_kind row-literal key from parsed sources. It uses the syntax tree,
// not text lines, so a call wrapped over several lines, a call with the kind
// on its own line and a nested call are all seen. The bodies of the two
// constructors themselves (appendConfigArtifactRow, and the closure assigned to
// addRow) are exempt from the literal rule, because they carry the kind as a
// parameter by design.
func scanRowConstructors(fset *token.FileSet, files []*ast.File) constructorScan {
	scan := constructorScan{scoped: map[string]bool{}, unscoped: map[string]bool{}}
	for _, file := range files {
		type span struct{ from, to token.Pos }
		var internal []span
		declared := map[*ast.Ident]bool{}
		called := map[*ast.Ident]bool{}

		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncDecl:
				if node.Name.Name == scopedConstructor {
					declared[node.Name] = true
					internal = append(internal, span{node.Pos(), node.End()})
				}
			case *ast.AssignStmt:
				if len(node.Lhs) == 1 && len(node.Rhs) == 1 {
					if ident, ok := node.Lhs[0].(*ast.Ident); ok && ident.Name == unscopedConstructor {
						declared[ident] = true
						if lit, ok := node.Rhs[0].(*ast.FuncLit); ok {
							internal = append(internal, span{lit.Pos(), lit.End()})
						}
					}
				}
			case *ast.CallExpr:
				if ident, ok := node.Fun.(*ast.Ident); ok {
					called[ident] = true
				}
			}
			return true
		})
		inInternal := func(pos token.Pos) bool {
			for _, s := range internal {
				if pos >= s.from && pos < s.to {
					return true
				}
			}
			return false
		}
		where := func(pos token.Pos) string { return fset.Position(pos).String() }
		literal := func(expr ast.Expr) (string, bool) {
			lit, ok := expr.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return "", false
			}
			value, err := strconv.Unquote(lit.Value)
			return value, err == nil
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				ident, ok := node.Fun.(*ast.Ident)
				if !ok || (ident.Name != scopedConstructor && ident.Name != unscopedConstructor) {
					return true
				}
				want := 2
				kinds := scan.unscoped
				if ident.Name == scopedConstructor {
					want = 6
					kinds = scan.scoped
				}
				if len(node.Args) != want {
					scan.problems = append(scan.problems, fmt.Sprintf("%s: %s call has %d arguments, want %d", where(node.Pos()), ident.Name, len(node.Args), want))
					return true
				}
				kind, ok := literal(node.Args[want-1])
				if !ok {
					scan.problems = append(scan.problems, fmt.Sprintf("%s: %s kind argument is not a string literal", where(node.Pos()), ident.Name))
					return true
				}
				kinds[kind] = true
			case *ast.KeyValueExpr:
				key, ok := literal(node.Key)
				if !ok || key != evidenceKindKey || inInternal(node.Pos()) {
					return true
				}
				kind, ok := literal(node.Value)
				if !ok {
					scan.problems = append(scan.problems, fmt.Sprintf("%s: row literal evidence_kind is not a string literal", where(node.Pos())))
					return true
				}
				scan.unscoped[kind] = true
			case *ast.Ident:
				if (node.Name == scopedConstructor || node.Name == unscopedConstructor) && !declared[node] && !called[node] {
					scan.problems = append(scan.problems, fmt.Sprintf("%s: %s is used as a value; a call through it hides its kind", where(node.Pos()), node.Name))
				}
			}
			return true
		})
	}
	sort.Strings(scan.problems)
	return scan
}

// scanSource parses one Go source string and scans it.
func scanSource(t *testing.T, source string) constructorScan {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "probe.go", source, 0)
	if err != nil {
		t.Fatalf("parse probe source: %v", err)
	}
	return scanRowConstructors(fset, []*ast.File{file})
}

// rowConstructorKinds scans the non-test sources of this package. A problem the
// scan cannot attribute to one literal kind fails the test that asked.
func rowConstructorKinds(t *testing.T) (scoped, unscoped map[string]bool) {
	t.Helper()
	fset := token.NewFileSet()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	scan := scanRowConstructors(fset, files)
	if len(scan.problems) != 0 {
		t.Fatalf("the row-constructor scan cannot read every evidence kind:\n  %s", strings.Join(scan.problems, "\n  "))
	}
	return scan.scoped, scan.unscoped
}

// nameScopeViolations compares a name-scope table against the kinds the row
// constructors emit and returns one message per disagreement: a kind in both
// families, a kind in neither, a table value that contradicts the family of
// the constructor, and a table entry no constructor emits.
func nameScopeViolations(table map[string]bool, scoped, unscoped map[string]bool) []string {
	var out []string
	kinds := map[string]struct{}{}
	for kind := range scoped {
		kinds[kind] = struct{}{}
	}
	for kind := range unscoped {
		kinds[kind] = struct{}{}
	}
	for kind := range table {
		kinds[kind] = struct{}{}
	}
	for kind := range kinds {
		value, inTable := table[kind]
		switch {
		case scoped[kind] && unscoped[kind]:
			out = append(out, kind+": built both through appendConfigArtifactRow and directly")
		case !scoped[kind] && !unscoped[kind]:
			out = append(out, kind+": in the table but emitted by no row constructor")
		case !inTable:
			out = append(out, kind+": emitted by a row constructor but missing from the table")
		case scoped[kind] && !value:
			out = append(out, kind+": built through appendConfigArtifactRow but the table says unscoped")
		case unscoped[kind] && value:
			out = append(out, kind+": built directly but the table says name-scoped")
		}
	}
	sort.Strings(out)
	return out
}

// TestNameScopeTableMatchesTheRowConstructors is the second S0 guard of the
// #7126 decision record: each evidence_kind maps to exactly one name_scoped
// value, and the table agrees with how the extractors build the row. A future
// extractor that emits a new kind, or one kind through both constructors, fails
// here instead of storing a row whose read-time name step is ambiguous.
func TestNameScopeTableMatchesTheRowConstructors(t *testing.T) {
	scoped, unscoped := rowConstructorKinds(t)
	if len(scoped) != 10 || len(unscoped) != 6 {
		t.Fatalf("constructor scan found %d scoped kinds %v and %d unscoped kinds %v, want 10 HCL and compose, 5 Ansible and 1 kustomize",
			len(scoped), scoped, len(unscoped), unscoped)
	}
	if violations := nameScopeViolations(nameScopedByEvidenceKind, scoped, unscoped); len(violations) != 0 {
		t.Fatalf("name-scope table disagrees with the row constructors:\n  %s", strings.Join(violations, "\n  "))
	}
	for _, kind := range EvidenceKinds() {
		if _, known := NameScoped(kind); !known {
			t.Errorf("NameScoped(%q) is unknown for a listed kind", kind)
		}
	}
	if _, known := NameScoped("no_such_kind"); known {
		t.Error("NameScoped reports a kind no extractor emits as known")
	}
}

// TestNameScopeGuardCatchesAWrongTable is the seeded RED leg of the guard. Each
// planted fault must produce a violation on the same scan that is clean on the
// real table.
func TestNameScopeGuardCatchesAWrongTable(t *testing.T) {
	scoped, unscoped := rowConstructorKinds(t)

	clone := func() map[string]bool {
		out := make(map[string]bool, len(nameScopedByEvidenceKind))
		for kind, value := range nameScopedByEvidenceKind {
			out[kind] = value
		}
		return out
	}
	if got := nameScopeViolations(clone(), scoped, unscoped); len(got) != 0 {
		t.Fatalf("the clean table has violations: %v", got)
	}

	cases := map[string]func(table map[string]bool, scoped, unscoped map[string]bool){
		"a compose kind flipped to unscoped": func(table map[string]bool, _, _ map[string]bool) {
			table["docker_compose_env_file"] = false
		},
		"an ansible kind flipped to scoped": func(table map[string]bool, _, _ map[string]bool) {
			table["ansible_vars"] = true
		},
		"an emitted kind missing from the table": func(table map[string]bool, _, _ map[string]bool) {
			delete(table, "terraform_var_file")
		},
		"a table kind no constructor emits": func(table map[string]bool, _, _ map[string]bool) {
			table["terraform_made_up_kind"] = true
		},
		"one kind built through both constructors": func(_ map[string]bool, scoped, unscoped map[string]bool) {
			unscoped["terraform_var_file"] = true
		},
	}
	for name, mutate := range cases {
		table := clone()
		s, u := map[string]bool{}, map[string]bool{}
		for kind := range scoped {
			s[kind] = true
		}
		for kind := range unscoped {
			u[kind] = true
		}
		mutate(table, s, u)
		if got := nameScopeViolations(table, s, u); len(got) == 0 {
			t.Errorf("fault %q was not caught", name)
		}
	}
}

// TestEveryEmittedEvidenceKindIsInTheNameScopeTable runs the corpus the
// name-free proof uses and requires every emitted kind to have a table entry,
// so a kind reachable only through content (not through the source scan) is
// still covered.
func TestEveryEmittedEvidenceKindIsInTheNameScopeTable(t *testing.T) {
	agg := nameFreeRunCorpus(t, nameFreeApply{scoped: nameFreeScopedKinds(t)}, 60)
	for kind, rows := range agg.kindRows {
		if _, known := NameScoped(kind); !known {
			t.Errorf("emitted evidence_kind %q (%d rows) has no name-scope table entry", kind, rows)
		}
	}
	if len(agg.kindRows) < 14 {
		t.Errorf("corpus emitted %d distinct evidence kinds; want at least 14 of 16", len(agg.kindRows))
	}
}

// TestRowConstructorScanReadsWrappedAndNestedCalls is a green leg: a call
// split over lines, with a kind on its own line, and a call inside a branch are
// both attributed to their literal kind. The line-oriented scan this replaced
// saw neither.
func TestRowConstructorScanReadsWrappedAndNestedCalls(t *testing.T) {
	scan := scanSource(t, `package p

func extract(rows []map[string]any, seen map[string]struct{}) []map[string]any {
	if rare() {
		rows = appendConfigArtifactRow(
			rows,
			seen,
			"x",
			"repo",
			"rel",
			"brand_new_scoped_kind",
		)
	}
	addRow("rel", "brand_new_unscoped_kind")
	return append(rows, map[string]any{
		"evidence_kind": "brand_new_row_literal_kind",
	})
}
`)
	if len(scan.problems) != 0 {
		t.Fatalf("clean source reported problems: %v", scan.problems)
	}
	if !scan.scoped["brand_new_scoped_kind"] || !scan.unscoped["brand_new_unscoped_kind"] || !scan.unscoped["brand_new_row_literal_kind"] {
		t.Fatalf("scan missed a kind: scoped=%v unscoped=%v", scan.scoped, scan.unscoped)
	}
}

// TestNameScopeGuardFailsOnAKindMissingFromTheTable is the seeded RED leg the
// F1 review finding asked for: a new kind added through a wrapped call is read
// by the scan and the guard fails because the table does not name it.
func TestNameScopeGuardFailsOnAKindMissingFromTheTable(t *testing.T) {
	scan := scanSource(t, `package p

func extract(rows []map[string]any, seen map[string]struct{}) []map[string]any {
	return appendConfigArtifactRow(
		rows, seen, "x", "repo", "rel",
		"brand_new_unscoped_kind",
	)
}
`)
	violations := nameScopeViolations(nameScopedByEvidenceKind, scan.scoped, scan.unscoped)
	if len(violations) == 0 {
		t.Fatal("a new kind absent from the table was not reported")
	}
}

// TestRowConstructorScanFailsOnWhatItCannotRead is the second seeded RED leg:
// a variable kind, a wrapper that forwards its parameter, a constructor stored
// in a variable, and a row literal with a computed kind each make the scan
// report a problem instead of passing silently.
func TestRowConstructorScanFailsOnWhatItCannotRead(t *testing.T) {
	cases := map[string]string{
		"a variable kind": `package p
func extract(rows []map[string]any, seen map[string]struct{}) []map[string]any {
	kind := pick()
	return appendConfigArtifactRow(rows, seen, "x", "repo", "rel", kind)
}`,
		"a wrapper that forwards its kind parameter": `package p
func wrap(rows []map[string]any, seen map[string]struct{}, kind string) []map[string]any {
	return appendConfigArtifactRow(rows, seen, "x", "repo", "rel", kind)
}`,
		"a constructor used as a value": `package p
func extract() {
	build := appendConfigArtifactRow
	_ = build
}`,
		"a computed kind in a row literal": `package p
func extract() map[string]any {
	return map[string]any{"evidence_kind": "ansible_" + suffix()}
}`,
		"a call with the wrong arity": `package p
func extract(rows []map[string]any) {
	appendConfigArtifactRow(rows, "k")
}`,
	}
	for name, source := range cases {
		scan := scanSource(t, source)
		if len(scan.problems) == 0 {
			t.Errorf("%s: the scan reported no problem (scoped=%v unscoped=%v)", name, scan.scoped, scan.unscoped)
		}
	}
}

// TestRowConstructorScanExemptsTheConstructorBodies proves the exemption is
// narrow: the declaration of appendConfigArtifactRow and the addRow closure may
// carry the kind as a parameter, and a row literal inside them is not a
// construction site.
func TestRowConstructorScanExemptsTheConstructorBodies(t *testing.T) {
	scan := scanSource(t, `package p

func appendConfigArtifactRow(rows []map[string]any, seen map[string]struct{}, pathValue, repoName, relativePath, evidenceKind string) []map[string]any {
	return append(rows, map[string]any{"evidence_kind": evidenceKind})
}

func extract() {
	addRow := func(relativePath, evidenceKind string) {
		_ = map[string]any{"evidence_kind": evidenceKind}
	}
	addRow("rel", "ansible_vars")
}
`)
	if len(scan.problems) != 0 {
		t.Fatalf("constructor bodies were treated as construction sites: %v", scan.problems)
	}
	if !scan.unscoped["ansible_vars"] || len(scan.scoped) != 0 {
		t.Fatalf("scan = scoped %v unscoped %v, want only ansible_vars unscoped", scan.scoped, scan.unscoped)
	}
}
